package splitter

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Chunk represents a contiguous slice of a source file.
type Chunk struct {
	Content   string
	StartLine int
	EndLine   int
}

// EmitChunkFunc handles one emitted chunk.
type EmitChunkFunc func(Chunk) error

// Splitter splits source code into overlapping chunks.
type Splitter interface {
	Split(reader io.Reader, filePath string, emit EmitChunkFunc) error
}

// TextSplitter is a character-based overlapping splitter.
type TextSplitter struct {
	ChunkSize int
	Overlap   int
}

// NewTextSplitter creates a TextSplitter with the given chunk size and overlap.
func NewTextSplitter(chunkSize, overlap int) *TextSplitter {
	return &TextSplitter{
		ChunkSize: chunkSize,
		Overlap:   overlap,
	}
}

// Split splits content from reader into overlapping chunks. filePath is reserved for future use.
func (s *TextSplitter) Split(reader io.Reader, filePath string, emit EmitChunkFunc) error {
	_ = filePath

	chunkSize, overlap := s.normalizedConfig()
	lineReader := bufio.NewReader(reader)

	type textLine struct {
		content    string
		lineNumber int
		runeCount  int
	}

	var (
		buffer             []textLine
		lineNumber         = 1
		sawNonWhitespace   bool
		lastEndedWithBreak bool
	)

	bufferRuneCount := func(lines []textLine) int {
		total := 0
		for i := range lines {
			total += lines[i].runeCount
		}

		return total
	}

	chunkFromLines := func(lines []textLine) Chunk {
		var builder strings.Builder
		for i := range lines {
			builder.WriteString(lines[i].content)
		}

		return Chunk{
			Content:   builder.String(),
			StartLine: lines[0].lineNumber,
			EndLine:   lines[len(lines)-1].lineNumber,
		}
	}

	nextChunkEnd := func(lines []textLine, atEOF bool) (int, bool) {
		if atEOF && bufferRuneCount(lines) <= chunkSize {
			return len(lines), true
		}

		size := 0

		for endIdx := range lines {
			lineSize := lines[endIdx].runeCount
			if lineSize > chunkSize {
				return endIdx, true
			}

			size += lineSize
			if size > chunkSize && endIdx > 0 {
				return endIdx, true
			}

			if size >= chunkSize {
				if atEOF || endIdx+1 < len(lines) {
					return endIdx + 1, true
				}

				return 0, false
			}
		}

		return 0, false
	}

	drain := func(atEOF bool) error {
		if !sawNonWhitespace {
			return nil
		}

		for len(buffer) > 0 {
			if buffer[0].content == "" {
				buffer = buffer[1:]

				continue
			}

			if buffer[0].runeCount > chunkSize {
				for _, chunk := range splitOversizedLine(buffer[0].content, buffer[0].lineNumber, chunkSize, overlap) {
					if err := emit(chunk); err != nil {
						return err
					}
				}

				buffer = buffer[1:]

				continue
			}

			endIdx, ready := nextChunkEnd(buffer, atEOF)
			if !ready {
				return nil
			}

			if err := emit(chunkFromLines(buffer[:endIdx])); err != nil {
				return err
			}

			if endIdx >= len(buffer) {
				buffer = nil

				return nil
			}

			backed := 0

			backIdx := endIdx
			for backIdx > 1 && backed < overlap {
				backIdx--
				backed += buffer[backIdx].runeCount
			}

			buffer = buffer[backIdx:]
		}

		return nil
	}

	for {
		line, err := lineReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read line: %w", err)
		}

		if line == "" {
			break
		}

		buffer = append(buffer, textLine{
			content:    line,
			lineNumber: lineNumber,
			runeCount:  utf8.RuneCountInString(line),
		})
		if strings.TrimSpace(line) != "" {
			sawNonWhitespace = true
		}

		lastEndedWithBreak = strings.HasSuffix(line, "\n")
		lineNumber++

		if drainErr := drain(false); drainErr != nil {
			return drainErr
		}

		if err == io.EOF {
			break
		}
	}

	if !sawNonWhitespace {
		return nil
	}

	if lastEndedWithBreak {
		buffer = append(buffer, textLine{lineNumber: lineNumber})
	}

	return drain(true)
}

func (s *TextSplitter) normalizedConfig() (int, int) {
	chunkSize := s.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 4000
	}

	overlap := s.Overlap
	if overlap <= 0 {
		overlap = 500
	}

	if overlap > chunkSize/2 {
		overlap = chunkSize / 2
	}

	return chunkSize, overlap
}

func splitOversizedLine(line string, lineNumber, chunkSize, overlap int) []Chunk {
	runes := []rune(line)
	if len(runes) == 0 {
		return nil
	}

	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}

	chunks := make([]Chunk, 0, (len(runes)+step-1)/step)
	for start := 0; start < len(runes); start += step {
		end := min(start+chunkSize, len(runes))
		chunks = append(chunks, Chunk{
			Content:   string(runes[start:end]),
			StartLine: lineNumber,
			EndLine:   lineNumber,
		})

		if end == len(runes) {
			break
		}
	}

	return chunks
}

// splitLines splits content preserving line endings.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}

	var lines []string

	for {
		idx := strings.Index(content, "\n")
		if idx < 0 {
			lines = append(lines, content)
			break
		}

		lines = append(lines, content[:idx+1])
		content = content[idx+1:]
	}

	return lines
}
