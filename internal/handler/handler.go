package handler

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// errPathNotDir is the sentinel used when a supplied path is missing or not a directory.
var (
	errPathNotDir            = errors.New("path does not exist or is not a directory")
	errSearchPathOutsideRoot = errors.New("requested path is outside search root")
)

// This client targets the custom cf-workers-milvus backend, not native Milvus
// LIKE handling. The worker compiles subtree prefix filters into literal range
// queries for SQL/Vectorize, so `%` and `_` inside the path prefix must stay
// unescaped. Only backslashes are escaped here so the filter string survives
// transport intact.
var likePatternBackslashEscaper = strings.NewReplacer(`\`, `\\`)

var (
	validateStoredPath        = snapshot.ValidateStoredPath
	buildRelativePathFilterFn = buildRelativePathFilter
)

const (
	notIndexedMessage  = "not indexed, run index_codebase first"
	searchBackendLimit = 20
	searchBackendRRFK  = 60
	progressSavePeriod = time.Second
)

var auxiliaryBasenames = map[string]struct{}{
	"codeowners":               {},
	"owners":                   {},
	"owners_aliases":           {},
	"license":                  {},
	"copying":                  {},
	"notice":                   {},
	"security":                 {},
	"security.md":              {},
	"support":                  {},
	"support.md":               {},
	"code_of_conduct":          {},
	"code_of_conduct.md":       {},
	"contributing":             {},
	"contributing.md":          {},
	"governance":               {},
	"governance.md":            {},
	"maintainers":              {},
	"authors":                  {},
	"contributors":             {},
	"pull_request_template":    {},
	"pull_request_template.md": {},
	"issue_template":           {},
	"issue_template.md":        {},
	"dependabot.yml":           {},
	"renovate.json":            {},
	"renovate.json5":           {},
	"release-drafter.yml":      {},
	"funding.yml":              {},
}

// Handler holds dependencies for the MCP tool handlers.
type Handler struct {
	milvus                  milvus.VectorClient
	snapshot                snapshot.StatusManager
	cfg                     *config.Config
	splitter                splitter.Splitter
	syncMgr                 *filesync.Manager // may be nil if sync disabled
	indexSem                chan struct{}     // single-slot semaphore: at most one indexing op per process
	activeManualIndexMu     sync.Mutex
	activeManualIndexByPath map[string]*activeManualIndex
}

type activeManualIndex struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// New creates a Handler with the provided splitter and optional sync manager.
func New(mc milvus.VectorClient, sm snapshot.StatusManager, cfg *config.Config, sp splitter.Splitter, syncMgr *filesync.Manager) *Handler {
	return &Handler{
		milvus:                  mc,
		snapshot:                sm,
		cfg:                     cfg,
		splitter:                sp,
		syncMgr:                 syncMgr,
		indexSem:                make(chan struct{}, 1),
		activeManualIndexByPath: make(map[string]*activeManualIndex),
	}
}

// HandleIndex implements the index_codebase tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleIndex(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := h.repairIndexPathMismatches(ctx, path); err != nil {
		return mcp.NewToolResultError("failed to clear remote index: " + formatMilvusToolError(err, path)), nil
	}

	if parent, _, ok := h.nearestManagedAncestor(path); ok {
		return mcp.NewToolResultError(
			fmt.Sprintf("cannot index child path %q: parent path %q is already tracked", path, parent),
		), nil
	}

	select {
	case h.indexSem <- struct{}{}:
		// acquired — proceed with indexing
	default:
		return mcp.NewToolResultText("Indexing already in progress, please wait"), nil
	}

	releaseSem := true

	defer func() {
		if releaseSem {
			<-h.indexSem
		}
	}()

	if h.snapshot.IsIndexing(path) {
		if !h.recoverStaleIndexingState(path) {
			return mcp.NewToolResultError("already indexing: " + path), nil
		}
	}

	reindex := req.GetBool("reindex", false)
	async := req.GetBool("async", true)

	ignorePatterns := req.GetStringSlice("ignorePatterns", []string{})
	collectionName := snapshot.CollectionName(path)

	status := h.snapshot.GetStatus(path)
	if (status == snapshot.StatusIndexed || status == snapshot.StatusFailed) && !reindex {
		if errText, ok := ensureRemoteCollectionForIncrementalIndex(ctx, h.milvus, path, collectionName); !ok {
			return mcp.NewToolResultError(errText), nil
		}

		// Trigger incremental sync: failed codebases continue from partial progress.
		tracker := snapshot.NewTracker(h.snapshot, path, snapshot.OperationMetadata{
			Operation: "indexing",
			Source:    "manual",
			Mode:      "incremental",
		})
		tracker.Start("Starting incremental sync")

		releaseSem = false

		var (
			indexCtx    context.Context
			finishIndex func()
		)
		if async {
			indexCtx, finishIndex = h.startManualIndex(context.WithoutCancel(ctx), path)
		} else {
			indexCtx, finishIndex = h.startManualIndex(ctx, path)
		}

		if async {
			go h.incrementalIndex(indexCtx, path, ignorePatterns, tracker, finishIndex)

			return mcp.NewToolResultText(fmt.Sprintf("Incremental sync started for %s. Only changed files will be updated. Use reindex=true for a full reindex.", path)), nil
		}

		h.incrementalIndex(indexCtx, path, ignorePatterns, tracker, finishIndex)

		return h.completedIndexResult(path, "Incremental sync complete"), nil
	}

	asyncIgnored := !async

	if reindex && (status == snapshot.StatusIndexed || status == snapshot.StatusFailed) {
		if err := h.clearIndex(ctx, path); err != nil {
			return mcp.NewToolResultError("failed to clear remote index: " + formatMilvusToolError(err, path)), nil
		}
	}

	if err := h.milvus.CreateCollection(ctx, collectionName, h.cfg.EmbeddingDimension, true); err != nil {
		return mcp.NewToolResultError("failed to create collection: " + formatMilvusToolError(err, path)), nil
	}

	tracker := snapshot.NewTracker(h.snapshot, path, snapshot.OperationMetadata{
		Operation: "indexing",
		Source:    "manual",
		Mode:      "full",
	})
	tracker.Start("Starting")

	indexCtx, finishIndex := h.startManualIndex(context.WithoutCancel(ctx), path)

	releaseSem = false

	go h.backgroundIndex(indexCtx, path, collectionName, ignorePatterns, tracker, finishIndex)

	return mcp.NewToolResultText(startedFullIndexMessage(path, asyncIgnored)), nil
}

// HandleSearch implements the search_code tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	searchRoot := path
	if err := validateStoredPath(searchRoot); err != nil {
		return mcp.NewToolResultError(formatMovedStatusError(err)), nil
	}

	status := h.snapshot.GetStatus(path)

	if !isSearchableStatus(status) {
		ancestor, ancestorStatus, ok, ancestorErr := h.nearestSearchableStatusAncestor(path)
		if ancestorErr != nil {
			return mcp.NewToolResultError(formatMovedStatusError(ancestorErr)), nil
		}

		if ok {
			searchRoot = ancestor
			status = ancestorStatus
		}
	}

	if !isSearchableStatus(status) {
		return mcp.NewToolResultError(notIndexedMessage), nil
	}

	var preamble string

	if status == snapshot.StatusIndexing {
		snapInfo := h.snapshot.GetInfo(searchRoot)

		step := ""
		if snapInfo != nil {
			step = snapInfo.Step
		}

		preamble = fmt.Sprintf("Indexing in progress (%s). Results may be incomplete.\n\n", step)
	}

	requestedLimit := min(max(int(req.GetFloat("limit", 10)), 1), searchBackendLimit)

	extensionFilter := req.GetStringSlice("extensionFilter", []string{})

	pathFilter, err := buildRelativePathFilterFn(searchRoot, path)
	if err != nil {
		if errors.Is(err, errSearchPathOutsideRoot) {
			log.Printf("handler: search fallback scope %q -> %q: %v", searchRoot, path, err)
			return mcp.NewToolResultError(notIndexedMessage), nil
		}

		return mcp.NewToolResultError(err.Error()), nil
	}

	filter := buildSearchFilter(extensionFilter, pathFilter)

	collectionName := snapshot.CollectionName(searchRoot)

	results, err := h.milvus.HybridSearch(ctx, collectionName, query, searchBackendLimit, searchBackendRRFK, filter)
	if err != nil {
		return mcp.NewToolResultError("search failed: " + formatMilvusToolError(err, searchRoot)), nil
	}

	results = rerankAuxiliaryResults(results)
	if len(results) > requestedLimit {
		results = results[:requestedLimit]
	}

	if len(results) == 0 {
		return mcp.NewToolResultText(preamble + "No results found for query"), nil
	}

	enricher := newSearchResultEnricher(searchRoot, loadSearchManifest(searchRoot))

	var sb strings.Builder
	sb.WriteString(preamble)
	fmt.Fprintf(&sb, "Found %d results for %q:\n", len(results), query)

	for i, r := range results {
		enrichment := enricher.Enrich(&results[i])
		fmt.Fprintf(&sb, "\n### %d. %s (lines %d-%d)\n", i+1, r.RelativePath, r.StartLine, r.EndLine)

		if enrichment.symbol != nil {
			fmt.Fprintf(&sb, "Symbol: %s (%s, lines %d-%d)\n", enrichment.symbol.Name, enrichment.symbol.Kind, enrichment.symbol.StartLine, enrichment.symbol.EndLine)
		}

		if enrichment.note != "" {
			fmt.Fprintf(&sb, "%s\n", enrichment.note)
		}

		fmt.Fprintf(&sb, "```%s\n%s\n```\n", r.FileExtension, r.Content)
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func isSearchableStatus(status snapshot.Status) bool {
	return status == snapshot.StatusIndexed || status == snapshot.StatusIndexing
}

func isManagedIndexStatus(status snapshot.Status) bool {
	return status == snapshot.StatusIndexed || status == snapshot.StatusIndexing || status == snapshot.StatusFailed
}

func ensureRemoteCollectionForIncrementalIndex(ctx context.Context, client milvus.VectorClient, path, collectionName string) (string, bool) {
	exists, err := client.HasCollection(ctx, collectionName)
	if err != nil {
		return "failed to verify remote index: " + formatMilvusToolError(err, path), false
	}

	if !exists {
		return formatMissingRemoteIndexError(path), false
	}

	return "", true
}

func formatAskUserBeforeReindex(action string) string {
	return fmt.Sprintf("This is an unexpected hard stop. Do not auto-reindex or silently swallow this error. Ask the user whether they want to %s.", action)
}

func formatMissingRemoteIndexError(path string) string {
	return "remote index is missing. " + formatAskUserBeforeReindex(
		fmt.Sprintf("run index_codebase with reindex=true for %q to rebuild remote state", path),
	)
}

func formatMilvusToolError(err error, path string) string {
	switch {
	case errors.Is(err, milvus.ErrBackendUnavailable):
		return fmt.Sprintf("backend appears unavailable. Ask the human to deploy the backend, then retry. Original backend error: %v", err)
	case errors.Is(err, milvus.ErrSearchStateMissing):
		return fmt.Sprintf("backend search state is missing. %s Original backend error: %v", formatAskUserBeforeReindex(
			fmt.Sprintf("run index_codebase with reindex=true for %q to rebuild remote state", path),
		), err)
	default:
		return err.Error()
	}
}

func formatMovedStatusError(err error) string {
	return fmt.Sprintf("Detected that this codebase was moved or renamed after it was indexed. %s Details: %v", formatAskUserBeforeReindex(
		"run index_codebase with reindex=true at the current path to rebuild the index",
	), err)
}

func rerankAuxiliaryResults(results []milvus.SearchResult) []milvus.SearchResult {
	if len(results) < 2 {
		return results
	}

	primary := make([]milvus.SearchResult, 0, len(results))
	auxiliary := make([]milvus.SearchResult, 0, len(results))

	for _, result := range results {
		if isAuxiliaryResult(result.RelativePath) {
			auxiliary = append(auxiliary, result)
			continue
		}

		primary = append(primary, result)
	}

	if len(auxiliary) == 0 {
		return results
	}

	return append(primary, auxiliary...)
}

func isAuxiliaryResult(relativePath string) bool {
	_, ok := auxiliaryBasenames[strings.ToLower(filepath.Base(relativePath))]

	return ok
}

// HandleClear implements the clear_index tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleClear(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	handled, err := h.repairStoredPathMismatch(ctx, path, map[string]struct{}{}, map[string]struct{}{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"Failed to clear remote index: %s. Local state was cleaned up — re-run to retry remote cleanup.", formatMilvusToolError(err, path),
		)), nil
	}

	if handled {
		return mcp.NewToolResultText("Index cleared for " + path), nil
	}

	if err := h.clearIndex(ctx, path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"Failed to clear remote index: %s. Local state was cleaned up — re-run to retry remote cleanup.", formatMilvusToolError(err, path),
		)), nil
	}

	return mcp.NewToolResultText("Index cleared for " + path), nil
}

// HandleStatus implements the get_indexing_status tool.
//
//nolint:gocritic // hugeParam: MCP handler signature requires value type
func (h *Handler) HandleStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	path, err = canonicalizePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	statusPath := path
	if err := validateStoredPath(statusPath); err != nil {
		return mcp.NewToolResultError(formatMovedStatusError(err)), nil
	}

	info := h.snapshot.GetInfo(statusPath)
	if info == nil {
		ancestor, _, ok, ancestorErr := h.nearestManagedStatusAncestor(path)
		if ancestorErr != nil {
			return mcp.NewToolResultError(formatMovedStatusError(ancestorErr)), nil
		}

		if !ok {
			return mcp.NewToolResultError(notIndexedMessage), nil
		}

		statusPath = ancestor
		info = h.snapshot.GetInfo(statusPath)
	}

	if info == nil {
		return mcp.NewToolResultError(notIndexedMessage), nil
	}

	const timeFmt = "2006-01-02T15:04:05Z07:00"

	var msg string

	startedAt := info.StartedAt
	if startedAt.IsZero() {
		startedAt = info.LastUpdated
	}

	var details []string

	details = append(details, "Path: "+statusPath)

	details = append(details, fmt.Sprintf("Status: %s", info.Status))
	if info.Operation != "" {
		details = append(details, "Operation: "+info.Operation)
	}

	if info.Source != "" {
		details = append(details, "Source: "+info.Source)
	}

	if info.Mode != "" {
		details = append(details, "Mode: "+info.Mode)
	}

	if info.Step != "" {
		details = append(details, "Step: "+info.Step)
	}

	if !startedAt.IsZero() {
		details = append(details, "Started: "+startedAt.Format(timeFmt))
	}

	if !info.StepUpdatedAt.IsZero() {
		details = append(details, "Step updated: "+info.StepUpdatedAt.Format(timeFmt))
	}

	if !info.LastProgressAt.IsZero() {
		details = append(details, "Last progress: "+info.LastProgressAt.Format(timeFmt))
	}

	switch info.Status {
	case snapshot.StatusIndexing:
		if info.FilesTotal > 0 {
			msg = fmt.Sprintf("Indexing in progress\nFiles: %d/%d split\nChunks: %d generated, %d inserted",
				info.FilesDone, info.FilesTotal, info.ChunksTotal, info.ChunksInserted)
		} else {
			msg = fmt.Sprintf("Indexing: %s\nStarted: %s", info.Step, startedAt.Format(timeFmt))
		}
	case snapshot.StatusIndexed:
		msg = fmt.Sprintf("Index complete\nFiles: %d\nChunks: %d\nLast updated: %s", info.IndexedFiles, info.TotalChunks, info.LastUpdated.Format(timeFmt))
	case snapshot.StatusFailed:
		msg = fmt.Sprintf("Indexing failed: %s\nLast attempt: %s", info.ErrorMessage, info.LastUpdated.Format(timeFmt))
		if strings.Contains(strings.ToLower(info.ErrorMessage), "try again") {
			msg += "\n\nPartial progress was saved. Run index_codebase (without reindex) to continue where indexing left off."
		}
	default:
		msg = "Unknown status for " + statusPath
	}

	if len(details) > 0 {
		msg += "\n" + strings.Join(details, "\n")
	}

	return mcp.NewToolResultText(msg), nil
}

func (h *Handler) completedIndexResult(path, successPrefix string) *mcp.CallToolResult {
	info := h.snapshot.GetInfo(path)
	if info == nil {
		switch h.snapshot.GetStatus(path) {
		case snapshot.StatusIndexed:
			return mcp.NewToolResultText(fmt.Sprintf("%s for %s.", successPrefix, path))
		case snapshot.StatusFailed:
			return mcp.NewToolResultError(fmt.Sprintf("%s failed for %s", strings.ToLower(successPrefix), path))
		default:
			return mcp.NewToolResultError("indexing did not complete for " + path)
		}
	}

	if info.Status == snapshot.StatusFailed {
		if info.ErrorMessage != "" {
			return mcp.NewToolResultError(info.ErrorMessage)
		}

		return mcp.NewToolResultError(fmt.Sprintf("%s failed for %s", strings.ToLower(successPrefix), path))
	}

	if info.Status == snapshot.StatusIndexed {
		return mcp.NewToolResultText(
			fmt.Sprintf("%s for %s. Index now tracks %d files and %d chunks.", successPrefix, path, info.IndexedFiles, info.TotalChunks),
		)
	}

	return mcp.NewToolResultError("indexing did not complete for " + path)
}

func startedFullIndexMessage(path string, asyncIgnored bool) string {
	if asyncIgnored {
		return fmt.Sprintf(
			"Indexing started for %s in the background. async=false was ignored because an initial index or reindex may exceed MCP client timeouts. Use get_indexing_status to check progress.",
			path,
		)
	}

	return fmt.Sprintf("Indexing started for %s. Use get_indexing_status to check progress.", path)
}

// CanonicalizePath resolves a raw user-supplied path to its canonical absolute
// form: relative paths are made absolute, ".." segments are cleaned, and
// symlinks are resolved. Returns an error if the path does not exist or is not
// a directory.
func CanonicalizePath(rawPath string) (string, error) {
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %s", errPathNotDir, rawPath)
	}

	fi, err := os.Stat(canonical)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%w: %s", errPathNotDir, rawPath)
	}

	return canonical, nil
}

func canonicalizePath(rawPath string) (string, error) {
	return CanonicalizePath(rawPath)
}

// pipelineResult holds the outcome of processFiles.
type pipelineResult struct {
	totalChunks  int
	chunkCounts  map[string]int // relPath → chunk count
	fileChunkIDs map[string][]string
	err          string // first error message, empty if success
}

type manifestProgressSaver struct {
	mu       sync.Mutex
	hashPath string
	manifest *filesync.FileHashMap
	interval time.Duration
	lastSave time.Time
	dirty    bool
	now      func() time.Time
	save     func(string, *filesync.FileHashMap) error
}

func newManifestProgressSaver(hashPath string, manifest *filesync.FileHashMap) *manifestProgressSaver {
	return &manifestProgressSaver{
		hashPath: hashPath,
		manifest: manifest,
		interval: progressSavePeriod,
	}
}

func (s *manifestProgressSaver) Record(relPath string, entry filesync.FileEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.manifest == nil {
		s.manifest = filesync.NewFileHashMap()
	}

	s.manifest.Files[relPath] = entry
	s.dirty = true

	now := s.timeNow()
	if s.lastSave.IsZero() || s.interval <= 0 || now.Sub(s.lastSave) >= s.interval {
		return s.saveLocked(now)
	}

	return nil
}

func (s *manifestProgressSaver) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.dirty {
		return nil
	}

	return s.saveLocked(s.timeNow())
}

func (s *manifestProgressSaver) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}

	return time.Now()
}

func (s *manifestProgressSaver) saveLocked(now time.Time) error {
	if !s.dirty {
		return nil
	}

	saveFn := s.save
	if saveFn == nil {
		saveFn = func(path string, manifest *filesync.FileHashMap) error {
			return manifest.Save(path)
		}
	}

	if err := saveFn(s.hashPath, s.manifest); err != nil {
		return err
	}

	s.dirty = false
	s.lastSave = now

	return nil
}

// processFiles runs the parallel split+insert pipeline for the given files.
// onFileIndexed, if non-nil, is called from insert goroutines (must be goroutine-safe)
// once all chunks for a file have been successfully inserted.
// Returns chunk counts per file and total chunks inserted.
func (h *Handler) processFiles(ctx context.Context, path, collection string, files []walker.CodeFile, onFileIndexed func(relPath string, chunkCount int), collectFileChunkIDs bool, trackers ...*snapshot.Tracker) pipelineResult {
	var progressHook func(filesDone, filesTotal, chunksTotal, chunksInserted int)
	if len(trackers) > 0 && trackers[0] != nil {
		progressHook = trackers[0].ProgressCallback()
	}

	cfg := &pipeline.Config{
		Concurrency:         h.cfg.IndexConcurrency,
		InsertConcurrency:   h.cfg.InsertConcurrency,
		InsertBatchSize:     h.cfg.InsertBatchSize,
		Collection:          collection,
		CodebasePath:        path,
		CollectFileChunkIDs: collectFileChunkIDs,
		OnProgress:          progressHook,
		OnFileIndexed:       onFileIndexed,
	}

	res, err := pipeline.Run(ctx, cfg, files, h.splitter, h.milvus)

	if len(trackers) > 0 && trackers[0] != nil {
		trackers[0].Flush()
	}

	result := pipelineResult{
		totalChunks:  res.TotalChunks,
		chunkCounts:  res.ChunkCounts,
		fileChunkIDs: res.FileChunkIDs,
	}

	if err != nil {
		result.err = err.Error()
	}

	return result
}

// walkFiles prepares ignores and walks the directory.
func (h *Handler) walkFiles(ctx context.Context, path string, ignorePatterns []string) ([]walker.CodeFile, error) {
	files, err := walker.Walk(ctx, path, h.effectiveIgnorePatterns(ignorePatterns))
	if err != nil {
		return nil, fmt.Errorf("walking files: %w", err)
	}

	return files, nil
}

func (h *Handler) effectiveIgnorePatterns(ignorePatterns []string) []string {
	effective := make([]string, 0, len(h.cfg.CustomIgnore)+len(ignorePatterns))
	effective = append(effective, h.cfg.CustomIgnore...)
	effective = append(effective, ignorePatterns...)

	return effective
}

func (h *Handler) saveManifest(path string, manifest *filesync.FileHashMap, chunkCounts map[string]int) error {
	if err := filesync.SaveManifest(path, manifest, chunkCounts); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	return nil
}

func (h *Handler) nearestSearchableStatusAncestor(path string) (string, snapshot.Status, bool, error) {
	current := filepath.Dir(path)
	for current != path {
		if hasSnapshotState(current) {
			if err := validateStoredPath(current); err != nil {
				return "", snapshot.StatusNotFound, false, fmt.Errorf("validate stored path for %q: %w", current, err)
			}
		}

		status := h.snapshot.GetStatus(current)
		if isSearchableStatus(status) {
			return current, status, true, nil
		}

		next := filepath.Dir(current)
		if next == current {
			break
		}

		path = current
		current = next
	}

	return "", snapshot.StatusNotFound, false, nil
}

func (h *Handler) nearestManagedAncestor(path string) (string, snapshot.Status, bool) {
	current := filepath.Dir(path)
	for current != path {
		if hasSnapshotState(current) {
			status := h.snapshot.GetStatus(current)
			if isManagedIndexStatus(status) {
				return current, status, true
			}
		}

		next := filepath.Dir(current)
		if next == current {
			break
		}

		path = current
		current = next
	}

	return "", snapshot.StatusNotFound, false
}

func (h *Handler) nearestManagedStatusAncestor(path string) (string, snapshot.Status, bool, error) {
	current := filepath.Dir(path)
	for current != path {
		if hasSnapshotState(current) {
			if err := validateStoredPath(current); err != nil {
				return "", snapshot.StatusNotFound, false, fmt.Errorf("validate stored path for %q: %w", current, err)
			}

			status := h.snapshot.GetStatus(current)
			if isManagedIndexStatus(status) {
				return current, status, true, nil
			}
		}

		next := filepath.Dir(current)
		if next == current {
			break
		}

		path = current
		current = next
	}

	return "", snapshot.StatusNotFound, false, nil
}

func hasSnapshotState(path string) bool {
	_, err := os.Stat(filepath.Join(snapshot.MetadataDirPath(path), "state.json"))

	return err == nil
}

func (h *Handler) repairIndexPathMismatches(ctx context.Context, path string) error {
	clearedStoredPaths := map[string]struct{}{}
	cleanedCurrentPaths := map[string]struct{}{}

	if _, err := h.repairStoredPathMismatch(ctx, path, clearedStoredPaths, cleanedCurrentPaths); err != nil {
		return err
	}

	current := filepath.Dir(path)
	for current != path {
		if _, err := h.repairStoredPathMismatch(ctx, current, clearedStoredPaths, cleanedCurrentPaths); err != nil {
			return err
		}

		next := filepath.Dir(current)
		if next == current {
			break
		}

		path = current
		current = next
	}

	return nil
}

func (h *Handler) repairStoredPathMismatch(ctx context.Context, path string, clearedStoredPaths, cleanedCurrentPaths map[string]struct{}) (bool, error) {
	err := validateStoredPath(path)
	if err == nil {
		return false, nil
	}

	var mismatch *snapshot.StoredPathMismatchError
	if !errors.As(err, &mismatch) {
		return false, fmt.Errorf("validate stored path for %q: %w", path, err)
	}

	if _, ok := clearedStoredPaths[mismatch.StoredPath]; !ok {
		clearErr := h.clearIndex(ctx, mismatch.StoredPath)

		clearedStoredPaths[mismatch.StoredPath] = struct{}{}
		if _, cleaned := cleanedCurrentPaths[mismatch.Path]; !cleaned {
			h.clearLocalIndexState(mismatch.Path)
			cleanedCurrentPaths[mismatch.Path] = struct{}{}
		}

		if clearErr != nil {
			return true, fmt.Errorf("clear stale index for %q using stored path %q: %w", mismatch.Path, mismatch.StoredPath, clearErr)
		}

		return true, nil
	}

	if _, cleaned := cleanedCurrentPaths[mismatch.Path]; !cleaned {
		h.clearLocalIndexState(mismatch.Path)
		cleanedCurrentPaths[mismatch.Path] = struct{}{}
	}

	return true, nil
}

func (h *Handler) recoverStaleIndexingState(path string) bool {
	if !hasSnapshotState(path) || snapshot.HasActiveLock(path) {
		return false
	}

	log.Printf("handler: recovering stale indexing state for %s", path)
	h.snapshot.SetFailed(path, "stale indexing state recovered after restart: no active lock found")

	return true
}

func (h *Handler) backgroundIndex(ctx context.Context, path, collection string, ignorePatterns []string, tracker *snapshot.Tracker, cleanup func()) {
	defer cleanup()

	filesync.RunFull(h.fullRunParams(ctx, path, collection, ignorePatterns, tracker))
}

func (h *Handler) incrementalIndex(ctx context.Context, path string, ignorePatterns []string, tracker *snapshot.Tracker, cleanup func()) {
	defer cleanup()

	filesync.RunIncremental(h.incrementalRunParams(ctx, path, ignorePatterns, tracker))
}

func (h *Handler) startManualIndex(parent context.Context, path string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent) //nolint:gosec // cancel is retained for clear_index and deferred run cleanup
	active := &activeManualIndex{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	h.activeManualIndexMu.Lock()
	h.activeManualIndexByPath[path] = active
	h.activeManualIndexMu.Unlock()

	return ctx, func() {
		active.once.Do(func() {
			active.cancel()

			h.activeManualIndexMu.Lock()
			if h.activeManualIndexByPath[path] == active {
				delete(h.activeManualIndexByPath, path)
			}
			h.activeManualIndexMu.Unlock()

			close(active.done)
		})
	}
}

func (h *Handler) cancelActiveManualIndex(path string) {
	h.activeManualIndexMu.Lock()
	active := h.activeManualIndexByPath[path]
	h.activeManualIndexMu.Unlock()

	if active == nil {
		return
	}

	active.cancel()
	<-active.done
}

// buildExtensionFilter converts a slice of file extensions (with or without leading dot)
// into a Milvus filter expression, e.g. `fileExtension in ["go", "ts"]`.
// Returns an empty string when the slice is empty or all entries are dot-only.
func buildExtensionFilter(extensions []string) string {
	var quoted []string

	for _, e := range extensions {
		ext := strings.TrimPrefix(e, ".")
		if ext == "" {
			continue
		}

		quoted = append(quoted, fmt.Sprintf("%q", ext))
	}

	if len(quoted) == 0 {
		return ""
	}

	return fmt.Sprintf(`fileExtension in [%s]`, strings.Join(quoted, ", "))
}

func buildRelativePathFilter(searchRoot, requestedPath string) (string, error) {
	if searchRoot == requestedPath {
		return "", nil
	}

	relPath, err := filepath.Rel(searchRoot, requestedPath)
	if err != nil {
		return "", fmt.Errorf("compute relative search path: %w", err)
	}

	relPath = filepath.ToSlash(relPath)
	if relPath == "." {
		return "", nil
	}

	if relPath == ".." || strings.HasPrefix(relPath, "../") {
		return "", fmt.Errorf("%w: requested=%q searchRoot=%q", errSearchPathOutsideRoot, requestedPath, searchRoot)
	}

	prefix := strings.TrimSuffix(relPath, "/")
	// Keep `%` and `_` literal inside the prefix for cf-workers-milvus. The
	// trailing `/%` is the intentional subtree wildcard that the worker lowers to
	// a literal prefix range query for SQL/Vectorize.
	pattern := likePatternBackslashEscaper.Replace(prefix) + "/%"

	return fmt.Sprintf(`relativePath like %q`, pattern), nil
}

func buildSearchFilter(extensions []string, pathFilter string) string {
	clauses := make([]string, 0, 2)
	if pathFilter != "" {
		clauses = append(clauses, pathFilter)
	}

	if extensionFilter := buildExtensionFilter(extensions); extensionFilter != "" {
		clauses = append(clauses, extensionFilter)
	}

	return strings.Join(clauses, " and ")
}

// clearIndex tears down a codebase's index. Local cleanup (snapshot removal, hash file deletion)
// always runs regardless of whether DropCollection succeeds. The DropCollection error, if any, is
// returned so callers can surface it to the user.
func (h *Handler) clearIndex(ctx context.Context, path string) error {
	h.cancelActiveManualIndex(path)

	collectionName := snapshot.CollectionName(path)

	dropErr := h.milvus.DropCollection(ctx, collectionName)
	if dropErr != nil {
		dropErr = fmt.Errorf("drop collection %s: %w", collectionName, dropErr)
		log.Printf("handler: %v", dropErr)
	}

	h.clearLocalIndexState(path)

	return dropErr
}

func (h *Handler) clearLocalIndexState(path string) {
	h.snapshot.Remove(path)

	if h.syncMgr != nil {
		h.syncMgr.UntrackPath(path)
	}

	os.RemoveAll(snapshot.MetadataDirPath(path)) //nolint:gosec // G104: best-effort cleanup
}
