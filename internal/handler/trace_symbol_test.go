package handler

import (
	"cfmantic-code/internal/splitter"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTraceSource is a Go source file with known line numbers for tracing.
// Line 5:  func doWork() {
// Line 7:      helper()
// Line 8:      helper()
// Line 11: func helper() {
// Line 15: func main() {
// Line 16:     helper()
// Line 17:     doWork()
const testTraceSource = `package main

import "fmt"

func doWork() {
	fmt.Println("working")
	helper()
	helper()
}

func helper() {
	fmt.Println("helper")
}

func main() {
	helper()
	doWork()
}
`

func writeTraceGoFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))

	return dir
}

func TestHandleTraceSymbol_AllMode_ReturnsDefinitionsAndCalls(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Definition at line 11
	assert.Contains(t, text, "Definitions (1):")
	assert.Contains(t, text, "- defined at")
	assert.Contains(t, text, "Call sites (3):")
	assert.Contains(t, text, "- called at")
	assert.Contains(t, text, "main.go:11")
	// Calls at lines 7, 8, 16
	assert.Contains(t, text, "main.go:7")
	assert.Contains(t, text, "main.go:8")
	assert.Contains(t, text, "main.go:16")
	assert.NotContains(t, text, "\n\n- ")
}

func TestHandleTraceSymbol_DefinitionsMode_OnlyDefinitions(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "definitions",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "main.go:11")
	assert.NotContains(t, text, "main.go:7")
	assert.NotContains(t, text, "main.go:8")
	assert.NotContains(t, text, "main.go:16")
}

func TestHandleTraceSymbol_CallersMode_OnlyCallsInsideFunctions(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "callers",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Calls inside doWork and main
	assert.Contains(t, text, "main.go:7")
	assert.Contains(t, text, "doWork")
	assert.Contains(t, text, "main.go:8")
	assert.Contains(t, text, "main.go:16")
	assert.Contains(t, text, "main")
	// No definition
	assert.NotContains(t, text, "main.go:11")
}

func TestHandleTraceSymbol_ReferencesMode_CallsAndReferencesNoDefinitions(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "references",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Calls are included
	assert.Contains(t, text, "main.go:7")
	assert.Contains(t, text, "main.go:16")
	// Definition is excluded
	assert.NotContains(t, text, "main.go:11")
}

func TestHandleTraceSymbol_TracesDoWork(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "doWork",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Definition at line 5
	assert.Contains(t, text, "main.go:5")
	// Call at line 17 inside main
	assert.Contains(t, text, "main.go:17")
	assert.Contains(t, text, "main")
}

func TestHandleTraceSymbol_NoMatches(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "nonexistent",
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "No matches found")
}

func TestHandleTraceSymbol_MissingPath(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"symbol": "helper",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleTraceSymbol_MissingSymbol(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleTraceSymbol_InvalidMode(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "invalid",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleTraceSymbol_InvalidPath(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   filepath.Join(t.TempDir(), "does-not-exist"),
		"symbol": "helper",
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleTraceSymbol_GracefullyIgnoresUnparseableFile(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("not valid go {{{"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "valid.go"), []byte(testTraceSource), 0o644))

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "valid.go:11")
}

func TestHandleTraceSymbol_DefaultModeIsAll(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	// Omit mode entirely - should default to "all"
	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Should include both definition and calls
	assert.Contains(t, text, "main.go:11")
	assert.Contains(t, text, "main.go:7")
}

func TestHandleTraceSymbol_ReferenceNotCall(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	// A source where the symbol appears as a reference (not followed by `(`)
	const src = `package main

var x = helper

func helper() int {
	return 1
}
`

	dir := writeTraceGoFile(t, "main.go", src)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "references",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	// Line 3 has `var x = helper` which is a reference (not a call)
	assert.Contains(t, text, "main.go:3")
	// Line 5 is the definition, should be excluded in references mode
	assert.NotContains(t, text, "main.go:5")
}

func TestHandleTraceSymbol_EnclosingSymbolInfo(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTraceGoFile(t, "main.go", testTraceSource)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "callers",
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	// The call at line 7 is inside doWork (function)
	assert.Contains(t, text, "doWork")
	// The call at line 16 is inside main (function)
	assert.Contains(t, text, "main")
}

func TestFormatTraceMatches_GroupsOccurrencesByLexicalRelation(t *testing.T) {
	matches := []traceMatch{
		{RelPath: "refs.go", Line: 30, MatchType: "reference"},
		{RelPath: "calls.go", Line: 20, MatchType: "call", Enclosing: &splitter.SymbolContext{Name: "run", Kind: "function"}},
		{RelPath: "defs.go", Line: 10, MatchType: "definition", Enclosing: &splitter.SymbolContext{Name: "helper", Kind: "function"}},
		{RelPath: "calls.go", Line: 21, MatchType: "call"},
	}

	assert.Equal(t, `Found 4 matches for "helper":
Definitions (1):
- defined at defs.go:10 in helper (function)
Call sites (2):
- called at calls.go:20 in run (function)
- called at calls.go:21
References (1):
- referenced at refs.go:30
`, formatTraceMatches("helper", matches))
}

func TestFormatTraceMatches_OmitsEmptyGroups(t *testing.T) {
	matches := []traceMatch{{RelPath: "refs.go", Line: 30, MatchType: "reference"}}

	text := formatTraceMatches("helper", matches)
	assert.Contains(t, text, "References (1):")
	assert.NotContains(t, text, "Definitions")
	assert.NotContains(t, text, "Call sites")
}

func TestHandleTraceSymbol_PreservesLexicalOccurrencesInCommentsAndStrings(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	const src = `package main

// helper comment
var label = "helper"
func helper() {}
`

	dir := writeTraceGoFile(t, "main.go", src)

	res, err := h.HandleTraceSymbol(context.Background(), makeReq(map[string]any{
		"path":   dir,
		"symbol": "helper",
		"mode":   "all",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "References (2):")
	assert.Contains(t, text, "main.go:3")
	assert.Contains(t, text, "main.go:4")
	assert.Contains(t, text, "Definitions (1):")
	assert.Contains(t, text, "main.go:5")
}

func TestClassifyMatch_DefinitionUsesSymbolStartLine(t *testing.T) {
	source := []byte("func helper() { helper() }")
	symbols := []splitter.SymbolContext{{Name: "helper", StartLine: 1}}

	assert.Equal(t, "definition", classifyMatch(source, 11, 1, "helper", symbols))
	assert.Equal(t, "definition", classifyMatch(source, 22, 1, "helper", symbols))
}
