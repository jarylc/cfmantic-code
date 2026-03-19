package pipeline_test

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockVectorClient struct {
	testifymock.Mock
}

func mockError(args testifymock.Arguments, index int) error {
	if got := args.Get(index); got != nil {
		err, _ := got.(error)
		return err
	}

	return nil
}

func newMockVectorClient(t *testing.T) *mockVectorClient {
	t.Helper()

	mock := &mockVectorClient{}

	t.Cleanup(func() {
		mock.AssertExpectations(t)
	})

	return mock
}

func (m *mockVectorClient) Insert(ctx context.Context, collection string, entities []milvus.Entity) (*milvus.InsertResult, error) {
	args := m.Called(ctx, collection, entities)
	result, _ := args.Get(0).(*milvus.InsertResult)

	return result, mockError(args, 1)
}

type mockSplitter struct {
	testifymock.Mock
}

func newMockSplitter(t *testing.T) *mockSplitter {
	t.Helper()

	mock := &mockSplitter{}

	t.Cleanup(func() {
		mock.AssertExpectations(t)
	})

	return mock
}

func (m *mockSplitter) Split(reader io.Reader, filePath string, emit splitter.EmitChunkFunc) error {
	args := m.Called(reader, filePath, emit)

	return mockError(args, 0)
}

// makeFile creates a real file with the given content in t.TempDir() and returns a CodeFile.
func makeFile(t *testing.T, dir, name, content string) walker.CodeFile {
	t.Helper()

	abs := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))

	ext := filepath.Ext(name)

	return walker.CodeFile{AbsPath: abs, RelPath: name, Extension: ext}
}

// baseConfig returns a Config with the given collection and concurrency suitable for tests.
func baseConfig(collection, codebasePath string) pipeline.Config {
	return pipeline.Config{
		Concurrency:     2,
		InsertBatchSize: 100,
		Collection:      collection,
		CodebasePath:    codebasePath,
	}
}

func expectSplit(tb testing.TB, sp *mockSplitter, filePath any, chunks []splitter.Chunk) {
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

func expectSplitError(sp *mockSplitter, filePath any, err error) {
	sp.On("Split", testifymock.Anything, filePath, testifymock.Anything).Return(err)
}

// ─── Run: flush-remaining path (batch < batchSize) ───────────────────────────

func TestRun_FlushRemainingBatch(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "hello", StartLine: 1, EndLine: 1}
	expectSplit(t, sp, "main.go", []splitter.Chunk{chunk})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil)

	files := []walker.CodeFile{makeFile(t, dir, "main.go", "hello\n")}
	cfg := baseConfig("col", dir)

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, 1, result.ChunkCounts["main.go"])
}

// ��── Run: batch-loop path (batch >= batchSize triggers mid-loop insert) ───────

func TestRun_BatchLoopInsert(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk1 := splitter.Chunk{Content: "chunk1", StartLine: 1, EndLine: 1}
	chunk2 := splitter.Chunk{Content: "chunk2", StartLine: 2, EndLine: 2}
	expectSplit(t, sp, "a.go", []splitter.Chunk{chunk1, chunk2})
	// batchSize=1 → each chunk triggers a separate insert.
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Times(2)

	files := []walker.CodeFile{makeFile(t, dir, "a.go", "ab\n")}

	cfg := pipeline.Config{
		Concurrency:     2,
		InsertBatchSize: 1,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalChunks)
	assert.Equal(t, 2, result.ChunkCounts["a.go"])
}

// ─── Run: insert error ────────────────────────────────────────────────────────

func TestRun_InsertError(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunks := []splitter.Chunk{
		{Content: "x1", StartLine: 1, EndLine: 1},
		{Content: "x2", StartLine: 2, EndLine: 2},
		{Content: "x3", StartLine: 3, EndLine: 3},
	}
	expectSplit(t, sp, "b.go", chunks)

	var insertCalls atomic.Int32

	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Run(func(testifymock.Arguments) {
			insertCalls.Add(1)
		}).
		Return(nil, errors.New("quota exceeded"))

	files := []walker.CodeFile{makeFile(t, dir, "b.go", "x\n")}
	cfg := pipeline.Config{
		Concurrency:       1,
		InsertConcurrency: 1,
		InsertBatchSize:   1,
		Collection:        "col",
		CodebasePath:      dir,
	}

	_, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	assert.Equal(t, int32(1), insertCalls.Load(), "pipeline should stop scheduling inserts after the first insert error")
}

func TestRun_SplitError(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	expectSplitError(sp, "broken.go", errors.New("split failed"))

	var laterSplitCalled atomic.Bool

	sp.On("Split", testifymock.Anything, "later.go", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			laterSplitCalled.Store(true)

			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(t, ok)
			require.NoError(t, emit(splitter.Chunk{Content: "later", StartLine: 1, EndLine: 1}))
		}).
		Return(nil).
		Maybe()
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).
		Maybe()

	files := []walker.CodeFile{
		makeFile(t, dir, "broken.go", "x\n"),
		makeFile(t, dir, "later.go", "later\n"),
	}
	cfg := baseConfig("col", dir)
	cfg.Concurrency = 1

	_, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "split failed")
	assert.False(t, laterSplitCalled.Load(), "pipeline should stop processing later files after the first split/build error")
	mc.AssertNotCalled(t, "Insert", testifymock.Anything, "col", testifymock.Anything)
}

func TestRun_SendResultCanceledAfterConcurrentStop(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	insertCtxDone := make(chan (<-chan struct{}), 1)

	sp.On("Split", testifymock.Anything, "blocked.go", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(t, ok)
			require.NoError(t, emit(splitter.Chunk{Content: "first", StartLine: 1, EndLine: 1}))

			ctxDone := <-insertCtxDone
			<-ctxDone

			_ = emit(splitter.Chunk{Content: "second", StartLine: 2, EndLine: 2})
		}).
		Return(context.Canceled).
		Once()

	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)

			insertCtxDone <- ctx.Done()
		}).
		Return(nil, errors.New("quota exceeded")).
		Once()

	files := []walker.CodeFile{
		makeFile(t, dir, "blocked.go", "blocked\n"),
	}
	cfg := baseConfig("col", dir)
	cfg.Concurrency = 1
	cfg.InsertBatchSize = 1

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, 1, result.ChunkCounts["blocked.go"])
}

func TestRun_EmptyChunkResultReturnsQuietlyAfterConcurrentStop(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	insertCtxDone := make(chan (<-chan struct{}), 1)

	sp.On("Split", testifymock.Anything, "first.go", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(t, ok)
			require.NoError(t, emit(splitter.Chunk{Content: "first", StartLine: 1, EndLine: 1}))
		}).
		Return(nil).
		Once()

	sp.On("Split", testifymock.Anything, "empty.go", testifymock.Anything).
		Run(func(testifymock.Arguments) {
			ctxDone := <-insertCtxDone
			<-ctxDone
		}).
		Return(nil).
		Once()

	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)

			insertCtxDone <- ctx.Done()
		}).
		Return(nil, errors.New("quota exceeded")).
		Once()

	files := []walker.CodeFile{
		makeFile(t, dir, "first.go", "first\n"),
		makeFile(t, dir, "empty.go", "\n"),
	}
	cfg := baseConfig("col", dir)
	cfg.Concurrency = 2
	cfg.InsertBatchSize = 1

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	assert.Equal(t, 1, result.TotalChunks)
}

func TestRun_StopBeforeFileDoneResultReturnsInsertError(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	insertCtxDone := make(chan (<-chan struct{}), 1)

	sp.On("Split", testifymock.Anything, "first.go", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(t, ok)
			require.NoError(t, emit(splitter.Chunk{Content: "first", StartLine: 1, EndLine: 1}))

			ctxDone := <-insertCtxDone
			<-ctxDone
		}).
		Return(nil).
		Once()

	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)

			insertCtxDone <- ctx.Done()
		}).
		Return(nil, errors.New("quota exceeded")).
		Once()

	files := []walker.CodeFile{
		makeFile(t, dir, "first.go", "first\n"),
		makeFile(t, dir, "second.go", "second\n"),
		makeFile(t, dir, "third.go", "third\n"),
		makeFile(t, dir, "fourth.go", "fourth\n"),
		makeFile(t, dir, "fifth.go", "fifth\n"),
	}
	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 1,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, 1, result.ChunkCounts["first.go"])
	assert.Empty(t, result.CompletedFiles)
}

// ─── Run: empty files list ────────────────────────────────────────────────────

func TestRun_EmptyFiles(t *testing.T) {
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	cfg := baseConfig("col", "/some/path")

	result, err := pipeline.Run(context.Background(), &cfg, nil, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalChunks)
	assert.Empty(t, result.ChunkCounts)
}

// ─── Run: OnResultsDrained callback ──────────────────────────────────────────

func TestRun_OnResultsDrainedCalled(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "y", StartLine: 1, EndLine: 1}
	expectSplit(t, sp, testifymock.Anything, []splitter.Chunk{chunk})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil)

	called := false

	cfg := pipeline.Config{
		Concurrency:      2,
		InsertBatchSize:  100,
		Collection:       "col",
		CodebasePath:     dir,
		OnResultsDrained: func() { called = true },
	}

	files := []walker.CodeFile{makeFile(t, dir, "c.go", "y\n")}

	_, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.True(t, called)
}

// ─── Run: unreadable file fails the run ──────────────────────────────────────

func TestRun_UnreadableFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	var laterSplitCalled atomic.Bool

	sp.On("Split", testifymock.Anything, "later.go", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			laterSplitCalled.Store(true)

			emit, ok := args.Get(2).(splitter.EmitChunkFunc)
			require.True(t, ok)
			require.NoError(t, emit(splitter.Chunk{Content: "later", StartLine: 1, EndLine: 1}))
		}).
		Return(nil).
		Maybe()
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).
		Maybe()

	files := []walker.CodeFile{
		{AbsPath: "/nonexistent/path/missing.go", RelPath: "missing.go", Extension: ".go"},
		makeFile(t, dir, "later.go", "later\n"),
	}
	cfg := baseConfig("col", dir)
	cfg.Concurrency = 1

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.go")
	assert.Contains(t, err.Error(), "no such file or directory")
	assert.False(t, laterSplitCalled.Load(), "pipeline should stop processing later files after the first open error")
	assert.Equal(t, 0, result.TotalChunks)
	assert.Empty(t, result.ChunkCounts)
	mc.AssertNotCalled(t, "Insert", testifymock.Anything, "col", testifymock.Anything)
}

// ─── Run: worker skips file whose splitter returns no chunks ─────────────────

func TestRun_EmptyChunksSkipped(t *testing.T) {
	// Splitter returns an empty slice — worker continues without sending a result.
	// No insert should occur and TotalChunks must be zero.
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	expectSplit(t, sp, "empty.go", []splitter.Chunk{})

	files := []walker.CodeFile{makeFile(t, dir, "empty.go", "// no chunks\n")}
	cfg := baseConfig("col", dir)

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalChunks)
	assert.Empty(t, result.ChunkCounts)
}

// ─── Run: OnProgress callback ─────────────────────────────────────────────────

func TestRunOnProgressCalled(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	type progressSnapshot struct {
		filesDone      int
		filesTotal     int
		chunksTotal    int
		chunksInserted int
	}

	chunk1 := splitter.Chunk{Content: "func a() {}", StartLine: 1, EndLine: 1}
	chunk2 := splitter.Chunk{Content: "func b() {}", StartLine: 2, EndLine: 2}
	chunk3 := splitter.Chunk{Content: "func c() {}", StartLine: 3, EndLine: 3}

	expectSplit(t, sp, "a.go", []splitter.Chunk{chunk1, chunk2})
	expectSplit(t, sp, "b.go", []splitter.Chunk{chunk3})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil)

	var (
		mu           sync.Mutex
		lastProgress progressSnapshot
	)

	cfg := pipeline.Config{
		Concurrency:     2,
		InsertBatchSize: 100,
		Collection:      "col",
		CodebasePath:    dir,
		OnProgress: func(filesDone, filesTotal, chunksTotal, chunksInserted int) {
			mu.Lock()
			lastProgress = progressSnapshot{
				filesDone:      filesDone,
				filesTotal:     filesTotal,
				chunksTotal:    chunksTotal,
				chunksInserted: chunksInserted,
			}
			mu.Unlock()
		},
	}

	files := []walker.CodeFile{
		makeFile(t, dir, "a.go", "func a() {}\nfunc b() {}\n"),
		makeFile(t, dir, "b.go", "func c() {}\n"),
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalChunks)

	mu.Lock()
	lastProgressSnapshot := lastProgress
	mu.Unlock()

	// Final progress call must reflect all files done and all chunks accounted for.
	assert.Equal(t, 2, lastProgressSnapshot.filesDone)
	assert.Equal(t, 2, lastProgressSnapshot.filesTotal)
	assert.Equal(t, 3, lastProgressSnapshot.chunksTotal)
	assert.Equal(t, 3, lastProgressSnapshot.chunksInserted)
}

// ─── Run: OnFileIndexed callback ─────────────────────────────────────────────

func TestRunOnFileIndexedCallback(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "x", StartLine: 1, EndLine: 1}
	expectSplit(t, sp, "a.go", []splitter.Chunk{chunk})
	expectSplit(t, sp, "b.go", []splitter.Chunk{chunk})
	expectSplit(t, sp, "c.go", []splitter.Chunk{chunk})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 3}, nil)

	var mu sync.Mutex

	called := make(map[string]int)

	cfg := pipeline.Config{
		Concurrency:     2,
		InsertBatchSize: 100, // all 3 entities in one batch
		Collection:      "col",
		CodebasePath:    dir,
		OnFileIndexed: func(relPath string, chunkCount int) {
			mu.Lock()
			called[relPath] = chunkCount
			mu.Unlock()
		},
	}

	files := []walker.CodeFile{
		makeFile(t, dir, "a.go", "x\n"),
		makeFile(t, dir, "b.go", "x\n"),
		makeFile(t, dir, "c.go", "x\n"),
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalChunks)

	// OnFileIndexed must be called once per file with correct chunk count.
	assert.Len(t, called, 3)
	assert.Equal(t, 1, called["a.go"])
	assert.Equal(t, 1, called["b.go"])
	assert.Equal(t, 1, called["c.go"])

	// Result.CompletedFiles must contain all 3 files.
	assert.Len(t, result.CompletedFiles, 3)
	assert.Equal(t, 1, result.CompletedFiles["a.go"])
	assert.Equal(t, 1, result.CompletedFiles["b.go"])
	assert.Equal(t, 1, result.CompletedFiles["c.go"])
}

// ─── Run: partial failure — CompletedFiles only has fully-inserted files ──────

func TestRunPartialFailure_CompletedFiles(t *testing.T) {
	// 1 file with 2 chunks, batchSize=1: first insert succeeds, second fails.
	// Since only 1/2 chunks inserted, the file is NOT complete.
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk1 := splitter.Chunk{Content: "chunk1", StartLine: 1, EndLine: 1}
	chunk2 := splitter.Chunk{Content: "chunk2", StartLine: 2, EndLine: 2}
	expectSplit(t, sp, "a.go", []splitter.Chunk{chunk1, chunk2})

	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).
		Return(nil, errors.New("quota exceeded")).Once()

	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 1, // each chunk is its own batch
		Collection:      "col",
		CodebasePath:    dir,
	}

	files := []walker.CodeFile{makeFile(t, dir, "a.go", "chunk1\nchunk2\n")}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
	// a.go had 2 expected chunks but only 1 was successfully inserted → NOT complete.
	assert.Empty(t, result.CompletedFiles, "partially-inserted file must not appear in CompletedFiles")
}

func TestRun_FileChunkIDsDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "x", StartLine: 1, EndLine: 1}
	expectSplit(t, sp, "main.go", []splitter.Chunk{chunk})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	files := []walker.CodeFile{makeFile(t, dir, "main.go", "x\n")}
	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 100,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	assert.Nil(t, result.FileChunkIDs)
}

func TestRun_CollectFileChunkIDsWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "x", StartLine: 1, EndLine: 1}
	expectSplit(t, sp, "a.go", []splitter.Chunk{chunk})
	expectSplit(t, sp, "b.go", []splitter.Chunk{chunk})
	mc.On("Insert", testifymock.Anything, "col", testifymock.Anything).Return(&milvus.InsertResult{InsertCount: 2}, nil).Once()

	files := []walker.CodeFile{
		makeFile(t, dir, "a.go", "x\n"),
		makeFile(t, dir, "b.go", "x\n"),
	}
	cfg := pipeline.Config{
		Concurrency:         1,
		InsertBatchSize:     100,
		Collection:          "col",
		CodebasePath:        dir,
		CollectFileChunkIDs: true,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"a.go": {pipeline.BuildEntity("a.go", ".go", dir, chunk).ID},
		"b.go": {pipeline.BuildEntity("b.go", ".go", dir, chunk).ID},
	}, result.FileChunkIDs)
}

// ─── BuildEntity: deterministic ID and field mapping ─────────────────────────

func TestBuildEntity_FieldsAndDeterministicID(t *testing.T) {
	chunk := splitter.Chunk{Content: "func main() {}", StartLine: 1, EndLine: 1}

	e := pipeline.BuildEntity("main.go", ".go", "/code", chunk)

	assert.NotEmpty(t, e.ID)
	assert.Equal(t, "func main() {}", e.Content)
	assert.Equal(t, "main.go", e.RelativePath)
	assert.Equal(t, 1, e.StartLine)
	assert.Equal(t, 1, e.EndLine)
	assert.Equal(t, "go", e.FileExtension) // leading dot stripped

	// Same inputs → same ID.
	e2 := pipeline.BuildEntity("main.go", ".go", "/code", chunk)
	assert.Equal(t, e.ID, e2.ID)

	// Different content → different ID.
	other := splitter.Chunk{Content: "other", StartLine: 1, EndLine: 1}
	e3 := pipeline.BuildEntity("main.go", ".go", "/code", other)
	assert.NotEqual(t, e.ID, e3.ID)
}

// ─── BuildEntity: extension stripping edge cases ──────────────────────────────

func TestBuildEntity_ExtensionStripping(t *testing.T) {
	chunk := splitter.Chunk{Content: "x", StartLine: 1, EndLine: 1}

	// Extension with leading dot → dot stripped.
	e1 := pipeline.BuildEntity("a.py", ".py", "/base", chunk)
	assert.Equal(t, "py", e1.FileExtension)

	// Extension without leading dot → unchanged (TrimPrefix no-op).
	e2 := pipeline.BuildEntity("a.py", "py", "/base", chunk)
	assert.Equal(t, "py", e2.FileExtension)

	// Empty extension → empty string.
	e3 := pipeline.BuildEntity("Makefile", "", "/base", chunk)
	assert.Empty(t, e3.FileExtension)
}

// ─── BuildEntity: metadata encodes codebasePath ───────────────────────────────

func TestBuildEntity_MetadataContainsCodebasePath(t *testing.T) {
	chunk := splitter.Chunk{Content: "code", StartLine: 5, EndLine: 10}
	codebasePath := "/home/user/myproject"

	e := pipeline.BuildEntity("pkg/lib.go", ".go", codebasePath, chunk)

	// Metadata must be valid JSON embedding the codebasePath.
	assert.Contains(t, e.Metadata, "codebasePath")
	assert.Contains(t, e.Metadata, codebasePath)

	// Different codebases produce the same ID (ID is path+lines+content, not codebasePath).
	e2 := pipeline.BuildEntity("pkg/lib.go", ".go", "/other/codebase", chunk)
	assert.Equal(t, e.ID, e2.ID, "ID must not depend on codebasePath")
	assert.NotEqual(t, e.Metadata, e2.Metadata, "Metadata must differ for different codebasePaths")
}

func TestRun_BelowPayloadLimitChunkRemainsUnchanged(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	chunk := splitter.Chunk{Content: "alpha\nbeta\ngamma", StartLine: 10, EndLine: 12}
	expectSplit(t, sp, "main.go", []splitter.Chunk{chunk})

	var inserted []milvus.Entity

	mc.On("Insert", testifymock.Anything, "col", testifymock.MatchedBy(func(entities []milvus.Entity) bool {
		inserted = append([]milvus.Entity(nil), entities...)
		return true
	})).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	files := []walker.CodeFile{makeFile(t, dir, "main.go", chunk.Content)}
	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 100,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	require.Len(t, inserted, 1)
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, chunk.Content, inserted[0].Content)
	assert.Equal(t, chunk.StartLine, inserted[0].StartLine)
	assert.Equal(t, chunk.EndLine, inserted[0].EndLine)
	assert.LessOrEqual(t, entityPayloadBytes(t, &inserted[0]), 10240)
}

func TestRun_OversizedChunkIsSplitWithinPayloadLimit(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	content := makeLargeChunkContent(90, 140)
	chunk := splitter.Chunk{Content: content, StartLine: 40, EndLine: 129}
	expectSplit(t, sp, "big.go", []splitter.Chunk{chunk})

	var inserted []milvus.Entity

	mc.On("Insert", testifymock.Anything, "col", testifymock.MatchedBy(func(entities []milvus.Entity) bool {
		inserted = append([]milvus.Entity(nil), entities...)
		return true
	})).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	files := []walker.CodeFile{makeFile(t, dir, "big.go", content)}
	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 100,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	require.Greater(t, len(inserted), 1, "oversized chunk should be re-split")
	assert.Equal(t, len(inserted), result.TotalChunks)
	assert.Equal(t, chunk.StartLine, inserted[0].StartLine)
	assert.Equal(t, chunk.EndLine, inserted[len(inserted)-1].EndLine)
	assertEntityContentMatchesOriginal(t, content, inserted)

	for i, entity := range inserted {
		assert.LessOrEqual(t, entityPayloadBytes(t, &entity), 10240, "entity[%d] still exceeds payload limit", i)
		assert.GreaterOrEqual(t, entity.StartLine, chunk.StartLine, "entity[%d] start line moved backwards", i)
		assert.LessOrEqual(t, entity.EndLine, chunk.EndLine, "entity[%d] end line moved past original chunk", i)
		assert.LessOrEqual(t, entity.StartLine, entity.EndLine, "entity[%d] has invalid line range", i)

		if i > 0 {
			assert.GreaterOrEqual(t, entity.StartLine, inserted[i-1].StartLine, "entity[%d] start line should be monotonic", i)
		}
	}
}

func TestRun_SingleLineOversizedChunkIsSplitWithinPayloadLimit(t *testing.T) {
	dir := t.TempDir()
	mc := newMockVectorClient(t)
	sp := newMockSplitter(t)

	content := makeJSONLikeSingleLineContent(16_000)
	chunk := splitter.Chunk{Content: content, StartLine: 7, EndLine: 7}
	expectSplit(t, sp, "single-line.json", []splitter.Chunk{chunk})

	var inserted []milvus.Entity

	mc.On("Insert", testifymock.Anything, "col", testifymock.MatchedBy(func(entities []milvus.Entity) bool {
		inserted = append([]milvus.Entity(nil), entities...)
		return true
	})).Return(&milvus.InsertResult{InsertCount: 1}, nil).Once()

	files := []walker.CodeFile{makeFile(t, dir, "single-line.json", content)}
	cfg := pipeline.Config{
		Concurrency:     1,
		InsertBatchSize: 100,
		Collection:      "col",
		CodebasePath:    dir,
	}

	result, err := pipeline.Run(context.Background(), &cfg, files, sp, mc)

	require.NoError(t, err)
	require.Greater(t, len(inserted), 1, "oversized single line should be re-split")
	assert.Equal(t, len(inserted), result.TotalChunks)
	assertEntityContentMatchesOriginal(t, content, inserted)

	for i, entity := range inserted {
		assert.LessOrEqual(t, entityPayloadBytes(t, &entity), 10240, "entity[%d] still exceeds payload limit", i)
		assert.Equal(t, 7, entity.StartLine, "entity[%d] should keep original start line", i)
		assert.Equal(t, 7, entity.EndLine, "entity[%d] should keep original end line", i)
	}
}

func entityPayloadBytes(t *testing.T, entity *milvus.Entity) int {
	t.Helper()

	payload, err := json.Marshal(entity)
	require.NoError(t, err)

	return len(payload)
}

func makeLargeChunkContent(lineCount, lineWidth int) string {
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%03d:%s", i+1, strings.Repeat("x", lineWidth))
	}

	return strings.Join(lines, "\n")
}

func makeJSONLikeSingleLineContent(minBytes int) string {
	var builder strings.Builder
	builder.WriteString(`{"payload":[`)

	for i := 0; builder.Len() < minBytes; i++ {
		if i > 0 {
			builder.WriteByte(',')
		}

		fmt.Fprintf(&builder, `"item-%06d-value-%06d"`, i, i*i)
	}

	builder.WriteString(`]}`)

	return builder.String()
}

func assertEntityContentMatchesOriginal(t *testing.T, original string, entities []milvus.Entity) {
	t.Helper()

	originalRunes := []rune(original)
	consumed := 0

	for i, entity := range entities {
		partRunes := []rune(entity.Content)
		matched := false

		for overlap := min(consumed, len(partRunes)); overlap >= 0; overlap-- {
			start := consumed - overlap

			end := start + len(partRunes)
			if end > len(originalRunes) {
				continue
			}

			if string(originalRunes[start:end]) == entity.Content {
				consumed = end
				matched = true

				break
			}
		}

		require.Truef(t, matched, "entity[%d] content does not align with original payload", i)
	}

	assert.Equal(t, len(originalRunes), consumed, "re-split content should reconstruct the original payload exactly")
}
