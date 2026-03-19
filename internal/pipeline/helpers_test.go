package pipeline

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/splitter"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitFileEntityBatches_SplitsFileEntitiesIntoBatches(t *testing.T) {
	chunks := []splitter.Chunk{
		{Content: "chunk1", StartLine: 1, EndLine: 1},
		{Content: "chunk2", StartLine: 2, EndLine: 2},
		{Content: "chunk3", StartLine: 3, EndLine: 3},
	}

	var batches [][]milvus.Entity

	total, err := emitFileEntityBatches("a.go", ".go", "/codebase", 2, func(emitChunk func(splitter.Chunk) error) error {
		for _, chunk := range chunks {
			if err := emitChunk(chunk); err != nil {
				return err
			}
		}

		return nil
	}, func(batch []milvus.Entity) error {
		copied := append([]milvus.Entity(nil), batch...)
		batches = append(batches, copied)

		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 3, total)
	require.Len(t, batches, 2)
	assert.Len(t, batches[0], 2)
	assert.Len(t, batches[1], 1)
	assert.Equal(t, "a.go", batches[0][0].RelativePath)
	assert.Equal(t, "a.go", batches[1][0].RelativePath)
}

func TestFileTracker_SetExpectedAfterAllInsertsStillFiresCallback(t *testing.T) {
	var (
		calledRelPath string
		calledCount   int
	)

	tracker := newFileTracker(func(relPath string, chunkCount int) {
		calledRelPath = relPath
		calledCount = chunkCount
	})

	tracker.recordInserted([]milvus.Entity{{RelativePath: "a.go"}, {RelativePath: "a.go"}})
	tracker.setExpected("a.go", 2)

	assert.Equal(t, "a.go", calledRelPath)
	assert.Equal(t, 2, calledCount)
	assert.Equal(t, map[string]int{"a.go": 2}, tracker.completedFiles())
}

func TestEmitFileEntityBatches_ReturnsBuildError(t *testing.T) {
	total, err := emitFileEntityBatches(
		"spaces.txt",
		".txt",
		"/codebase",
		1,
		func(emitChunk func(splitter.Chunk) error) error {
			return emitChunk(splitter.Chunk{
				Content:   strings.Repeat(" ", maxEntityPayloadBytes),
				StartLine: 1,
				EndLine:   1,
			})
		},
		func([]milvus.Entity) error {
			t.Fatal("emit should not be called when entity building fails")

			return nil
		},
	)

	require.Error(t, err)
	assert.Zero(t, total)
	assert.Contains(t, err.Error(), "entity payload")
	assert.ErrorIs(t, err, errCannotSplitOversizedChunk)
}

func TestEmitFileEntityBatches_ReturnsEmitErrorDuringLoopFlush(t *testing.T) {
	wantErr := errors.New("emit failed")

	total, err := emitFileEntityBatches(
		"a.go",
		".go",
		"/codebase",
		1,
		func(emitChunk func(splitter.Chunk) error) error {
			return emitChunk(splitter.Chunk{Content: "chunk", StartLine: 1, EndLine: 1})
		},
		func([]milvus.Entity) error {
			return wantErr
		},
	)

	require.ErrorIs(t, err, wantErr)
	assert.Zero(t, total)
}

func TestEmitFileEntityBatches_ReturnsEmitErrorDuringFinalFlush(t *testing.T) {
	wantErr := errors.New("emit failed")

	total, err := emitFileEntityBatches(
		"a.go",
		".go",
		"/codebase",
		2,
		func(emitChunk func(splitter.Chunk) error) error {
			return emitChunk(splitter.Chunk{Content: "chunk", StartLine: 1, EndLine: 1})
		},
		func([]milvus.Entity) error {
			return wantErr
		},
	)

	require.ErrorIs(t, err, wantErr)
	assert.Zero(t, total)
}

func TestBuildEntitiesForChunk_ReturnsWrappedOversizeError(t *testing.T) {
	_, err := buildEntitiesForChunk(
		"spaces.txt",
		".txt",
		"/codebase",
		splitter.Chunk{
			Content:   strings.Repeat(" ", maxEntityPayloadBytes),
			StartLine: 12,
			EndLine:   12,
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "spaces.txt:12-12")
	assert.ErrorIs(t, err, errCannotSplitOversizedChunk)
}

func TestSplitOversizedChunk_ReturnsErrorWhenChunkDoesNotShrink(t *testing.T) {
	_, err := splitOversizedChunk("a.txt", splitter.Chunk{Content: "x", StartLine: 7, EndLine: 7})

	require.ErrorIs(t, err, errCannotSplitOversizedChunk)
}

func TestSplitOversizedChunk_ReturnsErrorWhenTooFewChunksRemain(t *testing.T) {
	_, err := splitOversizedChunk(
		"spaces.txt",
		splitter.Chunk{Content: strings.Repeat(" ", 32), StartLine: 9, EndLine: 9},
	)

	require.ErrorIs(t, err, errCannotSplitOversizedChunk)
}
