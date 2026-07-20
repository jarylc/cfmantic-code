package handler

import (
	"cfmantic-code/internal/splitter"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

var extractSymbolContexts = splitter.ExtractSymbolContexts

// HandleSearchSymbols implements the search_symbols tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleSearchSymbols(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	query := req.GetString("query", "")
	kinds := req.GetStringSlice("kinds", []string{})

	files, err := h.walkFiles(ctx, path, nil)
	if err != nil {
		return mcp.NewToolResultError("failed to walk codebase: " + err.Error()), nil //nolint:nilerr // MCP pattern: surface error in result
	}

	kindFilter := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		kindFilter[strings.ToLower(k)] = true
	}

	type symbolHit struct {
		splitter.SymbolContext

		RelPath string
	}

	var hits []symbolHit

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return mcp.NewToolResultError("search canceled: " + err.Error()), nil //nolint:nilerr // MCP pattern: surface error in result
		}

		source, err := os.ReadFile(file.AbsPath)
		if err != nil {
			continue
		}

		symbols, err := extractSymbolContexts(source, file.AbsPath)
		if err != nil {
			continue
		}

		for _, sym := range symbols {
			if !matchesSymbolQuery(sym.Name, query) {
				continue
			}

			if len(kindFilter) > 0 && !kindFilter[strings.ToLower(sym.Kind)] {
				continue
			}

			hits = append(hits, symbolHit{RelPath: file.RelPath, SymbolContext: sym})
		}
	}

	if len(hits) == 0 {
		return mcp.NewToolResultText("No symbols found"), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d symbols:\n", len(hits))

	for _, hit := range hits {
		fmt.Fprintf(&sb, "\n- %s (%s) in %s (lines %d-%d)\n", hit.Name, hit.Kind, hit.RelPath, hit.StartLine, hit.EndLine)
	}

	return mcp.NewToolResultText(sb.String()), nil
}

// matchesSymbolQuery returns true when the symbol name matches the query.
// An empty query matches all symbols. If the query contains glob metacharacters
// (*, ?, [) it is treated as a glob pattern via filepath.Match; otherwise a
// case-insensitive substring match is used.
func matchesSymbolQuery(name, query string) bool {
	if query == "" {
		return true
	}

	if strings.ContainsAny(query, "*?[") {
		matched, err := filepath.Match(query, name)
		return err == nil && matched
	}

	return strings.Contains(strings.ToLower(name), strings.ToLower(query))
}
