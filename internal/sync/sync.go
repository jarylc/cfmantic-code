package filesync

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/pipeline"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type syncTicker interface {
	C() <-chan time.Time
	Stop()
}

type realSyncTicker struct {
	ticker *time.Ticker
}

func (t *realSyncTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *realSyncTicker) Stop() {
	t.ticker.Stop()
}

// Manager periodically checks indexed codebases for file changes and
// incrementally updates the vector store. Paths must be registered via
// TrackPath before they are included in sync cycles.
type Manager struct {
	milvus       milvus.VectorClient
	snapshot     snapshot.StatusManager
	splitter     splitter.Splitter
	cfg          *config.Config
	interval     time.Duration
	after        func(time.Duration) <-chan time.Time
	newTicker    func(time.Duration) syncTicker
	done         chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
	mu           sync.RWMutex
	syncMu       sync.Mutex
	syncCancel   context.CancelFunc
	trackedPaths map[string]bool
}

// NewManager creates a sync Manager. If intervalSeconds <= 0, a default of
// 60 seconds (1 minute) is used.
func NewManager(mc milvus.VectorClient, sm snapshot.StatusManager, sp splitter.Splitter, cfg *config.Config, intervalSeconds int) *Manager {
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}

	return &Manager{
		milvus:   mc,
		snapshot: sm,
		splitter: sp,
		cfg:      cfg,
		interval: time.Duration(intervalSeconds) * time.Second,
		after:    time.After,
		newTicker: func(d time.Duration) syncTicker {
			return &realSyncTicker{ticker: time.NewTicker(d)}
		},
		done:         make(chan struct{}),
		trackedPaths: make(map[string]bool),
	}
}

// Start launches the background sync goroutine. It waits m.interval before
// the first sync to let the server settle, then runs every m.interval.
func (m *Manager) Start() {
	m.wg.Go(func() {
		// Initial delay — let the server finish startup.
		select {
		case <-m.after(m.interval):
		case <-m.done:
			return
		}

		m.syncAll()

		ticker := m.newTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C():
				m.syncAll()
			case <-m.done:
				return
			}
		}
	})
}

// Stop signals the sync goroutine to exit and waits for it to finish.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.done)
	})
	m.cancelActiveSync()
	m.wg.Wait()
}

// TrackPath registers codebasePath for periodic incremental sync. Thread-safe.
func (m *Manager) TrackPath(path string) {
	m.mu.Lock()
	m.trackedPaths[path] = true
	m.mu.Unlock()
}

// TrackedParent returns the nearest tracked ancestor of path.
// Exact-path matches are ignored.
func (m *Manager) TrackedParent(path string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	current := path
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}

		if m.trackedPaths[parent] {
			return parent, true
		}

		current = parent
	}
}

// AutoTrackWorkingDirectory registers the current process working directory for
// background sync. Failures are logged and skipped so startup can continue.
func (m *Manager) AutoTrackWorkingDirectory(canonicalize func(string) (string, error)) {
	m.autoTrackWorkingDirectory(os.Getwd, canonicalize, log.Printf)
}

// UntrackPath removes codebasePath from the sync set. Thread-safe.
func (m *Manager) UntrackPath(path string) {
	m.mu.Lock()
	delete(m.trackedPaths, path)
	m.mu.Unlock()
}

func (m *Manager) autoTrackWorkingDirectory(
	getwd func() (string, error),
	canonicalize func(string) (string, error),
	logf func(string, ...any),
) {
	cwd, err := getwd()
	if err != nil {
		logf("sync: startup auto-track skipped: resolve working directory: %v", err)

		return
	}

	path, err := canonicalize(cwd)
	if err != nil {
		logf("sync: startup auto-track skipped for %q: %v", cwd, err)

		return
	}

	m.TrackPath(path)
}

func (m *Manager) syncAll() {
	m.mu.RLock()

	paths := make([]string, 0, len(m.trackedPaths))
	for p := range m.trackedPaths {
		paths = append(paths, p)
	}

	m.mu.RUnlock()

	for _, p := range paths {
		if m.isStopped() {
			return
		}

		m.syncCodebase(p)
	}
}

func (m *Manager) syncCodebase(path string) {
	var tracker *snapshot.Tracker
	if _, ok := m.snapshot.(snapshot.OperationStarter); ok {
		tracker = snapshot.NewTracker(m.snapshot, path, snapshot.OperationMetadata{
			Operation: "indexing",
			Source:    "background_sync",
			Mode:      "auto-sync",
		})
	}

	// Only sync codebases that are fully indexed and not currently being re-indexed.
	if m.snapshot.GetStatus(path) != snapshot.StatusIndexed {
		return
	}

	if m.snapshot.IsIndexing(path) {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	if !m.startActiveSync(cancel) {
		cancel()

		return
	}
	defer m.finishActiveSync(cancel)

	RunIncremental(m.syncRunParamsWithContext(ctx, path, tracker))
}

func (m *Manager) isStopped() bool {
	select {
	case <-m.done:
		return true
	default:
		return false
	}
}

func (m *Manager) startActiveSync(cancel context.CancelFunc) bool {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	if m.isStopped() {
		return false
	}

	m.syncCancel = cancel

	return true
}

func (m *Manager) finishActiveSync(cancel context.CancelFunc) {
	m.syncMu.Lock()
	m.syncCancel = nil
	m.syncMu.Unlock()

	cancel()
}

func (m *Manager) cancelActiveSync() {
	m.syncMu.Lock()
	cancel := m.syncCancel
	m.syncCancel = nil
	m.syncMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// BuildEntity creates a milvus.Entity from a code chunk.
// Delegates to pipeline.BuildEntity; retained for backward compatibility.
func BuildEntity(relPath, ext, codebasePath string, chunk splitter.Chunk) milvus.Entity {
	return pipeline.BuildEntity(relPath, ext, codebasePath, chunk)
}
