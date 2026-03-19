package filesync

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/mocks"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"encoding/hex"
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
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"
)

type recordingStatusManager struct {
	*snapshot.Manager

	mu              sync.Mutex
	operations      []snapshot.OperationMetadata
	steps           []string
	progress        []snapshot.Progress
	getStatusCalls  int
	getStatusReady  chan struct{}
	setIndexedReady chan struct{}
}

type manualTicker struct {
	ch chan time.Time
}

func (t *manualTicker) C() <-chan time.Time {
	return t.ch
}

func (t *manualTicker) Stop() {}

func newRecordingStatusManager() *recordingStatusManager {
	return &recordingStatusManager{Manager: snapshot.NewManager()}
}

func (r *recordingStatusManager) StartOperation(path string, meta snapshot.OperationMetadata) {
	r.Manager.StartOperation(path, meta)
	r.mu.Lock()
	r.operations = append(r.operations, meta)
	r.mu.Unlock()
}

func (r *recordingStatusManager) GetStatus(path string) snapshot.Status {
	status := r.Manager.GetStatus(path)

	r.mu.Lock()

	r.getStatusCalls++
	if r.getStatusCalls == 2 && r.getStatusReady != nil {
		close(r.getStatusReady)
	}
	r.mu.Unlock()

	return status
}

func (r *recordingStatusManager) SetStep(path, step string) {
	r.Manager.SetStep(path, step)
	r.mu.Lock()
	r.steps = append(r.steps, step)
	r.mu.Unlock()
}

func (r *recordingStatusManager) SetProgress(path string, progress snapshot.Progress) {
	r.Manager.SetProgress(path, progress)
	r.mu.Lock()
	r.progress = append(r.progress, progress)
	r.mu.Unlock()
}

func (r *recordingStatusManager) SetIndexed(path string, files, chunks int) {
	r.Manager.SetIndexed(path, files, chunks)
	r.mu.Lock()
	if r.setIndexedReady != nil {
		select {
		case r.setIndexedReady <- struct{}{}:
		default:
		}
	}
	r.mu.Unlock()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// testConfig sets the minimum required env vars and returns a loaded config.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("WORKER_URL", "http://test-worker.example.com")
	t.Setenv("AUTH_TOKEN", "test-token")

	cfg, err := config.Load()
	require.NoError(t, err)

	return cfg
}

// writeLockFile writes a live lock file owned by the current PID so that
// AcquireLock returns an error (codebase locked by a live process).
func writeLockFile(t *testing.T, codebasePath string) {
	t.Helper()

	indexDir := snapshot.MetadataDirPath(codebasePath)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))

	type lockInfo struct {
		PID       int       `json:"pid"`
		StartedAt time.Time `json:"startedAt"`
	}

	info := lockInfo{PID: os.Getpid(), StartedAt: time.Now()}
	data, err := json.Marshal(info)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, ".lock"), data, 0o644))
}

// oneChunk returns a single trivial Chunk for splitter mock return values.
func oneChunk() []splitter.Chunk {
	return []splitter.Chunk{{Content: "chunk content", StartLine: 1, EndLine: 1}}
}

func expectSplitChunks(tb testing.TB, sp *mocks.MockSplitter, filePath any, chunks []splitter.Chunk) {
	tb.Helper()

	sp.On("Split", testifymock.Anything, filePath, testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(tb, ok)

			for _, chunk := range chunks {
				require.NoError(tb, emit(chunk))
			}
		}).
		Return(nil)
}

func expectSplitOneChunk(tb testing.TB, sp *mocks.MockSplitter, filePath any) {
	tb.Helper()

	expectSplitChunks(tb, sp, filePath, oneChunk())
}

func expectSplitNoChunks(sp *mocks.MockSplitter, filePath any) {
	sp.On("Split", testifymock.Anything, filePath, testifymock.Anything).Return(nil)
}

// hexBLAKE3 returns the hex-encoded BLAKE3 hash of data, mirroring
// the algorithm used by ComputeFileHashMap.
func hexBLAKE3(data []byte) string {
	sum := blake3.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func relPaths(files []walker.CodeFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.RelPath
	}

	return paths
}

// ─── NewManager ───────────────────────────────────────────────────────────────

func TestNewManager_DefaultInterval(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 0,
	)
	assert.Equal(t, 60*time.Second, mgr.interval)
}

func TestNewManager_NegativeInterval(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, -1,
	)
	assert.Equal(t, 60*time.Second, mgr.interval)
}

func TestNewManager_CustomInterval(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 60,
	)
	assert.Equal(t, 60*time.Second, mgr.interval)
}

// ─── TrackPath / UntrackPath ──────────────────────────────────────────────────

func TestTrackUntrack_AddAndRemovePaths(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	mgr.TrackPath("/a")
	mgr.TrackPath("/b")

	mgr.mu.RLock()
	assert.True(t, mgr.trackedPaths["/a"])
	assert.True(t, mgr.trackedPaths["/b"])
	mgr.mu.RUnlock()

	mgr.UntrackPath("/a")

	mgr.mu.RLock()
	assert.False(t, mgr.trackedPaths["/a"])
	assert.True(t, mgr.trackedPaths["/b"])
	mgr.mu.RUnlock()
}

func TestTrackUntrack_Concurrent(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	var wg sync.WaitGroup

	for i := range 50 {
		path := fmt.Sprintf("/path/%d", i)

		wg.Add(2)

		go func(p string) {
			defer wg.Done()

			mgr.TrackPath(p)
		}(path)
		go func(p string) {
			defer wg.Done()

			mgr.UntrackPath(p)
		}(path)
	}

	wg.Wait()
}

func TestTrackedParent_ReturnsNearestTrackedAncestor(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))

	mgr.TrackPath(root)
	mgr.TrackPath(parent)

	trackedParent, ok := mgr.TrackedParent(child)
	require.True(t, ok)
	assert.Equal(t, parent, trackedParent)
}

func TestTrackedParent_IgnoresExactPathAndNonAncestors(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	root := t.TempDir()
	tracked := filepath.Join(root, "tracked")
	sibling := filepath.Join(root, "sibling")
	parent := filepath.Dir(tracked)
	require.NoError(t, os.MkdirAll(tracked, 0o755))
	require.NoError(t, os.MkdirAll(sibling, 0o755))

	mgr.TrackPath(tracked)

	trackedParent, ok := mgr.TrackedParent(tracked)
	assert.False(t, ok)
	assert.Empty(t, trackedParent)

	trackedParent, ok = mgr.TrackedParent(parent)
	assert.False(t, ok)
	assert.Empty(t, trackedParent)

	trackedParent, ok = mgr.TrackedParent(sibling)
	assert.False(t, ok)
	assert.Empty(t, trackedParent)
}

// ─── Start / Stop ─────────────────────────────────────────────────────────────

func TestStart_Stop_NoDeadlock(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	mgr.Start()
	mgr.Stop()
}

func TestStop_CancelsActiveBackgroundSync(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	mgr := NewManager(mc, sm, sp, cfg, 1)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	sm.SetIndexed(dir, 1, 0)
	mgr.TrackPath(dir)

	collection := snapshot.CollectionName(dir)

	expectSplitOneChunk(t, sp, testifymock.Anything)

	insertStarted := make(chan context.Context, 1)

	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).
		Run(func(ctx context.Context, _ string, _ []milvus.Entity) {
			select {
			case insertStarted <- ctx:
			default:
			}

			<-ctx.Done()
		}).
		Return(nil, context.Canceled).
		Once()

	mgr.Start()

	var insertCtx context.Context

	select {
	case insertCtx = <-insertStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("background sync insert did not start")
	}

	stopped := make(chan struct{})

	go func() {
		mgr.Stop()
		close(stopped)
	}()

	select {
	case <-insertCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("background sync context was not canceled")
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on active background sync")
	}
}

func TestAutoTrackWorkingDirectory_TracksCanonicalizedPath(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	var logs []string

	mgr.autoTrackWorkingDirectory(
		func() (string, error) { return link, nil },
		filepath.EvalSymlinks,
		func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	)

	mgr.mu.RLock()
	assert.True(t, mgr.trackedPaths[realDir])
	assert.Len(t, mgr.trackedPaths, 1)
	mgr.mu.RUnlock()
	assert.Empty(t, logs)
}

func TestAutoTrackWorkingDirectory_GetwdFailure_LogsAndSkips(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	var logs []string

	mgr.autoTrackWorkingDirectory(
		func() (string, error) { return "", errors.New("boom") },
		func(path string) (string, error) {
			return path, nil
		},
		func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	)

	mgr.mu.RLock()
	assert.Empty(t, mgr.trackedPaths)
	mgr.mu.RUnlock()
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0], "startup auto-track skipped")
	assert.Contains(t, logs[0], "working directory")
}

func TestAutoTrackWorkingDirectory_CanonicalizeFailure_LogsAndSkips(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	var logs []string

	mgr.autoTrackWorkingDirectory(
		func() (string, error) { return "/tmp/project", nil },
		func(string) (string, error) {
			return "", errors.New("not a directory")
		},
		func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	)

	mgr.mu.RLock()
	assert.Empty(t, mgr.trackedPaths)
	mgr.mu.RUnlock()
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0], "startup auto-track skipped")
	assert.Contains(t, logs[0], "/tmp/project")
}

func TestAutoTrackWorkingDirectory_UsesProcessWorkingDirectory(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	t.Chdir(dir)

	mgr.AutoTrackWorkingDirectory(func(path string) (string, error) {
		return path, nil
	})

	mgr.mu.RLock()
	assert.True(t, mgr.trackedPaths[dir])
	mgr.mu.RUnlock()
}

// ─── syncAll ──────────────────────────────────────────────────────────────────

func TestSyncAll_IteratesTrackedPaths(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	mgr.TrackPath(dir)

	// syncCodebase will return immediately at the first status check.
	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusNotFound)

	mgr.syncAll()
}

func TestSyncAll_StopsBeforeProcessingTrackedPaths(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	mgr.TrackPath(t.TempDir())
	close(mgr.done)

	mgr.syncAll()
}

// ─── syncCodebase: early-return scenarios ─────────────────────────────────────

func TestSyncCodebase_StatusNotIndexed(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusNotFound)

	mgr.syncCodebase(dir) // must return without calling any other mock
}

func TestSyncCodebase_IsIndexing(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(true)

	mgr.syncCodebase(dir) // must return without acquiring lock
}

func TestSyncCodebase_LockFailure(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	writeLockFile(t, dir) // lock already held by the current PID → AcquireLock fails

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)

	mgr.syncCodebase(dir) // returns after logging the lock error
}

func TestSyncCodebase_StoppedBeforeActiveSyncSkipsRun(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	close(mgr.done)

	mgr.syncCodebase(dir)
}

// ─── ComputeFileHashMap: error handling ───────────────────────────────────────

// TestComputeFileHashMap_NilErrorOnUnreadable verifies ComputeFileHashMap
// always returns a nil error even when individual files are unreadable
// (they are silently skipped). This underpins the error-handling contract
// that syncCodebase relies on at the newHashes assignment site.
func TestComputeFileHashMap_NilErrorOnUnreadable(t *testing.T) {
	files := []walker.CodeFile{
		{AbsPath: "/nonexistent/does-not-exist.go", RelPath: "does-not-exist.go"},
	}
	m, err := ComputeFileHashMap(files)
	require.NoError(t, err, "ComputeFileHashMap must never return a non-nil error")
	assert.Empty(t, m.Files, "unreadable file should be silently skipped")
}

// ─── syncCodebase: hash file errors ───────────────────────────────────────────

func TestSyncCodebase_CorruptHashFile_FailsIncrementalSync(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sm := newRecordingStatusManager()
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()

	// Write corrupt JSON where the hash file is expected.
	hashPath := HashFilePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(hashPath), 0o755))
	require.NoError(t, os.WriteFile(hashPath, []byte("{not valid json"), 0o644))

	// One .go file so walker.Walk finds a change (all files appear as Added).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	sm.SetIndexed(dir, 1, 10)

	mgr.syncCodebase(dir)

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "sync: compute changes failed:")
	assert.Contains(t, info.ErrorMessage, "unmarshal hashes")
	require.NotEmpty(t, sm.operations)
	assert.Contains(t, sm.steps, "Computing file changes")
	assert.Equal(t, snapshot.OperationMetadata{Operation: "indexing", Source: "background_sync", Mode: "auto-sync"}, sm.operations[0])
}

func TestSyncCodebase_IndexesExtensionlessTextFiles(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("docs\n"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 0})
	sm.EXPECT().SetIndexed(dir, 1, 1).Return()

	mgr.syncCodebase(dir)
}

func TestSyncCodebase_IndexesUnsupportedTextFiles(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "template.hbs"), []byte("{{title}}\n"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, "template.hbs")
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 0})
	sm.EXPECT().SetIndexed(dir, 1, 1).Return()

	mgr.syncCodebase(dir)
}

func TestSyncRunParams_UsesPersistedIgnorePatterns(t *testing.T) {
	cfg := testConfig(t)
	cfg.CustomIgnore = []string{"config-only/"}
	sm := snapshot.NewManager()
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config-only"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "persisted-only"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config-only", "kept.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "persisted-only", "ignored.go"), []byte("package main"), 0o644))

	sm.SetIndexed(dir, 1, 1)
	sm.SetIgnorePatterns(dir, []string{"persisted-only/"})

	files, err := mgr.syncRunParams(dir, nil).WalkFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"config-only/kept.go", "main.go"}, relPaths(files))
}

func TestSyncRunParams_FallsBackToConfigIgnoreWithoutPersistedPatterns(t *testing.T) {
	cfg := testConfig(t)
	cfg.CustomIgnore = []string{"config-only/"}
	sm := snapshot.NewManager()
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config-only"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config-only", "ignored.go"), []byte("package main"), 0o644))

	sm.SetIndexed(dir, 1, 1)

	files, err := mgr.syncRunParams(dir, nil).WalkFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

// ─── syncCodebase: no changes ─────────────────────────────────────────────────

func TestSyncCodebase_NoChanges(t *testing.T) {
	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()

	// Create a .go file and save its hash as the old hash → Diff returns nothing.
	content := []byte("package main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), content, 0o644))

	hashes := NewFileHashMap()
	info, err := os.Stat(filepath.Join(dir, "main.go"))
	require.NoError(t, err)

	hashes.Files["main.go"] = FileEntry{
		Hash:            hexBLAKE3(content),
		ChunkCount:      0,
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}
	require.NoError(t, hashes.Save(HashFilePath(dir)))

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)

	mgr.syncCodebase(dir) // returns at "no changes" log; no Delete / Insert expected
}

// ─── syncCodebase: deleted files ─────────────────────────────────────────────

func TestSyncCodebase_DeletedFiles_TriggersDelete(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(mc, sm, mocks.NewMockSplitter(t), cfg, 300)

	dir := t.TempDir()

	// Old hash records a file that no longer exists on disk.
	oldHashes := NewFileHashMap()
	oldHashes.Files["deleted.go"] = FileEntry{Hash: "old-hash", ChunkCount: 5}
	require.NoError(t, oldHashes.Save(HashFilePath(dir)))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	mc.EXPECT().Delete(testifymock.Anything, collection, `relativePath == "deleted.go"`).Return(nil).Once()
	// TotalChunks=3 < removedChunks=5 → negative → safety floor → newChunkTotal=0.
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 3})
	sm.EXPECT().SetIndexed(dir, 0, 0).Return()

	mgr.syncCodebase(dir)
}

func TestSyncCodebase_DeleteError_SetsFailedAndReturnsEarly(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(mc, sm, mocks.NewMockSplitter(t), cfg, 300)

	dir := t.TempDir()

	oldHashes := NewFileHashMap()
	oldHashes.Files["gone.go"] = FileEntry{Hash: "h1", ChunkCount: 2}
	require.NoError(t, oldHashes.Save(HashFilePath(dir)))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	// Delete returns an error → SetFailed must be called; SetIndexed and hash save must NOT.
	mc.EXPECT().Delete(testifymock.Anything, collection, `relativePath == "gone.go"`).
		Return(errors.New("delete failed"))
	sm.EXPECT().SetFailed(dir, testifymock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "sync: delete failed: delete chunks for gone.go in ") && strings.Contains(msg, ": delete failed")
	})).Return()

	mgr.syncCodebase(dir)
}

// ─── syncCodebase: added files ────────────────────────────────────────────────

// TestSyncCodebase_AddedFiles_FlushPath exercises the "flush remaining batch"
// code path: InsertBatchSize=800 (default), 1 entity → batch never fills,
// flush goroutine fires at the end.
func TestSyncCodebase_AddedFiles_FlushPath(t *testing.T) {
	cfg := testConfig(t) // default InsertBatchSize=800

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 10})
	sm.EXPECT().SetIndexed(dir, 1, 11).Return() // 10 - 0 + 1

	mgr.syncCodebase(dir)
}

func TestSyncCodebase_EmitsVisibilityForChangedFiles(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := newRecordingStatusManager()
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	sm.SetIndexed(dir, 1, 10)

	collection := snapshot.CollectionName(dir)

	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)

	mgr.syncCodebase(dir)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	require.NotEmpty(t, sm.operations)
	assert.Equal(t, snapshot.OperationMetadata{Operation: "indexing", Source: "background_sync", Mode: "auto-sync"}, sm.operations[0])
	assert.Contains(t, sm.steps, "Removing stale chunks")
	assert.Contains(t, sm.steps, "Indexing 1 changed files")
	assert.Contains(t, sm.steps, "Finalizing incremental sync")
	require.NotEmpty(t, sm.progress)
	assert.Equal(t, snapshot.Progress{FilesDone: 1, FilesTotal: 1, ChunksTotal: 1, ChunksInserted: 1}, sm.progress[len(sm.progress)-1])
}

// TestSyncCodebase_AddedFiles_BatchLoopPath exercises the "insert full batch
// inside the accumulation loop" path: InsertBatchSize=1 → every entity drains
// the batch immediately rather than waiting for the flush.
func TestSyncCodebase_AddedFiles_BatchLoopPath(t *testing.T) {
	t.Setenv("WORKER_URL", "http://test-worker.example.com")
	t.Setenv("AUTH_TOKEN", "test-token")
	t.Setenv("INSERT_BATCH_SIZE", "1") // batch size=1 → loop drains on every entity

	cfg, err := config.Load()
	require.NoError(t, err)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	// With batchSize=1 the batch loop (not flush) fires Insert.
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 5})
	sm.EXPECT().SetIndexed(dir, 1, 6).Return() // 5 - 0 + 1

	mgr.syncCodebase(dir)
}

// TestSyncCodebase_AddedFiles_InsertError_BatchLoop verifies that an insert
// error inside the batch-drain goroutine causes SetFailed to be called and
// prevents hashes from being saved or SetIndexed from being called.
func TestSyncCodebase_AddedFiles_InsertError_BatchLoop(t *testing.T) {
	t.Setenv("WORKER_URL", "http://test-worker.example.com")
	t.Setenv("AUTH_TOKEN", "test-token")
	t.Setenv("INSERT_BATCH_SIZE", "1")

	cfg, err := config.Load()
	require.NoError(t, err)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).
		Return(nil, errors.New("insert failed"))
	// Insert failure → SetFailed must be called; SetIndexed and hash save must NOT.
	sm.EXPECT().SetFailed(dir, "sync: insert failed").Return()

	mgr.syncCodebase(dir)

	// Hash file must not have been written.
	_, statErr := os.Stat(HashFilePath(dir))
	assert.True(t, os.IsNotExist(statErr), "hash file must not be written on insert failure")
}

// TestSyncCodebase_AddedFiles_InsertError_Flush verifies that an insert error
// inside the flush goroutine causes SetFailed to be called and prevents hashes
// from being saved or SetIndexed from being called.
func TestSyncCodebase_AddedFiles_InsertError_Flush(t *testing.T) {
	cfg := testConfig(t) // InsertBatchSize=200 → flush path

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).
		Return(nil, errors.New("flush failed"))
	// Insert failure ��� SetFailed must be called; SetIndexed and hash save must NOT.
	sm.EXPECT().SetFailed(dir, "sync: insert failed").Return()

	mgr.syncCodebase(dir)

	// Hash file must not have been written.
	_, statErr := os.Stat(HashFilePath(dir))
	assert.True(t, os.IsNotExist(statErr), "hash file must not be written on insert failure")
}

// TestSyncCodebase_AddedFiles_EmptyChunks verifies that when the splitter
// returns no chunks for a file the worker skips it gracefully (no insert).
func TestSyncCodebase_AddedFiles_EmptyChunks(t *testing.T) {
	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	// Splitter returns nothing → no entities → no Insert call expected.
	expectSplitNoChunks(sp, testifymock.Anything)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 5})
	sm.EXPECT().SetIndexed(dir, 1, 5).Return() // 5 - 0 + 0 = 5

	mgr.syncCodebase(dir)
}

// ─── syncCodebase: modified files ─────────────────────────────────────────────

func TestSyncCodebase_ModifiedFile_UsesTwoPhaseReplace(t *testing.T) {
	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()

	// Create the file with new content on disk.
	newContent := []byte("package main\n// updated")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "updated.go"), newContent, 0o644))

	// Old hash for the same path but with a stale hash → change type = Modified.
	oldHashes := NewFileHashMap()
	oldHashes.Files["updated.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 3}
	require.NoError(t, oldHashes.Save(HashFilePath(dir)))

	collection := snapshot.CollectionName(dir)
	chunk := splitter.Chunk{Content: string(newContent), StartLine: 1, EndLine: 2}
	keptID := pipeline.BuildEntity("updated.go", ".go", dir, chunk).ID

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	mc.EXPECT().Query(testifymock.Anything, collection, `relativePath == "updated.go"`, 3).Return([]milvus.Entity{
		{ID: keptID},
		{ID: "chunk-stale-a"},
		{ID: "chunk-stale-b"},
	}, nil).Once()
	expectSplitChunks(t, sp, testifymock.Anything, []splitter.Chunk{chunk})
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	mc.EXPECT().Delete(testifymock.Anything, collection, `id in ["chunk-stale-a","chunk-stale-b"]`).Return(nil).Once()
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 10})
	// removedChunks=3 (old ChunkCount), addedChunks=1 → 10 - 3 + 1 = 8
	sm.EXPECT().SetIndexed(dir, 1, 8).Return()

	mgr.syncCodebase(dir)
}

// ─── syncCodebase: carry-forward of unchanged chunk counts ───────────────────

func TestSyncCodebase_CarryForwardChunkCounts(t *testing.T) {
	// file a.go is unchanged (carry forward ChunkCount=7 from old hashes).
	// file b.go is new (Added → triggers processing, adds 1 chunk).
	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()

	aContent := []byte("package a")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), aContent, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0o644))

	// Old hashes: a.go with its correct hash (unchanged), b.go absent.
	oldHashes := NewFileHashMap()
	aInfo, err := os.Stat(filepath.Join(dir, "a.go"))
	require.NoError(t, err)

	oldHashes.Files["a.go"] = FileEntry{
		Hash:            hexBLAKE3(aContent),
		ChunkCount:      7,
		Size:            aInfo.Size(),
		ModTimeUnixNano: aInfo.ModTime().UnixNano(),
	}
	require.NoError(t, oldHashes.Save(HashFilePath(dir)))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	// Only b.go is Added → Insert is called; no Delete needed.
	expectSplitOneChunk(t, sp, "b.go")
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().GetInfo(dir).Return(&snapshot.CodebaseInfo{TotalChunks: 10})
	// a.go's ChunkCount=7 is unchanged; b.go adds 1 → 10 - 0 + 1 = 11.
	sm.EXPECT().SetIndexed(dir, 2, 11).Return()

	mgr.syncCodebase(dir)
}

// ─── syncCodebase: GetInfo returns nil ────────────────────────────────────────

func TestSyncCodebase_NilInfo_SkipsSetIndexed(t *testing.T) {
	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	// GetInfo returns nil → SetIndexed must NOT be called (no expectation set up).
	sm.EXPECT().GetInfo(dir).Return(nil)

	mgr.syncCodebase(dir)
}

// ─── Start: goroutine body after initial delay ────────────────────────────────

// TestStart_GoroutineBody_InitialDelayAndTicker exercises the three uncovered
// paths inside Start's goroutine:
//  1. The time.After branch of the initial-delay select (line 64).
//  2. The first m.syncAll() call after the delay (lines 69-73).
//  3. At least one ticker.C fire triggering a second syncAll (lines 75-76).
//  4. The m.done branch of the inner-loop select that exits the goroutine (77-78).
//
// Interval is set to 1 s so the whole sequence completes in ~2.5 s.
// The first syncAll inserts the new file; the second syncAll finds no changes.
func TestStart_GoroutineBody_InitialDelayAndTicker(t *testing.T) {
	cfg := testConfig(t) // default InsertBatchSize=200

	mc := mocks.NewMockVectorClient(t)
	sm := newRecordingStatusManager()
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 1) // interval=1 s → initial delay = 1 s

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	sm.SetIndexed(dir, 0, 0)
	mgr.TrackPath(dir)

	collection := snapshot.CollectionName(dir)
	firstSyncDone := make(chan struct{}, 1)
	statusStartDone := make(chan struct{})
	sm.setIndexedReady = firstSyncDone
	sm.getStatusReady = statusStartDone

	initialDelay := make(chan time.Time)
	close(initialDelay)

	ticker := &manualTicker{ch: make(chan time.Time, 1)}
	mgr.after = func(time.Duration) <-chan time.Time { return initialDelay }
	mgr.newTicker = func(time.Duration) syncTicker { return ticker }

	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)

	mgr.Start()
	<-firstSyncDone

	ticker.ch <- time.Now()

	<-statusStartDone

	mgr.Stop()
}

// ─── syncCodebase: unreadable file fails indexing ────────────────────────────

func TestSyncCodebase_WorkerReadError_FailsSync(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test filesystem permission errors when running as root")
	}

	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := newRecordingStatusManager()
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main"), 0o644))
	t.Cleanup(func() { _ = os.Chmod(goFile, 0o644) }) // restore for t.TempDir cleanup

	// Record a stale hash so Diff reports "Modified" → file goes into filesToProcess.
	oldHashes := NewFileHashMap()
	oldHashes.Files["main.go"] = FileEntry{Hash: "stale-hash", ChunkCount: 2}
	require.NoError(t, oldHashes.Save(HashFilePath(dir)))

	collection := snapshot.CollectionName(dir)

	sm.SetIndexed(dir, 1, 5)

	// Capture the old IDs first, then make the modified file unreadable before replacement indexing.
	mc.EXPECT().Query(testifymock.Anything, collection, `relativePath == "main.go"`, 2).
		Run(func(context.Context, string, string, int) {
			_ = os.Chmod(goFile, 0o000)
		}).Return([]milvus.Entity{{ID: "chunk-old"}}, nil).Once()

	mgr.syncCodebase(dir)

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Equal(t, "sync: insert failed", info.ErrorMessage)
	assert.Equal(t, 5, info.TotalChunks)
	assert.Contains(t, sm.steps, "Removing stale chunks")
	assert.Contains(t, sm.steps, "Indexing 1 changed files")
}

// ─── syncCodebase: hash-file save error ───────────────────────────────────────

// TestSyncCodebase_SaveHashesError_FailsSync verifies that manifest persistence
// is part of sync success.
//
// The trick: pre-create <hashFile>.tmp as a directory so os.WriteFile in Save's
// atomic-write strategy fails with EISDIR, while still allowing AcquireLock to
// succeed (the .cfmantic dir itself remains writable).
func TestSyncCodebase_SaveHashesError_FailsSync(t *testing.T) {
	cfg := testConfig(t)

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, sm, sp, cfg, 300)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	// Create .cfmantic as a writable directory so AcquireLock succeeds.
	indexDir := snapshot.MetadataDirPath(dir)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))

	// Pre-create the tmp file path as a directory so os.WriteFile fails (EISDIR).
	hashTmp := HashFilePath(dir) + ".tmp"
	require.NoError(t, os.Mkdir(hashTmp, 0o755))

	collection := snapshot.CollectionName(dir)

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)
	expectSplitOneChunk(t, sp, testifymock.Anything)
	mc.EXPECT().Insert(testifymock.Anything, collection, testifymock.Anything).Return(nil, nil)
	sm.EXPECT().SetFailed(dir, testifymock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "sync: save hashes failed: ") && strings.Contains(msg, "is a directory")
	})).Return()

	mgr.syncCodebase(dir)
}

// ─── syncCodebase: walker error ───────────────────────────────────────────────

func TestSyncCodebase_WalkerError_ReturnsEarly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test filesystem permission errors when running as root")
	}

	cfg := testConfig(t)
	sm := mocks.NewMockStatusManager(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		sm,
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	dir := t.TempDir()

	// A subdirectory that cannot be listed causes walker.Walk to return an error.
	badDir := filepath.Join(dir, "unreadable")
	require.NoError(t, os.Mkdir(badDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(badDir, 0o755) }) // restore for cleanup

	sm.EXPECT().GetStatus(dir).Return(snapshot.StatusIndexed)
	sm.EXPECT().IsIndexing(dir).Return(false)

	// walker.Walk fails → function logs and returns early.
	// No Insert / Delete / GetInfo / SetIndexed calls expected.
	mgr.syncCodebase(dir)
}

func TestSyncStateHelpers_ReportStoppedAndRefuseActiveSync(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		mocks.NewMockStatusManager(t),
		mocks.NewMockSplitter(t),
		cfg, 300,
	)

	assert.False(t, mgr.isStopped())
	close(mgr.done)
	assert.True(t, mgr.isStopped())
	assert.False(t, mgr.startActiveSync(func() {}))
	assert.Nil(t, mgr.syncCancel)
}
