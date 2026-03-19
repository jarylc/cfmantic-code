package filesync

import (
	"bytes"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/mocks"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/walker"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSyncRunParams_LoadOldHashesMissingFileReturnsEmptyMap(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	params := mgr.syncRunParams(path, nil)
	require.NotNil(t, params)
	assert.Equal(t, path, params.Boundary.Path)
	hashes, err := params.LoadOldHashes()
	require.NoError(t, err)
	assert.Empty(t, hashes.Files)
}

func TestSyncRunParams_LoadOldHashesReturnsError(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	hashPath := HashFilePath(path)
	require.NoError(t, os.MkdirAll(filepath.Dir(hashPath), 0o755))
	require.NoError(t, os.WriteFile(hashPath, []byte("{"), 0o644))

	params := mgr.syncRunParams(path, nil)
	require.NotNil(t, params)

	hashes, err := params.LoadOldHashes()
	assert.Nil(t, hashes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal hashes")
}

func TestSyncRunParams_ProcessFilesReportsTrackerProgress(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	status := newRecordingStatusManager()
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(mc, status, sp, cfg, 300)
	path := t.TempDir()
	file := writeRunCodeFile(t, path, "main.go")

	expectSplitOneChunk(t, sp, "main.go")
	mc.On("Insert", mock.Anything, snapshot.CollectionName(path), mock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	params := mgr.syncRunParams(path, snapshot.NewTracker(status, path, snapshot.OperationMetadata{
		Operation: "indexing",
		Source:    "background_sync",
		Mode:      "auto-sync",
	}))

	result := params.ProcessFiles([]walker.CodeFile{file}, nil)
	require.Empty(t, result.Err)
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, map[string]int{"main.go": 1}, result.ChunkCounts)
	require.NotEmpty(t, status.progress)
	assert.Equal(t, snapshot.Progress{
		FilesDone:      1,
		FilesTotal:     1,
		ChunksTotal:    1,
		ChunksInserted: 1,
	}, status.progress[len(status.progress)-1])
}

func TestSyncRunParams_OnIndexedUsesSnapshotWithoutTracker(t *testing.T) {
	cfg := testConfig(t)
	status := newRecordingStatusManager()
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		status,
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	status.SetIndexed(path, 1, 2)

	params := mgr.syncRunParams(path, nil)
	params.OnIndexed(3, 7)

	info := status.GetInfo(path)
	require.NotNil(t, info)
	assert.Equal(t, snapshot.StatusIndexed, info.Status)
	assert.Equal(t, 3, info.IndexedFiles)
	assert.Equal(t, 7, info.TotalChunks)
}

func TestSyncRunParams_AfterSuccessSkipsLogWhenSnapshotPersistenceFails(t *testing.T) {
	cfg := testConfig(t)
	status := snapshot.NewManager()
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		status,
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(snapshot.MetadataDirPath(path), "state.json.tmp"), 0o755))
	status.SetIndexed(path, 1, 2)

	require.Equal(t, snapshot.StatusFailed, status.GetStatus(path))

	params := mgr.syncRunParams(path, nil)
	require.NotNil(t, params.AfterSuccess)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	params.AfterSuccess()

	assert.NotContains(t, buf.String(), "sync: completed sync for "+path)
}

func TestSyncRunParams_ModifiedFileChunkCallbacksUseSafeFilters(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	mgr := NewManager(
		mc,
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	params := mgr.syncRunParams(path, nil)

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 3).Return([]milvus.Entity{{ID: "chunk-a"}, {ID: "chunk-b"}}, nil).Once()

	ids, err := params.QueryFileChunkIDs("main.go", 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk-a", "chunk-b"}, ids)

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale-a","chunk-stale-b"]`).Return(nil).Once()
	require.NoError(t, params.DeleteChunkIDs([]string{"chunk-stale-a", "chunk-stale-b"}))

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale"]`).Return(nil).Once()
	require.NoError(t, params.DeleteChunkID("chunk-stale"))
}

func TestSyncRunParamsWithContext_UsesProvidedContext(t *testing.T) {
	type ctxKey struct{}

	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	sp := mocks.NewMockSplitter(t)
	mgr := NewManager(
		mc,
		newRecordingStatusManager(),
		sp,
		cfg,
		300,
	)

	path := t.TempDir()
	file := writeRunCodeFile(t, path, "main.go")
	collection := snapshot.CollectionName(path)
	ctx := context.WithValue(context.Background(), ctxKey{}, "background-sync")
	params := mgr.syncRunParamsWithContext(ctx, path, nil)

	matchCtx := mock.MatchedBy(func(actual context.Context) bool {
		return actual.Value(ctxKey{}) == "background-sync"
	})

	mc.On("Query", matchCtx, collection, `relativePath == "main.go"`, 1).
		Return([]milvus.Entity{{ID: "chunk-a"}}, nil).
		Once()

	ids, err := params.QueryFileChunkIDs("main.go", 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk-a"}, ids)

	mc.On("Delete", matchCtx, collection, `relativePath == "main.go"`).Return(nil).Once()
	require.NoError(t, params.DeleteFile("main.go"))

	mc.On("Delete", matchCtx, collection, `id in ["chunk-a"]`).Return(nil).Once()
	require.NoError(t, params.DeleteChunkID("chunk-a"))

	expectSplitOneChunk(t, sp, "main.go")
	mc.On("Insert", matchCtx, collection, mock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).
		Once()

	result := params.ProcessFiles([]walker.CodeFile{file}, nil)
	require.Empty(t, result.Err)
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, map[string]int{"main.go": 1}, result.ChunkCounts)
}

func TestSyncRunParamsWithContext_WalkFilesUsesProvidedContext(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(
		mocks.NewMockVectorClient(t),
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := mgr.syncRunParamsWithContext(ctx, path, nil)

	files, err := params.WalkFiles()
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, files)
}

func TestSyncRunParams_QueryFileChunkIDsHandlesLimitsAndErrors(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	mgr := NewManager(
		mc,
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	params := mgr.syncRunParams(path, nil)

	ids, err := params.QueryFileChunkIDs("main.go", 0)
	require.NoError(t, err)
	assert.Nil(t, ids)

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 2).Return(nil, errors.New("boom")).Once()

	ids, err = params.QueryFileChunkIDs("main.go", 2)
	assert.Nil(t, ids)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query chunks for main.go")

	mc.On("Query", mock.Anything, collection, `relativePath == "main.go"`, 3).Return([]milvus.Entity{{ID: ""}, {ID: "chunk-a"}}, nil).Once()

	ids, err = params.QueryFileChunkIDs("main.go", 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk-a"}, ids)
}

func TestSyncRunParams_DeleteChunkIDErrorIsWrapped(t *testing.T) {
	cfg := testConfig(t)
	mc := mocks.NewMockVectorClient(t)
	mgr := NewManager(
		mc,
		newRecordingStatusManager(),
		mocks.NewMockSplitter(t),
		cfg,
		300,
	)

	path := t.TempDir()
	collection := snapshot.CollectionName(path)
	params := mgr.syncRunParams(path, nil)

	mc.On("Delete", mock.Anything, collection, `id in ["chunk-stale"]`).Return(errors.New("boom")).Once()

	err := params.DeleteChunkID("chunk-stale")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete chunk chunk-stale")
}

func TestSyncRunParams_TrackerFailureCallbacks(t *testing.T) {
	meta := snapshot.OperationMetadata{Operation: "indexing", Source: "background_sync", Mode: "auto-sync"}

	t.Run("walk error", func(t *testing.T) {
		cfg := testConfig(t)
		status := newRecordingStatusManager()
		mgr := NewManager(mocks.NewMockVectorClient(t), status, mocks.NewMockSplitter(t), cfg, 300)
		path := t.TempDir()

		params := mgr.syncRunParamsWithContext(context.Background(), path, snapshot.NewTracker(status, path, meta))
		params.OnWalkError(errors.New("walk boom"))

		info := status.GetInfo(path)
		require.NotNil(t, info)
		assert.Equal(t, snapshot.StatusFailed, info.Status)
		assert.Contains(t, info.ErrorMessage, "sync: walk failed: walk boom")
		assert.Contains(t, status.steps, "Walking files")
	})

	t.Run("delete error", func(t *testing.T) {
		cfg := testConfig(t)
		status := newRecordingStatusManager()
		mgr := NewManager(mocks.NewMockVectorClient(t), status, mocks.NewMockSplitter(t), cfg, 300)
		path := t.TempDir()

		params := mgr.syncRunParamsWithContext(context.Background(), path, snapshot.NewTracker(status, path, meta))
		params.OnDeleteError(errors.New("delete boom"))

		info := status.GetInfo(path)
		require.NotNil(t, info)
		assert.Equal(t, snapshot.StatusFailed, info.Status)
		assert.Equal(t, "sync: delete failed: delete boom", info.ErrorMessage)
	})

	t.Run("save manifest error", func(t *testing.T) {
		cfg := testConfig(t)
		status := newRecordingStatusManager()
		mgr := NewManager(mocks.NewMockVectorClient(t), status, mocks.NewMockSplitter(t), cfg, 300)
		path := t.TempDir()

		params := mgr.syncRunParamsWithContext(context.Background(), path, snapshot.NewTracker(status, path, meta))
		params.OnSaveManifestError(errors.New("save boom"))

		info := status.GetInfo(path)
		require.NotNil(t, info)
		assert.Equal(t, snapshot.StatusFailed, info.Status)
		assert.Contains(t, info.ErrorMessage, "sync: save hashes failed: save boom")
	})
}

func TestSyncRunParams_CanceledDeleteErrorDoesNotFailStatus(t *testing.T) {
	cfg := testConfig(t)
	status := newRecordingStatusManager()
	mgr := NewManager(mocks.NewMockVectorClient(t), status, mocks.NewMockSplitter(t), cfg, 300)
	path := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := mgr.syncRunParamsWithContext(ctx, path, nil)
	params.OnDeleteError(errors.New("delete boom"))

	assert.Nil(t, status.GetInfo(path))
}
