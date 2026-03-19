package pipeline

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

const (
	// Observed Milvus API cap for one entity's compact JSON payload.
	maxEntityPayloadBytes      = 10_240
	oversizedSplitOverlapRunes = 1
)

var errCannotSplitOversizedChunk = errors.New("cannot split oversized chunk smaller without truncation")

// Inserter is the minimal interface the pipeline needs to insert entities into a collection.
type Inserter interface {
	Insert(ctx context.Context, collection string, entities []milvus.Entity) (*milvus.InsertResult, error)
}

// Config holds parameters for Run.
type Config struct {
	Concurrency         int
	InsertConcurrency   int // concurrent HTTP insert calls; defaults to Concurrency when 0
	InsertBatchSize     int
	Collection          string
	CodebasePath        string
	CollectFileChunkIDs bool
	// OnResultsDrained is an optional callback invoked inside the inserter goroutine
	// after all file-split results have been received but before the final batch flush.
	// Use it to update progress status. May be nil.
	OnResultsDrained func()
	// OnProgress is an optional callback fired after each worker completes a file
	// and after each successful batch insert. May be nil.
	OnProgress func(filesDone, filesTotal, chunksTotal, chunksInserted int)
	// OnFileIndexed is an optional callback fired when all chunks for a file have
	// been successfully inserted. It is called from a doInsert goroutine (parallel),
	// so the callback must be goroutine-safe. May be nil.
	OnFileIndexed func(relPath string, chunkCount int)
}

// Result holds the outcome of a Run.
type Result struct {
	ChunkCounts    map[string]int // relPath → chunk count for each processed file
	CompletedFiles map[string]int // relPath → chunk count for fully-inserted files only
	FileChunkIDs   map[string][]string
	TotalChunks    int
}

// fileTracker records, per file, how many chunks were expected and how many were
// successfully inserted. It is safe for concurrent use from multiple doInsert goroutines.
type fileTracker struct {
	mu       sync.Mutex
	expected map[string]int
	inserted map[string]int
	callback func(relPath string, chunkCount int)
}

func newFileTracker(callback func(relPath string, chunkCount int)) *fileTracker {
	return &fileTracker{
		expected: make(map[string]int),
		inserted: make(map[string]int),
		callback: callback,
	}
}

// setExpected records the total expected chunk count for a file.
// Must be called from the single inserter goroutine before any doInsert may fire
// for that file's entities.
func (ft *fileTracker) setExpected(relPath string, count int) {
	var callback func(string, int)

	ft.mu.Lock()

	ft.expected[relPath] = count
	if ft.callback != nil && count > 0 && ft.inserted[relPath] == count {
		callback = ft.callback
	}
	ft.mu.Unlock()

	if callback != nil {
		callback(relPath, count)
	}
}

// recordInserted counts inserted entities per file from a successful batch and fires
// OnFileIndexed when a file's full expected count is reached.
func (ft *fileTracker) recordInserted(entities []milvus.Entity) {
	perFile := make(map[string]int, len(entities))
	for i := range entities {
		perFile[entities[i].RelativePath]++
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	for relPath, count := range perFile {
		ft.inserted[relPath] += count
		if ft.callback != nil && ft.expected[relPath] > 0 && ft.inserted[relPath] == ft.expected[relPath] {
			ft.callback(relPath, ft.expected[relPath])
		}
	}
}

// completedFiles returns a snapshot of files whose all chunks were successfully inserted.
// Must be called after all doInsert goroutines have finished.
func (ft *fileTracker) completedFiles() map[string]int {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	result := make(map[string]int, len(ft.expected))

	for relPath, exp := range ft.expected {
		if exp > 0 && ft.inserted[relPath] == exp {
			result[relPath] = exp
		}
	}

	return result
}

// Run executes the concurrent split+insert pipeline.
// N worker goroutines read files, split content, and send results to a single
// inserter goroutine that batches and inserts via ins.
// Returns per-file chunk counts and total chunks inserted, or the first insert error.
func Run(ctx context.Context, cfg *Config, files []walker.CodeFile, sp splitter.Splitter, ins Inserter) (Result, error) {
	type fileResult struct {
		relPath       string
		entities      []milvus.Entity
		expectedCount int
		fileDone      bool
	}

	concurrency := cfg.Concurrency

	insertConcurrency := cfg.InsertConcurrency
	if insertConcurrency <= 0 {
		insertConcurrency = concurrency
	}

	filesTotal := len(files)
	workerBatchSize := max(cfg.InsertBatchSize, 1)

	filesCh := make(chan walker.CodeFile, concurrency*2)
	resultsCh := make(chan fileResult, concurrency*2)

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var (
		filesDone      atomic.Int32
		chunksTotal    atomic.Int32
		chunksInserted atomic.Int32
		firstErr       error
	)

	stopCh := make(chan struct{})

	var stopOnce sync.Once

	stop := func(err error) {
		stopOnce.Do(func() {
			firstErr = err

			close(stopCh)
			cancel(err)
		})
	}

	isStopped := func() bool {
		select {
		case <-stopCh:
			return true
		default:
			return false
		}
	}

	sendResult := func(result fileResult) error {
		select {
		case <-stopCh:
			return context.Canceled
		case resultsCh <- result:
			return nil
		}
	}

	fireProgress := func() {
		if cfg.OnProgress != nil {
			cfg.OnProgress(
				int(filesDone.Load()),
				filesTotal,
				int(chunksTotal.Load()),
				int(chunksInserted.Load()),
			)
		}
	}

	// Start N worker goroutines: read file → split → emit entities.
	var workerWg sync.WaitGroup

	for range concurrency {
		workerWg.Go(func() {
			for {
				var file walker.CodeFile

				select {
				case <-stopCh:
					return
				case nextFile, ok := <-filesCh:
					if !ok {
						return
					}

					file = nextFile
				}

				fh, err := os.Open(file.AbsPath)
				if err != nil {
					wrappedErr := fmt.Errorf("open %s: %w", file.RelPath, err)
					log.Printf("pipeline: %v", wrappedErr)
					stop(wrappedErr)

					return
				}

				totalEntities, buildErr := emitFileEntityBatches(
					file.RelPath,
					file.Extension,
					cfg.CodebasePath,
					workerBatchSize,
					func(emitChunk func(splitter.Chunk) error) error {
						return sp.Split(fh, file.RelPath, emitChunk)
					},
					func(batch []milvus.Entity) error {
						if err := sendResult(fileResult{relPath: file.RelPath, entities: batch}); err != nil {
							return err
						}

						chunksTotal.Add(int32(len(batch))) //nolint:gosec // batch size fits int32

						return nil
					},
				)

				closeErr := fh.Close()
				if buildErr == nil && closeErr != nil {
					// Defensive: os.Open on a regular file should not later fail Close without an injected or OS-specific fault.
					buildErr = closeErr
				}

				if buildErr != nil {
					if isStopped() {
						return
					}

					wrappedErr := fmt.Errorf("process %s: %w", file.RelPath, buildErr)
					stop(wrappedErr)

					return
				}

				if totalEntities == 0 {
					if isStopped() {
						return
					}

					continue
				}

				if err := sendResult(fileResult{relPath: file.RelPath, expectedCount: totalEntities, fileDone: true}); err != nil {
					return
				}

				filesDone.Add(1)
				fireProgress()
			}
		})
	}

	// Start inserter goroutine: batch entities, fire parallel inserts bounded by semaphore.
	var (
		insertWg    sync.WaitGroup
		totalChunks int64
	)

	insertSem := make(chan struct{}, insertConcurrency)

	// chunkCounts is written only by the single inserter goroutine, safe to read after insertWg.Wait().
	chunkCounts := make(map[string]int)

	var fileChunkIDs map[string][]string
	if cfg.CollectFileChunkIDs {
		fileChunkIDs = make(map[string][]string)
	}

	tracker := newFileTracker(cfg.OnFileIndexed)

	// doInsert runs a single batch insert as a semaphore-bounded goroutine.
	doInsert := func(b []milvus.Entity) {
		defer func() { <-insertSem; insertWg.Done() }()

		if _, err := ins.Insert(runCtx, cfg.Collection, b); err != nil {
			log.Printf("pipeline: insert error: %v", err)

			wrappedErr := fmt.Errorf("insert: %w", err)

			stop(wrappedErr)

			return
		}

		tracker.recordInserted(b)
		chunksInserted.Add(int32(len(b))) //nolint:gosec // batch size fits int32
		fireProgress()
	}

	scheduleInsert := func(entities []milvus.Entity) bool {
		select {
		case <-stopCh:
			return false
		case insertSem <- struct{}{}:
		}

		if isStopped() {
			// Defensive race: stop can win after we acquire the semaphore but before the insert goroutine starts.
			<-insertSem

			return false
		}

		insertWg.Add(1)

		go doInsert(entities)

		return true
	}

	insertWg.Go(func() {
		var batch []milvus.Entity

		batchSize := max(cfg.InsertBatchSize, 1)

		for {
			var result fileResult

			select {
			case <-stopCh:
				return
			case nextResult, ok := <-resultsCh:
				if !ok {
					goto flush
				}

				result = nextResult
			}

			if len(result.entities) > 0 {
				batch = append(batch, result.entities...)
				atomic.AddInt64(&totalChunks, int64(len(result.entities)))

				chunkCounts[result.relPath] += len(result.entities)
				if fileChunkIDs != nil {
					for i := range result.entities {
						fileChunkIDs[result.relPath] = append(fileChunkIDs[result.relPath], result.entities[i].ID)
					}
				}

				for len(batch) >= batchSize {
					toInsert := make([]milvus.Entity, batchSize)
					copy(toInsert, batch[:batchSize])
					batch = batch[batchSize:]

					if !scheduleInsert(toInsert) {
						return
					}
				}
			}

			if result.fileDone {
				tracker.setExpected(result.relPath, result.expectedCount)
			}
		}

	flush:
		// All splits received — invoke optional progress hook.
		if cfg.OnResultsDrained != nil {
			cfg.OnResultsDrained()
		}

		// Flush remaining batch.
		if len(batch) > 0 {
			scheduleInsert(batch)
		}
	})

	// Feed files into channel, then close to signal workers.
feedLoop:
	for _, file := range files {
		select {
		case <-stopCh:
			break feedLoop
		case filesCh <- file:
		}
	}

	close(filesCh)

	// Wait for all workers to finish, then signal inserter that no more results are coming.
	workerWg.Wait()
	close(resultsCh)

	// Wait for inserter and all insert goroutines to finish.
	insertWg.Wait()

	result := Result{
		TotalChunks:    int(atomic.LoadInt64(&totalChunks)),
		ChunkCounts:    chunkCounts,
		CompletedFiles: tracker.completedFiles(),
	}
	if fileChunkIDs != nil {
		result.FileChunkIDs = fileChunkIDs
	}

	if firstErr != nil {
		return result, firstErr
	}

	return result, nil
}

func emitFileEntityBatches(
	relPath, ext, codebasePath string,
	batchSize int,
	split func(func(splitter.Chunk) error) error,
	emit func([]milvus.Entity) error,
) (int, error) {
	batchSize = max(batchSize, 1)
	batch := make([]milvus.Entity, 0, batchSize)
	total := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		toEmit := batch
		batch = make([]milvus.Entity, 0, batchSize)

		return emit(toEmit)
	}

	err := split(func(chunk splitter.Chunk) error {
		chunkEntities, err := buildEntitiesForChunk(relPath, ext, codebasePath, chunk)
		if err != nil {
			return err
		}

		total += len(chunkEntities)

		for _, entity := range chunkEntities {
			batch = append(batch, entity)
			if len(batch) == batchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	if err := flush(); err != nil {
		return 0, err
	}

	return total, nil
}

func buildEntitiesForChunk(relPath, ext, codebasePath string, chunk splitter.Chunk) ([]milvus.Entity, error) {
	entity := BuildEntity(relPath, ext, codebasePath, chunk)

	payloadBytes, err := entityPayloadBytes(&entity)
	if err != nil {
		return nil, err
	}

	if payloadBytes <= maxEntityPayloadBytes {
		return []milvus.Entity{entity}, nil
	}

	subChunks, err := splitOversizedChunk(relPath, chunk)
	if err != nil {
		return nil, fmt.Errorf(
			"entity payload %d bytes exceeds %d-byte limit at %s:%d-%d: %w",
			payloadBytes,
			maxEntityPayloadBytes,
			relPath,
			chunk.StartLine,
			chunk.EndLine,
			err,
		)
	}

	entities := make([]milvus.Entity, 0, len(subChunks))
	for _, subChunk := range subChunks {
		subEntities, err := buildEntitiesForChunk(relPath, ext, codebasePath, subChunk)
		if err != nil {
			return nil, err
		}

		entities = append(entities, subEntities...)
	}

	return entities, nil
}

func entityPayloadBytes(entity *milvus.Entity) (int, error) {
	payload, err := json.Marshal(entity)
	if err != nil {
		// Defensive: milvus.Entity is strings+ints only today; keep the error if that invariant ever changes.
		return 0, fmt.Errorf("marshal entity payload: %w", err)
	}

	return len(payload), nil
}

func splitOversizedChunk(relPath string, chunk splitter.Chunk) ([]splitter.Chunk, error) {
	nextChunkSize := max(utf8.RuneCountInString(chunk.Content)/2, 1)

	var subChunks []splitter.Chunk

	err := splitter.NewTextSplitter(nextChunkSize, oversizedSplitOverlapRunes).Split(
		strings.NewReader(chunk.Content),
		relPath,
		func(subChunk splitter.Chunk) error {
			subChunks = append(subChunks, subChunk)

			return nil
		},
	)
	if err != nil {
		// Defensive: TextSplitter reads from strings.NewReader and this callback only appends, so tests would need an artificial seam to hit this.
		return nil, fmt.Errorf("split oversized chunk %s: %w", relPath, err)
	}

	lineOffset := chunk.StartLine - 1
	adjusted := make([]splitter.Chunk, 0, len(subChunks))

	for _, subChunk := range subChunks {
		if subChunk.Content == "" {
			continue
		}

		subChunk.StartLine += lineOffset

		subChunk.EndLine += lineOffset
		if subChunk.Content == chunk.Content {
			return nil, errCannotSplitOversizedChunk
		}

		adjusted = append(adjusted, subChunk)
	}

	if len(adjusted) < 2 {
		return nil, errCannotSplitOversizedChunk
	}

	return adjusted, nil
}

// BuildEntity creates a milvus.Entity from a code chunk. It generates a stable
// ID via SHA-256, strips the leading dot from ext, and encodes codebasePath as
// JSON metadata.
func BuildEntity(relPath, ext, codebasePath string, chunk splitter.Chunk) milvus.Entity {
	content := chunk.Content

	key := relPath + ":" + strconv.Itoa(chunk.StartLine) + ":" + strconv.Itoa(chunk.EndLine) + ":" + chunk.Content
	sum := sha256.Sum256([]byte(key))
	id := fmt.Sprintf("chunk_%x", sum[:8])

	metaBytes, err := json.Marshal(map[string]string{"codebasePath": codebasePath})
	if err != nil { // Defensive: map[string]string Marshal cannot fail without changing the metadata shape.
		metaBytes = []byte("{}")
	}

	return milvus.Entity{
		ID:            id,
		Content:       content,
		RelativePath:  relPath,
		StartLine:     chunk.StartLine,
		EndLine:       chunk.EndLine,
		FileExtension: strings.TrimPrefix(ext, "."),
		Metadata:      string(metaBytes),
	}
}
