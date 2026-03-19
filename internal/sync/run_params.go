package filesync

import (
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/walker"
	"context"
	"errors"
	"fmt"
	"log"
)

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

	deleteChunkIDs := func(ids []string) error {
		if len(ids) == 0 {
			return nil
		}

		err := m.milvus.Delete(ctx, collection, ExactIDListFilter(ids...))
		if err != nil {
			err = fmt.Errorf("delete chunks in %s: %w", path, err)
			log.Printf("sync: %v", err)
		}

		return err
	}

	return &IncrementalParams{
		Boundary: Boundary{
			Path: path,
			OnLockError: func(err error) {
				log.Printf("sync: skip %s: %s", path, err)
			},
		},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return walker.Walk(ctx, path, m.syncIgnorePatterns(path))
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return LoadFileHashMap(HashFilePath(path))
		},
		QueryFileChunkIDs: func(relPath string, limit int) ([]string, error) {
			if limit <= 0 {
				return nil, nil
			}

			entities, err := m.milvus.Query(ctx, collection, fmt.Sprintf("relativePath == %q", relPath), limit)
			if err != nil {
				err = fmt.Errorf("query chunks for %s in %s: %w", relPath, path, err)
				log.Printf("sync: %v", err)

				return nil, err
			}

			ids := make([]string, 0, len(entities))
			for _, entity := range entities {
				if entity.ID == "" {
					continue
				}

				ids = append(ids, entity.ID)
			}

			return ids, nil
		},
		DeleteFile: func(relPath string) error {
			err := m.milvus.Delete(ctx, collection, fmt.Sprintf("relativePath == %q", relPath))
			if err != nil {
				err = fmt.Errorf("delete chunks for %s in %s: %w", relPath, path, err)
				log.Printf("sync: %v", err)
			}

			return err
		},
		DeleteChunkIDs: deleteChunkIDs,
		DeleteChunkID: func(id string) error {
			err := m.milvus.Delete(ctx, collection, ExactIDListFilter(id))
			if err != nil {
				err = fmt.Errorf("delete chunk %s in %s: %w", id, path, err)
				log.Printf("sync: %v", err)
			}

			return err
		},
		ProcessFiles: func(files []walker.CodeFile, _ func(string, int)) ProcessResult {
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
		},
		SaveManifest: func(manifest *FileHashMap, chunkCounts map[string]int) error {
			return SaveManifest(path, manifest, chunkCounts)
		},
		CurrentTotalChunks: func() (int, bool) {
			info := m.snapshot.GetInfo(path)
			if info == nil {
				return 0, false
			}

			return info.TotalChunks, true
		},
		OnWalkError: func(err error) {
			log.Printf("sync: walk %s: %v", path, err)

			if tracker != nil {
				tracker.Start("Walking files")
				tracker.Failed(fmt.Sprintf("sync: walk failed: %v", err))
			}
		},
		OnComputeError: func(err error) {
			log.Printf("sync: compute manifest for %s: %v", path, err)

			if tracker != nil {
				tracker.Start("Computing file changes")
				tracker.Failed(fmt.Sprintf("sync: compute changes failed: %v", err))
			}
		},
		OnChanges: func(diff *ManifestDiff) {
			added, modified, deleted := diff.ChangeCounts()
			log.Printf("sync: %d changes in %s (%d added, %d modified, %d deleted)", len(diff.Changes), path, added, modified, deleted)
		},
		OnNoChanges: func() {
			log.Printf("sync: no changes in %s", path)
		},
		OnDeleteStart: func() {
			if tracker != nil {
				tracker.Step("Removing stale chunks")
			}
		},
		OnDeleteError: func(err error) {
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
		},
		OnIndexStart: func(fileCount int) {
			if tracker != nil {
				tracker.Step(fmt.Sprintf("Indexing %d changed files", fileCount))
			}
		},
		OnFinalizeStart: func() {
			if tracker != nil {
				tracker.Step(FinalizingIncrementalSyncStep)
			}
		},
		OnInsertError: func(err string) {
			if isCanceled() {
				log.Printf("sync: canceled sync for %s", path)

				return
			}

			log.Printf("sync: insert failed for %s: %v", path, err)

			if tracker != nil {
				tracker.Failed("sync: insert failed")
			} else {
				m.snapshot.SetFailed(path, "sync: insert failed")
			}
		},
		OnSaveManifestError: func(err error) {
			log.Printf("sync: save hashes for %s: %v", path, err)

			msg := fmt.Sprintf("sync: save hashes failed: %v", err)
			if tracker != nil {
				tracker.Failed(msg)
			} else {
				m.snapshot.SetFailed(path, msg)
			}
		},
		OnIndexed: func(files, chunks int) {
			if m.snapshot.GetInfo(path) == nil {
				return
			}

			if tracker != nil {
				tracker.Indexed(files, chunks)
			} else {
				m.snapshot.SetIndexed(path, files, chunks)
			}
		},
		AfterSuccess: func() {
			if m.snapshot.GetStatus(path) != snapshot.StatusIndexed {
				return
			}

			log.Printf("sync: completed sync for %s", path)
		},
	}
}
