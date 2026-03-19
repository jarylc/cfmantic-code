package handler

import (
	"cfmantic-code/internal/snapshot"
	filesync "cfmantic-code/internal/sync"
	"cfmantic-code/internal/walker"
	"context"
	"fmt"
	"log"
)

func (h *Handler) persistIgnorePatterns(path string, ignorePatterns []string) {
	setter, ok := h.snapshot.(snapshot.IgnorePatternSetter)
	if !ok {
		return
	}

	setter.SetIgnorePatterns(path, h.effectiveIgnorePatterns(ignorePatterns))
}

func (h *Handler) fullRunParams(
	ctx context.Context,
	path, collection string,
	ignorePatterns []string,
	tracker *snapshot.Tracker,
) *filesync.FullParams {
	return &filesync.FullParams{
		Boundary: filesync.Boundary{
			Path: path,
			ExtraCleanups: []func(){
				func() { <-h.indexSem },
			},
			OnLockError: func(err error) {
				tracker.Failed(fmt.Sprintf("lock: %v", err))
			},
		},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return h.walkFiles(ctx, path, ignorePatterns)
		},
		OnWalkStart: func() {
			tracker.Step("Walking files")
		},
		OnWalkError: func(err error) {
			tracker.Failed(err.Error())
		},
		OnFilesPrepared: func(fileCount int) {
			log.Printf("Indexing %d files from %s (concurrency: %d)", fileCount, path, h.cfg.IndexConcurrency)
			tracker.Step(fmt.Sprintf("Indexing %d files", fileCount))
		},
		OnManifestError: func(err error) {
			tracker.Failed(fmt.Sprintf("computing file hashes: %v", err))
		},
		OnSaveManifestError: func(err error) {
			log.Printf("handler: failed to save file hashes for %s: %v", path, err)
			tracker.Failed(fmt.Sprintf("saving file hashes: %v", err))
		},
		OnProgressSaveError: func(err error) {
			log.Printf("handler: failed to save progress hashes: %v", err)
		},
		NewProgressSaver: func(manifest *filesync.FileHashMap) filesync.ProgressSaver {
			return newManifestProgressSaver(filesync.HashFilePath(path), manifest)
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) filesync.ProcessResult {
			result := h.processFiles(ctx, path, collection, files, onFileIndexed, false, tracker)

			return filesync.ProcessResult{
				TotalChunks: result.totalChunks,
				ChunkCounts: result.chunkCounts,
				Err:         result.err,
			}
		},
		OnInsertError: func(err string) {
			tracker.Failed("insert failed: " + err)
		},
		OnIndexed: func(files, chunks int) {
			tracker.Indexed(files, chunks)
			log.Printf("handler: indexing complete for %s: %d files, %d chunks", path, files, chunks)
		},
		SaveManifest: func(manifest *filesync.FileHashMap, chunkCounts map[string]int) error {
			return h.saveManifest(path, manifest, chunkCounts)
		},
		AfterSuccess: func() {
			if h.snapshot.GetStatus(path) != snapshot.StatusIndexed {
				return
			}

			h.persistIgnorePatterns(path, ignorePatterns)

			if h.syncMgr != nil {
				h.syncMgr.TrackPath(path)
			}
		},
	}
}

func (h *Handler) incrementalRunParams(
	ctx context.Context,
	path string,
	ignorePatterns []string,
	tracker *snapshot.Tracker,
) *filesync.IncrementalParams {
	collectionName := snapshot.CollectionName(path)

	var (
		changeCount  int
		insertedRows int
	)

	deleteChunkIDs := func(ids []string) error {
		if len(ids) == 0 {
			return nil
		}

		err := h.milvus.Delete(ctx, collectionName, filesync.ExactIDListFilter(ids...))
		if err != nil {
			err = fmt.Errorf("delete chunks for %s: %w", path, err)
			log.Printf("handler: %v", err)
		}

		return err
	}

	return &filesync.IncrementalParams{
		Boundary: filesync.Boundary{
			Path: path,
			ExtraCleanups: []func(){
				func() { <-h.indexSem },
			},
			OnLockError: func(err error) {
				tracker.Failed(fmt.Sprintf("lock: %v", err))
			},
		},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return h.walkFiles(ctx, path, ignorePatterns)
		},
		LoadOldHashes: func() (*filesync.FileHashMap, error) {
			return filesync.LoadFileHashMap(filesync.HashFilePath(path))
		},
		QueryFileChunkIDs: func(relPath string, limit int) ([]string, error) {
			if limit <= 0 {
				return nil, nil
			}

			entities, err := h.milvus.Query(ctx, collectionName, fmt.Sprintf(`relativePath == %q`, relPath), limit)
			if err != nil {
				err = fmt.Errorf("query chunks for %s: %w", relPath, err)
				log.Printf("handler: %v", err)

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
			err := h.milvus.Delete(ctx, collectionName, fmt.Sprintf(`relativePath == %q`, relPath))
			if err != nil {
				err = fmt.Errorf("delete chunks for %s: %w", relPath, err)
				log.Printf("handler: %v", err)
			}

			return err
		},
		DeleteChunkIDs: deleteChunkIDs,
		DeleteChunkID: func(id string) error {
			err := h.milvus.Delete(ctx, collectionName, filesync.ExactIDListFilter(id))
			if err != nil {
				err = fmt.Errorf("delete chunk %s: %w", id, err)
				log.Printf("handler: %v", err)
			}

			return err
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) filesync.ProcessResult {
			result := h.processFiles(ctx, path, collectionName, files, onFileIndexed, true, tracker)
			insertedRows = result.totalChunks

			return filesync.ProcessResult{
				TotalChunks:  result.totalChunks,
				ChunkCounts:  result.chunkCounts,
				FileChunkIDs: result.fileChunkIDs,
				Err:          result.err,
			}
		},
		NewProgressSaver: func(manifest *filesync.FileHashMap) filesync.ProgressSaver {
			return newManifestProgressSaver(filesync.HashFilePath(path), manifest)
		},
		OnProgressSaveError: func(err error) {
			log.Printf("handler: failed to save progress hashes: %v", err)
		},
		SaveManifest: func(manifest *filesync.FileHashMap, chunkCounts map[string]int) error {
			return h.saveManifest(path, manifest, chunkCounts)
		},
		CurrentTotalChunks: func() (int, bool) {
			info := h.snapshot.GetInfo(path)
			if info == nil {
				return 0, false
			}

			return info.TotalChunks, true
		},
		OnWalkStart: func() {
			tracker.Step("Walking files")
		},
		OnWalkError: func(err error) {
			tracker.Failed(err.Error())
		},
		OnComputeStart: func() {
			tracker.Step("Computing file changes")
		},
		OnComputeError: func(err error) {
			log.Printf("handler: failed to compute file hashes for incremental index: %v", err)
			tracker.Failed(fmt.Sprintf("computing file hashes: %v", err))
		},
		OnChanges: func(diff *filesync.ManifestDiff) {
			changeCount = len(diff.Changes)
			log.Printf("handler: incremental sync for %s: %d changes", path, changeCount)
		},
		OnNoChanges: func() {
			log.Printf("handler: no changes detected for %s", path)

			info := h.snapshot.GetInfo(path)
			if info != nil {
				h.snapshot.SetIndexed(path, info.IndexedFiles, info.TotalChunks)
			}
		},
		OnDeleteStart: func() {
			tracker.Step("Removing stale chunks")
		},
		OnDeleteError: func(err error) {
			tracker.Failed(fmt.Sprintf("incremental index: delete failed: %v", err))
		},
		OnIndexStart: func(fileCount int) {
			tracker.Step(fmt.Sprintf("Indexing %d changed files", fileCount))
		},
		OnFinalizeStart: func() {
			tracker.Step(filesync.FinalizingIncrementalSyncStep)
		},
		OnInsertError: func(err string) {
			tracker.Failed("insert failed: " + err)
		},
		OnSaveManifestError: func(err error) {
			log.Printf("handler: failed to save file hashes for %s: %v", path, err)
			tracker.Failed(fmt.Sprintf("saving file hashes: %v", err))
		},
		OnIndexed: func(files, chunks int) {
			tracker.Indexed(files, chunks)
			log.Printf("handler: incremental sync complete for %s: %d changes, %d new chunks", path, changeCount, insertedRows)
		},
		AfterSuccess: func() {
			if h.snapshot.GetStatus(path) != snapshot.StatusIndexed {
				return
			}

			h.persistIgnorePatterns(path, ignorePatterns)

			if h.syncMgr != nil {
				h.syncMgr.TrackPath(path)
			}
		},
	}
}
