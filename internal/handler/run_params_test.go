package handler

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/mocks"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"os"
	"path/filepath"
	"testing"

	filesync "cfmantic-code/internal/sync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFullRunParams_ExtraCleanupReleasesSemaphore(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(snapshot.NewManager(), path, snapshot.OperationMetadata{})
	params := h.fullRunParams(context.Background(), path, snapshot.CollectionName(path), nil, tracker)
	require.NotNil(t, params)
	require.Len(t, params.Boundary.ExtraCleanups, 1)

	h.indexSem <- struct{}{}

	params.Boundary.ExtraCleanups[0]()

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore was not released")
	}
}

func TestIncrementalRunParams_ExtraCleanupReleasesSemaphore(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(snapshot.NewManager(), path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)
	require.NotNil(t, params)
	require.Len(t, params.Boundary.ExtraCleanups, 1)

	h.indexSem <- struct{}{}

	params.Boundary.ExtraCleanups[0]()

	select {
	case h.indexSem <- struct{}{}:
		<-h.indexSem
	default:
		t.Fatal("index semaphore was not released")
	}
}

func TestFullRunParams_WalkFilesUsesProvidedContext(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(snapshot.NewManager(), path, snapshot.OperationMetadata{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := h.fullRunParams(ctx, path, snapshot.CollectionName(path), nil, tracker)

	files, err := params.WalkFiles()
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, files)
}

func TestStartManualIndex_DetachedParentPreservesValues(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	type ctxKey struct{}

	path := t.TempDir()
	parent, parentCancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "request-only"))
	indexCtx, cleanup := h.startManualIndex(context.WithoutCancel(parent), path)
	t.Cleanup(cleanup)

	parentCancel()

	assert.Equal(t, "request-only", indexCtx.Value(ctxKey{}))

	select {
	case <-indexCtx.Done():
		t.Fatal("manual index context should not inherit parent cancellation")
	default:
	}
}

func TestHandlerProcessFilesFlushesTrackerProgress(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := snapshot.NewManager()
	sp := mocks.NewMockSplitter(t)
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	filePath := filepath.Join(path, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("package main\n"), 0o644))

	collection := snapshot.CollectionName(path)

	expectSplitChunks(t, sp, "main.go", []splitter.Chunk{{Content: "package main", StartLine: 1, EndLine: 1}})
	mc.On("Insert", mock.Anything, collection, mock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{Operation: "indexing", Source: "manual", Mode: "full"})
	result := h.processFiles(context.Background(), path, collection, []walker.CodeFile{{RelPath: "main.go", AbsPath: filePath, Extension: ".go"}}, nil, false, tracker)
	require.Empty(t, result.err)

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, 1, info.FilesDone)
	assert.Equal(t, 1, info.FilesTotal)
	assert.Equal(t, 1, info.ChunksTotal)
	assert.Equal(t, 1, info.ChunksInserted)
}

func TestFullRunParams_AfterSuccessPersistsEffectiveIgnorePatterns(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)
	h.cfg.CustomIgnore = []string{"config-only/"}

	path := t.TempDir()
	sm.SetIndexed(path, 1, 2)

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.fullRunParams(context.Background(), path, snapshot.CollectionName(path), []string{"request-only/"}, tracker)
	require.NotNil(t, params)
	require.NotNil(t, params.AfterSuccess)

	params.AfterSuccess()

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	require.NotNil(t, info.IgnorePatterns)
	assert.Equal(t, []string{"config-only/", "request-only/"}, *info.IgnorePatterns)
}

func TestIncrementalRunParams_AfterSuccessPersistsEffectiveIgnorePatterns(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)
	h.cfg.CustomIgnore = []string{"config-only/"}

	path := t.TempDir()
	sm.SetIndexed(path, 1, 2)

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, []string{"request-only/"}, tracker)
	require.NotNil(t, params)
	require.NotNil(t, params.AfterSuccess)

	params.AfterSuccess()

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	require.NotNil(t, info.IgnorePatterns)
	assert.Equal(t, []string{"config-only/", "request-only/"}, *info.IgnorePatterns)
}

func prepareFailedSnapshotForAfterSuccess(t *testing.T, sm *snapshot.Manager, path string) {
	t.Helper()

	conflict := filepath.Join(snapshot.MetadataDirPath(path), "state.json.tmp")
	require.NoError(t, os.MkdirAll(conflict, 0o755))

	sm.SetIndexed(path, 1, 2)

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "failed to persist indexed state")
}

func TestFullRunParams_AfterSuccessSkipsFollowUpsOnPersistenceFailure(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	path := t.TempDir()
	prepareFailedSnapshotForAfterSuccess(t, sm, path)

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.fullRunParams(context.Background(), path, snapshot.CollectionName(path), []string{"request-only/"}, tracker)
	require.NotNil(t, params.AfterSuccess)

	params.AfterSuccess()

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Nil(t, info.IgnorePatterns)

	trackedParent, ok := syncMgr.TrackedParent(filepath.Join(path, "child"))
	assert.False(t, ok)
	assert.Empty(t, trackedParent)
}

func TestIncrementalRunParams_AfterSuccessSkipsFollowUpsOnPersistenceFailure(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	path := t.TempDir()
	prepareFailedSnapshotForAfterSuccess(t, sm, path)

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, []string{"request-only/"}, tracker)
	require.NotNil(t, params.AfterSuccess)

	params.AfterSuccess()

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Nil(t, info.IgnorePatterns)

	trackedParent, ok := syncMgr.TrackedParent(filepath.Join(path, "child"))
	assert.False(t, ok)
	assert.Empty(t, trackedParent)
}

func TestIncrementalRunParams_ModifiedFileChunkCallbacksUseSafeFilters(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)
	require.NotNil(t, params)

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 2).Return([]milvus.Entity{{ID: "chunk-a"}, {ID: "chunk-b"}}, nil).Once()

	ids, err := params.QueryFileChunkIDs("main.go", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk-a", "chunk-b"}, ids)

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale-a","chunk-stale-b"]`).Return(nil).Once()
	require.NoError(t, params.DeleteChunkIDs([]string{"chunk-stale-a", "chunk-stale-b"}))

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale"]`).Return(nil).Once()
	require.NoError(t, params.DeleteChunkID("chunk-stale"))
}

func TestFullRunParams_OnManifestErrorMarksTrackerFailed(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.fullRunParams(context.Background(), path, snapshot.CollectionName(path), nil, tracker)
	require.NotNil(t, params)
	require.NotNil(t, params.OnManifestError)

	params.OnManifestError(assert.AnError)

	info := sm.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusFailed, info.Status)
	assert.Contains(t, info.ErrorMessage, "computing file hashes")
}

func TestIncrementalRunParams_QueryFileChunkIDs_LimitZeroReturnsNil(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)

	ids, err := params.QueryFileChunkIDs("main.go", 0)
	require.NoError(t, err)
	assert.Nil(t, ids)
}

func TestIncrementalRunParams_QueryFileChunkIDs_Error(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 1).Return([]milvus.Entity(nil), assert.AnError).Once()

	ids, err := params.QueryFileChunkIDs("main.go", 1)
	require.Error(t, err)
	assert.Nil(t, ids)
	assert.Contains(t, err.Error(), "query chunks for main.go")
}

func TestIncrementalRunParams_QueryFileChunkIDs_SkipsEmptyIDs(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 3).Return([]milvus.Entity{{ID: ""}, {ID: "chunk-a"}, {ID: "chunk-b"}}, nil).Once()

	ids, err := params.QueryFileChunkIDs("main.go", 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk-a", "chunk-b"}, ids)
}

func TestIncrementalRunParams_DeleteChunkID_Error(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale"]`).Return(assert.AnError).Once()

	err := params.DeleteChunkID("chunk-stale")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete chunk chunk-stale")
}

func TestIncrementalRunParams_CurrentTotalChunksWithoutSnapshotInfo(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	h := New(mc, sm, loadTestConfig(t), sp, nil)

	path := t.TempDir()
	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)

	total, ok := params.CurrentTotalChunks()
	assert.False(t, ok)
	assert.Zero(t, total)
}

func TestIncrementalRunParams_AfterSuccessTracksPathWithSyncMgr(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	sm := snapshot.NewManager()
	cfg := loadTestConfig(t)
	syncMgr := filesync.NewManager(mc, sm, sp, cfg, 300)
	h := New(mc, sm, cfg, sp, syncMgr)

	path := t.TempDir()
	sm.SetIndexed(path, 1, 2)

	tracker := snapshot.NewTracker(sm, path, snapshot.OperationMetadata{})
	params := h.incrementalRunParams(context.Background(), path, nil, tracker)
	require.NotNil(t, params.AfterSuccess)

	params.AfterSuccess()

	parent, ok := syncMgr.TrackedParent(filepath.Join(path, "child"))
	assert.True(t, ok)
	assert.Equal(t, path, parent)
}
