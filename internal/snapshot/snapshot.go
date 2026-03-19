// Package snapshot persists indexing state to disk. It is thread-safe.
package snapshot

import (
	"cfmantic-code/internal/fileutil"
	"crypto/md5" //nolint:gosec // G501: MD5 used for deterministic naming, not security
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Status represents the indexing state of a codebase.
type Status string

const (
	StatusIndexing Status = "indexing"
	StatusIndexed  Status = "indexed"
	StatusFailed   Status = "failed"
	StatusNotFound Status = "not_found"
)

// ErrStoredPathMismatch is returned when a persisted state file belongs to a
// different canonical codebase path, which usually means the codebase was
// moved or renamed after indexing.
var ErrStoredPathMismatch = errors.New("stored snapshot path mismatch")

var marshalState = json.MarshalIndent

// StoredPathMismatchError describes a state file whose persisted codebase root
// no longer matches the canonical path where it was found.
type StoredPathMismatchError struct {
	Path       string
	StoredPath string
}

func (e *StoredPathMismatchError) Error() string {
	return fmt.Sprintf("state file at %q points to %q", e.Path, e.StoredPath)
}

func (e *StoredPathMismatchError) Is(target error) bool {
	return target == ErrStoredPathMismatch
}

// Progress holds granular pipeline progress counters reported during indexing.
type Progress struct {
	FilesDone      int
	FilesTotal     int
	ChunksTotal    int
	ChunksInserted int
}

// CodebaseInfo holds the persisted state for a single indexed codebase.
type CodebaseInfo struct {
	Path                   string    `json:"path"`
	Status                 Status    `json:"status"`
	Operation              string    `json:"operation,omitempty"`
	Source                 string    `json:"source,omitempty"`
	Mode                   string    `json:"mode,omitempty"`
	IgnorePatterns         *[]string `json:"ignorePatterns,omitempty"`
	Step                   string    `json:"step,omitempty"` // current step description, only meaningful during indexing
	StartedAt              time.Time `json:"startedAt,omitzero"`
	StepUpdatedAt          time.Time `json:"stepUpdatedAt,omitzero"`
	LastProgressAt         time.Time `json:"lastProgressAt,omitzero"`
	IndexedFiles           int       `json:"indexedFiles"`
	TotalChunks            int       `json:"totalChunks"`
	LastUpdated            time.Time `json:"lastUpdated"`
	ErrorMessage           string    `json:"errorMessage,omitempty"`
	FilesTotal             int       `json:"filesTotal,omitempty"`
	FilesDone              int       `json:"filesDone,omitempty"`
	ChunksTotal            int       `json:"chunksTotal,omitempty"`
	ChunksInserted         int       `json:"chunksInserted,omitempty"`
	unsavedTerminalFailure bool
}

// StatusManager is the interface consumed by handler and sync packages.
type StatusManager interface {
	GetStatus(path string) Status
	GetInfo(path string) *CodebaseInfo
	SetStep(path, step string)
	SetProgress(path string, progress Progress)
	SetIndexed(path string, files, chunks int)
	SetFailed(path, errMsg string)
	Remove(path string)
	IsIndexing(path string) bool
}

// IgnorePatternSetter augments StatusManager with persisted per-codebase ignore patterns.
type IgnorePatternSetter interface {
	SetIgnorePatterns(path string, patterns []string)
}

// IgnorePatternReader augments StatusManager with persisted per-codebase ignore patterns.
type IgnorePatternReader interface {
	GetIgnorePatterns(path string) ([]string, bool)
}

// Manager manages persistent indexing state across codebases.
// State is stored per-codebase at <codebasePath>/.cfmantic/state.json.
type Manager struct {
	mu        sync.RWMutex
	codebases map[string]*CodebaseInfo // in-memory cache, keyed by absolute path
	observers []Observer
	now       func() time.Time
}

// NewManager creates a Manager with an empty in-memory cache.
// State is lazily loaded from each codebase's .cfmantic/state.json on first access.
func NewManager() *Manager {
	return &Manager{
		codebases: make(map[string]*CodebaseInfo),
		now:       time.Now,
	}
}

// AddObserver registers an event observer for snapshot lifecycle changes.
func (m *Manager) AddObserver(observer Observer) {
	if observer == nil {
		return
	}

	m.mu.Lock()
	m.observers = append(m.observers, observer)
	m.mu.Unlock()
}

// stateFilePath returns the path to the state file for a given codebase.
func stateFilePath(codebasePath string) string {
	return filepath.Join(MetadataDirPath(codebasePath), "state.json")
}

// loadFromDisk reads a single codebase's state from its .cfmantic/state.json.
// Returns nil if the file doesn't exist or can't be parsed.
func loadFromDisk(path string) *CodebaseInfo {
	data, err := os.ReadFile(stateFilePath(path))
	if err != nil {
		return nil
	}

	var info CodebaseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		log.Printf("snapshot: unmarshal %s: %v", stateFilePath(path), err)
		return nil
	}

	return &info
}

// ValidateStoredPath returns an error only when a persisted state file exists
// and its stored canonical path points at a different codebase root. Missing,
// unreadable, or invalid state files are ignored so callers can keep their
// existing graceful fallback behavior.
func ValidateStoredPath(path string) error {
	info := loadFromDisk(path)
	if info == nil || info.Path == "" || info.Path == path {
		return nil
	}

	return &StoredPathMismatchError{Path: path, StoredPath: info.Path}
}

// GetStatus returns the current Status for the given path, or StatusNotFound.
func (m *Manager) GetStatus(path string) Status {
	m.resolve(path)

	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.codebases[path]
	if !ok {
		return StatusNotFound
	}

	return info.Status
}

// GetInfo returns a copy of CodebaseInfo for the given path, or nil if not tracked.
func (m *Manager) GetInfo(path string) *CodebaseInfo {
	m.resolve(path)

	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.codebases[path]
	if !ok {
		return nil
	}

	copied := *info
	copied.IgnorePatterns = cloneStringSlicePtr(info.IgnorePatterns)

	return &copied
}

// SetStep creates or updates an entry with StatusIndexing and the given step description.
func (m *Manager) SetStep(path, step string) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusIndexing

	info.Step = step
	if info.StartedAt.IsZero() {
		info.StartedAt = now
	}

	info.StepUpdatedAt = now
	info.LastUpdated = now
	info.ErrorMessage = ""
	m.mu.Unlock()

	ignoreError(m.saveToDisk(path))
	m.emit(path, EventStepUpdated)
}

// SetProgress updates the pipeline progress counters for a codebase that is currently indexing.
// It refreshes the progress freshness timestamps in addition to the counters.
func (m *Manager) SetProgress(path string, progress Progress) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusIndexing
	if info.StartedAt.IsZero() {
		info.StartedAt = now
	}

	info.FilesDone = progress.FilesDone
	info.FilesTotal = progress.FilesTotal
	info.ChunksTotal = progress.ChunksTotal
	info.ChunksInserted = progress.ChunksInserted
	info.LastProgressAt = now
	info.LastUpdated = now

	m.mu.Unlock()

	ignoreError(m.saveToDisk(path))
	m.emit(path, EventProgressUpdated)
}

// StartOperation records structured metadata for a new indexing lifecycle.
func (m *Manager) StartOperation(path string, meta OperationMetadata) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusIndexing
	info.Operation = meta.Operation
	info.Source = meta.Source
	info.Mode = meta.Mode
	info.Step = ""
	info.StartedAt = now
	info.StepUpdatedAt = time.Time{}
	info.LastProgressAt = time.Time{}
	info.LastUpdated = now
	info.ErrorMessage = ""
	info.FilesDone = 0
	info.FilesTotal = 0
	info.ChunksTotal = 0
	info.ChunksInserted = 0

	m.mu.Unlock()

	ignoreError(m.saveToDisk(path))
	m.emit(path, EventOperationStarted)
}

// SetIndexed marks a codebase as successfully indexed.
func (m *Manager) SetIndexed(path string, files, chunks int) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusIndexed
	info.IndexedFiles = files
	info.TotalChunks = chunks
	info.Step = ""
	info.LastUpdated = now
	info.FilesDone = 0
	info.FilesTotal = 0
	info.ChunksTotal = 0
	info.ChunksInserted = 0
	info.ErrorMessage = ""
	m.mu.Unlock()

	if err := m.saveToDisk(path); err != nil {
		m.markTerminalPersistenceFailure(path, fmt.Sprintf("failed to persist indexed state: %v", err))
		m.emit(path, EventOperationFailed)

		return
	}

	m.emit(path, EventOperationCompleted)
}

// SetIgnorePatterns persists the effective ignore patterns for a codebase.
func (m *Manager) SetIgnorePatterns(path string, patterns []string) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	cloned := cloneStrings(patterns)
	info.IgnorePatterns = &cloned
	info.LastUpdated = now

	m.mu.Unlock()

	ignoreError(m.saveToDisk(path))
}

// GetIgnorePatterns returns persisted ignore patterns for a codebase when available.
func (m *Manager) GetIgnorePatterns(path string) ([]string, bool) {
	info := m.GetInfo(path)
	if info == nil || info.IgnorePatterns == nil {
		return nil, false
	}

	return cloneStrings(*info.IgnorePatterns), true
}

// SetFailed marks a codebase as failed with the given error message.
func (m *Manager) SetFailed(path, errMsg string) {
	now := m.timeNow()

	m.mu.Lock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusFailed
	info.ErrorMessage = errMsg

	info.Step = ""
	if info.StartedAt.IsZero() {
		info.StartedAt = now
	}

	info.LastUpdated = now
	m.mu.Unlock()

	if err := m.saveToDisk(path); err != nil {
		m.markTerminalPersistenceFailure(path, fmt.Sprintf("%s: snapshot persistence failed: %v", errMsg, err))
	}

	m.emit(path, EventOperationFailed)
}

// Remove deletes the entry for the given path from the tracked state and disk.
func (m *Manager) Remove(path string) {
	m.mu.Lock()
	delete(m.codebases, path)
	m.mu.Unlock()

	os.Remove(stateFilePath(path)) //nolint:gosec // G104: best-effort cleanup
}

// IsIndexing reports whether the given path is currently being indexed.
func (m *Manager) IsIndexing(path string) bool {
	return m.GetStatus(path) == StatusIndexing
}

func (m *Manager) timeNow() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}

	return time.Now()
}

func (m *Manager) markTerminalPersistenceFailure(path, errMsg string) {
	now := m.timeNow()

	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.codebases[path]
	if !ok {
		info = &CodebaseInfo{Path: path}
		m.codebases[path] = info
	}

	info.Status = StatusFailed
	info.ErrorMessage = errMsg

	info.Step = ""
	if info.StartedAt.IsZero() {
		info.StartedAt = now
	}

	info.LastUpdated = now
	info.unsavedTerminalFailure = true
}

func (m *Manager) clearUnsavedTerminalFailure(path string) {
	m.mu.Lock()
	if info, ok := m.codebases[path]; ok {
		info.unsavedTerminalFailure = false
	}
	m.mu.Unlock()
}

func ignoreError(error) {}

// saveToDisk writes a single codebase's state to its .cfmantic/state.json atomically and returns any error.
func (m *Manager) saveToDisk(path string) error {
	m.mu.RLock()

	info, ok := m.codebases[path]
	if !ok {
		m.mu.RUnlock()
		return nil
	}

	data, err := marshalState(info, "", "  ")

	m.mu.RUnlock()

	if err != nil {
		err = fmt.Errorf("marshal state for %s: %w", path, err)
		log.Printf("snapshot: %v", err)

		return err
	}

	if err := EnsureMetadataDir(path); err != nil {
		err = fmt.Errorf("create %s dir for %s: %w", MetadataDirName, path, err)
		log.Printf("snapshot: %v", err)

		return err
	}

	fp := stateFilePath(path)

	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		err = fmt.Errorf("write tmp state for %s: %w", path, err)
		log.Printf("snapshot: %v", err)

		return err
	}

	if err := fileutil.ReplaceFile(tmp, fp); err != nil {
		err = fmt.Errorf("rename state file for %s: %w", path, err)
		log.Printf("snapshot: %v", err)

		return err
	}

	m.clearUnsavedTerminalFailure(path)

	return nil
}

func (m *Manager) emit(path string, eventType EventType) {
	m.mu.RLock()

	info, ok := m.codebases[path]
	if !ok || len(m.observers) == 0 {
		m.mu.RUnlock()
		return
	}

	observers := append([]Observer(nil), m.observers...)
	copied := *info
	copied.IgnorePatterns = cloneStringSlicePtr(info.IgnorePatterns)

	m.mu.RUnlock()

	event := Event{
		Type:      eventType,
		Path:      path,
		Timestamp: m.timeNow(),
		Info:      copied,
	}

	for _, observer := range observers {
		observer.Observe(&event)
	}
}

// resolve ensures the codebase at path is loaded into the in-memory cache.
// Must be called without holding the lock.
func (m *Manager) resolve(path string) {
	info := loadFromDisk(path)

	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.codebases[path]
	if info == nil {
		if ok && (current.Status == StatusIndexing || current.unsavedTerminalFailure) {
			return
		}

		delete(m.codebases, path)

		return
	}

	if ok && current.LastUpdated.After(info.LastUpdated) {
		return
	}

	m.codebases[path] = info
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}

func cloneStringSlicePtr(values *[]string) *[]string {
	if values == nil {
		return nil
	}

	cloned := cloneStrings(*values)

	return &cloned
}

// CollectionName returns a deterministic Vectorize collection name derived from
// the hostname and codebase path using the first 8 hex characters of its MD5
// hash. Including the hostname ensures per-machine isolation when multiple
// hosts share the same Vectorize backend.
func CollectionName(codebasePath string) string {
	//nolint:errcheck // Hostname is best-effort input for deterministic collection naming.
	host, _ := os.Hostname()

	key := strings.ToLower(host) + ":" + codebasePath
	sum := md5.Sum([]byte(key)) //nolint:gosec // G401: MD5 for deterministic naming, not security

	return fmt.Sprintf("code_chunks_%x", sum)
}
