package handler

import (
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/splitter"
	filesync "cfmantic-code/internal/sync"
	"log"
	"os"
	"path/filepath"
)

const staleSymbolContextMessage = "Symbol context unavailable: file changed since indexing"

var extractSearchSymbolContexts = splitter.ExtractSymbolContexts

type searchResultEnrichment struct {
	symbol *splitter.SymbolContext
	note   string
}

type searchResultEnricher struct {
	searchRoot string
	manifest   *filesync.FileHashMap
	cache      map[string]cachedSearchFileSymbols
}

type cachedSearchFileSymbols struct {
	stale   bool
	symbols []splitter.SymbolContext
}

func newSearchResultEnricher(searchRoot string, manifest *filesync.FileHashMap) *searchResultEnricher {
	return &searchResultEnricher{
		searchRoot: searchRoot,
		manifest:   manifest,
		cache:      make(map[string]cachedSearchFileSymbols),
	}
}

func loadSearchManifest(searchRoot string) *filesync.FileHashMap {
	manifest, err := filesync.LoadFileHashMap(filesync.HashFilePath(searchRoot))
	if err != nil {
		log.Printf("handler: search enrichment manifest unavailable for %s: %v", searchRoot, err)
		return nil
	}

	return manifest
}

func (e *searchResultEnricher) Enrich(result *milvus.SearchResult) searchResultEnrichment {
	if result == nil {
		return searchResultEnrichment{}
	}

	fileSymbols := e.loadFileSymbols(result.RelativePath)
	if fileSymbols.stale {
		return searchResultEnrichment{note: staleSymbolContextMessage}
	}

	symbol := splitter.FindEnclosingSymbol(fileSymbols.symbols, result.StartLine, result.EndLine)
	if symbol == nil {
		return searchResultEnrichment{}
	}

	return searchResultEnrichment{symbol: symbol}
}

func (e *searchResultEnricher) loadFileSymbols(relPath string) cachedSearchFileSymbols {
	if e == nil {
		return cachedSearchFileSymbols{}
	}

	if cached, ok := e.cache[relPath]; ok {
		return cached
	}

	loaded := e.readFreshFileSymbols(relPath)
	e.cache[relPath] = loaded

	return loaded
}

func (e *searchResultEnricher) readFreshFileSymbols(relPath string) cachedSearchFileSymbols {
	loaded := cachedSearchFileSymbols{}
	if e.manifest == nil {
		return loaded
	}

	entry, ok := e.manifest.Files[relPath]
	if !ok {
		return loaded
	}

	absPath := filepath.Join(e.searchRoot, filepath.FromSlash(relPath))

	fresh, err := filesync.IsFileFresh(absPath, entry)
	if err != nil {
		return loaded
	}

	if !fresh {
		loaded.stale = true
		return loaded
	}

	source, err := os.ReadFile(absPath)
	if err != nil {
		return loaded
	}

	symbols, err := extractSearchSymbolContexts(source, absPath)
	if err != nil {
		log.Printf("handler: search enrichment unavailable for %s: %v", absPath, err)
		return loaded
	}

	loaded.symbols = symbols

	return loaded
}
