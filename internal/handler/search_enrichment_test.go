package handler

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/mocks"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"context"
	"os"
	"path/filepath"
	"testing"

	filesync "cfmantic-code/internal/sync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func withExtractSearchSymbolContextsStub(t *testing.T, fn func([]byte, string) ([]splitter.SymbolContext, error)) {
	t.Helper()

	prev := extractSearchSymbolContexts
	extractSearchSymbolContexts = fn

	t.Cleanup(func() {
		extractSearchSymbolContexts = prev
	})
}

func TestHandleSearch_WithFreshSymbolEnrichment(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	content := "package main\n\nfunc helper() int {\n\treturn 1\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644))
	saveSearchManifestForFile(t, dir, "main.go")

	results := []milvus.SearchResult{{
		RelativePath:  "main.go",
		StartLine:     3,
		EndLine:       4,
		FileExtension: "go",
		Content:       "\treturn 1",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "helper", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "helper",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Found 1 results")
	assert.Contains(t, text, "### 1. main.go (lines 3-4)\nSymbol: helper (function, lines 3-5)\n```go\n\treturn 1\n```")
	assert.NotContains(t, text, "Symbol context unavailable")
}

func TestHandleSearch_WithFreshTypeScriptVariableSymbolEnrichment(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	content := "export const answer = 42\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ts"), []byte(content), 0o644))
	saveSearchManifestForFile(t, dir, "main.ts")

	results := []milvus.SearchResult{{
		RelativePath:  "main.ts",
		StartLine:     1,
		EndLine:       1,
		FileExtension: "ts",
		Content:       "export const answer = 42",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "answer", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "answer",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Found 1 results")
	assert.Contains(t, text, "### 1. main.ts (lines 1-1)\nSymbol: answer (variable, lines 1-1)\n```ts\nexport const answer = 42\n```")
	assert.NotContains(t, text, "Symbol context unavailable")
}

func TestHandleSearch_WithFreshTSXExportedFunctionSymbolEnrichment(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	content := "export default function Title() {\n  return <h1>Hello</h1>;\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tsx"), []byte(content), 0o644))
	saveSearchManifestForFile(t, dir, "main.tsx")

	results := []milvus.SearchResult{{
		RelativePath:  "main.tsx",
		StartLine:     2,
		EndLine:       2,
		FileExtension: "tsx",
		Content:       "  return <h1>Hello</h1>;",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "Title", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "Title",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "Found 1 results")
	assert.Contains(t, text, "### 1. main.tsx (lines 2-2)\nSymbol: Title (function, lines 1-3)\n```tsx\n  return <h1>Hello</h1>;\n```")
	assert.NotContains(t, text, "Symbol context unavailable")
}

func TestHandleSearch_StaleFileFallsBackToRawChunk(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	filePath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("package main\n\nfunc helper() int {\n\treturn 1\n}\n"), 0o644))
	saveSearchManifestForFile(t, dir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("package main\n\nfunc helper() int {\n\treturn 1\n}\n// changed since indexing\n"), 0o644))

	results := []milvus.SearchResult{{
		RelativePath:  "main.go",
		StartLine:     3,
		EndLine:       4,
		FileExtension: "go",
		Content:       "\treturn 1",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "helper", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "helper",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "### 1. main.go (lines 3-4)\n"+staleSymbolContextMessage+"\n```go\n\treturn 1\n```")
	assert.NotContains(t, text, "Symbol: helper")
}

func TestHandleSearch_WithSupportedFileOutsideSymbolOmitsSymbolAnnotation(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	content := "package main\n\nfunc helper() int {\n\treturn 1\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644))
	saveSearchManifestForFile(t, dir, "main.go")

	results := []milvus.SearchResult{{
		RelativePath:  "main.go",
		StartLine:     1,
		EndLine:       2,
		FileExtension: "go",
		Content:       "package main",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "package", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "package",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "### 1. main.go (lines 1-2)\n```go\npackage main\n```")
	assert.NotContains(t, text, "Symbol:")
	assert.NotContains(t, text, staleSymbolContextMessage)
}

func TestHandleSearch_UnsupportedLanguageFallsBackToRawChunk(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain text\n"), 0o644))
	saveSearchManifestForFile(t, dir, "notes.txt")

	results := []milvus.SearchResult{{
		RelativePath:  "notes.txt",
		StartLine:     1,
		EndLine:       1,
		FileExtension: "txt",
		Content:       "plain text",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "plain", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "plain",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.NotContains(t, text, "Symbol:")
	assert.NotContains(t, text, "Symbol context unavailable")
	assert.Contains(t, text, "plain text")
}

func TestHandleSearch_MalformedSupportedFileFallsBackToRawChunk(t *testing.T) {
	mc := mocks.NewMockVectorClient(t)
	sm := mocks.NewMockStatusManager(t)
	sp := mocks.NewMockSplitter(t)
	h := newTestHandler(t, mc, sm, sp, nil)

	dir := t.TempDir()
	collection := snapshot.CollectionName(dir)
	content := "package main\n\nfunc (\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644))
	saveSearchManifestForFile(t, dir, "main.go")

	results := []milvus.SearchResult{{
		RelativePath:  "main.go",
		StartLine:     1,
		EndLine:       3,
		FileExtension: "go",
		Content:       "package main\n\nfunc (",
	}}

	sm.On("GetStatus", dir).Return(snapshot.StatusIndexed)
	mc.On("HybridSearch", mock.Anything, collection, "broken", 20, 60, "").Return(results, nil)

	res, err := h.HandleSearch(context.Background(), makeReq(map[string]any{
		"path":  dir,
		"query": "broken",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	text := resultText(t, res)
	assert.Contains(t, text, "### 1. main.go (lines 1-3)\n```go\npackage main\n\nfunc (\n```")
	assert.NotContains(t, text, "Symbol:")
	assert.NotContains(t, text, staleSymbolContextMessage)
}

func TestLoadSearchManifest_MissingManifestReturnsNil(t *testing.T) {
	manifest := loadSearchManifest(t.TempDir())
	require.NotNil(t, manifest)
	assert.Empty(t, manifest.Files)
}

func TestLoadSearchManifest_ReadErrorReturnsNil(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filesync.HashFilePath(root), 0o755))
	assert.Nil(t, loadSearchManifest(root))
}

func TestSearchResultEnricher_EnrichNilResult(t *testing.T) {
	assert.Equal(t, searchResultEnrichment{}, newSearchResultEnricher(t.TempDir(), nil).Enrich(nil))
}

func TestSearchResultEnricher_LoadFileSymbolsNilReceiver(t *testing.T) {
	var enricher *searchResultEnricher
	assert.Equal(t, cachedSearchFileSymbols{}, enricher.loadFileSymbols("main.go"))
}

func TestSearchResultEnricher_LoadFileSymbolsUsesCache(t *testing.T) {
	enricher := newSearchResultEnricher(t.TempDir(), nil)
	enricher.cache["main.go"] = cachedSearchFileSymbols{stale: true}

	loaded := enricher.loadFileSymbols("main.go")
	assert.True(t, loaded.stale)
}

func TestSearchResultEnricher_ReadFreshFileSymbolsWithoutManifest(t *testing.T) {
	enricher := newSearchResultEnricher(t.TempDir(), nil)
	assert.Equal(t, cachedSearchFileSymbols{}, enricher.readFreshFileSymbols("main.go"))
}

func TestSearchResultEnricher_ReadFreshFileSymbolsFreshnessError(t *testing.T) {
	manifest := filesync.NewFileHashMap()
	manifest.Files["missing.go"] = filesync.FileEntry{Size: 1, ModTimeUnixNano: 1}
	enricher := newSearchResultEnricher(t.TempDir(), manifest)

	assert.Equal(t, cachedSearchFileSymbols{}, enricher.readFreshFileSymbols("missing.go"))
}

func TestSearchResultEnricher_ReadFreshFileSymbolsReadError(t *testing.T) {
	root := t.TempDir()
	entryPath := filepath.Join(root, "dir.go")
	require.NoError(t, os.Mkdir(entryPath, 0o755))
	info, err := os.Stat(entryPath)
	require.NoError(t, err)

	manifest := filesync.NewFileHashMap()
	manifest.Files["dir.go"] = filesync.FileEntry{Size: info.Size(), ModTimeUnixNano: info.ModTime().UnixNano()}
	enricher := newSearchResultEnricher(root, manifest)

	assert.Equal(t, cachedSearchFileSymbols{}, enricher.readFreshFileSymbols("dir.go"))
}

func TestSearchResultEnricher_ReadFreshFileSymbolsExtractorError(t *testing.T) {
	root := t.TempDir()
	content := []byte("package main\n\nfunc main() {}\n")
	filePath := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(filePath, content, 0o644))
	info, err := os.Stat(filePath)
	require.NoError(t, err)

	manifest := filesync.NewFileHashMap()
	manifest.Files["main.go"] = filesync.FileEntry{Size: info.Size(), ModTimeUnixNano: info.ModTime().UnixNano()}
	enricher := newSearchResultEnricher(root, manifest)

	withExtractSearchSymbolContextsStub(t, func([]byte, string) ([]splitter.SymbolContext, error) {
		return nil, assert.AnError
	})

	assert.Equal(t, cachedSearchFileSymbols{}, enricher.readFreshFileSymbols("main.go"))
}
