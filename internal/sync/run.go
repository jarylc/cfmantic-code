package filesync

import (
	"cfmantic-code/internal/runtimeutil"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/walker"
)

var (
	acquireLock = snapshot.AcquireLock
	finalizeRun = runtimeutil.FinalizeRun
)

type Boundary struct {
	Path          string
	ExtraCleanups []func()
	OnLockError   func(error)
}

type ProgressSaver interface {
	Record(relPath string, entry FileEntry) error
	Flush() error
}

type ProcessResult struct {
	TotalChunks  int
	ChunkCounts  map[string]int
	FileChunkIDs map[string][]string
	Err          string
}

const FinalizingIncrementalSyncStep = "Finalizing incremental sync"

type FullParams struct {
	Boundary            Boundary
	WalkFiles           func() ([]walker.CodeFile, error)
	OnWalkStart         func()
	OnWalkError         func(error)
	OnFilesPrepared     func(int)
	OnManifestError     func(error)
	OnSaveManifestError func(error)
	OnProgressSaveError func(error)
	NewProgressSaver    func(*FileHashMap) ProgressSaver
	ProcessFiles        func([]walker.CodeFile, func(string, int)) ProcessResult
	OnInsertError       func(string)
	OnIndexed           func(int, int)
	SaveManifest        func(*FileHashMap, map[string]int) error
	AfterSuccess        func()
}

type IncrementalParams struct {
	Boundary            Boundary
	WalkFiles           func() ([]walker.CodeFile, error)
	LoadOldHashes       func() (*FileHashMap, error)
	QueryFileChunkIDs   func(string, int) ([]string, error)
	DeleteFile          func(string) error
	DeleteChunkIDs      func([]string) error
	DeleteChunkID       func(string) error
	ProcessFiles        func([]walker.CodeFile, func(string, int)) ProcessResult
	NewProgressSaver    func(*FileHashMap) ProgressSaver
	OnProgressSaveError func(error)
	SaveManifest        func(*FileHashMap, map[string]int) error
	CurrentTotalChunks  func() (int, bool)
	OnWalkStart         func()
	OnWalkError         func(error)
	OnComputeStart      func()
	OnComputeError      func(error)
	OnChanges           func(*ManifestDiff)
	OnNoChanges         func()
	OnDeleteStart       func()
	OnDeleteError       func(error)
	OnIndexStart        func(int)
	OnFinalizeStart     func()
	OnInsertError       func(string)
	OnSaveManifestError func(error)
	OnIndexed           func(int, int)
	AfterSuccess        func()
}

func WithBoundary(boundary Boundary, body func()) {
	var releaseLock func()

	cleanups := make([]func(), 0, len(boundary.ExtraCleanups)+1)
	cleanups = append(cleanups,
		func() {
			if releaseLock != nil {
				releaseLock()
			}
		},
	)
	cleanups = append(cleanups, boundary.ExtraCleanups...)

	defer finalizeRun(cleanups...)

	var err error

	releaseLock, err = acquireLock(boundary.Path)
	if err != nil {
		callErr(boundary.OnLockError, err)
		return
	}

	if body != nil {
		body()
	}
}

func RunFull(params *FullParams) {
	WithBoundary(params.Boundary, func() {
		call(params.OnWalkStart)

		files, err := params.WalkFiles()
		if err != nil {
			callErr(params.OnWalkError, err)
			return
		}

		callInt(params.OnFilesPrepared, len(files))

		manifestDiff := ComputeManifestDiff(files, nil)

		var progressSaver ProgressSaver
		if params.NewProgressSaver != nil {
			progressSaver = params.NewProgressSaver(manifestDiff.ProgressManifest())
		}

		result := params.ProcessFiles(files, progressRecorder(progressSaver, manifestDiff.Manifest, params.OnProgressSaveError))
		flushProgressSaver(progressSaver, params.OnProgressSaveError)

		if result.Err != "" {
			callString(params.OnInsertError, result.Err)
			return
		}

		if err := callSaveManifest(params.SaveManifest, manifestDiff.Manifest, result.ChunkCounts); err != nil {
			callErr(params.OnSaveManifestError, err)
			return
		}

		callIndexed(params.OnIndexed, len(files), result.TotalChunks)
		call(params.AfterSuccess)
	})
}

func RunIncremental(params *IncrementalParams) {
	WithBoundary(params.Boundary, func() {
		call(params.OnWalkStart)

		files, err := params.WalkFiles()
		if err != nil {
			callErr(params.OnWalkError, err)
			return
		}

		call(params.OnComputeStart)

		oldHashes := NewFileHashMap()

		if params.LoadOldHashes != nil {
			loaded, err := params.LoadOldHashes()
			if err != nil {
				callErr(params.OnComputeError, err)
				return
			}

			if loaded != nil {
				oldHashes = loaded
			}
		}

		manifestDiff := ComputeManifestDiff(files, oldHashes)

		if len(manifestDiff.Changes) == 0 {
			call(params.OnNoChanges)
			call(params.AfterSuccess)

			return
		}

		callManifestDiff(params.OnChanges, manifestDiff)
		call(params.OnDeleteStart)

		modifiedFileChunkIDs, queryErr := captureModifiedFileChunkIDs(manifestDiff, oldHashes, params.QueryFileChunkIDs)
		if queryErr != nil {
			callErr(params.OnDeleteError, queryErr)
			return
		}

		if deleteErr := deleteDeletedFiles(manifestDiff, params.DeleteFile); deleteErr != nil {
			callErr(params.OnDeleteError, deleteErr)
			return
		}

		filesToProcess := manifestDiff.FilesToProcess(files)
		addedChunks := 0

		var (
			chunkCounts  map[string]int
			fileChunkIDs map[string][]string
		)

		if len(filesToProcess) > 0 {
			callInt(params.OnIndexStart, len(filesToProcess))

			var progressSaver ProgressSaver
			if params.NewProgressSaver != nil {
				progressSaver = params.NewProgressSaver(manifestDiff.ProgressManifest())
			}

			result := params.ProcessFiles(filesToProcess, progressRecorder(progressSaver, manifestDiff.Manifest, params.OnProgressSaveError))
			if result.Err == "" {
				call(params.OnFinalizeStart)
			}

			flushProgressSaver(progressSaver, params.OnProgressSaveError)

			if result.Err != "" {
				callString(params.OnInsertError, result.Err)
				return
			}

			addedChunks = result.TotalChunks
			chunkCounts = result.ChunkCounts
			fileChunkIDs = result.FileChunkIDs
		}

		if deleteErr := deleteModifiedFileChunks(modifiedFileChunkIDs, fileChunkIDs, params.DeleteChunkIDs, params.DeleteChunkID); deleteErr != nil {
			callErr(params.OnDeleteError, deleteErr)
			return
		}

		if err := callSaveManifest(params.SaveManifest, manifestDiff.Manifest, chunkCounts); err != nil {
			callErr(params.OnSaveManifestError, err)
			return
		}

		totalChunks := addedChunks

		if params.CurrentTotalChunks != nil {
			if existingTotal, ok := params.CurrentTotalChunks(); ok {
				removedChunks := oldHashes.ChunkCountForFiles(manifestDiff.RemovedPaths())

				totalChunks = existingTotal - removedChunks + addedChunks
				if totalChunks < 0 {
					totalChunks = addedChunks
				}
			}
		}

		callIndexed(params.OnIndexed, len(files), totalChunks)
		call(params.AfterSuccess)
	})
}

type modifiedFileChunkIDs struct {
	relPath string
	ids     []string
}

func captureModifiedFileChunkIDs(
	diff *ManifestDiff,
	oldHashes *FileHashMap,
	queryFileChunkIDs func(string, int) ([]string, error),
) ([]modifiedFileChunkIDs, error) {
	if diff == nil || queryFileChunkIDs == nil {
		return nil, nil
	}

	plans := make([]modifiedFileChunkIDs, 0)

	for _, change := range diff.Changes {
		if change.Type != Modified {
			continue
		}

		limit := 0
		if oldHashes != nil {
			limit = oldHashes.Files[change.RelPath].ChunkCount
		}

		if limit <= 0 {
			continue
		}

		ids, err := queryFileChunkIDs(change.RelPath, limit)
		if err != nil {
			return nil, err
		}

		ids = dedupeIDs(ids)
		if len(ids) == 0 {
			continue
		}

		plans = append(plans, modifiedFileChunkIDs{relPath: change.RelPath, ids: ids})
	}

	return plans, nil
}

func deleteDeletedFiles(diff *ManifestDiff, deleteFile func(string) error) error {
	if diff == nil || deleteFile == nil {
		return nil
	}

	var deleteErr error

	for _, change := range diff.Changes {
		if change.Type != Deleted {
			continue
		}

		if err := deleteFile(change.RelPath); err != nil {
			deleteErr = err
		}
	}

	return deleteErr
}

func deleteModifiedFileChunks(
	plans []modifiedFileChunkIDs,
	fileChunkIDs map[string][]string,
	deleteChunkIDs func([]string) error,
	deleteChunkID func(string) error,
) error {
	if len(plans) == 0 || (deleteChunkIDs == nil && deleteChunkID == nil) {
		return nil
	}

	var deleteErr error

	const maxDeleteChunkIDsPerRequest = 100

	for _, plan := range plans {
		newIDs := make(map[string]struct{}, len(fileChunkIDs[plan.relPath]))

		staleIDs := make([]string, 0, len(plan.ids))
		for _, id := range fileChunkIDs[plan.relPath] {
			if id == "" {
				continue
			}

			newIDs[id] = struct{}{}
		}

		for _, id := range plan.ids {
			if _, keep := newIDs[id]; keep {
				continue
			}

			staleIDs = append(staleIDs, id)
		}

		if len(staleIDs) == 0 {
			continue
		}

		if deleteChunkIDs != nil {
			for len(staleIDs) > 0 {
				batchSize := min(len(staleIDs), maxDeleteChunkIDsPerRequest)

				if err := deleteChunkIDs(staleIDs[:batchSize]); err != nil {
					deleteErr = err
				}

				staleIDs = staleIDs[batchSize:]
			}

			continue
		}

		for _, id := range staleIDs {
			if err := deleteChunkID(id); err != nil {
				deleteErr = err
			}
		}
	}

	return deleteErr
}

func dedupeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))

	for _, id := range ids {
		if id == "" {
			continue
		}

		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}

func flushProgressSaver(saver ProgressSaver, onError func(error)) {
	if saver == nil {
		return
	}

	if err := saver.Flush(); err != nil {
		callErr(onError, err)
	}
}

func progressRecorder(saver ProgressSaver, manifest *FileHashMap, onError func(error)) func(string, int) {
	if saver == nil || manifest == nil {
		return nil
	}

	return func(relPath string, chunkCount int) {
		entry, ok := manifest.Files[relPath]
		if !ok {
			return
		}

		entry.ChunkCount = chunkCount
		if err := saver.Record(relPath, entry); err != nil {
			callErr(onError, err)
		}
	}
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

func callErr(fn func(error), err error) {
	if fn != nil {
		fn(err)
	}
}

func callInt(fn func(int), value int) {
	if fn != nil {
		fn(value)
	}
}

func callString(fn func(string), value string) {
	if fn != nil {
		fn(value)
	}
}

func callIndexed(fn func(int, int), files, chunks int) {
	if fn != nil {
		fn(files, chunks)
	}
}

func callManifestDiff(fn func(*ManifestDiff), diff *ManifestDiff) {
	if fn != nil {
		fn(diff)
	}
}

func callSaveManifest(fn func(*FileHashMap, map[string]int) error, manifest *FileHashMap, chunkCounts map[string]int) error {
	if fn == nil {
		return nil
	}

	return fn(manifest, chunkCounts)
}
