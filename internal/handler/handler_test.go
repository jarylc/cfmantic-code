package handler

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/mocks"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("WORKER_URL", "http://fake")
	t.Setenv("AUTH_TOKEN", "fake")

	cfg, err := config.Load()
	require.NoError(t, err)

	return cfg
}

func newTestHandler(t *testing.T, mc *mocks.MockVectorClient, sm *mocks.MockStatusManager, sp *mocks.MockSplitter, syncMgr *filesync.Manager) *Handler {
	t.Helper()
	cfg := loadTestConfig(t)

	return New(mc, sm, cfg, sp, syncMgr)
}

func singleFileInsert(relPath string) any {
	return mock.MatchedBy(func(entities []milvus.Entity) bool {
		return len(entities) == 1 && entities[0].RelativePath == relPath
	})
}

func expectRemoteCollectionExists(mc *mocks.MockVectorClient, dir string) {
	mc.On("HasCollection", mock.Anything, snapshot.CollectionName(dir)).Return(true, nil)
}

func expectSplitChunks(tb testing.TB, sp *mocks.MockSplitter, filePath any, chunks []splitter.Chunk) {
	tb.Helper()

	sp.On("Split", mock.Anything, filePath, mock.Anything).
		Run(func(args mock.Arguments) {
			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(tb, ok)

			for _, chunk := range chunks {
				require.NoError(tb, emit(chunk))
			}
		}).
		Return(nil)
}

func makeReq(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	return req
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, res)
	require.NotEmpty(t, res.Content)
	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent")

	return tc.Text
}

func relPaths(files []walker.CodeFile) []string {
	out := make([]string, len(files))
	for i, file := range files {
		out[i] = file.RelPath
	}

	return out
}

func makeSearchResults(total int) []milvus.SearchResult {
	results := make([]milvus.SearchResult, 0, total)
	for i := range total {
		results = append(results, milvus.SearchResult{
			RelativePath:  fmt.Sprintf("file-%02d.go", i+1),
			StartLine:     i + 1,
			EndLine:       i + 2,
			FileExtension: "go",
			Content:       fmt.Sprintf("result %d", i+1),
		})
	}

	return results
}

func searchResultPaths(results []milvus.SearchResult) []string {
	paths := make([]string, len(results))
	for i, result := range results {
		paths[i] = result.RelativePath
	}

	return paths
}

func saveSearchManifestForFile(t *testing.T, root, relPath string) {
	t.Helper()

	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	hashMap, err := filesync.ComputeFileHashMap([]walker.CodeFile{{
		AbsPath:   absPath,
		RelPath:   relPath,
		Extension: filepath.Ext(relPath),
	}})
	require.NoError(t, err)
	require.NoError(t, hashMap.Save(filesync.HashFilePath(root)))
}

func requireErrorResult(t *testing.T, res *mcp.CallToolResult, contains string) {
	t.Helper()
	require.NotNil(t, res)
	assert.True(t, res.IsError, "expected IsError=true")
	assert.Contains(t, resultText(t, res), contains)
}

func assertAskUserBeforeReindexMessage(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "unexpected hard stop")
	assert.Contains(t, text, "Do not auto-reindex")
	assert.Contains(t, text, "silently swallow this error")
	assert.Contains(t, text, "Ask the user whether they want to run index_codebase with reindex=true")
}

func assertAsyncFalseIgnoredMessage(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "Indexing started")
	assert.Contains(t, text, "async=false was ignored")
	assert.Contains(t, text, "may exceed MCP client timeouts")
}

// waitForDone waits for the done channel or fails the test on timeout.
func waitForDone(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for goroutine to complete")
	}
}

func requireIndexSemaphoreReleased(t *testing.T, h *Handler) {
	t.Helper()

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore leaked")
	}
}

func requireNoIndexLock(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(snapshot.LockFilePath(path))
	require.True(t, os.IsNotExist(err), "index lock should be released")
}

func withValidateStoredPathStub(t *testing.T, fn func(string) error) {
	t.Helper()

	prev := validateStoredPath
	validateStoredPath = fn

	t.Cleanup(func() {
		validateStoredPath = prev
	})
}

func withRelativePathFilterBuilderStub(t *testing.T, fn func(string, string) (string, error)) {
	t.Helper()

	prev := buildRelativePathFilterFn
	buildRelativePathFilterFn = fn

	t.Cleanup(func() {
		buildRelativePathFilterFn = prev
	})
}

// writeLockForCurrentProcess creates a fresh lock file in dir that
// makes AcquireLock believe the current process already holds the lock.
func writeLockForCurrentProcess(t *testing.T, dir string) {
	t.Helper()

	lockPath := snapshot.LockFilePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))

	content := fmt.Sprintf(`{"pid":%d,"startedAt":%q}`, os.Getpid(), time.Now().Format(time.RFC3339))
	require.NoError(t, os.WriteFile(lockPath, []byte(content), 0o644))
}

// ─── canonicalizePath ────────────────────────────────────────────────────────

func TestCanonicalizePath_AbsoluteDir(t *testing.T) {
	dir := t.TempDir()

	got, err := canonicalizePath(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestCanonicalizePath_RelativePath(t *testing.T) {
	dir := t.TempDir()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	rel, err := filepath.Rel(cwd, dir)
	require.NoError(t, err)

	got, err := canonicalizePath(rel)
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestCanonicalizePath_DotDotSegments(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "sub")
	require.NoError(t, os.Mkdir(child, 0o755))

	// ../sub resolves back to child
	dotDotPath := filepath.Join(child, "..", "sub")

	got, err := canonicalizePath(dotDotPath)
	require.NoError(t, err)
	assert.Equal(t, child, got)
}

func TestCanonicalizePath_NonExistent(t *testing.T) {
	_, err := canonicalizePath("/nonexistent/path/does/not/exist/abc123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestCanonicalizePath_File(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test*.go")
	require.NoError(t, err)
	f.Close()

	_, err = canonicalizePath(f.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestCanonicalizePath_Symlink(t *testing.T) {
	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	got, err := canonicalizePath(link)
	require.NoError(t, err)
	assert.Equal(t, realDir, got)
}

// ─── TestNew ─────────────────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)

	h := New(mc, sm, cfg, sp, nil)
	require.NotNil(t, h)
	assert.Equal(t, mc, h.milvus)
	assert.Equal(t, sm, h.snapshot)
	assert.Equal(t, cfg, h.cfg)
	assert.Equal(t, sp, h.splitter)
	assert.Nil(t, h.syncMgr)
}

// ─── HandleIndex ─────────────────────────────────────────────────────────────

func TestHandleIndex_MissingPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{}))
	require.NoError(t, err)
	requireErrorResult(t, res, "required argument")
}

func TestHandleIndex_PathStatFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": "/nonexistent/path/does/not/exist",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleIndex_PathIsFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	f, err := os.CreateTemp(t.TempDir(), "test*.go")
	require.NoError(t, err)
	f.Close()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": f.Name(),
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleIndex_RelativePath(t *testing.T) {
	// Relative path input should be canonicalized; downstream mocks receive the absolute path.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	rel, err := filepath.Rel(cwd, dir)
	require.NoError(t, err)

	// Mock expects the canonical absolute path, not the relative one.
	sm.On("IsIndexing", dir).Return(true)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": rel,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "already indexing")
}

func TestHandleIndex_SymlinkResolved(t *testing.T) {
	// Symlink path should resolve to the real directory; downstream mocks receive canonical path.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	// Mock expects the real canonical path.
	sm.On("IsIndexing", realDir).Return(true)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": link,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "already indexing")
}

func TestHandleIndex_AlreadyIndexing(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	sm.On("IsIndexing", dir).Return(true)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "already indexing")
}

func TestHandleIndex_SemaphoreBlocks(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	// Pre-fill the semaphore to simulate an in-progress indexing operation.
	h.indexSem <- struct{}{}

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError, "expected non-error result")
	assert.Contains(t, resultText(t, res), "already in progress")

	// Drain the semaphore so cleanup does not block.
	<-h.indexSem
}

func TestHandleIndex_FreshIndex_ExplicitSyncIsIgnoredAndStartsInBackground(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": false,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assertAsyncFalseIgnoredMessage(t, resultText(t, res))

	require.Eventually(t, func() bool {
		info := sm.GetInfo(dir)
		return info != nil && info.Status == snapshot.StatusIndexed
	}, 5*time.Second, 5*time.Millisecond)

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, 0, info.IndexedFiles)
	assert.Equal(t, 0, info.TotalChunks)
	assert.Empty(t, info.ErrorMessage)
	assert.Empty(t, info.Step)

	require.Eventually(t, func() bool {
		select {
		case h.indexSem <- struct{}{}:
			<-h.indexSem
			return true
		default:
			return false
		}
	}, 5*time.Second, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		_, err := os.Stat(snapshot.LockFilePath(dir))
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond)

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_FreshIndex_DefaultAsyncStartsInBackground(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	// backgroundIndex goroutine on empty dir:
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")

	waitForDone(t, done, 5*time.Second)
	// backgroundIndex calls saveHashes AFTER SetIndexed; wait for the goroutine to
	// fully exit (lock released via defer) before TempDir cleanup runs.
	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, err := os.Stat(lockPath)
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_FreshIndex_AsyncIgnoresRequestCancellation(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := mocks.NewMockStatusManager(t)
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil).Once()
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{{Content: "package main", StartLine: 1, EndLine: 1}})

	insertStarted := make(chan struct{})
	insertMayFinish := make(chan struct{})
	insertCanceled := make(chan struct{})

	mc.On("Insert", mock.Anything, collection, singleFileInsert("main.go")).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)

		close(insertStarted)

		select {
		case <-ctx.Done():
			close(insertCanceled)
		case <-insertMayFinish:
		}
	}).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 1 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 1, 1).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(ctx, makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")

	waitForDone(t, insertStarted, 5*time.Second)
	cancel()

	select {
	case <-insertCanceled:
		t.Fatal("async full indexing inherited request cancellation")
	case <-time.After(100 * time.Millisecond):
	}

	close(insertMayFinish)
	waitForDone(t, done, 5*time.Second)

	require.Eventually(t, func() bool {
		_, err := os.Stat(snapshot.LockFilePath(dir))
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond)

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_FreshIndex_ExplicitSyncIsIgnoredEvenWhenIndexLaterFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil).Once()
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{{Content: "package main", StartLine: 1, EndLine: 1}})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(nil, errors.New("quota exceeded")).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": false,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assertAsyncFalseIgnoredMessage(t, resultText(t, res))

	require.Eventually(t, func() bool {
		info := sm.GetInfo(dir)
		return info != nil && info.Status == snapshot.StatusFailed
	}, 5*time.Second, 5*time.Millisecond)

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "insert failed")
	assert.Contains(t, info.ErrorMessage, "quota exceeded")

	require.Eventually(t, func() bool {
		select {
		case h.indexSem <- struct{}{}:
			<-h.indexSem
			return true
		default:
			return false
		}
	}, 5*time.Second, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		_, err := os.Stat(snapshot.LockFilePath(dir))
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond)

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_FreshIndex_CreateCollectionFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(errors.New("connection refused"))

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "failed to create collection")

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore leaked after create collection failure")
	}
}

func TestHandleIndex_FreshIndex_CreateCollectionBackendUnavailable(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).
		Return(fmt.Errorf("%w: milvus: unexpected non-JSON response: POST /v2/vectordb/collections/create: HTTP 404: backend missing", milvus.ErrBackendUnavailable))

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "backend appears unavailable")
	assert.Contains(t, text, "deploy the backend")
	assert.Contains(t, text, "HTTP 404")

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore leaked after create collection failure")
	}
}

func TestHandleIndex_FreshIndex_WithSyncMgr(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_IndexedAncestorFromSnapshotAfterRestart_ReturnsMachineFriendlyError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)

	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))

	persistedSnapshot := snapshot.NewManager()
	persistedSnapshot.SetIndexed(parent, 2, 4)

	h := New(mc, snapshot.NewManager(), cfg, sp, nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  child,
		"async": true,
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	assert.Equal(t,
		fmt.Sprintf("cannot index child path %q: parent path %q is already tracked", child, parent),
		resultText(t, res),
	)

	mc.AssertNotCalled(t, "CreateCollection", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleIndex_FailedAncestorFromSnapshotAfterRestart_ReturnsMachineFriendlyError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)

	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))

	persistedSnapshot := snapshot.NewManager()
	persistedSnapshot.SetFailed(parent, "boom")

	h := New(mc, snapshot.NewManager(), cfg, sp, nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  child,
		"async": true,
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	assert.Equal(t,
		fmt.Sprintf("cannot index child path %q: parent path %q is already tracked", child, parent),
		resultText(t, res),
	)

	mc.AssertNotCalled(t, "CreateCollection", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleIndex_MoveRenameDetectedAtPath_ClearsStaleIndexAndStartsFresh(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	oldCollection := snapshot.CollectionName(storedPath)
	newCollection := snapshot.CollectionName(dir)

	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	mc.On("DropCollection", mock.Anything, oldCollection).Return(nil).Once()
	mc.On("CreateCollection", mock.Anything, newCollection, cfg.EmbeddingDimension, true).Return(nil).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")
	assert.NotContains(t, resultText(t, res), "Incremental sync started")

	require.Eventually(t, func() bool {
		return sm.GetStatus(dir) == snapshot.StatusIndexed
	}, 5*time.Second, 5*time.Millisecond)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_MoveRenameDetectedAtManagedAncestor_ClearsStaleIndexAndStartsFresh(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	storedPath := filepath.Join(t.TempDir(), "old-root")
	oldCollection := snapshot.CollectionName(storedPath)
	childCollection := snapshot.CollectionName(child)

	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(root), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(root), "state.json"), data, 0o644))

	mc.On("DropCollection", mock.Anything, oldCollection).Return(nil).Once()
	mc.On("CreateCollection", mock.Anything, childCollection, cfg.EmbeddingDimension, true).Return(nil).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  child,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")
	assert.NotContains(t, resultText(t, res), "Incremental sync started")

	require.Eventually(t, func() bool {
		return sm.GetStatus(child) == snapshot.StatusIndexed
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, snapshot.StatusNotFound, sm.GetStatus(root))

	_, statErr := os.Stat(filepath.Join(snapshot.MetadataDirPath(root), "state.json"))
	assert.True(t, os.IsNotExist(statErr))

	lockPath := snapshot.LockFilePath(child)

	require.Eventually(t, func() bool {
		_, err := os.Stat(lockPath)
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_StalePersistedIndexingAfterRestart_ResumesIncrementalSync(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	persistedSnapshot := snapshot.NewManager()
	persistedSnapshot.SetStep(dir, "Walking files")

	sm := snapshot.NewManager()
	h := New(mc, sm, cfg, sp, nil)

	mc.On("HasCollection", mock.Anything, collection).Return(true, nil).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	require.Eventually(t, func() bool {
		return sm.GetStatus(dir) == snapshot.StatusIndexed
	}, 5*time.Second, 5*time.Millisecond)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_Reindex(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	sm.On("Remove", dir).Return()
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	// backgroundIndex goroutine on empty dir:
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":    dir,
		"reindex": true,
		"async":   false,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assertAsyncFalseIgnoredMessage(t, resultText(t, res))

	waitForDone(t, done, 5*time.Second)

	// Wait for goroutine to fully exit (saveHashes writes to .cfmantic after SetIndexed).
	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_Reindex_FailedStatusClearsRemoteIndex(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusFailed)
	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	sm.On("Remove", dir).Return()
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":    dir,
		"reindex": true,
		"async":   true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")

	waitForDone(t, done, 5*time.Second)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_AlreadyIndexed_NoReindex_MissingRemoteCollection(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HasCollection", mock.Anything, collection).Return(false, nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "remote index is missing")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, dir)

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore leaked after missing remote collection")
	}
}

func TestHandleIndex_AlreadyIndexed_NoReindex_NoChanges_ExplicitSyncReturnsCompletion(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	sm.SetIndexed(dir, 10, 50)
	expectRemoteCollectionExists(mc, dir)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": false,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync complete")
	assert.NotContains(t, resultText(t, res), "Incremental sync started")

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusIndexed, info.Status)
	assert.Equal(t, 10, info.IndexedFiles)
	assert.Equal(t, 50, info.TotalChunks)
	assert.Empty(t, info.ErrorMessage)

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_NoChanges_DefaultAsyncStartsInBackground(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	// Handler sets step before launching goroutine
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	// incrementalIndex goroutine on empty dir with no stored hashes → no changes
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	// No changes: GetInfo then SetIndexed
	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 10, TotalChunks: 50, Status: snapshot.StatusIndexed}

	done := make(chan struct{})

	sm.On("GetInfo", dir).Return(existingInfo)
	sm.On("SetIndexed", dir, 10, 50).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_AsyncIgnoresRequestCancellation(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := mocks.NewMockStatusManager(t)
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{IndexedFiles: 5, TotalChunks: 20, Status: snapshot.StatusIndexed})
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return()
	sm.On("SetStep", dir, "Finalizing incremental sync").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{{Content: "package main", StartLine: 1, EndLine: 1}})

	insertStarted := make(chan struct{})
	insertMayFinish := make(chan struct{})
	insertCanceled := make(chan struct{})

	mc.On("Insert", mock.Anything, collection, singleFileInsert("main.go")).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)

		close(insertStarted)

		select {
		case <-ctx.Done():
			close(insertCanceled)
		case <-insertMayFinish:
		}
	}).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 1, 21).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(ctx, makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, insertStarted, 5*time.Second)
	cancel()

	select {
	case <-insertCanceled:
		t.Fatal("async incremental indexing inherited request cancellation")
	case <-time.After(100 * time.Millisecond):
	}

	close(insertMayFinish)
	waitForDone(t, done, 5*time.Second)

	require.Eventually(t, func() bool {
		_, err := os.Stat(snapshot.LockFilePath(dir))
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond)

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_ExplicitSyncReturnsErrorOnFailure(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	sm.SetFailed(dir, "previous failure")
	expectRemoteCollectionExists(mc, dir)

	hashFilePath := filesync.HashFilePath(dir)
	require.NoError(t, os.MkdirAll(hashFilePath, 0o755))

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": false,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "computing file hashes")
	assert.Contains(t, resultText(t, res), "is a directory")

	info := sm.GetInfo(dir)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "computing file hashes")

	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_NilInfo(t *testing.T) {
	// Like NoChanges but GetInfo returns nil — SetIndexed should NOT be called.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()

	done := make(chan struct{})
	// GetInfo returns nil → no SetIndexed; we signal done via GetInfo call
	sm.On("GetInfo", dir).Run(func(args mock.Arguments) { close(done) }).Return((*snapshot.CodebaseInfo)(nil))

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_WithAddedFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	// Create a .go file for the walker to find
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	// incrementalIndex goroutine — file is "Added" (no stored hash)
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return()
	sm.On("SetStep", dir, "Finalizing incremental sync").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{}, nil)

	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 5, TotalChunks: 20, Status: snapshot.StatusIndexed}
	sm.On("GetInfo", dir).Return(existingInfo)

	done := make(chan struct{})
	// addedChunks=1, removedChunks=0, totalChunks=20-0+1=21
	sm.On("SetIndexed", dir, 1, 21).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_WithDeletedFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Create a stored hash file with a "deleted" file entry
	hashMap := filesync.NewFileHashMap()
	hashMap.Files["old_file.go"] = filesync.FileEntry{Hash: "abc123", ChunkCount: 5}
	require.NoError(t, hashMap.Save(filesync.HashFilePath(dir)))

	// dir is empty → walker finds nothing → "old_file.go" appears as Deleted
	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	mc.On("Delete", mock.Anything, collection, `relativePath == "old_file.go"`).Return(nil).Once()

	// info.TotalChunks=3 so totalChunks = 3-5+0 = -2 → safety clamp → 0
	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 3, TotalChunks: 3, Status: snapshot.StatusIndexed}
	sm.On("GetInfo", dir).Return(existingInfo)

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_AlreadyIndexed_NoReindex_WithSyncMgr(t *testing.T) {
	// Covers the syncMgr.TrackPath branch in incrementalIndex.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	dir := t.TempDir()

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()

	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 10, TotalChunks: 50, Status: snapshot.StatusIndexed}
	done := make(chan struct{})

	sm.On("GetInfo", dir).Return(existingInfo)
	sm.On("SetIndexed", dir, 10, 50).Run(func(args mock.Arguments) { close(done) }).Return()
	_, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)

	waitForDone(t, done, 5*time.Second)
}

// ─── incrementalIndex: LoadFileHashMap error path ────────────────────────────

func TestHandleIndex_IncrementalIndex_LoadHashMapError(t *testing.T) {
	// Write malformed JSON to the hash file → LoadFileHashMap returns an error →
	// incremental sync must fail before deleting or indexing anything.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	// Create a real .go file so the walker finds something (otherwise Diff = empty → early return)
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	// Write invalid JSON to the hash file → LoadFileHashMap will error
	hashFilePath := filesync.HashFilePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(hashFilePath), 0o755))
	require.NoError(t, os.WriteFile(hashFilePath, []byte("not valid json{{{"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "computing file hashes: ") && strings.Contains(msg, "unmarshal hashes")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, done, 5*time.Second)
}

// ─── incrementalIndex: Delete error → SetFailed + early return ───────────────

func TestHandleIndex_IncrementalIndex_DeleteError_SetsFailedAndReturnsEarly(t *testing.T) {
	// Scenario: TempDir has main.go (unchanged) and oldHashes has main.go + deleted.go.
	// Diff: deleted.go is Deleted (changes > 0), main.go unchanged.
	// Delete returns error → SetFailed is called and function returns early.
	// saveHashes and SetIndexed must NOT be called.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Create main.go and compute its real hash
	goFile := filepath.Join(dir, "main.go")
	content := []byte("package main\n")
	require.NoError(t, os.WriteFile(goFile, content, 0o644))

	// Compute the real hash of main.go using filesync helpers so it matches exactly
	newHashes, err := filesync.ComputeFileHashMap([]walker.CodeFile{
		{AbsPath: goFile, RelPath: "main.go", Extension: ".go"},
	})
	require.NoError(t, err)

	mainHash := newHashes.Files["main.go"].Hash

	hashMap := filesync.NewFileHashMap()
	hashMap.Files["main.go"] = filesync.FileEntry{Hash: mainHash, ChunkCount: 7}
	hashMap.Files["deleted.go"] = filesync.FileEntry{Hash: "deadbeef", ChunkCount: 3}
	require.NoError(t, hashMap.Save(filesync.HashFilePath(dir)))

	// Walker finds main.go → Diff: deleted.go is Deleted, main.go unchanged → changes=[{deleted.go,Deleted}]
	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	// Delete returns an error → SetFailed is called; saveHashes/SetIndexed are NOT called.
	mc.On("Delete", mock.Anything, collection, `relativePath == "deleted.go"`).Return(errors.New("delete failed")).Once()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "incremental index: delete failed: delete chunks for deleted.go: ") && strings.Contains(msg, "delete failed")
	})).
		Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

// ─── walkFiles error: backgroundIndex and incrementalIndex ───────────────────

func TestHandleIndex_BackgroundIndex_WalkFilesError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test: running as root bypasses chmod")
	}

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Create an unreadable subdirectory so filepath.WalkDir returns an error
	restricted := filepath.Join(dir, "restricted")
	require.NoError(t, os.Mkdir(restricted, 0o000))
	t.Cleanup(func() { os.Chmod(restricted, 0o700) })

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.AnythingOfType("string")).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_IncrementalIndex_WalkFilesError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test: running as root bypasses chmod")
	}

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	restricted := filepath.Join(dir, "restricted")
	require.NoError(t, os.Mkdir(restricted, 0o000))
	t.Cleanup(func() { os.Chmod(restricted, 0o700) })

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.AnythingOfType("string")).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_BackgroundIndex_LockFails(t *testing.T) {
	// Covers the AcquireLock error branch in backgroundIndex.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Write a fresh lock file for the current process — AcquireLock will see it and fail.
	writeLockForCurrentProcess(t, dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "lock:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError) // handler returns OK; goroutine fails internally

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_IncrementalIndex_LockFails(t *testing.T) {
	// Covers the AcquireLock error branch in incrementalIndex.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	writeLockForCurrentProcess(t, dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "lock:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_WithIgnorePatterns(t *testing.T) {
	// Verify handler accepts ignorePatterns and starts indexing.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 0, 0).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":           dir,
		"ignorePatterns": []string{"vendor/"},
		"async":          true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Wait for goroutine to fully exit (saveHashes writes to .cfmantic after SetIndexed).
	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, err := os.Stat(lockPath)
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestWalkFiles_IncludesUnsupportedTextFiles(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("docs\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "template.hbs"), []byte("{{title}}\n"), 0o644))

	files, err := h.walkFiles(context.Background(), dir, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"README", "notes.txt", "template.hbs"}, relPaths(files))
}

// ─── buildExtensionFilter ─────────────────────────────────────────────────────

func TestBuildExtensionFilter_Empty(t *testing.T) {
	assert.Empty(t, buildExtensionFilter([]string{}))
}

func TestBuildExtensionFilter_SingleWithDot(t *testing.T) {
	assert.Equal(t, `fileExtension in ["go"]`, buildExtensionFilter([]string{".go"}))
}

func TestBuildExtensionFilter_SingleWithoutDot(t *testing.T) {
	assert.Equal(t, `fileExtension in ["ts"]`, buildExtensionFilter([]string{"ts"}))
}

func TestBuildExtensionFilter_SkipsEmpty(t *testing.T) {
	assert.Empty(t, buildExtensionFilter([]string{"."}))
}

func TestBuildExtensionFilter_MultipleExtensions(t *testing.T) {
	assert.Equal(t, `fileExtension in ["go", "ts"]`, buildExtensionFilter([]string{".go", ".ts"}))
}

func TestBuildExtensionFilter_SkipsDotOnlyEntries(t *testing.T) {
	assert.Equal(t, `fileExtension in ["go"]`, buildExtensionFilter([]string{".", ".go", "."}))
}

func TestBuildRelativePathFilter_SameRoot(t *testing.T) {
	root := t.TempDir()

	filter, err := buildRelativePathFilter(root, root)
	require.NoError(t, err)
	assert.Empty(t, filter)
}

func TestBuildRelativePathFilter_Subdirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	filter, err := buildRelativePathFilter(root, child)
	require.NoError(t, err)
	assert.Equal(t, `relativePath like "pkg/service/%"`, filter)
}

func TestBuildRelativePathFilter_PreservesLiteralPercentAndUnderscoreForWorkerPrefixContract(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg", "100%_done")
	require.NoError(t, os.MkdirAll(child, 0o755))

	filter, err := buildRelativePathFilter(root, child)
	require.NoError(t, err)
	// CFmantic Code talks to the custom cf-workers-milvus worker, which compiles
	// prefix LIKE filters into literal range queries for SQL/Vectorize. `%` and
	// `_` inside the subtree prefix are therefore real path bytes, not native
	// Milvus wildcards, and must be preserved.
	assert.Equal(t, `relativePath like "pkg/100%_done/%"`, filter)
	assert.NotContains(t, filter, `\\%`)
	assert.NotContains(t, filter, `\\_`)
}

func TestBuildRelativePathFilter_EscapesBackslashes(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg") + `/dir\name`
	require.NoError(t, os.MkdirAll(child, 0o755))

	filter, err := buildRelativePathFilter(root, child)
	require.NoError(t, err)
	assert.Equal(t, `relativePath like "pkg/dir\\\\name/%"`, filter)
}

func TestBuildSearchFilter_PathAndExtension(t *testing.T) {
	assert.Equal(t,
		`relativePath like "pkg/service/%" and fileExtension in ["go", "ts"]`,
		buildSearchFilter([]string{".go", ".ts"}, `relativePath like "pkg/service/%"`),
	)
}

func TestBuildSearchFilter_PreservesLiteralPercentAndUnderscoreForWorkerPrefixContract(t *testing.T) {
	assert.Equal(t,
		`relativePath like "pkg/100%_done/%" and fileExtension in ["go"]`,
		buildSearchFilter([]string{".go"}, `relativePath like "pkg/100%_done/%"`),
	)
}

// ─── HandleSearch ─────────────────────────────────────────────────────────────

func TestHandleSearch_SymlinkResolved(t *testing.T) {
	// Symlink path should resolve; downstream mocks receive canonical path.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	sm.On("GetStatus", mock.Anything).Return(snapshot.StatusNotFound)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  link,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "not indexed")
}

func TestHandleSearch_MissingPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{}))
	require.NoError(t, err)
	requireErrorResult(t, res, "required argument")
}

func TestHandleSearch_MissingQuery(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "required argument")
}

func TestHandleSearch_PathStatFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  "/nonexistent/path",
		"query": "test query",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleSearch_StatusNotFound(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetStatus", mock.Anything).Return(snapshot.StatusNotFound)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "not indexed")
}

func TestHandleSearch_StatusNotFound_UsesIndexedAncestorFromSnapshot(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	collection := snapshot.CollectionName(root)
	results := []milvus.SearchResult{
		{RelativePath: "pkg/service/main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
	}

	sm.On("GetStatus", child).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", filepath.Join(root, "pkg")).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", root).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, `relativePath like "pkg/service/%"`).Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  child,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "pkg/service/main.go")
	assert.Contains(t, text, "Found 1 results")
}

func TestHandleSearch_StatusNotFound_UsesIndexingAncestorFromSnapshotWithEmptySyncManager(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	collection := snapshot.CollectionName(root)

	sm.On("GetStatus", child).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", filepath.Join(root, "pkg")).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", root).Return(snapshot.StatusIndexing)
	sm.On("GetInfo", root).Return(&snapshot.CodebaseInfo{Step: "Walking files", Status: snapshot.StatusIndexing})
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, `relativePath like "pkg/service/%"`).Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  child,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing in progress (Walking files)")
}

func TestHandleSearch_WithIndexedAncestor_PreservesLiteralPercentAndUnderscoreInSubtreeFilterForWorker(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "100%_done")
	require.NoError(t, os.MkdirAll(child, 0o755))

	collection := snapshot.CollectionName(root)
	results := []milvus.SearchResult{
		{RelativePath: "pkg/100%_done/main.go", FileExtension: "go", StartLine: 1, EndLine: 3, Content: "package main"},
	}

	sm.On("GetStatus", child).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", filepath.Join(root, "pkg")).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", root).Return(snapshot.StatusIndexed)
	// The subtree filter must reach the cf-workers-milvus backend unchanged so it
	// can apply its literal prefix-range translation for SQL/Vectorize.
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, `relativePath like "pkg/100%_done/%"`).Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  child,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Found 1 results")
	assert.Contains(t, text, "pkg/100%_done/main.go")
}

func TestHandleSearch_MoveRenameDetectedAtPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")

	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "moved or renamed")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, storedPath)
	assert.Contains(t, text, dir)
	assert.Contains(t, text, "index_codebase")

	mc.AssertNotCalled(t, "HybridSearch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleSearch_MoveRenameDetectedAtManagedAncestor(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(root), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(root), "state.json"), data, 0o644))

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  child,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "moved or renamed")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, storedPath)
	assert.Contains(t, text, root)
	assert.Contains(t, text, "index_codebase")

	mc.AssertNotCalled(t, "HybridSearch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleSearch_StatusFailed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetStatus", dir).Return(snapshot.StatusFailed).Once()
	sm.On("GetStatus", mock.MatchedBy(func(path string) bool { return path != dir })).Return(snapshot.StatusNotFound)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "not indexed")
}

func TestHandleSearch_StatusIndexing_WithStep(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexing)
	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{Step: "Walking files", Status: snapshot.StatusIndexing})
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing in progress (Walking files)")
}

func TestHandleSearch_StatusIndexing_NilInfo(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexing)
	sm.On("GetInfo", dir).Return((*snapshot.CodebaseInfo)(nil))
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	// step is empty string
	assert.Contains(t, resultText(t, res), "Indexing in progress ()")
}

func TestHandleSearch_LimitDefault(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	// No "limit" arg → default output 10, backend fetch still uses 20 for rerank headroom.
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestHandleSearch_LimitZero(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	// limit=0 → output clamped to 1, backend fetch still uses 20 for rerank headroom.
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
		"limit": float64(0),
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestHandleSearch_LimitOver20(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	// limit=100 → output clamped to 20, backend fetch stays capped at 20.
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
		"limit": float64(100),
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestHandleSearch_SearchError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return(nil, errors.New("search failed"))

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "search failed")
}

func TestHandleSearch_BackendUnavailableError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").
		Return(nil, fmt.Errorf("%w: milvus: unexpected non-JSON response: POST /v2/vectordb/entities/hybrid_search: HTTP 404: backend missing", milvus.ErrBackendUnavailable))

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "backend appears unavailable")
	assert.Contains(t, text, "deploy the backend")
	assert.Contains(t, text, "HTTP 404")
}

func TestHandleSearch_MissingSearchStateError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").
		Return(nil, fmt.Errorf("%w: milvus: API error: POST /v2/vectordb/entities/hybrid_search: code 1: Error: D1_ERROR: no such table: fts_code_chunks_deadbeef: SQLITE_ERROR", milvus.ErrSearchStateMissing))

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "search state is missing")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, dir)
	assert.Contains(t, text, "no such table")
}

func TestHandleSearch_NoResults(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return([]milvus.SearchResult{}, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "No results found")
}

func TestHandleSearch_WithResults(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	results := []milvus.SearchResult{
		{
			RelativePath:  "main.go",
			StartLine:     1,
			EndLine:       10,
			FileExtension: "go",
			Content:       "package main",
		},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "main function", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "main function",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Found 1 results")
	assert.Contains(t, text, "main.go")
	assert.Contains(t, text, "package main")
}

func TestHandleSearch_RequestedLimitTruncatesAfterRerank(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	results := []milvus.SearchResult{
		{RelativePath: ".github/CODEOWNERS", FileExtension: "txt", StartLine: 1, EndLine: 3, Content: "* @team"},
		{RelativePath: "pkg/service.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package service"},
		{RelativePath: "cmd/main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
		"limit": float64(2),
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Found 2 results")
	assert.Contains(t, text, "pkg/service.go")
	assert.Contains(t, text, "cmd/main.go")
	assert.NotContains(t, text, "CODEOWNERS")
	assert.Less(t, strings.Index(text, "pkg/service.go"), strings.Index(text, "cmd/main.go"))
}

func TestHandleSearch_RequestedLimitOver20UsesBackendCap(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	results := makeSearchResults(20)

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
		"limit": float64(100),
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Found 20 results")
	assert.Contains(t, text, "file-20.go")
	assert.Equal(t, 20, strings.Count(text, "\n### "))
}

func TestHandleSearch_CODEOWNERSIsDemotedBehindSourceFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	results := []milvus.SearchResult{
		{RelativePath: "docs/CodeOwners", FileExtension: "txt", StartLine: 1, EndLine: 3, Content: "* @team"},
		{RelativePath: "pkg/service.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package service"},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Less(t, strings.Index(text, "pkg/service.go"), strings.Index(text, "docs/CodeOwners"))
}

func TestIsAuxiliaryResult_DemotesConfiguredBasenames(t *testing.T) {
	paths := []string{
		".github/CODEOWNERS",
		"docs/owners",
		"team/OWNERS_ALIASES",
		"LICENSE",
		"legal/copying",
		"NOTICE",
		"docs/security",
		"docs/SECURITY.md",
		"docs/support",
		"docs/SUPPORT.md",
		"community/code_of_conduct",
		"community/CODE_OF_CONDUCT.md",
		"docs/contributing",
		"docs/CONTRIBUTING.md",
		"docs/governance",
		"docs/GOVERNANCE.md",
		"docs/maintainers",
		"docs/AUTHORS",
		"docs/contributors",
		".github/pull_request_template",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/issue_template",
		".github/ISSUE_TEMPLATE.md",
		".github/dependabot.yml",
		".github/renovate.json",
		".github/RENOVATE.JSON5",
		".github/release-drafter.yml",
		".github/FUNDING.yml",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			assert.True(t, isAuxiliaryResult(path))
		})
	}
}

func TestIsAuxiliaryResult_DoesNotDemotePrimaryBasenames(t *testing.T) {
	paths := []string{
		"README",
		"README.md",
		"Makefile",
		"Dockerfile",
		"go.mod",
		"package.json",
		".gitignore",
		"docs/SECURITY.txt",
		"src/license.go",
		".github/ISSUE_TEMPLATE.yml",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			assert.False(t, isAuxiliaryResult(path))
		})
	}
}

func TestRerankAuxiliaryResults_PreservesStableOrdering(t *testing.T) {
	results := []milvus.SearchResult{
		{RelativePath: "docs/SECURITY.md"},
		{RelativePath: "pkg/service.go"},
		{RelativePath: ".github/dependabot.yml"},
		{RelativePath: "README.md"},
		{RelativePath: "docs/CONTRIBUTING"},
		{RelativePath: "cmd/main.go"},
	}

	reranked := rerankAuxiliaryResults(results)

	assert.Equal(t, []string{
		"pkg/service.go",
		"README.md",
		"cmd/main.go",
		"docs/SECURITY.md",
		".github/dependabot.yml",
		"docs/CONTRIBUTING",
	}, searchResultPaths(reranked))
}

func TestHandleSearch_WithExtensionFilter(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Server-side filter applied: Milvus returns only go results.
	goResults := []milvus.SearchResult{
		{RelativePath: "main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, `fileExtension in ["go"]`).Return(goResults, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":            dir,
		"query":           "test",
		"extensionFilter": []string{".go"},
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "main.go")
	assert.NotContains(t, text, "index.ts")
	assert.NotContains(t, text, "util.py")
}

func TestHandleSearch_WithMultipleExtensionFilter(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	results := []milvus.SearchResult{
		{RelativePath: "main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
		{RelativePath: "index.ts", FileExtension: "ts", StartLine: 1, EndLine: 3, Content: "const x = 1"},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, `fileExtension in ["go", "ts"]`).Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":            dir,
		"query":           "test",
		"extensionFilter": []string{".go", ".ts"},
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "main.go")
	assert.Contains(t, text, "index.ts")
	assert.NotContains(t, text, "util.py")
}

func TestHandleSearch_WithIndexedAncestorAndExtensionFilter(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	collection := snapshot.CollectionName(root)
	results := []milvus.SearchResult{
		{RelativePath: "pkg/service/main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
		{RelativePath: "pkg/service/index.ts", FileExtension: "ts", StartLine: 1, EndLine: 3, Content: "const x = 1"},
	}

	sm.On("GetStatus", child).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", filepath.Join(root, "pkg")).Return(snapshot.StatusNotFound)
	sm.On("GetStatus", root).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60,
		`relativePath like "pkg/service/%" and fileExtension in ["go", "ts"]`,
	).Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":            child,
		"query":           "test",
		"extensionFilter": []string{".go", ".ts"},
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "pkg/service/main.go")
	assert.Contains(t, text, "pkg/service/index.ts")
}

func TestHandleSearch_WithExtensionFilter_EmptyReturnsAll(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mixedResults := []milvus.SearchResult{
		{RelativePath: "main.go", FileExtension: "go", StartLine: 1, EndLine: 5, Content: "package main"},
		{RelativePath: "index.ts", FileExtension: "ts", StartLine: 1, EndLine: 5, Content: "const x = 1"},
	}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "test", 20, 60, "").Return(mixedResults, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "main.go")
	assert.Contains(t, text, "index.ts")
}

// ─── HandleStatus ─────────────────────────────────────────────────────────────

func TestHandleStatus_PathStatFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": "/nonexistent/path/does/not/exist",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleStatus_PathIsFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	f, err := os.CreateTemp(t.TempDir(), "test*.go")
	require.NoError(t, err)
	f.Close()

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": f.Name(),
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleStatus_SymlinkResolved(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	sm.On("GetInfo", realDir).Return((*snapshot.CodebaseInfo)(nil))

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": link,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "not indexed, run index_codebase first")
}

func TestHandleStatus_MissingPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{}))
	require.NoError(t, err)
	requireErrorResult(t, res, "required argument")
}

func TestHandleStatus_NotFound(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return((*snapshot.CodebaseInfo)(nil))

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "not indexed, run index_codebase first")
}

func TestHandleStatus_FallsBackToManagedAncestor(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	child := filepath.Join(dir, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	sm.SetIndexed(dir, 42, 300)

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": child,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Index complete")
	assert.Contains(t, text, "Path: "+dir)
	assert.Contains(t, text, "Status: indexed")
	assert.Contains(t, text, "Files: 42")
	assert.Contains(t, text, "Chunks: 300")
}

func TestHandleStatus_MoveRenameDetectedAtPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")

	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:         storedPath,
		Status:       snapshot.StatusIndexed,
		IndexedFiles: 42,
		TotalChunks:  300,
		LastUpdated:  time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "moved or renamed")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, storedPath)
	assert.Contains(t, text, dir)
	assert.Contains(t, text, "index_codebase")
}

func TestHandleStatus_MoveRenameDetectedAtManagedAncestor(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	child := filepath.Join(dir, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))

	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:         storedPath,
		Status:       snapshot.StatusIndexed,
		IndexedFiles: 42,
		TotalChunks:  300,
		LastUpdated:  time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": child,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "moved or renamed")
	assertAskUserBeforeReindexMessage(t, text)
	assert.Contains(t, text, storedPath)
	assert.Contains(t, text, dir)
}

func TestHandleStatus_Indexing(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status: snapshot.StatusIndexing,
		Step:   "Walking files",
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Indexing: Walking files")
	assert.Contains(t, text, "Started:")
}

func TestHandleStatus_IndexingWithProgress(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status:         snapshot.StatusIndexing,
		Step:           "Indexing 10 files",
		FilesTotal:     10,
		FilesDone:      7,
		ChunksTotal:    70,
		ChunksInserted: 42,
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Indexing in progress")
	assert.Contains(t, text, "Files: 7/10 split")
	assert.Contains(t, text, "Chunks: 70 generated, 42 inserted")
}

func TestHandleStatus_IndexingWithVisibilityMetadata(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	tracker := snapshot.NewTracker(sm, dir, snapshot.OperationMetadata{
		Operation: "indexing",
		Source:    "manual",
		Mode:      "full",
	})
	tracker.Start("Walking files")
	tracker.Progress(snapshot.Progress{
		FilesDone:      2,
		FilesTotal:     5,
		ChunksTotal:    12,
		ChunksInserted: 8,
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Status: indexing")
	assert.Contains(t, text, "Operation: indexing")
	assert.Contains(t, text, "Source: manual")
	assert.Contains(t, text, "Mode: full")
	assert.Contains(t, text, "Step: Walking files")
	assert.Contains(t, text, "Last progress:")
	assert.Contains(t, text, "Files: 2/5 split")
	assert.Contains(t, text, "Chunks: 12 generated, 8 inserted")
}

func TestHandleStatus_Indexed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status:       snapshot.StatusIndexed,
		IndexedFiles: 42,
		TotalChunks:  300,
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Index complete")
	assert.Contains(t, text, "Path: "+dir)
	assert.Contains(t, text, "Files: 42")
	assert.Contains(t, text, "Chunks: 300")
}

func TestHandleStatus_Failed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status:       snapshot.StatusFailed,
		ErrorMessage: "connection error",
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Indexing failed: connection error")
}

func TestHandleStatus_FailedRetryable(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status:       snapshot.StatusFailed,
		ErrorMessage: "insert failed: AiError: 3040: Capacity temporarily exceeded, please try again.",
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Indexing failed")
	assert.Contains(t, text, "Run index_codebase (without reindex) to continue where indexing left off")
}

func TestHandleStatus_UnknownStatus(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("GetInfo", dir).Return(&snapshot.CodebaseInfo{
		Status: snapshot.Status("mystery"),
	})

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Unknown status")
}

// ─── HandleClear ─────────────────────────────────────────────────────────────

func TestHandleClear_PathStatFails(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": "/nonexistent/path/does/not/exist",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleClear_PathIsFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	f, err := os.CreateTemp(t.TempDir(), "test*.go")
	require.NoError(t, err)
	f.Close()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": f.Name(),
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "path does not exist or is not a directory")
}

func TestHandleClear_SymlinkResolved(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	realDir := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	require.NoError(t, os.Symlink(realDir, link))

	collection := snapshot.CollectionName(realDir)
	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	sm.On("Remove", realDir).Return()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": link,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Index cleared")
}

func TestHandleClear_MissingPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{}))
	require.NoError(t, err)
	requireErrorResult(t, res, "required argument")
}

func TestHandleClear_Success(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	sm.On("Remove", dir).Return()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Index cleared")
}

func TestHandleClear_DropCollectionError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mc.On("DropCollection", mock.Anything, collection).Return(errors.New("network error"))
	sm.On("Remove", dir).Return()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	// DropCollection error must be surfaced to the caller.
	assert.True(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Failed to clear remote index")
	assert.Contains(t, resultText(t, res), "network error")
	// Local cleanup must still have been attempted (sm.Remove was called).
}

func TestHandleClear_MoveRenameDetectedAtPath_UsesStoredPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	mc.On("DropCollection", mock.Anything, snapshot.CollectionName(storedPath)).Return(nil).Once()
	sm.On("Remove", storedPath).Return().Once()
	sm.On("Remove", dir).Return().Once()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Index cleared")

	_, statErr := os.Stat(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestHandleClear_MoveRenameDetectedAtPath_DropError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	mc.On("DropCollection", mock.Anything, snapshot.CollectionName(storedPath)).Return(errors.New("remote boom")).Once()
	sm.On("Remove", storedPath).Return().Once()
	sm.On("Remove", dir).Return().Once()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "Failed to clear remote index")
	assert.Contains(t, text, "remote boom")
	assert.Contains(t, text, "Local state was cleaned up")

	_, statErr := os.Stat(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestHandleClear_BackendUnavailableDropCollectionError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mc.On("DropCollection", mock.Anything, collection).
		Return(fmt.Errorf("%w: milvus: unexpected non-JSON response: POST /v2/vectordb/collections/drop: HTTP 404: backend missing", milvus.ErrBackendUnavailable))
	sm.On("Remove", dir).Return()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	text := resultText(t, res)
	assert.Contains(t, text, "backend appears unavailable")
	assert.Contains(t, text, "deploy the backend")
	assert.Contains(t, text, "Local state was cleaned up")
	assert.Contains(t, text, "HTTP 404")
}

func TestHandleClear_WithSyncMgr(t *testing.T) {
	// Covers the syncMgr.UntrackPath branch in clearIndex.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	sm.On("Remove", dir).Return()

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Index cleared")
}

func TestHandleClear_CancelsActiveIndexForSamePath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	filePath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("package main\n"), 0o644))

	insertStarted := make(chan struct{})
	insertCanceled := make(chan struct{})

	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil).Once()
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{{Content: "package main", StartLine: 1, EndLine: 1}})
	mc.On("Insert", mock.Anything, collection, singleFileInsert("main.go")).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)

		close(insertStarted)
		<-ctx.Done()
		close(insertCanceled)
	}).Return((*milvus.InsertResult)(nil), context.Canceled).Once()
	mc.On("DropCollection", mock.Anything, collection).Run(func(args mock.Arguments) {
		select {
		case <-insertCanceled:
		default:
			t.Fatal("drop collection raced active index instead of waiting for cancellation")
		}
	}).Return(nil).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")

	waitForDone(t, insertStarted, 5*time.Second)

	clearRes, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, clearRes.IsError)
	assert.Contains(t, resultText(t, clearRes), "Index cleared")
	assert.Equal(t, snapshot.StatusNotFound, sm.GetStatus(dir))
	requireIndexSemaphoreReleased(t, h)
	requireNoIndexLock(t, dir)
}

func TestHandleClear_SharedSnapshotWorkerStopsTreatingPathAsIndexed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	sharedSnapshot := snapshot.NewManager()
	syncMgr := filesync.NewManager(mc, sharedSnapshot, sp, cfg, 300)
	h := New(mc, sharedSnapshot, cfg, sp, syncMgr)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sharedSnapshot.SetIndexed(dir, 2, 4)

	require.Equal(t, snapshot.StatusIndexed, sharedSnapshot.GetStatus(dir))

	mc.On("DropCollection", mock.Anything, collection).Return(nil)

	res, err := h.HandleClear(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Equal(t, snapshot.StatusNotFound, sharedSnapshot.GetStatus(dir))
}

func TestHandleIndex_Reindex_SharedSnapshotWorkerStopsTreatingPathAsIndexed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	cfg := loadTestConfig(t)
	sharedSnapshot := snapshot.NewManager()
	syncMgr := filesync.NewManager(mc, sharedSnapshot, sp, cfg, 300)
	h := New(mc, sharedSnapshot, cfg, sp, syncMgr)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sharedSnapshot.SetIndexed(dir, 2, 4)

	require.Equal(t, snapshot.StatusIndexed, sharedSnapshot.GetStatus(dir))

	mc.On("DropCollection", mock.Anything, collection).Return(nil)
	mc.On("CreateCollection", mock.Anything, collection, cfg.EmbeddingDimension, true).Return(nil)

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":    dir,
		"reindex": true,
		"async":   true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	require.Eventually(t, func() bool {
		return sharedSnapshot.GetStatus(dir) != snapshot.StatusIndexed
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		return sharedSnapshot.GetStatus(dir) == snapshot.StatusIndexed
	}, 5*time.Second, 5*time.Millisecond)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

// ─── processFiles ─────────────────────────────────────────────────────────────

func TestProcessFiles_Empty(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	result := h.processFiles(context.Background(), dir, "test_collection", []walker.CodeFile{}, nil, false)
	assert.Equal(t, 0, result.totalChunks)
	assert.Empty(t, result.err)
}

func TestProcessFiles_WithChunks(t *testing.T) {
	// Covers the "flush remaining batch" path (batch < batchSize).
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	// Create a real file so os.ReadFile succeeds
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, "col", mock.Anything).Return(&milvus.InsertResult{}, nil)
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	files := []walker.CodeFile{
		{AbsPath: goFile, RelPath: "main.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.Equal(t, 1, result.totalChunks)
	assert.Empty(t, result.err)
	assert.Equal(t, 1, result.chunkCounts["main.go"])
}

func TestProcessFiles_BatchInsert(t *testing.T) {
	// Covers the "batch loop" insert path (batch >= batchSize triggers async insert).
	// Use INSERT_BATCH_SIZE=1 so that each entity fills the batch.
	t.Setenv("INSERT_BATCH_SIZE", "1")
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	chunk1 := splitter.Chunk{Content: "chunk1", StartLine: 1, EndLine: 1}
	chunk2 := splitter.Chunk{Content: "chunk2", StartLine: 2, EndLine: 2}
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{chunk1, chunk2})
	// With batchSize=1 and 2 chunks: batch loop fires twice
	mc.On("Insert", mock.Anything, "col", mock.Anything).Return(&milvus.InsertResult{}, nil).Times(2)
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	files := []walker.CodeFile{
		{AbsPath: goFile, RelPath: "main.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.Equal(t, 2, result.totalChunks)
	assert.Empty(t, result.err)
}

func TestProcessFiles_SplitterReturnsNoChunks(t *testing.T) {
	// Splitter returns empty slice → file is skipped (no Insert).
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "empty.go")
	require.NoError(t, os.WriteFile(goFile, []byte("// empty\n"), 0o644))

	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{})

	files := []walker.CodeFile{
		{AbsPath: goFile, RelPath: "empty.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.Equal(t, 0, result.totalChunks)
	assert.Empty(t, result.err)
}

func TestProcessFiles_UnreadableFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	files := []walker.CodeFile{
		{AbsPath: "/nonexistent/path/file.go", RelPath: "file.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.Equal(t, 0, result.totalChunks)
	assert.Contains(t, result.err, "file.go")
	assert.Contains(t, result.err, "no such file or directory")
}

func TestProcessFiles_InsertError(t *testing.T) {
	// Insert returns an error → result.err is set.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, "col", mock.Anything).Return(nil, errors.New("insert failed: quota exceeded"))
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	files := []walker.CodeFile{
		{AbsPath: goFile, RelPath: "main.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.NotEmpty(t, result.err)
	assert.Contains(t, result.err, "insert failed")
}

// ─── backgroundIndex insert-error path ───────────────────────────────────────

func TestHandleIndex_BackgroundIndex_InsertError(t *testing.T) {
	// Covers the "insert failed" branch in backgroundIndex via a real file that
	// the splitter splits and the mock Insert rejects.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 1 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(nil, errors.New("quota exceeded"))

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	_, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)

	waitForDone(t, done, 5*time.Second)
}

// ─── processFiles: batch-loop insert error ────────────────────────────────────

func TestProcessFiles_BatchInsertError(t *testing.T) {
	// INSERT_BATCH_SIZE=1 → batch loop goroutine fires with 1 chunk and Insert fails.
	// Covers the error-handling path inside the batch-loop goroutine (distinct from flush path).
	t.Setenv("INSERT_BATCH_SIZE", "1")
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, "col", mock.Anything).Return(nil, errors.New("batch quota exceeded"))
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	files := []walker.CodeFile{
		{AbsPath: goFile, RelPath: "main.go", Extension: ".go"},
	}

	result := h.processFiles(context.Background(), dir, "col", files, nil, false)
	assert.NotEmpty(t, result.err)
	assert.Contains(t, result.err, "batch quota exceeded")
}

func TestProcessFiles_DoesNotCollectFileChunkIDsWhenDisabled(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	collection := snapshot.CollectionName(dir)
	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	result := h.processFiles(context.Background(), dir, collection, []walker.CodeFile{{AbsPath: goFile, RelPath: "main.go", Extension: ".go"}}, nil, false)
	require.Empty(t, result.err)
	assert.Nil(t, result.fileChunkIDs)
}

func TestProcessFiles_CollectsFileChunkIDsWhenEnabled(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	collection := snapshot.CollectionName(dir)
	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectedID := pipeline.BuildEntity("main.go", ".go", dir, chunk).ID
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	result := h.processFiles(context.Background(), dir, collection, []walker.CodeFile{{AbsPath: goFile, RelPath: "main.go", Extension: ".go"}}, nil, true)
	require.Empty(t, result.err)
	require.Equal(t, map[string][]string{"main.go": {expectedID}}, result.fileChunkIDs)
}

// ─── incrementalIndex insert-error path ──────────────────────────────────────

func TestHandleIndex_IncrementalIndex_InsertError(t *testing.T) {
	// Covers the "insert failed" branch in incrementalIndex.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(nil, errors.New("quota exceeded"))

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	_, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)

	waitForDone(t, done, 5*time.Second)
}

// ─── Gap 1: clearIndex DropCollection error during reindex ───────────────────

func TestHandleIndex_Reindex_DropError(t *testing.T) {
	// Reindex must stop if remote clear fails.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("DropCollection", mock.Anything, collection).Return(errors.New("network error"))
	sm.On("Remove", dir).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":    dir,
		"reindex": true,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "failed to clear remote index")
}

// ─── TDD: graceful degradation — partial progress on failure ─────────────────

// TestHandleIndex_FailedRoutesToIncremental verifies that a StatusFailed codebase
// without reindex goes to the incremental path (not a fresh backgroundIndex).
func TestHandleIndex_FailedRoutesToIncremental(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusFailed)
	expectRemoteCollectionExists(mc, dir)
	// Incremental path steps (empty dir → no changes detected)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()

	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 3, TotalChunks: 15, Status: snapshot.StatusFailed}

	done := make(chan struct{})

	sm.On("GetInfo", dir).Return(existingInfo)
	sm.On("SetIndexed", dir, 3, 15).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, done, 5*time.Second)
}

// TestBackgroundIndex_SavesPartialOnFailure verifies that when backgroundIndex
// encounters an insert error and some files completed, a partial hash file is saved.
func TestBackgroundIndex_SavesPartialOnFailure(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1") // each chunk is its own batch
	t.Setenv("INDEX_CONCURRENCY", "1") // deterministic file ordering

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Two files: first insert (for whichever file is processed first) succeeds, second fails.
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")

	require.NoError(t, os.WriteFile(fileA, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("package b\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 2 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})

	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(nil, errors.New("quota exceeded")).Once()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Partial hash file should exist with exactly 1 completed file.
	hashFile := filesync.HashFilePath(dir)

	require.Eventually(t, func() bool {
		_, err := os.Stat(hashFile)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "hash file should exist after partial backgroundIndex failure")

	loaded, err := filesync.LoadFileHashMap(hashFile)
	require.NoError(t, err)
	assert.Len(t, loaded.Files, 1, "only completed file should be in hash map")
}

// TestIncrementalIndex_SavesPartialOnFailure verifies that on insert failure during
// incrementalIndex, partial progress is saved: old unchanged files + completed new
// files are persisted, minus deleted/modified files.
func TestIncrementalIndex_SavesPartialOnFailure(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1")
	t.Setenv("INDEX_CONCURRENCY", "1")

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// "old.go" is unchanged (same hash in old hashes → carried forward on partial save).
	oldFile := filepath.Join(dir, "old.go")
	require.NoError(t, os.WriteFile(oldFile, []byte("package old\n"), 0o644))

	// "modified.go" exists on disk but has a stale old hash entry, so incrementalIndex
	// treats it as Modified and must remove the old entry from progressHashes before
	// any partial save occurs.
	modifiedFile := filepath.Join(dir, "modified.go")
	require.NoError(t, os.WriteFile(modifiedFile, []byte("package modified\n"), 0o644))

	// Compute the real hash for old.go to store in the old hash file.
	oldFileList := []walker.CodeFile{{AbsPath: oldFile, RelPath: "old.go", Extension: ".go"}}
	computedHashes, err := filesync.ComputeFileHashMap(oldFileList)
	require.NoError(t, err)

	oldHashes := filesync.NewFileHashMap()
	oldHashes.Files["old.go"] = filesync.FileEntry{Hash: computedHashes.Files["old.go"].Hash, ChunkCount: 5}
	oldHashes.Files["modified.go"] = filesync.FileEntry{Hash: "deadbeef", ChunkCount: 7}
	oldHashes.Files["deleted.go"] = filesync.FileEntry{Hash: "cafebabe", ChunkCount: 11}
	require.NoError(t, oldHashes.Save(filesync.HashFilePath(dir)))

	// "a.go" and "b.go" are new (not in oldHashes → Added).
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")

	require.NoError(t, os.WriteFile(fileA, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("package b\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusFailed) // routes to incremental
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "Indexing 3 changed files")
	})).Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()
	mc.On("Delete", mock.Anything, collection, `relativePath == "deleted.go"`).Return(nil).Once()
	mc.On("Query", mock.Anything, collection, `relativePath == "modified.go"`, 7).Return([]milvus.Entity{{ID: "chunk-old"}}, nil).Once()

	chunk := splitter.Chunk{Content: "package", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, "a.go", []splitter.Chunk{chunk})
	expectSplitChunks(t, sp, "b.go", []splitter.Chunk{chunk})
	sp.On("Split", mock.Anything, "modified.go", mock.Anything).Return(nil).Maybe()

	// First insert succeeds, second fails.
	mc.On("Insert", mock.Anything, collection, singleFileInsert("a.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", mock.Anything, collection, singleFileInsert("b.go")).
		Return(nil, errors.New("quota exceeded")).Once()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Hash file should contain: old.go (unchanged, carried forward) + a.go.
	// deleted.go and modified.go must be removed from the partial save seed.
	hashFile := filesync.HashFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(hashFile)
		return statErr == nil
	}, 5*time.Second, 5*time.Millisecond, "hash file should be updated after partial incrementalIndex failure")

	loaded, err := filesync.LoadFileHashMap(hashFile)
	require.NoError(t, err)

	_, hasOld := loaded.Files["old.go"]
	assert.True(t, hasOld, "unchanged old.go must be carried forward into partial hash file")
	assert.Contains(t, loaded.Files, "a.go")
	assert.NotContains(t, loaded.Files, "b.go")
	assert.NotContains(t, loaded.Files, "deleted.go")
	assert.NotContains(t, loaded.Files, "modified.go")
	assert.Len(t, loaded.Files, 2, "hash file should have old.go + exactly one completed new file")
}

// TestIncrementalIndex_AfterPartialBackground verifies end-to-end graceful degradation:
// after a partial backgroundIndex saves hashes for 1 of 2 files, a subsequent
// incremental run (status=Failed, no reindex) processes only the 1 missing file.
func TestIncrementalIndex_AfterPartialBackground(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1")
	t.Setenv("INDEX_CONCURRENCY", "1")

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Create a.go and b.go. Insert outcomes are matched by file so a.go always
	// succeeds in phase 1 and b.go always fails, regardless of insert goroutine order.
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")

	require.NoError(t, os.WriteFile(fileA, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("package b\n"), 0o644))

	chunk := splitter.Chunk{Content: "package", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	// ── Phase 1: backgroundIndex, a.go succeeds, b.go fails ──────────────────
	sm.On("IsIndexing", dir).Return(false).Once()
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound).Once()
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil).Once()
	sm.On("SetStep", dir, "Starting").Return().Once()
	sm.On("SetStep", dir, "Walking files").Return().Twice() // both phases walk
	sm.On("SetStep", dir, "Indexing 2 files").Return().Once()

	// a.go insert succeeds, b.go insert fails.
	mc.On("Insert", mock.Anything, collection, singleFileInsert("a.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", mock.Anything, collection, singleFileInsert("b.go")).
		Return(nil, errors.New("quota exceeded")).Once()

	done1 := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done1) }).Return().Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done1, 5*time.Second)

	// Wait for goroutine to fully exit (lock released after SetFailed).
	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "backgroundIndex goroutine did not exit")

	// Phase 1 result: hash file exists with 1 entry (a.go completed).
	hashFile := filesync.HashFilePath(dir)
	loaded1, err := filesync.LoadFileHashMap(hashFile)
	require.NoError(t, err)
	require.Len(t, loaded1.Files, 1, "partial hash after phase 1 should have 1 entry (a.go)")
	_, aGoPresent := loaded1.Files["a.go"]
	require.True(t, aGoPresent, "a.go should be in partial hashes after phase 1")

	// ── Phase 2: incrementalIndex (status=Failed, no reindex) ────────────────
	// Status is now StatusFailed → routes to incremental.
	sm.On("IsIndexing", dir).Return(false).Once()
	sm.On("GetStatus", dir).Return(snapshot.StatusFailed).Twice()
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return().Once()
	sm.On("SetStep", dir, "Computing file changes").Return().Once()
	sm.On("SetStep", dir, "Removing stale chunks").Return().Once()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return().Once()
	sm.On("SetStep", dir, "Finalizing incremental sync").Return().Once()

	// Only b.go needs processing (a.go is already in hash file).
	mc.On("Insert", mock.Anything, collection, singleFileInsert("b.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 0, TotalChunks: 0, Status: snapshot.StatusFailed}
	sm.On("GetInfo", dir).Return(existingInfo).Once()

	done2 := make(chan struct{})

	sm.On("SetIndexed", dir, 2, mock.AnythingOfType("int")).
		Run(func(args mock.Arguments) { close(done2) }).Return().Once()

	res2, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res2.IsError)
	assert.Contains(t, resultText(t, res2), "Incremental sync started")

	waitForDone(t, done2, 5*time.Second)

	// Verify the mock was called the expected number of times.
	mc.AssertExpectations(t)
}

// ─── TDD: errcheck item 1 - processFiles type assertion safety ───────────────

// TestProcessFiles_ErrorResultString verifies that when Insert returns an error,
// result.err is correctly populated and contains the original error message.
func TestProcessFiles_ErrorResultString(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	errMsg := "forced insert failure for type assertion test"
	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, "col", mock.Anything).Return(nil, errors.New(errMsg))
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	files := []walker.CodeFile{{AbsPath: goFile, RelPath: "main.go", Extension: ".go"}}
	result := h.processFiles(context.Background(), dir, "col", files, nil, false)

	require.NotEmpty(t, result.err, "expected error to be propagated as a string")
	assert.Contains(t, result.err, errMsg)
}

// ─── TDD: errcheck item 2 - saveHashes ComputeFileHashMap error check ─────────

// TestBackgroundIndex_HashFileCreated verifies that after backgroundIndex completes,
// the hash file is created for the indexed path, exercising the saveHashes path.
// Written before the ComputeFileHashMap error check was added (TDD: errcheck item 2).
func TestBackgroundIndex_HashFileCreated(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{}, nil)

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 1 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 1, 1).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{"path": dir, "async": true}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Wait for goroutine to fully exit (saveHashes called after SetIndexed)
	hashFile := filesync.HashFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(hashFile)
		return statErr == nil
	}, 5*time.Second, 5*time.Millisecond, "hash file should be created after backgroundIndex")
}

// ─── TDD: errcheck item 3 - incrementalIndex ComputeFileHashMap error check ──

// TestIncrementalIndex_HashFileUpdated verifies that after incrementalIndex processes
// changes, the hash file is updated, exercising the ComputeFileHashMap path.
// Written before the ComputeFileHashMap error check was added (TDD: errcheck item 3).
func TestIncrementalIndex_HashFileUpdated(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return()
	sm.On("SetStep", dir, "Finalizing incremental sync").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{}, nil)

	existingInfo := &snapshot.CodebaseInfo{IndexedFiles: 0, TotalChunks: 0, Status: snapshot.StatusIndexed}
	sm.On("GetInfo", dir).Return(existingInfo)

	done := make(chan struct{})

	sm.On("SetIndexed", dir, 1, 1).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{"path": dir, "async": true}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Verify hash file created by saveHashes inside incrementalIndex
	hashFile := filesync.HashFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(hashFile)
		return statErr == nil
	}, 5*time.Second, 5*time.Millisecond, "hash file should be created after incrementalIndex")
}

// ─── saveManifest: direct unit tests ──────────────────────────────────────────

func TestSaveManifest_PreservesChunkCountAlreadyCarriedByManifest(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	files := []walker.CodeFile{{AbsPath: goFile, RelPath: "main.go", Extension: ".go"}}
	newHashes, err := filesync.ComputeFileHashMap(files)
	require.NoError(t, err)

	oldHashes := filesync.NewFileHashMap()
	oldHashes.Files["main.go"] = newHashes.Files["main.go"]
	oldHashes.Files["main.go"] = filesync.FileEntry{
		Hash:            oldHashes.Files["main.go"].Hash,
		ChunkCount:      7,
		Size:            newHashes.Files["main.go"].Size,
		ModTimeUnixNano: newHashes.Files["main.go"].ModTimeUnixNano,
	}

	manifestDiff := filesync.ComputeManifestDiff(files, oldHashes)

	require.NoError(t, h.saveManifest(dir, manifestDiff.Manifest, map[string]int{}))

	loaded, loadErr := filesync.LoadFileHashMap(filesync.HashFilePath(dir))
	require.NoError(t, loadErr)
	assert.Equal(t, 7, loaded.Files["main.go"].ChunkCount)
}

func TestSaveManifest_UsesPipelineChunkCountForProcessedFile(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	files := []walker.CodeFile{{AbsPath: goFile, RelPath: "main.go", Extension: ".go"}}
	manifest, err := filesync.ComputeFileHashMap(files)
	require.NoError(t, err)

	require.NoError(t, h.saveManifest(dir, manifest, map[string]int{"main.go": 3}))

	loaded, loadErr := filesync.LoadFileHashMap(filesync.HashFilePath(dir))
	require.NoError(t, loadErr)
	assert.Equal(t, 3, loaded.Files["main.go"].ChunkCount)
}

func TestSaveManifest_WritesEmptyManifestWhenNil(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	require.NoError(t, h.saveManifest(dir, nil, map[string]int{"main.go": 5}))

	loaded, loadErr := filesync.LoadFileHashMap(filesync.HashFilePath(dir))
	require.NoError(t, loadErr)
	assert.Empty(t, loaded.Files)
}

// ─── TestBackgroundIndex_ProgressiveSave ─────────────────────────────────────

// TestBackgroundIndex_ProgressiveSave verifies that hash saves happen progressively
// per completed file, not all at once at the end. Three files are indexed:
// a.go and b.go succeed, c.go fails. The hash file must contain exactly a.go
// and b.go BEFORE SetFailed is called — proving saves were written during the
// pipeline run, not accumulated and flushed afterwards.
func TestBackgroundIndex_ProgressiveSave(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1") // each chunk is its own batch
	t.Setenv("INDEX_CONCURRENCY", "1")

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")
	fileC := filepath.Join(dir, "c.go")

	require.NoError(t, os.WriteFile(fileA, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("package b\n"), 0o644))
	require.NoError(t, os.WriteFile(fileC, []byte("package c\n"), 0o644))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 3 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, mock.Anything, []splitter.Chunk{chunk})

	// First two inserts succeed, third fails.
	mc.On("Insert", mock.Anything, collection, singleFileInsert("a.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", mock.Anything, collection, singleFileInsert("b.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", mock.Anything, collection, singleFileInsert("c.go")).
		Return(nil, errors.New("quota exceeded")).Once()

	hashFile := filesync.HashFilePath(dir)
	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(s string) bool {
		return strings.HasPrefix(s, "insert failed:")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	// Hash file must exist now — written progressively during the pipeline run.
	// (savePartialProgress no longer exists; the only write path is the progressive saver.)
	_, statErr := os.Stat(hashFile)
	require.NoError(t, statErr, "hash file should exist via progressive saves")

	loaded, loadErr := filesync.LoadFileHashMap(hashFile)
	require.NoError(t, loadErr)
	assert.Len(t, loaded.Files, 2, "a.go and b.go should be progressively saved; c.go must not appear")
	assert.Contains(t, loaded.Files, "a.go")
	assert.Contains(t, loaded.Files, "b.go")
	assert.NotContains(t, loaded.Files, "c.go")
}

func TestManifestProgressSaver_RecordCoalescesWithinInterval(t *testing.T) {
	now := time.Unix(100, 0)
	saved := filesync.NewFileHashMap()
	saveCalls := 0

	saver := &manifestProgressSaver{
		hashPath: "/tmp/hashes.json",
		manifest: filesync.NewFileHashMap(),
		interval: time.Second,
		now: func() time.Time {
			return now
		},
		save: func(_ string, manifest *filesync.FileHashMap) error {
			saveCalls++
			saved = manifest.Clone()

			return nil
		},
	}

	require.NoError(t, saver.Record("a.go", filesync.FileEntry{Hash: "a", ChunkCount: 1}))
	require.Equal(t, 1, saveCalls)
	assert.Contains(t, saved.Files, "a.go")
	assert.NotContains(t, saved.Files, "b.go")

	require.NoError(t, saver.Record("b.go", filesync.FileEntry{Hash: "b", ChunkCount: 2}))
	require.Equal(t, 1, saveCalls, "second save should be coalesced within interval")
	assert.NotContains(t, saved.Files, "b.go")

	now = now.Add(time.Second)

	require.NoError(t, saver.Record("c.go", filesync.FileEntry{Hash: "c", ChunkCount: 3}))
	require.Equal(t, 2, saveCalls)
	assert.Contains(t, saved.Files, "a.go")
	assert.Contains(t, saved.Files, "b.go")
	assert.Contains(t, saved.Files, "c.go")
}

func TestHandleIndex_BackgroundIndex_ProgressSaveWriteErrorFailsRun(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1")
	t.Setenv("INDEX_CONCURRENCY", "1")

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	hashFilePath := filesync.HashFilePath(dir)
	require.NoError(t, os.MkdirAll(hashFilePath, 0o755))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 1 files").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, singleFileInsert("main.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "saving file hashes: ") && strings.Contains(msg, "rename hash file")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	waitForDone(t, done, 5*time.Second)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit")
}

// ─── Gap 2: saveHashes Save() error (handler.go lines 252-254) ───────────────

func TestHandleIndex_BackgroundIndex_SaveHashesWriteErrorFailsRun(t *testing.T) {
	// Covers handler.go lines 252-254: hashMap.Save() returns an error.
	// Strategy: pre-create the hash file path as a directory so that
	// Save's os.Rename(<name>.json.tmp → <name>.json) fails with EISDIR.
	// AcquireLock writes .cfmantic/.lock (not affected), so the goroutine reaches
	// saveHashes and marks the run failed.
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)

	// Pre-create .cfmantic/ so AcquireLock can write the lock file.
	indexDir := snapshot.MetadataDirPath(dir)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))

	// Create the hash file path as a directory — Save will fail at Rename (EISDIR).
	hashFilePath := filesync.HashFilePath(dir)
	require.NoError(t, os.MkdirAll(hashFilePath, 0o755))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusNotFound)
	mc.On("CreateCollection", mock.Anything, collection, h.cfg.EmbeddingDimension, true).Return(nil)
	sm.On("SetStep", dir, "Starting").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Indexing 0 files").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "saving file hashes: ") && strings.Contains(msg, "rename hash file")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Indexing started")

	waitForDone(t, done, 5*time.Second)
	// Wait for goroutine to fully exit (lock released after saveHashes logs error)
	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, err := os.Stat(lockPath)
		return os.IsNotExist(err)
	}, 5*time.Second, 5*time.Millisecond, "background goroutine did not exit in time")
}

func TestHandleIndex_IncrementalIndex_SaveHashesWriteErrorFailsRun(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))
	require.NoError(t, os.Mkdir(filesync.HashFilePath(dir)+".tmp", 0o755))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()
	sm.On("SetStep", dir, "Removing stale chunks").Return()
	sm.On("SetStep", dir, "Indexing 1 changed files").Return()
	sm.On("SetStep", dir, "Finalizing incremental sync").Return()
	sm.On("SetProgress", mock.Anything, mock.Anything).Maybe()

	chunk := splitter.Chunk{Content: "package main", StartLine: 1, EndLine: 1}
	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{chunk})
	mc.On("Insert", mock.Anything, collection, singleFileInsert("main.go")).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "saving file hashes: ") && strings.Contains(msg, "is a directory")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, done, 5*time.Second)
}

func TestHandleIndex_IncrementalIndex_LoadHashMapReadError(t *testing.T) {
	t.Setenv("INSERT_BATCH_SIZE", "1")
	t.Setenv("INDEX_CONCURRENCY", "1")

	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()

	goFile := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))

	hashFilePath := filesync.HashFilePath(dir)
	require.NoError(t, os.MkdirAll(hashFilePath, 0o755))

	sm.On("IsIndexing", dir).Return(false)
	sm.On("GetStatus", dir).Return(snapshot.StatusFailed)
	expectRemoteCollectionExists(mc, dir)
	sm.On("SetStep", dir, "Starting incremental sync").Return()
	sm.On("SetStep", dir, "Walking files").Return()
	sm.On("SetStep", dir, "Computing file changes").Return()

	done := make(chan struct{})

	sm.On("SetFailed", dir, mock.MatchedBy(func(msg string) bool {
		return strings.HasPrefix(msg, "computing file hashes: filesync: read hash file:") && strings.Contains(msg, "is a directory")
	})).Run(func(args mock.Arguments) { close(done) }).Return()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"async": true,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, resultText(t, res), "Incremental sync started")

	waitForDone(t, done, 5*time.Second)

	lockPath := snapshot.LockFilePath(dir)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return os.IsNotExist(statErr)
	}, 5*time.Second, 5*time.Millisecond, "incremental goroutine did not exit")
}

func TestHandleIndex_RepairPathMismatchClearError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	dir := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))

	data, err := json.Marshal(&snapshot.CodebaseInfo{
		Path:        storedPath,
		Status:      snapshot.StatusIndexed,
		LastUpdated: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), "state.json"), data, 0o644))

	mc.On("DropCollection", mock.Anything, snapshot.CollectionName(storedPath)).Return(errors.New("remote boom")).Once()

	res, err := h.HandleIndex(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "failed to clear remote index")
	assert.Contains(t, resultText(t, res), "remote boom")
}

func TestHandleSearch_PathFilterOutsideRootFallsBackToNotIndexed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	withRelativePathFilterBuilderStub(t, func(string, string) (string, error) {
		return "", fmt.Errorf("%w: forced", errSearchPathOutsideRoot)
	})

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, notIndexedMessage)
	mc.AssertNotCalled(t, "HybridSearch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleSearch_PathFilterErrorIsReturned(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	withRelativePathFilterBuilderStub(t, func(string, string) (string, error) {
		return "", errors.New("broken filter")
	})

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "test",
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, "broken filter")
	mc.AssertNotCalled(t, "HybridSearch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestEnsureRemoteCollectionForIncrementalIndex_HasCollectionError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	path := t.TempDir()
	collection := snapshot.CollectionName(path)

	mc.On("HasCollection", mock.Anything, collection).Return(false, errors.New("backend down")).Once()

	errText, ok := ensureRemoteCollectionForIncrementalIndex(context.Background(), mc, path, collection)
	assert.False(t, ok)
	assert.Contains(t, errText, "failed to verify remote index")
	assert.Contains(t, errText, "backend down")
}

func TestHandleStatus_AncestorInfoMissingReturnsNotIndexed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	root := t.TempDir()
	child := filepath.Join(root, "pkg", "service")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(root), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(root), "state.json"), []byte(`{"path":"`+root+`"}`), 0o644))

	sm.On("GetInfo", child).Return((*snapshot.CodebaseInfo)(nil)).Once()
	sm.On("GetStatus", root).Return(snapshot.StatusIndexed).Once()
	sm.On("GetInfo", root).Return((*snapshot.CodebaseInfo)(nil)).Once()

	res, err := h.HandleStatus(context.Background(), makeReq(map[string]any{
		"path": child,
	}))
	require.NoError(t, err)
	requireErrorResult(t, res, notIndexedMessage)
}

func TestCompletedIndexResult_CoversRemainingBranches(t *testing.T) {
	path := t.TempDir()

	t.Run("status indexed without info", func(t *testing.T) {
		mockSnapshot := mocks.NewMockStatusManager(t)
		mockSnapshot.On("GetInfo", path).Return((*snapshot.CodebaseInfo)(nil)).Once()
		mockSnapshot.On("GetStatus", path).Return(snapshot.StatusIndexed).Once()

		h := &Handler{snapshot: mockSnapshot}
		res := h.completedIndexResult(path, "Indexing complete")
		assert.False(t, res.IsError)
		assert.Contains(t, resultText(t, res), "Indexing complete for "+path+".")
	})

	t.Run("status failed without info", func(t *testing.T) {
		manager := snapshot.NewManager()
		manager.SetFailed(path, "boom")
		manager.Remove(path)

		mockSnapshot := mocks.NewMockStatusManager(t)
		mockSnapshot.On("GetInfo", path).Return((*snapshot.CodebaseInfo)(nil)).Once()
		mockSnapshot.On("GetStatus", path).Return(snapshot.StatusFailed).Once()

		h := &Handler{snapshot: mockSnapshot}

		res := h.completedIndexResult(path, "Indexing complete")
		assert.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "indexing complete failed for "+path)
	})

	t.Run("status unknown without info", func(t *testing.T) {
		mockSnapshot := mocks.NewMockStatusManager(t)
		mockSnapshot.On("GetInfo", path).Return((*snapshot.CodebaseInfo)(nil)).Once()
		mockSnapshot.On("GetStatus", path).Return(snapshot.StatusNotFound).Once()

		h := &Handler{snapshot: mockSnapshot}
		res := h.completedIndexResult(path, "Indexing complete")
		assert.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "indexing did not complete")
	})

	t.Run("failed info without message", func(t *testing.T) {
		mockSnapshot := mocks.NewMockStatusManager(t)
		mockSnapshot.On("GetInfo", path).Return(&snapshot.CodebaseInfo{Status: snapshot.StatusFailed}).Once()

		h := &Handler{snapshot: mockSnapshot}
		res := h.completedIndexResult(path, "Indexing complete")
		assert.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "indexing complete failed for "+path)
	})

	t.Run("unexpected in-progress info", func(t *testing.T) {
		mockSnapshot := mocks.NewMockStatusManager(t)
		mockSnapshot.On("GetInfo", path).Return(&snapshot.CodebaseInfo{Status: snapshot.StatusIndexing}).Once()

		h := &Handler{snapshot: mockSnapshot}
		res := h.completedIndexResult(path, "Indexing complete")
		assert.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "indexing did not complete")
	})
}

func TestCanonicalizePath_DeletedWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.RemoveAll(dir))

	_, err := CanonicalizePath(".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path")
}

func TestManifestProgressSaver_RecordInitializesNilManifest(t *testing.T) {
	saveCalls := 0
	saver := &manifestProgressSaver{
		hashPath: "/tmp/hashes.json",
		interval: 0,
		save: func(_ string, manifest *filesync.FileHashMap) error {
			saveCalls++

			assert.Contains(t, manifest.Files, "main.go")

			return nil
		},
	}

	require.NoError(t, saver.Record("main.go", filesync.FileEntry{Hash: "abc", ChunkCount: 1}))
	require.NotNil(t, saver.manifest)
	assert.Equal(t, 1, saveCalls)
	assert.Contains(t, saver.manifest.Files, "main.go")
}

func TestManifestProgressSaver_SaveLockedSkipsCleanState(t *testing.T) {
	saver := &manifestProgressSaver{
		hashPath: "/tmp/hashes.json",
		manifest: filesync.NewFileHashMap(),
		save: func(string, *filesync.FileHashMap) error {
			t.Fatal("save should not be called when saver is clean")

			return nil
		},
	}

	require.NoError(t, saver.saveLocked(time.Now()))
}

func TestRepairIndexPathMismatches_CurrentValidationError(t *testing.T) {
	h := &Handler{}
	path := filepath.Join(t.TempDir(), "child")
	withValidateStoredPathStub(t, func(candidate string) error {
		if candidate == path {
			return errors.New("broken state")
		}

		return nil
	})

	err := h.repairIndexPathMismatches(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken state")
}

func TestRepairIndexPathMismatches_AncestorValidationError(t *testing.T) {
	h := &Handler{}
	root := t.TempDir()
	path := filepath.Join(root, "child")
	parent := filepath.Dir(path)

	withValidateStoredPathStub(t, func(candidate string) error {
		if candidate == parent {
			return errors.New("ancestor broken")
		}

		return nil
	})

	err := h.repairIndexPathMismatches(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ancestor broken")
}

func TestRepairStoredPathMismatch_ValidateError(t *testing.T) {
	h := &Handler{}
	path := t.TempDir()
	withValidateStoredPathStub(t, func(string) error {
		return errors.New("invalid state")
	})

	handled, err := h.repairStoredPathMismatch(context.Background(), path, map[string]struct{}{}, map[string]struct{}{})
	require.False(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate stored path")
	assert.Contains(t, err.Error(), "invalid state")
}

func TestRepairStoredPathMismatch_ClearIndexError(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	path := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(path), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(path), "state.json"), []byte(`{"path":"`+storedPath+`"}`), 0o644))

	mc.On("DropCollection", mock.Anything, snapshot.CollectionName(storedPath)).Return(errors.New("drop failed")).Once()

	handled, err := h.repairStoredPathMismatch(context.Background(), path, map[string]struct{}{}, map[string]struct{}{})
	require.True(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clear stale index")
	assert.Contains(t, err.Error(), "drop failed")
}

func TestRepairStoredPathMismatch_ReusesClearedStoredPath(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	h := New(mc, sm, cfg, sp, nil)

	path := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(path), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(path), "state.json"), []byte(`{"path":"`+storedPath+`"}`), 0o644))

	cleanedCurrentPaths := map[string]struct{}{}
	handled, err := h.repairStoredPathMismatch(context.Background(), path, map[string]struct{}{storedPath: {}}, cleanedCurrentPaths)
	require.True(t, handled)
	require.NoError(t, err)
	assert.Contains(t, cleanedCurrentPaths, path)
	_, statErr := os.Stat(filepath.Join(snapshot.MetadataDirPath(path), "state.json"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestRepairStoredPathMismatch_AlreadyCleanedReturnsNil(t *testing.T) {
	h := &Handler{snapshot: snapshot.NewManager()}
	path := t.TempDir()
	storedPath := filepath.Join(t.TempDir(), "old-root")
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(path), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(path), "state.json"), []byte(`{"path":"`+storedPath+`"}`), 0o644))

	handled, err := h.repairStoredPathMismatch(context.Background(), path, map[string]struct{}{storedPath: {}}, map[string]struct{}{path: {}})
	require.True(t, handled)
	require.NoError(t, err)
}

func TestBuildRelativePathFilter_RelativeError(t *testing.T) {
	requestedPath := filepath.Join(t.TempDir(), "pkg")

	filter, err := buildRelativePathFilter("", requestedPath)
	require.Error(t, err)
	assert.Empty(t, filter)
	assert.Contains(t, err.Error(), "compute relative search path")
}

func TestBuildRelativePathFilter_TrailingSlashTreatsDotAsRoot(t *testing.T) {
	root := t.TempDir()

	filter, err := buildRelativePathFilter(root, root+string(os.PathSeparator))
	require.NoError(t, err)
	assert.Empty(t, filter)
}

func TestBuildRelativePathFilter_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	filter, err := buildRelativePathFilter(root, outside)
	require.Error(t, err)
	assert.Empty(t, filter)
	assert.ErrorIs(t, err, errSearchPathOutsideRoot)
}
