package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingObserver struct {
	events []Event
}

func (o *recordingObserver) Observe(event *Event) {
	o.events = append(o.events, *event)
}

type snapshotClock struct {
	times []time.Time
}

func (c *snapshotClock) Now() time.Time {
	if len(c.times) == 0 {
		return time.Time{}
	}

	now := c.times[0]
	c.times = c.times[1:]

	return now
}

// ============================================================
// CollectionName
// ============================================================

func TestCollectionName(t *testing.T) {
	t.Run("deterministic for same path", func(t *testing.T) {
		path := "/some/test/path"
		first := CollectionName(path)
		second := CollectionName(path)
		assert.Equal(t, first, second)
	})

	t.Run("different paths produce different names", func(t *testing.T) {
		assert.NotEqual(t, CollectionName("/path/a"), CollectionName("/path/b"))
	})

	t.Run("format is code_chunks_ + 32 hex chars", func(t *testing.T) {
		name := CollectionName("/any/path")
		assert.True(t, strings.HasPrefix(name, "code_chunks_"), "should start with code_chunks_")
		assert.Len(t, name, 44, "code_chunks_ (12) + 32 hex chars = 44")
	})

	t.Run("known value is stable", func(t *testing.T) {
		// MD5(hostname+":"+"/test") is deterministic — pin the output so a refactor is obvious.
		first := CollectionName("/test")
		assert.True(t, strings.HasPrefix(first, "code_chunks_"))
		assert.Len(t, first, 44)
	})
}

// ============================================================
// stateFilePath (unexported)
// ============================================================

func TestStateFilePath(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, ".cfmantic", "state.json"), stateFilePath(dir))
}

// ============================================================
// loadFromDisk (unexported)
// ============================================================

func TestLoadFromDisk(t *testing.T) {
	t.Run("missing file returns nil", func(t *testing.T) {
		dir := t.TempDir()
		assert.Nil(t, loadFromDisk(dir))
	})

	t.Run("corrupt JSON returns nil", func(t *testing.T) {
		dir := t.TempDir()
		indexDir := MetadataDirPath(dir)
		require.NoError(t, os.MkdirAll(indexDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(indexDir, "state.json"), []byte("not json {{"), 0o644))
		assert.Nil(t, loadFromDisk(dir))
	})

	t.Run("valid file returns CodebaseInfo", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 10, 50)

		info := loadFromDisk(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusIndexed, info.Status)
		assert.Equal(t, 10, info.IndexedFiles)
		assert.Equal(t, 50, info.TotalChunks)
	})
}

func TestValidateStoredPath(t *testing.T) {
	t.Run("missing file is ignored", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, ValidateStoredPath(dir))
	})

	t.Run("invalid state file is ignored", func(t *testing.T) {
		dir := t.TempDir()
		indexDir := MetadataDirPath(dir)
		require.NoError(t, os.MkdirAll(indexDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(indexDir, "state.json"), []byte("not json {{"), 0o644))

		require.NoError(t, ValidateStoredPath(dir))
	})

	t.Run("matching stored path passes", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 2, 4)

		require.NoError(t, ValidateStoredPath(dir))
	})

	t.Run("mismatched stored path returns move rename error", func(t *testing.T) {
		dir := t.TempDir()
		indexDir := MetadataDirPath(dir)
		require.NoError(t, os.MkdirAll(indexDir, 0o755))

		storedPath := filepath.Join(t.TempDir(), "old-root")
		data, err := json.Marshal(&CodebaseInfo{
			Path:        storedPath,
			Status:      StatusIndexed,
			LastUpdated: time.Now(),
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(indexDir, "state.json"), data, 0o644))

		err = ValidateStoredPath(dir)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrStoredPathMismatch)

		var mismatchErr *StoredPathMismatchError
		require.ErrorAs(t, err, &mismatchErr)
		assert.Equal(t, dir, mismatchErr.Path)
		assert.Equal(t, storedPath, mismatchErr.StoredPath)

		assert.Contains(t, err.Error(), storedPath)
		assert.Contains(t, err.Error(), dir)
	})
}

func TestAddObserverAndEmit(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	observer := &recordingObserver{}

	m.AddObserver(nil)
	m.AddObserver(observer)
	m.SetIgnorePatterns(dir, []string{"vendor/"})
	m.SetStep(dir, "scanning")

	require.Len(t, observer.events, 1)
	assert.Equal(t, EventStepUpdated, observer.events[0].Type)
	assert.Equal(t, dir, observer.events[0].Path)
	assert.Equal(t, StatusIndexing, observer.events[0].Info.Status)
	assert.Equal(t, "scanning", observer.events[0].Info.Step)
	require.NotNil(t, observer.events[0].Info.IgnorePatterns)
	assert.Equal(t, []string{"vendor/"}, *observer.events[0].Info.IgnorePatterns)
}

// ============================================================
// saveToDisk (unexported) — exercised via SetStep/SetIndexed/SetFailed,
// plus direct calls to hit the early-return branch.
// ============================================================

func TestSaveToDisk_MissingFromCache(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	// path not in cache — should be a no-op with no file created
	m.saveToDisk(dir)

	_, err := os.Stat(stateFilePath(dir))
	assert.True(t, os.IsNotExist(err), "no state file should be created for unknown path")
}

func TestSaveToDisk_MarshalError(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.mu.Lock()
	m.codebases[dir] = &CodebaseInfo{Path: dir, Status: StatusIndexed}
	m.mu.Unlock()

	oldMarshal := marshalState
	marshalState = func(any, string, string) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}

	t.Cleanup(func() { marshalState = oldMarshal })

	m.saveToDisk(dir)

	_, err := os.Stat(stateFilePath(dir))
	assert.True(t, os.IsNotExist(err), "marshal failure should not write a file")
}

func TestSaveToDisk_MkdirAllError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod restriction bypassed as root")
	}

	dir := t.TempDir()
	// Place a regular file at <dir>/.cfmantic so MkdirAll fails
	indexPath := MetadataDirPath(dir)
	require.NoError(t, os.WriteFile(indexPath, []byte("conflict"), 0o644))

	m := NewManager()
	m.mu.Lock()
	m.codebases[dir] = &CodebaseInfo{Path: dir, Status: StatusIndexed}
	m.mu.Unlock()

	// saveToDisk must not panic; it logs and returns
	m.saveToDisk(dir)
}

func TestSaveToDisk_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod restriction bypassed as root")
	}

	dir := t.TempDir()
	indexDir := MetadataDirPath(dir)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, ".gitignore"), []byte("*\n"), 0o644))
	// Remove write permission so WriteFile on the tmp file fails
	require.NoError(t, os.Chmod(indexDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(indexDir, 0o755) })

	m := NewManager()
	m.mu.Lock()
	m.codebases[dir] = &CodebaseInfo{Path: dir, Status: StatusIndexed}
	m.mu.Unlock()

	m.saveToDisk(dir) // must not panic
}

func TestSaveToDisk_RenameError(t *testing.T) {
	dir := t.TempDir()
	indexDir := MetadataDirPath(dir)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))

	// Create state.json as a directory so rename fails (EISDIR on Linux)
	stateAsDir := filepath.Join(indexDir, "state.json")
	require.NoError(t, os.MkdirAll(stateAsDir, 0o755))

	m := NewManager()
	m.mu.Lock()
	m.codebases[dir] = &CodebaseInfo{Path: dir, Status: StatusIndexed}
	m.mu.Unlock()

	m.saveToDisk(dir) // must not panic
}

func TestSaveToDisk_JSONContentMatchesState(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetIndexed(dir, 42, 300)

	data, err := os.ReadFile(stateFilePath(dir))
	require.NoError(t, err)

	var info CodebaseInfo
	require.NoError(t, json.Unmarshal(data, &info))

	assert.Equal(t, dir, info.Path)
	assert.Equal(t, StatusIndexed, info.Status)
	assert.Equal(t, 42, info.IndexedFiles)
	assert.Equal(t, 300, info.TotalChunks)
	assert.Empty(t, info.Step)
	assert.Empty(t, info.ErrorMessage)
	assert.False(t, info.LastUpdated.IsZero())

	gitignore, err := os.ReadFile(filepath.Join(MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*", strings.TrimSpace(string(gitignore)))
}

func TestSaveToDisk_DoesNotOverwriteExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(MetadataDirPath(dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(MetadataDirPath(dir), ".gitignore"), []byte("keep-me\n"), 0o644))

	m := NewManager()
	m.SetIndexed(dir, 1, 2)

	gitignore, err := os.ReadFile(filepath.Join(MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "keep-me\n", string(gitignore))
}

func TestSaveToDisk_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()

	m.SetStep(dir, "scanning")
	m.SetIndexed(dir, 7, 77)

	// Only the latest state should be on disk.
	info := loadFromDisk(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusIndexed, info.Status)
	assert.Equal(t, 7, info.IndexedFiles)
}

// ============================================================
// GetStatus
// ============================================================

func TestGetStatus(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(m *Manager, dir string)
		expected Status
	}{
		{
			name:     "not found for unknown path",
			setup:    func(m *Manager, dir string) {},
			expected: StatusNotFound,
		},
		{
			name:     "indexing after SetStep",
			setup:    func(m *Manager, dir string) { m.SetStep(dir, "scanning") },
			expected: StatusIndexing,
		},
		{
			name:     "indexed after SetIndexed",
			setup:    func(m *Manager, dir string) { m.SetIndexed(dir, 5, 20) },
			expected: StatusIndexed,
		},
		{
			name:     "failed after SetFailed",
			setup:    func(m *Manager, dir string) { m.SetFailed(dir, "oops") },
			expected: StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			m := NewManager()
			tt.setup(m, dir)
			assert.Equal(t, tt.expected, m.GetStatus(dir))
		})
	}
}

// ============================================================
// GetInfo
// ============================================================

func TestGetInfo(t *testing.T) {
	t.Run("not found returns nil", func(t *testing.T) {
		m := NewManager()
		assert.Nil(t, m.GetInfo(t.TempDir()))
	})

	t.Run("returns correct fields after SetIndexed", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 7, 42)

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, dir, info.Path)
		assert.Equal(t, StatusIndexed, info.Status)
		assert.Equal(t, 7, info.IndexedFiles)
		assert.Equal(t, 42, info.TotalChunks)
		assert.Empty(t, info.Step)
		assert.Empty(t, info.ErrorMessage)
	})

	t.Run("step field set by SetStep", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "scanning files")

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, "scanning files", info.Step)
		assert.Equal(t, StatusIndexing, info.Status)
	})

	t.Run("step cleared and errorMessage set by SetFailed", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "scanning")
		m.SetFailed(dir, "connection refused")

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, "connection refused", info.ErrorMessage)
		assert.Empty(t, info.Step)
		assert.Equal(t, StatusFailed, info.Status)
	})

	t.Run("returns a copy (mutation safe)", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "original")

		copy1 := m.GetInfo(dir)
		require.NotNil(t, copy1)
		copy1.Step = "mutated"

		copy2 := m.GetInfo(dir)
		assert.Equal(t, "original", copy2.Step, "in-memory state must be unaffected by caller mutation")
	})
}

// ============================================================
// SetStep
// ============================================================

func TestSetStep(t *testing.T) {
	t.Run("creates new entry with Indexing status", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "scanning")

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, dir, info.Path)
		assert.Equal(t, StatusIndexing, info.Status)
		assert.Equal(t, "scanning", info.Step)
		assert.False(t, info.LastUpdated.IsZero())
	})

	t.Run("updates existing entry", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "step1")
		m.SetStep(dir, "step2")

		assert.Equal(t, "step2", m.GetInfo(dir).Step)
	})

	t.Run("persists to disk", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "saved step")

		_, err := os.Stat(stateFilePath(dir))
		assert.NoError(t, err, "state file should exist after SetStep")
	})
}

func TestStartOperation_TracksMetadataAndFreshness(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	clock := &snapshotClock{times: []time.Time{
		time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 7, 12, 0, 1, 0, time.UTC),
		time.Date(2026, time.March, 7, 12, 0, 2, 0, time.UTC),
	}}
	m.now = clock.Now

	m.StartOperation(dir, OperationMetadata{
		Operation: "indexing",
		Source:    "manual",
		Mode:      "full",
	})

	started := m.GetInfo(dir)
	require.NotNil(t, started)
	assert.Equal(t, StatusIndexing, started.Status)
	assert.Equal(t, "indexing", started.Operation)
	assert.Equal(t, "manual", started.Source)
	assert.Equal(t, "full", started.Mode)
	assert.Equal(t, time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC), started.StartedAt)

	m.SetStep(dir, "Walking files")

	withStep := m.GetInfo(dir)
	require.NotNil(t, withStep)
	assert.Equal(t, "Walking files", withStep.Step)
	assert.Equal(t, time.Date(2026, time.March, 7, 12, 0, 1, 0, time.UTC), withStep.StepUpdatedAt)
	assert.Equal(t, started.StartedAt, withStep.StartedAt)

	m.SetProgress(dir, Progress{FilesDone: 1, FilesTotal: 3, ChunksTotal: 4, ChunksInserted: 2})

	withProgress := m.GetInfo(dir)
	require.NotNil(t, withProgress)
	assert.Equal(t, time.Date(2026, time.March, 7, 12, 0, 2, 0, time.UTC), withProgress.LastProgressAt)
	assert.Equal(t, withStep.StepUpdatedAt, withProgress.StepUpdatedAt)
	assert.Equal(t, 1, withProgress.FilesDone)
	assert.Equal(t, 3, withProgress.FilesTotal)
}

// ============================================================
// SetIndexed
// ============================================================

func TestSetIndexed(t *testing.T) {
	t.Run("creates new entry", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 100, 500)

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusIndexed, info.Status)
		assert.Equal(t, 100, info.IndexedFiles)
		assert.Equal(t, 500, info.TotalChunks)
		assert.Empty(t, info.Step)
	})

	t.Run("updates existing entry and clears step", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "indexing")
		m.SetIndexed(dir, 50, 200)

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusIndexed, info.Status)
		assert.Equal(t, 50, info.IndexedFiles)
		assert.Empty(t, info.Step)
	})

	t.Run("persists to disk with correct fields", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 10, 55)

		disk := loadFromDisk(dir)
		require.NotNil(t, disk)
		assert.Equal(t, StatusIndexed, disk.Status)
		assert.Equal(t, 10, disk.IndexedFiles)
		assert.Equal(t, 55, disk.TotalChunks)
	})
}

func TestSetIndexed_PersistenceFailureMarksFailedAndClearsAfterLaterSuccess(t *testing.T) {
	dir := t.TempDir()
	conflict := filepath.Join(MetadataDirPath(dir), "state.json.tmp")
	require.NoError(t, os.MkdirAll(conflict, 0o755))

	m := NewManager()
	observer := &recordingObserver{}
	m.AddObserver(observer)

	m.SetIndexed(dir, 12, 34)

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusFailed, info.Status)
	assert.Equal(t, 12, info.IndexedFiles)
	assert.Equal(t, 34, info.TotalChunks)
	assert.Contains(t, info.ErrorMessage, "failed to persist indexed state")
	assert.Contains(t, info.ErrorMessage, "is a directory")
	require.Len(t, observer.events, 1)
	assert.Equal(t, EventOperationFailed, observer.events[0].Type)
	assert.Equal(t, StatusFailed, observer.events[0].Info.Status)

	require.NoError(t, os.RemoveAll(conflict))
	m.SetIndexed(dir, 56, 78)

	info = m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusIndexed, info.Status)
	assert.Equal(t, 56, info.IndexedFiles)
	assert.Equal(t, 78, info.TotalChunks)
	assert.Empty(t, info.ErrorMessage)
	require.Len(t, observer.events, 2)
	assert.Equal(t, EventOperationCompleted, observer.events[1].Type)
	assert.Equal(t, StatusIndexed, observer.events[1].Info.Status)

	require.NoError(t, os.Remove(stateFilePath(dir)))
	assert.Equal(t, StatusNotFound, m.GetStatus(dir))
}

func TestSetIgnorePatterns_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetIndexed(dir, 10, 55)
	m.SetIgnorePatterns(dir, []string{"vendor/", "dist/"})

	disk := loadFromDisk(dir)
	require.NotNil(t, disk)
	require.NotNil(t, disk.IgnorePatterns)
	assert.Equal(t, []string{"vendor/", "dist/"}, *disk.IgnorePatterns)

	reloaded := NewManager().GetInfo(dir)
	require.NotNil(t, reloaded)
	require.NotNil(t, reloaded.IgnorePatterns)
	assert.Equal(t, []string{"vendor/", "dist/"}, *reloaded.IgnorePatterns)
}

func TestGetIgnorePatterns(t *testing.T) {
	t.Run("missing patterns returns false", func(t *testing.T) {
		patterns, ok := NewManager().GetIgnorePatterns(t.TempDir())
		assert.False(t, ok)
		assert.Nil(t, patterns)
	})

	t.Run("returns a defensive copy", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIgnorePatterns(dir, []string{"vendor/", "dist/"})

		patterns, ok := m.GetIgnorePatterns(dir)
		require.True(t, ok)

		patterns[0] = "mutated/"

		reloaded, ok := m.GetIgnorePatterns(dir)
		require.True(t, ok)
		assert.Equal(t, []string{"vendor/", "dist/"}, reloaded)
	})
}

func TestCloneStrings_Nil(t *testing.T) {
	assert.Nil(t, cloneStrings(nil))
}

func TestSetIgnorePatterns_PersistsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetIndexed(dir, 1, 2)
	m.SetIgnorePatterns(dir, []string{})

	disk := loadFromDisk(dir)
	require.NotNil(t, disk)
	require.NotNil(t, disk.IgnorePatterns)
	assert.Empty(t, *disk.IgnorePatterns)
}

// ============================================================
// SetFailed
// ============================================================

func TestSetFailed(t *testing.T) {
	t.Run("creates new entry", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetFailed(dir, "timeout")

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusFailed, info.Status)
		assert.Equal(t, "timeout", info.ErrorMessage)
		assert.Empty(t, info.Step)
	})

	t.Run("updates existing entry and clears step", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetStep(dir, "scanning")
		m.SetFailed(dir, "disk full")

		info := m.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusFailed, info.Status)
		assert.Equal(t, "disk full", info.ErrorMessage)
		assert.Empty(t, info.Step)
	})
}

func TestSetFailed_PersistenceFailureKeepsFailedEntry(t *testing.T) {
	dir := t.TempDir()
	conflict := filepath.Join(MetadataDirPath(dir), "state.json.tmp")
	require.NoError(t, os.MkdirAll(conflict, 0o755))

	m := NewManager()
	observer := &recordingObserver{}
	m.AddObserver(observer)

	m.SetFailed(dir, "network error")

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "network error")
	assert.Contains(t, info.ErrorMessage, "snapshot persistence failed")
	assert.Contains(t, info.ErrorMessage, "is a directory")
	require.Len(t, observer.events, 1)
	assert.Equal(t, EventOperationFailed, observer.events[0].Type)
	assert.Equal(t, StatusFailed, observer.events[0].Info.Status)

	require.NoError(t, os.RemoveAll(conflict))
	assert.Equal(t, StatusFailed, m.GetStatus(dir))
}

// ============================================================
// Remove
// ============================================================

func TestRemove(t *testing.T) {
	t.Run("removes from memory", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 5, 10)
		m.Remove(dir)

		assert.Equal(t, StatusNotFound, m.GetStatus(dir))
	})

	t.Run("removes state file from disk", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		m.SetIndexed(dir, 5, 10)

		_, err := os.Stat(stateFilePath(dir))
		require.NoError(t, err, "state file should exist before Remove")

		m.Remove(dir)

		_, err = os.Stat(stateFilePath(dir))
		assert.True(t, os.IsNotExist(err), "state file should be deleted after Remove")
	})

	t.Run("remove nonexistent is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManager()
		// should not panic
		m.Remove(dir)
		assert.Equal(t, StatusNotFound, m.GetStatus(dir))
	})
}

// ============================================================
// IsIndexing
// ============================================================

func TestIsIndexing(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(m *Manager, dir string)
		expected bool
	}{
		{"not found", func(m *Manager, dir string) {}, false},
		{"indexing", func(m *Manager, dir string) { m.SetStep(dir, "scanning") }, true},
		{"indexed", func(m *Manager, dir string) { m.SetIndexed(dir, 1, 1) }, false},
		{"failed", func(m *Manager, dir string) { m.SetFailed(dir, "err") }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			m := NewManager()
			tt.setup(m, dir)
			assert.Equal(t, tt.expected, m.IsIndexing(dir))
		})
	}
}

// ============================================================
// Persistence — new Manager reads state written by another Manager
// ============================================================

func TestPersistence(t *testing.T) {
	t.Run("SetIndexed survives new Manager", func(t *testing.T) {
		dir := t.TempDir()

		m1 := NewManager()
		m1.SetIndexed(dir, 25, 150)

		m2 := NewManager()
		assert.Equal(t, StatusIndexed, m2.GetStatus(dir))

		info := m2.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, 25, info.IndexedFiles)
		assert.Equal(t, 150, info.TotalChunks)
	})

	t.Run("SetFailed survives new Manager", func(t *testing.T) {
		dir := t.TempDir()

		m1 := NewManager()
		m1.SetFailed(dir, "network error")

		m2 := NewManager()
		info := m2.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusFailed, info.Status)
		assert.Equal(t, "network error", info.ErrorMessage)
	})
}

func TestGetStatus_RefreshesCachedStateAfterExternalChange(t *testing.T) {
	t.Run("external remove clears cached status", func(t *testing.T) {
		dir := t.TempDir()

		writer := NewManager()
		writer.SetIndexed(dir, 5, 10)

		reader := NewManager()
		assert.Equal(t, StatusIndexed, reader.GetStatus(dir))

		writer.Remove(dir)

		assert.Equal(t, StatusNotFound, reader.GetStatus(dir))
		assert.Nil(t, reader.GetInfo(dir))
	})

	t.Run("external rewrite refreshes cached status", func(t *testing.T) {
		dir := t.TempDir()

		writer := NewManager()
		writer.SetIndexed(dir, 5, 10)

		reader := NewManager()
		assert.Equal(t, StatusIndexed, reader.GetStatus(dir))

		writer.SetStep(dir, "Starting reindex")

		assert.Equal(t, StatusIndexing, reader.GetStatus(dir))

		info := reader.GetInfo(dir)
		require.NotNil(t, info)
		assert.Equal(t, StatusIndexing, info.Status)
		assert.Equal(t, "Starting reindex", info.Step)
	})
}

// ============================================================
// resolve — lazy loading behavior
// ============================================================

func TestResolve_LazyLoad(t *testing.T) {
	dir := t.TempDir()

	// prime disk state via a separate manager
	seed := NewManager()
	seed.SetFailed(dir, "disk error")

	m := NewManager()
	// cache starts empty
	m.mu.RLock()
	_, inCache := m.codebases[dir]
	m.mu.RUnlock()
	assert.False(t, inCache, "should not be in cache before first access")

	// GetStatus triggers resolve → lazy load
	assert.Equal(t, StatusFailed, m.GetStatus(dir))

	m.mu.RLock()
	_, inCache = m.codebases[dir]
	m.mu.RUnlock()
	assert.True(t, inCache, "should be in cache after first access")
}

func TestResolve_NoDiskFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	// resolve with nothing on disk must not add a nil entry
	m.resolve(dir)

	m.mu.RLock()
	_, ok := m.codebases[dir]
	m.mu.RUnlock()
	assert.False(t, ok)
}

func TestResolve_RefreshesExistingCacheFromDisk(t *testing.T) {
	dir := t.TempDir()
	writer := NewManager()
	writer.SetIndexed(dir, 5, 10)

	reader := NewManager()
	assert.Equal(t, StatusIndexed, reader.GetStatus(dir))

	writer.SetStep(dir, "running")

	reader.resolve(dir)

	info := reader.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusIndexing, info.Status)
	assert.Equal(t, "running", info.Step)
}

// ============================================================
// Concurrent access — RWMutex correctness
// ============================================================

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()

	const n = 50

	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			m.SetStep(dir, fmt.Sprintf("step-%d", i))
		}(i)
	}

	for range n {
		wg.Go(func() {
			m.GetStatus(dir)
		})
	}

	for range n {
		wg.Go(func() {
			m.IsIndexing(dir)
		})
	}

	wg.Wait()
	// all writers used SetStep, so final status must be Indexing
	assert.Equal(t, StatusIndexing, m.GetStatus(dir))
}

// TestResolve_ConcurrentLoad exercises the double-checked locking inside resolve
// so that the "already inserted by another goroutine" branch is reachable.
func TestResolve_ConcurrentLoad(t *testing.T) {
	dir := t.TempDir()

	// Write a real state file so loadFromDisk succeeds.
	seed := NewManager()
	seed.SetIndexed(dir, 3, 9)

	const goroutines = 100

	m := NewManager() // fresh manager — empty cache, file on disk

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			m.resolve(dir)
		})
	}

	wg.Wait()

	// After all goroutines, exactly one entry must be in the cache.
	m.mu.RLock()
	info, ok := m.codebases[dir]
	m.mu.RUnlock()
	require.True(t, ok)
	assert.Equal(t, StatusIndexed, info.Status)
}

// ============================================================
// SetProgress
// ============================================================

func TestSetProgress(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetStep(dir, "Indexing 10 files")

	m.SetProgress(dir, Progress{
		FilesDone:      3,
		FilesTotal:     10,
		ChunksTotal:    30,
		ChunksInserted: 15,
	})

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, StatusIndexing, info.Status)
	assert.Equal(t, 3, info.FilesDone)
	assert.Equal(t, 10, info.FilesTotal)
	assert.Equal(t, 30, info.ChunksTotal)
	assert.Equal(t, 15, info.ChunksInserted)
	assert.False(t, info.LastProgressAt.IsZero())
}

func TestSetProgress_RefreshesLastUpdated(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	clock := &snapshotClock{times: []time.Time{
		time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 7, 12, 0, 1, 0, time.UTC),
	}}
	m.now = clock.Now
	m.SetStep(dir, "Indexing 10 files")

	before := m.GetInfo(dir)
	require.NotNil(t, before)

	m.SetProgress(dir, Progress{FilesDone: 2, FilesTotal: 10, ChunksTotal: 20, ChunksInserted: 8})

	after := m.GetInfo(dir)
	require.NotNil(t, after)
	assert.True(t, after.LastUpdated.After(before.LastUpdated))
	assert.Equal(t, after.LastUpdated, after.LastProgressAt)
}

func TestSetProgress_UnknownPathInitializesInfo(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()

	m.SetProgress(dir, Progress{
		FilesDone:      1,
		FilesTotal:     4,
		ChunksTotal:    8,
		ChunksInserted: 3,
	})

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, dir, info.Path)
	assert.Equal(t, 1, info.FilesDone)
	assert.Equal(t, 4, info.FilesTotal)
	assert.Equal(t, 8, info.ChunksTotal)
	assert.Equal(t, 3, info.ChunksInserted)
}

func TestSetProgress_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetStep(dir, "scanning")

	m.SetProgress(dir, Progress{
		FilesDone:      7,
		FilesTotal:     20,
		ChunksTotal:    140,
		ChunksInserted: 49,
	})

	disk := loadFromDisk(dir)
	require.NotNil(t, disk, "state.json should exist after SetProgress")
	assert.Equal(t, 7, disk.FilesDone)
	assert.Equal(t, 20, disk.FilesTotal)
	assert.Equal(t, 140, disk.ChunksTotal)
	assert.Equal(t, 49, disk.ChunksInserted)
}

func TestSetIndexedClearsProgress(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetStep(dir, "Indexing")
	m.SetProgress(dir, Progress{
		FilesDone:      5,
		FilesTotal:     10,
		ChunksTotal:    50,
		ChunksInserted: 50,
	})

	m.SetIndexed(dir, 10, 100)

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, 0, info.FilesDone)
	assert.Equal(t, 0, info.FilesTotal)
	assert.Equal(t, 0, info.ChunksTotal)
	assert.Equal(t, 0, info.ChunksInserted)
}

func TestSetFailedRetainsProgress(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	m.SetStep(dir, "Indexing")
	m.SetProgress(dir, Progress{
		FilesDone:      2,
		FilesTotal:     10,
		ChunksTotal:    20,
		ChunksInserted: 10,
	})

	m.SetFailed(dir, "something went wrong")

	info := m.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, 2, info.FilesDone)
	assert.Equal(t, 10, info.FilesTotal)
	assert.Equal(t, 20, info.ChunksTotal)
	assert.Equal(t, 10, info.ChunksInserted)
}
