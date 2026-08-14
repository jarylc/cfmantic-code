package filesync

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/walker"
	"context"
	"errors"
	"fmt"
	"log"
)

// IncrementalParamsConfig contains the shared dependencies for incremental sync callbacks.
type IncrementalParamsConfig struct {
	Path                string
	Collection          string
	Client              milvus.VectorClient
	LogPrefix           string
	IncludePathInErrors bool
	SaveManifest        func(*FileHashMap, map[string]int) error
}

// NewIncrementalParams builds the shared file hash and stale chunk callbacks.
func NewIncrementalParams(ctx context.Context, cfg *IncrementalParamsConfig) *IncrementalParams {
	cfgValue := IncrementalParamsConfig{}
	if cfg != nil {
		cfgValue = *cfg
	}

	cfg = &cfgValue

	collection := cfg.Collection
	if collection == "" {
		collection = snapshot.CollectionName(cfg.Path)
	}

	logPrefix := cfg.LogPrefix
	if logPrefix == "" {
		logPrefix = "sync"
	}

	saveManifest := cfg.SaveManifest
	if saveManifest == nil {
		saveManifest = func(manifest *FileHashMap, chunkCounts map[string]int) error {
			return SaveManifest(cfg.Path, manifest, chunkCounts)
		}
	}

	return &IncrementalParams{
		LoadOldHashes: func() (*FileHashMap, error) {
			return LoadFileHashMap(HashFilePath(cfg.Path))
		},
		QueryFileChunkIDs: func(relPath string, limit int) ([]string, error) {
			if limit <= 0 {
				return nil, nil
			}

			entities, err := cfg.Client.Query(ctx, collection, fmt.Sprintf("relativePath == %q", relPath), limit)
			if err != nil {
				if cfg.IncludePathInErrors {
					err = fmt.Errorf("query chunks for %s in %s: %w", relPath, cfg.Path, err)
				} else {
					err = fmt.Errorf("query chunks for %s: %w", relPath, err)
				}

				log.Printf("%s: %v", logPrefix, err)

				return nil, err
			}

			return nonEmptyEntityIDs(entities), nil
		},
		DeleteFile: func(relPath string) error {
			err := cfg.Client.Delete(ctx, collection, fmt.Sprintf("relativePath == %q", relPath))
			if err != nil {
				if cfg.IncludePathInErrors {
					err = fmt.Errorf("delete chunks for %s in %s: %w", relPath, cfg.Path, err)
				} else {
					err = fmt.Errorf("delete chunks for %s: %w", relPath, err)
				}

				log.Printf("%s: %v", logPrefix, err)
			}

			return err
		},
		DeleteChunkIDs: func(ids []string) error {
			if len(ids) == 0 {
				return nil
			}

			err := cfg.Client.Delete(ctx, collection, ExactIDListFilter(ids...))
			if err != nil {
				if cfg.IncludePathInErrors {
					err = fmt.Errorf("delete chunks in %s: %w", cfg.Path, err)
				} else {
					err = fmt.Errorf("delete chunks for %s: %w", cfg.Path, err)
				}

				log.Printf("%s: %v", logPrefix, err)
			}

			return err
		},
		DeleteChunkID: func(id string) error {
			err := cfg.Client.Delete(ctx, collection, ExactIDListFilter(id))
			if err != nil {
				if cfg.IncludePathInErrors {
					err = fmt.Errorf("delete chunk %s in %s: %w", id, cfg.Path, err)
				} else {
					err = fmt.Errorf("delete chunk %s: %w", id, err)
				}

				log.Printf("%s: %v", logPrefix, err)
			}

			return err
		},
		SaveManifest: saveManifest,
	}
}

func nonEmptyEntityIDs(entities []milvus.Entity) []string {
	ids := make([]string, 0, len(entities))
	for _, entity := range entities {
		if entity.ID == "" {
			continue
		}

		ids = append(ids, entity.ID)
	}

	return ids
}

func (m *Manager) syncIgnorePatterns(path string) []string {
	reader, ok := m.snapshot.(snapshot.IgnorePatternReader)
	if ok {
		if ignore, ok := reader.GetIgnorePatterns(path); ok {
			return ignore
		}
	}

	ignore := make([]string, len(m.cfg.CustomIgnore))
	copy(ignore, m.cfg.CustomIgnore)

	return ignore
}

func (m *Manager) syncRunParams(path string, tracker *snapshot.Tracker) *IncrementalParams {
	return m.syncRunParamsWithContext(context.Background(), path, tracker)
}

func (m *Manager) syncRunParamsWithContext(ctx context.Context, path string, tracker *snapshot.Tracker) *IncrementalParams {
	collection := snapshot.CollectionName(path)

	isCanceled := func() bool {
		return errors.Is(ctx.Err(), context.Canceled)
	}

	params := NewIncrementalParams(ctx, &IncrementalParamsConfig{
		Path:                path,
		Collection:          collection,
		Client:              m.milvus,
		LogPrefix:           "sync",
		IncludePathInErrors: true,
	})

	params.Boundary = Boundary{
		Path: path,
		OnLockError: func(err error) {
			log.Printf("sync: skip %s: %s", path, err)
		},
	}
	params.WalkFiles = func() ([]walker.CodeFile, error) {
		return walker.Walk(ctx, path, m.syncIgnorePatterns(path))
	}
	params.ProcessFiles = func(files []walker.CodeFile, _ func(string, int)) ProcessResult {
		pipelineCfg := &pipeline.Config{
			Concurrency:         m.cfg.IndexConcurrency,
			InsertConcurrency:   m.cfg.InsertConcurrency,
			InsertBatchSize:     m.cfg.InsertBatchSize,
			CollectFileChunkIDs: true,
			Collection:          collection,
			CodebasePath:        path,
			OnProgress: func(filesDone, filesTotal, chunksTotal, chunksInserted int) {
				if tracker != nil {
					tracker.Progress(snapshot.Progress{
						FilesDone:      filesDone,
						FilesTotal:     filesTotal,
						ChunksTotal:    chunksTotal,
						ChunksInserted: chunksInserted,
					})
				}
			},
		}

		res, err := pipeline.Run(ctx, pipelineCfg, files, m.splitter, m.milvus)

		if tracker != nil {
			tracker.Flush()
		}

		if err != nil {
			return ProcessResult{Err: err.Error()}
		}

		return ProcessResult{
			TotalChunks:  res.TotalChunks,
			ChunkCounts:  res.ChunkCounts,
			FileChunkIDs: res.FileChunkIDs,
		}
	}
	params.CurrentTotalChunks = func() (int, bool) {
		info := m.snapshot.GetInfo(path)
		if info == nil {
			return 0, false
		}

		return info.TotalChunks, true
	}
	params.OnWalkError = func(err error) {
		log.Printf("sync: walk %s: %v", path, err)

		if tracker != nil {
			tracker.Start("Walking files")
			tracker.Failed(fmt.Sprintf("sync: walk failed: %v", err))
		}
	}
	params.OnComputeError = func(err error) {
		log.Printf("sync: compute manifest for %s: %v", path, err)

		if tracker != nil {
			tracker.Start("Computing file changes")
			tracker.Failed(fmt.Sprintf("sync: compute changes failed: %v", err))
		}
	}
	params.OnChanges = func(diff *ManifestDiff) {
		added, modified, deleted := diff.ChangeCounts()
		log.Printf("sync: %d changes in %s (%d added, %d modified, %d deleted)", len(diff.Changes), path, added, modified, deleted)
	}
	params.OnNoChanges = func() {
		log.Printf("sync: no changes in %s", path)
	}
	params.OnDeleteStart = func() {
		if tracker != nil {
			tracker.Step("Removing stale chunks")
		}
	}
	params.OnDeleteError = func(err error) {
		if isCanceled() {
			log.Printf("sync: canceled sync for %s", path)

			return
		}

		msg := fmt.Sprintf("sync: delete failed: %v", err)
		log.Printf("sync: delete failed for %s, marking failed and skipping state commit: %v", path, err)

		if tracker != nil {
			tracker.Failed(msg)
		} else {
			m.snapshot.SetFailed(path, msg)
		}
	}
	params.OnIndexStart = func(fileCount int) {
		if tracker != nil {
			tracker.Step(fmt.Sprintf("Indexing %d changed files", fileCount))
		}
	}
	params.OnFinalizeStart = func() {
		if tracker != nil {
			tracker.Step(FinalizingIncrementalSyncStep)
		}
	}
	params.OnInsertError = func(err string) {
		if isCanceled() {
			log.Printf("sync: canceled sync for %s", path)

			return
		}

		log.Printf("sync: insert failed for %s: %v", path, err)

		msg := "sync: insert failed: " + err
		if tracker != nil {
			tracker.Failed(msg)
		} else {
			m.snapshot.SetFailed(path, msg)
		}
	}
	params.OnSaveManifestError = func(err error) {
		log.Printf("sync: save hashes for %s: %v", path, err)

		msg := fmt.Sprintf("sync: save hashes failed: %v", err)
		if tracker != nil {
			tracker.Failed(msg)
		} else {
			m.snapshot.SetFailed(path, msg)
		}
	}
	params.OnIndexed = func(files, chunks int) {
		if m.snapshot.GetInfo(path) == nil {
			return
		}

		if tracker != nil {
			tracker.Indexed(files, chunks)
		} else {
			m.snapshot.SetIndexed(path, files, chunks)
		}
	}
	params.AfterSuccess = func() {
		if m.snapshot.GetStatus(path) != snapshot.StatusIndexed {
			return
		}

		log.Printf("sync: completed sync for %s", path)
	}

	return params
}
