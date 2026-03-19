package splitter

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectChunks(tb testing.TB, sp Splitter, reader io.Reader, filePath string) []Chunk {
	tb.Helper()

	var chunks []Chunk

	err := sp.Split(reader, filePath, func(chunk Chunk) error {
		chunks = append(chunks, chunk)

		return nil
	})
	require.NoError(tb, err)

	return chunks
}

func collectChunksFromString(t *testing.T, sp Splitter, content, filePath string) []Chunk {
	t.Helper()

	return collectChunks(t, sp, strings.NewReader(content), filePath)
}

func collectChunksFromStringTB(tb testing.TB, sp Splitter, content, filePath string) []Chunk {
	tb.Helper()

	return collectChunks(tb, sp, strings.NewReader(content), filePath)
}

// ─── splitLines ──────────────────────────────────────────────────────────────

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "no newline returns single element",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			// After "hello\n" is consumed the remainder is ""; the final
			// non-newline branch appends that trailing empty string.
			name:     "trailing newline appends empty trailing element",
			input:    "hello\n",
			expected: []string{"hello\n", ""},
		},
		{
			name:     "two lines no trailing newline",
			input:    "hello\nworld",
			expected: []string{"hello\n", "world"},
		},
		{
			name:     "both lines end with newline produce trailing empty",
			input:    "hello\nworld\n",
			expected: []string{"hello\n", "world\n", ""},
		},
		{
			name:     "crlf line endings preserved with trailing empty",
			input:    "hello\r\nworld\r\n",
			expected: []string{"hello\r\n", "world\r\n", ""},
		},
		{
			name:     "blank lines produce trailing empty",
			input:    "a\n\nb\n",
			expected: []string{"a\n", "\n", "b\n", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ─── NewTextSplitter ──────────────────────────────────────────────────────────

func TestNewTextSplitter(t *testing.T) {
	s := NewTextSplitter(1000, 200)
	assert.Equal(t, 1000, s.ChunkSize)
	assert.Equal(t, 200, s.Overlap)
}

// ─── TextSplitter.Split – empty / whitespace ─────────────────────────────────

func TestTextSplitter_Split_EmptyContent(t *testing.T) {
	s := NewTextSplitter(100, 10)

	assert.Nil(t, collectChunksFromString(t, s, "", "file.txt"), "empty string")
	assert.Nil(t, collectChunksFromString(t, s, "   ", "file.txt"), "spaces only")
	assert.Nil(t, collectChunksFromString(t, s, "\n\t  \n", "file.txt"), "whitespace with newlines")
}

// ─── TextSplitter.Split – single-chunk fast path ─────────────────────────────

func TestTextSplitter_Split_SingleChunk(t *testing.T) {
	s := NewTextSplitter(200, 20)
	content := "line one\nline two\nline three\n"

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Len(t, chunks, 1)
	assert.Equal(t, content, chunks[0].Content)
	assert.Equal(t, 1, chunks[0].StartLine)
	// splitLines("...\n") appends a trailing "" → 4 elements → EndLine=4
	assert.Equal(t, 4, chunks[0].EndLine)
}

// ─── TextSplitter.Split – default ChunkSize / Overlap applied when zero ───────

func TestTextSplitter_Split_DefaultsApplied(t *testing.T) {
	s := NewTextSplitter(0, 0) // triggers defaults: 4000 / 500
	content := strings.Repeat("x", 100)

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Len(t, chunks, 1, "100-char content should fit in default 4000-char chunk")
	assert.Equal(t, content, chunks[0].Content)
}

// ─── TextSplitter.Split – overlap capped at chunkSize/2 ──────────────────────

func TestTextSplitter_Split_OverlapCappedAtHalf(t *testing.T) {
	// overlap=80 exceeds chunkSize/2=50; should be silently capped
	s := NewTextSplitter(100, 80)

	// 6 lines × 21 chars = 126 chars > 100 → multi-chunk
	line := strings.Repeat("a", 20) + "\n"
	content := strings.Repeat(line, 6)

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Greater(t, len(chunks), 1, "should produce more than one chunk")
	// Verify monotonically advancing start lines (no infinite loop from bad overlap)
	for i := 1; i < len(chunks); i++ {
		assert.Greater(t, chunks[i].StartLine, chunks[i-1].StartLine,
			"chunk[%d].StartLine should advance past chunk[%d].StartLine", i, i-1)
	}
}

// ─── TextSplitter.Split – multi-chunk with overlap ────────────────────────────

func TestTextSplitter_Split_MultiChunkOverlap(t *testing.T) {
	// 10 lines × 21 chars = 210 chars > chunkSize=90 → multiple chunks.
	// overlap=50 backs up ~2 lines, producing genuine StartLine overlap
	// (chunks[i+1].StartLine < chunks[i].EndLine).
	s := NewTextSplitter(90, 50)
	line := strings.Repeat("b", 20) + "\n" // 21 chars
	content := strings.Repeat(line, 10)

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Greater(t, len(chunks), 1)
	assert.Equal(t, 1, chunks[0].StartLine, "first chunk must start at line 1")
	// splitLines appends a trailing "" for content ending with \n, so the
	// last EndLine is 11 (10 real lines + 1 empty element).
	assert.GreaterOrEqual(t, chunks[len(chunks)-1].EndLine, 10,
		"last chunk must cover at least through line 10")

	// Consecutive chunks must genuinely overlap (StartLine[i+1] < EndLine[i]).
	for i := 0; i+1 < len(chunks); i++ {
		assert.Less(t, chunks[i+1].StartLine, chunks[i].EndLine,
			"chunk[%d] and chunk[%d] should overlap", i, i+1)
	}

	// Every chunk has a valid line range.
	for i, c := range chunks {
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] has inverted line range", i)
	}
}

// ─── TextSplitter.Split – line number accuracy ───────────────────────────────

func TestTextSplitter_Split_LineNumbers(t *testing.T) {
	s := NewTextSplitter(30, 5)

	lines := make([]string, 8)
	for i := range lines {
		lines[i] = strings.Repeat("c", 10) + "\n" // 11 chars per line
	}

	content := strings.Join(lines, "")

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.NotEmpty(t, chunks)
	assert.Equal(t, 1, chunks[0].StartLine)

	for i, c := range chunks {
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine < 1", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] inverted range", i)
		// splitLines appends a trailing "" for content ending with \n, so
		// EndLine can be 9 (8 real lines + the extra empty element).
		assert.LessOrEqual(t, c.EndLine, 9, "chunk[%d] EndLine exceeds total lines", i)
	}
}

// ─── TextSplitter.Split – large single line exceeds chunkSize ─────────────────

func TestTextSplitter_Split_LineLargerThanChunk(t *testing.T) {
	s := NewTextSplitter(50, 10)

	var builder strings.Builder
	for i := range 120 {
		builder.WriteRune(rune(0x4E00 + i))
	}

	content := builder.String()

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Greater(t, len(chunks), 1, "oversized single line should be split")
	assert.Equal(t, content, mergeChunkContent(chunks), "split chunks must preserve exact content")

	for i, chunk := range chunks {
		assert.True(t, utf8.ValidString(chunk.Content), "chunk[%d] should preserve rune boundaries", i)
		assert.LessOrEqual(t, utf8.RuneCountInString(chunk.Content), 50, "chunk[%d] exceeds chunk size", i)
		assert.Equal(t, 1, chunk.StartLine, "chunk[%d] should keep original start line", i)
		assert.Equal(t, 1, chunk.EndLine, "chunk[%d] should keep original end line", i)

		if i > 0 {
			shared := sharedBoundaryRunes(chunks[i-1].Content, chunk.Content)
			assert.Positive(t, shared, "chunk[%d] should overlap previous chunk", i)
			assert.LessOrEqual(t, shared, 10, "chunk[%d] overlap should stay within configured limit", i)
		}
	}
}

func TestTextSplitter_Split_StreamsFromReader(t *testing.T) {
	s := NewTextSplitter(12, 4)
	content := "alpha\nbeta\ngamma\n"

	chunks := collectChunks(t, s, iotest.OneByteReader(strings.NewReader(content)), "file.txt")

	require.NotEmpty(t, chunks)
	assert.Equal(t, content, mergeChunkContent(chunks))
}

func TestTextSplitter_Split_ReadError(t *testing.T) {
	s := NewTextSplitter(12, 4)
	wantErr := assert.AnError

	err := s.Split(iotest.ErrReader(wantErr), "file.txt", func(Chunk) error {
		t.Fatal("emit must not be called")

		return nil
	})

	require.ErrorIs(t, err, wantErr)
}

func TestTextSplitter_Split_SmallLineBeforeOversizedLine(t *testing.T) {
	s := NewTextSplitter(12, 4)
	content := "short\n" + strings.Repeat("x", 20)

	chunks := collectChunksFromString(t, s, content, "file.txt")

	require.Len(t, chunks, 3)
	assert.Equal(t, "short\n", chunks[0].Content)
	assert.Equal(t, 1, chunks[0].StartLine)
	assert.Equal(t, 2, chunks[1].StartLine)
	assert.Contains(t, chunks[1].Content, "x")
}

func TestTextSplitter_Split_OversizedLineEmitError(t *testing.T) {
	s := NewTextSplitter(12, 4)
	wantErr := assert.AnError

	err := s.Split(strings.NewReader(strings.Repeat("x", 20)), "file.txt", func(Chunk) error {
		return wantErr
	})

	require.ErrorIs(t, err, wantErr)
}

func TestSplitOversizedLine_EdgeCases(t *testing.T) {
	assert.Nil(t, splitOversizedLine("", 7, 5, 2))

	chunks := splitOversizedLine("abcdef", 3, 2, 2)
	require.Len(t, chunks, 5)
	assert.Equal(t, []Chunk{
		{Content: "ab", StartLine: 3, EndLine: 3},
		{Content: "bc", StartLine: 3, EndLine: 3},
		{Content: "cd", StartLine: 3, EndLine: 3},
		{Content: "de", StartLine: 3, EndLine: 3},
		{Content: "ef", StartLine: 3, EndLine: 3},
	}, chunks)
}

func mergeChunkContent(chunks []Chunk) string {
	parts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		parts = append(parts, chunk.Content)
	}

	return mergeOverlappingContent(parts)
}

func mergeOverlappingContent(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	merged := []rune(parts[0])
	for _, part := range parts[1:] {
		partRunes := []rune(part)
		overlap := sharedBoundaryRunes(string(merged), part)
		merged = append(merged, partRunes[overlap:]...)
	}

	return string(merged)
}

func sharedBoundaryRunes(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	maxOverlap := min(len(leftRunes), len(rightRunes))

	for overlap := maxOverlap; overlap > 0; overlap-- {
		if string(leftRunes[len(leftRunes)-overlap:]) == string(rightRunes[:overlap]) {
			return overlap
		}
	}

	return 0
}
