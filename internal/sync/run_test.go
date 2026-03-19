package filesync

import (
	"cfmantic-code/internal/walker"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubRunBoundary(t *testing.T) {
	t.Helper()

	originalAcquireLock := acquireLock
	originalFinalizeRun := finalizeRun

	t.Cleanup(func() {
		acquireLock = originalAcquireLock
		finalizeRun = originalFinalizeRun
	})

	acquireLock = func(string) (func(), error) {
		return func() {}, nil
	}
	finalizeRun = func(cleanups ...func()) {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				cleanup()
			}
		}
	}
}

func writeRunCodeFile(t *testing.T, dir, name string) walker.CodeFile {
	t.Helper()

	absPath := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(absPath, []byte("package main\n"), 0o644))

	info, err := os.Stat(absPath)
	require.NoError(t, err)

	return walker.CodeFile{
		AbsPath:         absPath,
		RelPath:         name,
		Extension:       filepath.Ext(name),
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}
}

func TestWithBoundary_RunsCleanupsBeforeFinalize(t *testing.T) {
	originalAcquireLock := acquireLock
	originalFinalizeRun := finalizeRun

	t.Cleanup(func() {
		acquireLock = originalAcquireLock
		finalizeRun = originalFinalizeRun
	})

	var order []string

	acquireLock = func(string) (func(), error) {
		return func() { order = append(order, "lock") }, nil
	}
	finalizeRun = func(cleanups ...func()) {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				cleanup()
			}
		}

		order = append(order, "finalize")
	}

	WithBoundary(Boundary{
		Path: "/tmp/project",
		ExtraCleanups: []func(){
			func() { order = append(order, "guard") },
		},
	}, func() {
		order = append(order, "body")
	})

	assert.Equal(t, []string{"body", "lock", "guard", "finalize"}, order)
}

func TestWithBoundary_LockErrorStillFinalizes(t *testing.T) {
	originalAcquireLock := acquireLock
	originalFinalizeRun := finalizeRun

	t.Cleanup(func() {
		acquireLock = originalAcquireLock
		finalizeRun = originalFinalizeRun
	})

	var order []string

	lockErr := errors.New("locked")

	acquireLock = func(string) (func(), error) {
		return nil, lockErr
	}
	finalizeRun = func(cleanups ...func()) {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				cleanup()
			}
		}

		order = append(order, "finalize")
	}

	bodyCalled := false

	var handled error

	WithBoundary(Boundary{
		Path: "/tmp/project",
		ExtraCleanups: []func(){
			func() { order = append(order, "guard") },
		},
		OnLockError: func(err error) {
			handled = err

			order = append(order, "lock-error")
		},
	}, func() {
		bodyCalled = true
	})

	assert.False(t, bodyCalled)
	require.ErrorIs(t, handled, lockErr)
	assert.Equal(t, []string{"lock-error", "guard", "finalize"}, order)
}

func TestRunFull_SavesManifestBeforeIndexed(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	var order []string

	RunFull(&FullParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			return ProcessResult{
				TotalChunks: 2,
				ChunkCounts: map[string]int{"main.go": 2},
			}
		},
		OnIndexed: func(files, chunks int) {
			order = append(order, "indexed")
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return nil
		},
		AfterSuccess: func() {
			order = append(order, "track")
		},
	})

	assert.Equal(t, []string{"save", "indexed", "track"}, order)
}

func TestRunFull_SaveManifestErrorStopsSuccessCallbacks(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	var (
		order      []string
		handledErr error
	)

	RunFull(&FullParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			return ProcessResult{
				TotalChunks: 2,
				ChunkCounts: map[string]int{"main.go": 2},
			}
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return errors.New("boom")
		},
		OnSaveManifestError: func(err error) {
			handledErr = err

			order = append(order, "save-error")
		},
		OnIndexed: func(int, int) {
			order = append(order, "indexed")
		},
		AfterSuccess: func() {
			order = append(order, "track")
		},
	})

	require.EqualError(t, handledErr, "boom")
	assert.Equal(t, []string{"save", "save-error"}, order)
}

func TestRunIncremental_SavesManifestBeforeIndexed(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	var order []string

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return NewFileHashMap(), nil
		},
		DeleteFile: func(string) error { return nil },
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			return ProcessResult{
				TotalChunks: 3,
				ChunkCounts: map[string]int{"main.go": 3},
			}
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return nil
		},
		OnIndexed: func(files, chunks int) {
			order = append(order, "indexed")
		},
		AfterSuccess: func() {
			order = append(order, "track")
		},
	})

	assert.Equal(t, []string{"save", "indexed", "track"}, order)
}

func TestRunIncremental_SaveManifestErrorStopsSuccessCallbacks(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	var (
		order      []string
		handledErr error
	)

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return NewFileHashMap(), nil
		},
		DeleteFile: func(string) error { return nil },
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			return ProcessResult{
				TotalChunks: 3,
				ChunkCounts: map[string]int{"main.go": 3},
			}
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return errors.New("boom")
		},
		OnSaveManifestError: func(err error) {
			handledErr = err

			order = append(order, "save-error")
		},
		OnIndexed: func(int, int) {
			order = append(order, "indexed")
		},
		AfterSuccess: func() {
			order = append(order, "track")
		},
	})

	require.EqualError(t, handledErr, "boom")
	assert.Equal(t, []string{"save", "save-error"}, order)
}

func TestRunIncremental_LoadOldHashesErrorStopsIncrementalSync(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	var (
		order      []string
		handledErr error
	)

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			order = append(order, "walk")
			return []walker.CodeFile{file}, nil
		},
		OnComputeStart: func() {
			order = append(order, "compute-start")
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			order = append(order, "load")
			return nil, errors.New("boom")
		},
		OnComputeError: func(err error) {
			handledErr = err

			order = append(order, "compute-error")
		},
		DeleteFile: func(string) error {
			order = append(order, "delete")
			return nil
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			order = append(order, "process")
			return ProcessResult{}
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return nil
		},
		OnIndexed: func(int, int) {
			order = append(order, "indexed")
		},
		AfterSuccess: func() {
			order = append(order, "after")
		},
	})

	require.EqualError(t, handledErr, "boom")
	assert.Equal(t, []string{"walk", "compute-start", "load", "compute-error"}, order)
}

func TestRunIncremental_ModifiedFilesUseTwoPhaseReplace(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}

	var order []string

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		OnDeleteStart: func() {
			order = append(order, "delete-start")
		},
		QueryFileChunkIDs: func(relPath string, limit int) ([]string, error) {
			order = append(order, "query:"+relPath)
			assert.Equal(t, "main.go", relPath)
			assert.Equal(t, 2, limit)

			return []string{"chunk-keep", "chunk-stale"}, nil
		},
		DeleteFile: func(string) error {
			t.Fatal("modified file should not be deleted before replacement succeeds")
			return nil
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			order = append(order, "process")

			require.Len(t, files, 1)
			assert.Equal(t, "main.go", files[0].RelPath)

			return ProcessResult{
				TotalChunks:  1,
				ChunkCounts:  map[string]int{"main.go": 1},
				FileChunkIDs: map[string][]string{"main.go": {"chunk-keep"}},
			}
		},
		DeleteChunkID: func(id string) error {
			order = append(order, "delete-id:"+id)
			assert.Equal(t, "chunk-stale", id)

			return nil
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")

			return nil
		},
		OnIndexed: func(files, chunks int) {
			order = append(order, "indexed")

			assert.Equal(t, 1, files)
			assert.Equal(t, 1, chunks)
		},
		AfterSuccess: func() {
			order = append(order, "after")
		},
	})

	assert.Equal(t, []string{
		"delete-start",
		"query:main.go",
		"process",
		"delete-id:chunk-stale",
		"save",
		"indexed",
		"after",
	}, order)
}

func TestRunIncremental_PreservesOldEntryWhenHashingFails(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	brokenPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.Mkdir(brokenPath, 0o755))

	info, err := os.Stat(brokenPath)
	require.NoError(t, err)

	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{
		Hash:            "stale-hash",
		ChunkCount:      2,
		Size:            info.Size() + 1,
		ModTimeUnixNano: info.ModTime().UnixNano() - 1,
	}

	var (
		deleteCalled bool
		order        []string
	)

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{{
				AbsPath:         brokenPath,
				RelPath:         "main.go",
				Extension:       ".go",
				Size:            info.Size(),
				ModTimeUnixNano: info.ModTime().UnixNano(),
			}}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		DeleteFile: func(string) error {
			deleteCalled = true
			return nil
		},
		OnNoChanges: func() {
			order = append(order, "no-changes")
		},
		AfterSuccess: func() {
			order = append(order, "after")
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			t.Fatal("process should not run when an existing file cannot be rehashed")
			return ProcessResult{}
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			t.Fatal("save should not run when there are no changes")
			return nil
		},
	})

	assert.Equal(t, []string{"no-changes", "after"}, order)
	assert.False(t, deleteCalled)
}

func TestRunIncremental_ModifiedFileInsertErrorKeepsOldChunks(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")

	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}

	var order []string

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		QueryFileChunkIDs: func(relPath string, limit int) ([]string, error) {
			order = append(order, "query:"+relPath)

			assert.Equal(t, 2, limit)

			return []string{"chunk-old"}, nil
		},
		DeleteFile: func(string) error {
			t.Fatal("modified file should not be deleted before replacement succeeds")
			return nil
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			order = append(order, "process")
			return ProcessResult{Err: "boom"}
		},
		DeleteChunkID: func(string) error {
			t.Fatal("old modified chunks should not be deleted after insert failure")
			return nil
		},
		OnInsertError: func(err string) {
			order = append(order, "insert-error:"+err)
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			t.Fatal("manifest should not be saved after insert failure")

			return nil
		},
		OnIndexed: func(int, int) {
			t.Fatal("index should not be marked complete after insert failure")
		},
	})

	assert.Equal(t, []string{"query:main.go", "process", "insert-error:boom"}, order)
}

type recordingProgressSaver struct {
	records   map[string]FileEntry
	recordErr error
	flushErr  error
	onFlush   func()
}

func (s *recordingProgressSaver) Record(relPath string, entry FileEntry) error {
	if s.records == nil {
		s.records = make(map[string]FileEntry)
	}

	s.records[relPath] = entry

	return s.recordErr
}

func (s *recordingProgressSaver) Flush() error {
	if s.onFlush != nil {
		s.onFlush()
	}

	return s.flushErr
}

func TestRunIncremental_CallsFinalizeStartBeforeCleanupAndSave(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")
	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 1}

	var order []string

	saver := &recordingProgressSaver{onFlush: func() {
		order = append(order, "flush")
	}}

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		QueryFileChunkIDs: func(string, int) ([]string, error) {
			return []string{"chunk-stale"}, nil
		},
		DeleteFile: func(string) error { return nil },
		NewProgressSaver: func(*FileHashMap) ProgressSaver {
			return saver
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			order = append(order, "process")

			require.NotNil(t, onFileIndexed)
			onFileIndexed("main.go", 1)

			return ProcessResult{
				TotalChunks:  1,
				ChunkCounts:  map[string]int{"main.go": 1},
				FileChunkIDs: map[string][]string{"main.go": nil},
			}
		},
		OnFinalizeStart: func() {
			order = append(order, "finalize")
		},
		DeleteChunkID: func(id string) error {
			order = append(order, "delete:"+id)
			return nil
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			order = append(order, "save")
			return nil
		},
		OnIndexed: func(int, int) {
			order = append(order, "indexed")
		},
		AfterSuccess: func() {
			order = append(order, "after")
		},
	})

	assert.Equal(t, []string{"process", "finalize", "flush", "delete:chunk-stale", "save", "indexed", "after"}, order)
}

func TestRunFull_WalkErrorStopsBeforeProcessing(t *testing.T) {
	stubRunBoundary(t)

	var handled error

	RunFull(&FullParams{
		Boundary: Boundary{Path: t.TempDir()},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return nil, errors.New("walk boom")
		},
		OnWalkError: func(err error) {
			handled = err
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			t.Fatal("process should not run after walk failure")
			return ProcessResult{}
		},
	})

	require.EqualError(t, handled, "walk boom")
}

func TestRunFull_ProgressSaverReportsRecordAndFlushErrors(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")
	saver := &recordingProgressSaver{recordErr: errors.New("record boom"), flushErr: errors.New("flush boom")}

	var (
		progressErrs []string
		insertErr    string
	)

	RunFull(&FullParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		NewProgressSaver: func(*FileHashMap) ProgressSaver {
			return saver
		},
		ProcessFiles: func(files []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			require.Len(t, files, 1)
			require.NotNil(t, onFileIndexed)
			onFileIndexed("main.go", 4)

			return ProcessResult{Err: "insert boom"}
		},
		OnProgressSaveError: func(err error) {
			progressErrs = append(progressErrs, err.Error())
		},
		OnInsertError: func(err string) {
			insertErr = err
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			t.Fatal("manifest should not be saved after insert failure")
			return nil
		},
	})

	assert.Equal(t, FileEntry{Hash: saver.records["main.go"].Hash, ChunkCount: 4, Size: saver.records["main.go"].Size, ModTimeUnixNano: saver.records["main.go"].ModTimeUnixNano}, saver.records["main.go"])
	assert.Equal(t, []string{"record boom", "flush boom"}, progressErrs)
	assert.Equal(t, "insert boom", insertErr)
}

func TestRunIncremental_QueryErrorTriggersDeleteError(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")
	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}

	var handledErr error

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		QueryFileChunkIDs: func(string, int) ([]string, error) {
			return nil, errors.New("query boom")
		},
		DeleteFile: func(string) error {
			t.Fatal("delete file should not run after query failure")
			return nil
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			t.Fatal("process should not run after query failure")
			return ProcessResult{}
		},
		OnDeleteError: func(err error) {
			handledErr = err
		},
	})

	require.EqualError(t, handledErr, "query boom")
}

func TestRunIncremental_DeleteFileErrorTriggersDeleteError(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	oldHashes := NewFileHashMap()
	oldHashes.Files["gone.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}

	var handledErr error

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return nil, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		DeleteFile: func(relPath string) error {
			assert.Equal(t, "gone.go", relPath)

			return errors.New("delete file boom")
		},
		ProcessFiles: func([]walker.CodeFile, func(string, int)) ProcessResult {
			t.Fatal("process should not run after delete failure")
			return ProcessResult{}
		},
		OnDeleteError: func(err error) {
			handledErr = err
		},
	})

	require.EqualError(t, handledErr, "delete file boom")
}

func TestRunIncremental_DeleteChunkErrorStopsBeforeSave(t *testing.T) {
	stubRunBoundary(t)

	dir := t.TempDir()
	file := writeRunCodeFile(t, dir, "main.go")
	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}
	saver := &recordingProgressSaver{}

	var handledErr error

	RunIncremental(&IncrementalParams{
		Boundary: Boundary{Path: dir},
		WalkFiles: func() ([]walker.CodeFile, error) {
			return []walker.CodeFile{file}, nil
		},
		LoadOldHashes: func() (*FileHashMap, error) {
			return oldHashes, nil
		},
		QueryFileChunkIDs: func(string, int) ([]string, error) {
			return []string{"chunk-keep", "chunk-stale"}, nil
		},
		DeleteFile: func(string) error { return nil },
		NewProgressSaver: func(*FileHashMap) ProgressSaver {
			return saver
		},
		ProcessFiles: func(_ []walker.CodeFile, onFileIndexed func(string, int)) ProcessResult {
			require.NotNil(t, onFileIndexed)
			onFileIndexed("main.go", 1)

			return ProcessResult{
				TotalChunks:  1,
				ChunkCounts:  map[string]int{"main.go": 1},
				FileChunkIDs: map[string][]string{"main.go": {"", "chunk-keep"}},
			}
		},
		DeleteChunkID: func(string) error {
			return errors.New("delete boom")
		},
		OnDeleteError: func(err error) {
			handledErr = err
		},
		SaveManifest: func(*FileHashMap, map[string]int) error {
			t.Fatal("manifest should not be saved after delete failure")
			return nil
		},
	})

	require.EqualError(t, handledErr, "delete boom")
	require.Contains(t, saver.records, "main.go")
}

func TestRunHelpers_HandleEdgeCases(t *testing.T) {
	t.Run("captureModifiedFileChunkIDs skips zero limits and empty results", func(t *testing.T) {
		diff := &ManifestDiff{Changes: []FileChange{{RelPath: "zero.go", Type: Modified}, {RelPath: "empty.go", Type: Modified}}}
		oldHashes := &FileHashMap{Files: map[string]FileEntry{
			"zero.go":  {ChunkCount: 0},
			"empty.go": {ChunkCount: 2},
		}}

		plans, err := captureModifiedFileChunkIDs(diff, oldHashes, func(relPath string, limit int) ([]string, error) {
			assert.Equal(t, "empty.go", relPath)
			assert.Equal(t, 2, limit)

			return []string{"", ""}, nil
		})
		require.NoError(t, err)
		assert.Empty(t, plans)
	})

	t.Run("deleteDeletedFiles ignores nil inputs", func(t *testing.T) {
		assert.NoError(t, deleteDeletedFiles(nil, nil))
	})

	t.Run("deleteModifiedFileChunks returns delete error", func(t *testing.T) {
		err := deleteModifiedFileChunks([]modifiedFileChunkIDs{{relPath: "main.go", ids: []string{"chunk-keep", "chunk-old"}}}, map[string][]string{"main.go": {"", "chunk-keep"}}, nil, func(id string) error {
			assert.Equal(t, "chunk-old", id)
			return errors.New("delete boom")
		})
		require.EqualError(t, err, "delete boom")
	})

	t.Run("deleteModifiedFileChunks batches stale ids", func(t *testing.T) {
		staleIDs := make([]string, 101)
		ids := make([]string, 0, len(staleIDs)+1)
		ids = append(ids, "chunk-keep")

		for i := range staleIDs {
			staleIDs[i] = fmt.Sprintf("chunk-stale-%03d", i+1)
			ids = append(ids, staleIDs[i])
		}

		var batches [][]string

		err := deleteModifiedFileChunks([]modifiedFileChunkIDs{{relPath: "main.go", ids: ids}}, map[string][]string{"main.go": {"chunk-keep"}}, func(batch []string) error {
			copied := append([]string(nil), batch...)
			batches = append(batches, copied)

			return nil
		}, nil)
		require.NoError(t, err)
		require.Len(t, batches, 2)
		assert.Equal(t, staleIDs[:100], batches[0])
		assert.Equal(t, staleIDs[100:], batches[1])
	})

	t.Run("dedupeIDs handles empty blank and duplicate values", func(t *testing.T) {
		assert.Nil(t, dedupeIDs(nil))
		assert.Equal(t, []string{"chunk-a"}, dedupeIDs([]string{"", "chunk-a", "chunk-a"}))
	})

	t.Run("progress helpers handle nil and missing entries", func(t *testing.T) {
		require.NoError(t, callSaveManifest(nil, nil, nil))

		manifest := NewFileHashMap()
		saver := &recordingProgressSaver{}
		record := progressRecorder(saver, manifest, func(error) {
			t.Fatal("missing manifest entry should not record")
		})
		require.NotNil(t, record)
		record("missing.go", 1)
		assert.Empty(t, saver.records)
	})
}
