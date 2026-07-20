package handler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGoSource = `package main

import "fmt"

type Greeter struct {
	Name string
}

type Speaker interface {
	Speak() string
}

func NewGreeter(name string) *Greeter {
	return &Greeter{Name: name}
}

func (g *Greeter) Speak() string {
	return fmt.Sprintf("Hello, %s", g.Name)
}

func main() {
	g := NewGreeter("world")
	fmt.Println(g.Speak())
}
`

func TestHandleSearchSymbols_ReturnsAllSymbolsWhenNoFilters(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Greeter")
	assert.Contains(t, text, "type")
	assert.Contains(t, text, "Speaker")
	assert.Contains(t, text, "NewGreeter")
	assert.Contains(t, text, "function")
	assert.Contains(t, text, "Speak")
	assert.Contains(t, text, "method")
	assert.Contains(t, text, "main.go")
}

func TestHandleSearchSymbols_FiltersByQuerySubstring(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "speak",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Speak")
	assert.Contains(t, text, "method")
	assert.NotContains(t, text, "NewGreeter")
	assert.NotContains(t, text, "Greeter\n")
}

func TestHandleSearchSymbols_FiltersByQueryCaseInsensitive(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "SPEAK",
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "Speak")
}

func TestHandleSearchSymbols_FiltersByQueryGlob(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "Gr*",
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "Greeter")
	assert.NotContains(t, text, "NewGreeter")
	assert.NotContains(t, text, "Speaker")
}

func TestHandleSearchSymbols_FiltersByKinds(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"kinds": []string{"method"},
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Speak")
	assert.Contains(t, text, "method")
	assert.NotContains(t, text, "NewGreeter")
	assert.NotContains(t, text, "type")
}

func TestHandleSearchSymbols_FiltersByMultipleKinds(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"kinds": []string{"type", "method"},
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "Greeter")
	assert.Contains(t, text, "type")
	assert.Contains(t, text, "Speaker")
	assert.Contains(t, text, "Speak")
	assert.Contains(t, text, "method")
	assert.NotContains(t, text, "NewGreeter")
	assert.NotContains(t, text, "function")
}

func TestHandleSearchSymbols_CombinesQueryAndKindsFilters(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "reet",
		"kinds": []string{"type"},
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "Greeter")
	assert.Contains(t, text, "type")
	assert.NotContains(t, text, "Speaker")
	assert.NotContains(t, text, "NewGreeter")
}

func TestHandleSearchSymbols_NoResults(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "nonexistent",
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "No symbols found")
}

func TestHandleSearchSymbols_InvalidPath(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path": filepath.Join(t.TempDir(), "does-not-exist"),
	}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleSearchSymbols_MissingPath(t *testing.T) {
	h := newSearchSymbolsHandler(t)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{}))
	require.NoError(t, err)
	assert.True(t, res.IsError)
}

func TestHandleSearchSymbols_GracefullyIgnoresUnparseableFile(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("not valid go {{{"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "valid.go"), []byte(testGoSource), 0o644))

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "NewGreeter")
}

func TestHandleSearchSymbols_LineNumbersInOutput(t *testing.T) {
	h := newSearchSymbolsHandler(t)
	dir := writeTestGoFile(t, "main.go", testGoSource)

	res, err := h.HandleSearchSymbols(context.Background(), makeReq(map[string]any{
		"path": dir,
	}))
	require.NoError(t, err)

	text := resultText(t, res)
	assert.Contains(t, text, "lines")
	assert.Regexp(t, `lines \d+-\d+`, text)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func newSearchSymbolsHandler(t *testing.T) *Handler {
	t.Helper()
	return newTestHandler(t, nil, nil, nil, nil)
}

func writeTestGoFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))

	return dir
}
