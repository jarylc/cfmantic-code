package handler

import (
	"bytes"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

var findEnclosingSymbol = splitter.FindEnclosingSymbol

const (
	traceModeAll         = "all"
	traceModeCallers     = "callers"
	traceModeReferences  = "references"
	traceModeDefinitions = "definitions"
)

// traceMatch describes a single occurrence of a traced symbol.
type traceMatch struct {
	RelPath   string
	Line      int
	MatchType string // "definition", "call", "reference"
	Enclosing *splitter.SymbolContext
}

// HandleTraceSymbol implements the trace_symbol tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleTraceSymbol(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	symbol, err := req.RequireString("symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	mode := req.GetString("mode", traceModeAll)
	if !isValidTraceMode(mode) {
		return mcp.NewToolResultError(fmt.Sprintf("invalid mode %q: must be one of \"all\", \"callers\", \"references\", \"definitions\"", mode)), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	files, err := h.walkFiles(ctx, path, nil)
	if err != nil {
		return mcp.NewToolResultError("failed to walk codebase: " + err.Error()), nil //nolint:nilerr // MCP pattern: surface error in result
	}

	matches := traceSymbolInFiles(ctx, files, symbol)

	matches = filterTraceMatches(matches, mode)

	if len(matches) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No matches found for %q", symbol)), nil
	}

	return mcp.NewToolResultText(formatTraceMatches(symbol, matches)), nil
}

func traceSymbolInFiles(ctx context.Context, files []walker.CodeFile, symbol string) []traceMatch {
	if symbol == "" {
		return nil
	}

	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbol) + `\b`)

	matches := make([]traceMatch, 0, len(files))

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			break
		}

		source, err := os.ReadFile(file.AbsPath)
		if err != nil {
			continue
		}

		symbols, err := extractSymbolContexts(source, file.AbsPath)
		if err != nil {
			continue
		}

		fileMatches := findMatchesInFile(source, file.RelPath, symbol, pattern, symbols)
		matches = append(matches, fileMatches...)
	}

	return matches
}

func findMatchesInFile(source []byte, relPath, symbol string, pattern *regexp.Regexp, symbols []splitter.SymbolContext) []traceMatch {
	indices := pattern.FindAllIndex(source, -1)
	matches := make([]traceMatch, 0, len(indices))

	for _, m := range indices {
		startOffset := m[0]
		endOffset := m[1]

		line := bytes.Count(source[:startOffset], []byte{'\n'}) + 1

		matchType := classifyMatch(source, endOffset, line, symbol, symbols)

		enclosing := findEnclosingSymbol(symbols, line, line)

		matches = append(matches, traceMatch{
			RelPath:   relPath,
			Line:      line,
			MatchType: matchType,
			Enclosing: enclosing,
		})
	}

	return matches
}

// classifyMatch determines whether an occurrence is a definition, call, or reference.
func classifyMatch(source []byte, endOffset, line int, symbol string, symbols []splitter.SymbolContext) string {
	// Definition: the match is on the start line of a symbol with the same name.
	for _, sym := range symbols {
		if sym.Name == symbol && sym.StartLine == line {
			return "definition"
		}
	}

	// Call: the match is followed by optional whitespace then `(`.
	if isCallSite(source, endOffset) {
		return "call"
	}

	return "reference"
}

// isCallSite returns true if the character after the match (skipping whitespace)
// is an opening parenthesis.
func isCallSite(source []byte, endOffset int) bool {
	for i := endOffset; i < len(source); i++ {
		c := source[i]
		if c == ' ' || c == '\t' {
			continue
		}

		return c == '('
	}

	return false
}

func filterTraceMatches(matches []traceMatch, mode string) []traceMatch {
	filtered := make([]traceMatch, 0, len(matches))

	for _, m := range matches {
		switch mode {
		case traceModeDefinitions:
			if m.MatchType == "definition" {
				filtered = append(filtered, m)
			}

		case traceModeCallers:
			if m.MatchType == "call" && m.Enclosing != nil {
				filtered = append(filtered, m)
			}

		case traceModeReferences:
			if m.MatchType != "definition" {
				filtered = append(filtered, m)
			}

		default: // traceModeAll
			filtered = append(filtered, m)
		}
	}

	return filtered
}

func formatTraceMatches(symbol string, matches []traceMatch) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d matches for %q:\n", len(matches), symbol)

	for _, group := range []struct {
		heading   string
		matchType string
		verb      string
	}{
		{heading: "Definitions", matchType: "definition", verb: "defined"},
		{heading: "Call sites", matchType: "call", verb: "called"},
		{heading: "References", matchType: "reference", verb: "referenced"},
	} {
		count := 0

		for _, m := range matches {
			if m.MatchType == group.matchType {
				count++
			}
		}

		if count == 0 {
			continue
		}

		fmt.Fprintf(&sb, "%s (%d):\n", group.heading, count)

		for _, m := range matches {
			if m.MatchType != group.matchType {
				continue
			}

			enclosing := ""
			if m.Enclosing != nil {
				enclosing = fmt.Sprintf(" in %s (%s)", m.Enclosing.Name, m.Enclosing.Kind)
			}

			fmt.Fprintf(&sb, "- %s at %s:%d%s\n", group.verb, m.RelPath, m.Line, enclosing)
		}
	}

	return sb.String()
}

func isValidTraceMode(mode string) bool {
	switch mode {
	case traceModeAll, traceModeCallers, traceModeReferences, traceModeDefinitions:
		return true
	default:
		return false
	}
}
