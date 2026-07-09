package handler

import (
	"cfmantic-code/internal/milvus"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type searchOutputOptions struct {
	metadataOnly    bool
	maxContentLines int
	maxContentChars int
}

func searchOutputOptionsFromRequest(req *mcp.CallToolRequest) searchOutputOptions {
	return searchOutputOptions{
		metadataOnly:    req.GetBool("metadataOnly", false),
		maxContentLines: max(int(req.GetFloat("maxContentLines", searchDefaultMaxLines)), 0),
		maxContentChars: max(int(req.GetFloat("maxContentChars", searchDefaultMaxContent)), 0),
	}
}

func mergeSearchResults(results []milvus.SearchResult) []milvus.SearchResult {
	if len(results) < 2 {
		return results
	}

	merged := make([]milvus.SearchResult, 0, len(results))
	for _, result := range results {
		if idx := mergeSearchResultIndex(merged, &result); idx >= 0 {
			merged[idx] = mergeSearchResultPair(&merged[idx], &result)
			merged = coalesceMergedSearchResult(merged, idx)

			continue
		}

		merged = append(merged, result)
	}

	return merged
}

func mergeSearchResultIndex(results []milvus.SearchResult, candidate *milvus.SearchResult) int {
	for i, result := range results {
		if canMergeSearchResults(&result, candidate) {
			return i
		}
	}

	return -1
}

func coalesceMergedSearchResult(results []milvus.SearchResult, idx int) []milvus.SearchResult {
	for i := 0; i < len(results); i++ {
		if i == idx || !canMergeSearchResults(&results[idx], &results[i]) {
			continue
		}

		results[idx] = mergeSearchResultPair(&results[idx], &results[i])

		results = append(results[:i], results[i+1:]...)
		if i < idx {
			idx--
		}

		i = -1
	}

	return results
}

func canMergeSearchResults(a, b *milvus.SearchResult) bool {
	return a.RelativePath == b.RelativePath && b.StartLine <= a.EndLine+1 && a.StartLine <= b.EndLine+1
}

func mergeSearchResultPair(a, b *milvus.SearchResult) milvus.SearchResult {
	if b.StartLine < a.StartLine {
		return mergeSearchResultPair(b, a)
	}

	merged := *a
	if b.EndLine > merged.EndLine {
		merged.EndLine = b.EndLine
	}

	if merged.FileExtension == "" {
		merged.FileExtension = b.FileExtension
	}

	if b.Score > merged.Score {
		merged.Score = b.Score
	}

	if b.Distance > merged.Distance {
		merged.Distance = b.Distance
	}

	merged.Content = mergeSearchContent(a, b)

	return merged
}

func mergeSearchContent(a, b *milvus.SearchResult) string {
	if b.StartLine <= a.EndLine {
		overlapLines := a.EndLine - b.StartLine + 1
		aLines := strings.Split(a.Content, "\n")
		bLines := strings.Split(b.Content, "\n")

		lineOffset := b.StartLine - a.StartLine

		overlapLines = min(overlapLines, len(aLines)-lineOffset, len(bLines))
		if overlapLines <= 0 {
			return joinSearchContent(a.Content, b.Content)
		}

		mergedLines := append([]string(nil), aLines...)

		for i := range overlapLines {
			aIdx := lineOffset + i
			mergedLines[aIdx] = mergeOverlappingText(mergedLines[aIdx], bLines[i])
		}

		if overlapLines >= len(bLines) {
			return strings.Join(mergedLines, "\n")
		}

		return joinSearchContent(strings.Join(mergedLines, "\n"), strings.Join(bLines[overlapLines:], "\n"))
	}

	return joinSearchContent(a.Content, b.Content)
}

func mergeOverlappingText(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.Contains(a, b):
		return a
	case strings.Contains(b, a):
		return b
	}

	aThenBOverlap := textSuffixPrefixOverlap(a, b)

	bThenAOverlap := textSuffixPrefixOverlap(b, a)
	if bThenAOverlap > aThenBOverlap {
		return b + string([]rune(a)[bThenAOverlap:])
	}

	return a + string([]rune(b)[aThenBOverlap:])
}

func textSuffixPrefixOverlap(a, b string) int {
	aRunes := []rune(a)
	bRunes := []rune(b)

	maxOverlap := min(len(aRunes), len(bRunes))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if strings.HasSuffix(a, string(bRunes[:overlap])) {
			return overlap
		}
	}

	return 0
}

func joinSearchContent(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n" + b
	}
}

func truncateSearchContent(content string, options searchOutputOptions) (string, bool) {
	truncated := false

	if options.maxContentLines > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > options.maxContentLines {
			content = strings.Join(lines[:options.maxContentLines], "\n")
			truncated = true
		}
	}

	if options.maxContentChars > 0 {
		runes := []rune(content)
		if len(runes) > options.maxContentChars {
			content = string(runes[:options.maxContentChars])
			truncated = true
		}
	}

	return content, truncated
}
