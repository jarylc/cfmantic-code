package snapshot

import (
	"sync"
	"time"
)

const defaultTrackerProgressInterval = time.Second

type progressTimer interface {
	Stop() bool
}

type progressTimerFactory func(time.Duration, func()) progressTimer

// OperationMetadata describes the indexing lifecycle that produced the current
// snapshot state.
type OperationMetadata struct {
	Operation string
	Source    string
	Mode      string
}

// EventType identifies the kind of snapshot lifecycle change that occurred.
type EventType string

const (
	EventOperationStarted   EventType = "operation_started"
	EventStepUpdated        EventType = "step_updated"
	EventProgressUpdated    EventType = "progress_updated"
	EventOperationCompleted EventType = "operation_completed"
	EventOperationFailed    EventType = "operation_failed"
)

// Event is emitted whenever the authoritative snapshot state changes.
type Event struct {
	Type      EventType
	Path      string
	Timestamp time.Time
	Info      CodebaseInfo
}

// Observer receives best-effort lifecycle events derived from snapshot state.
type Observer interface {
	Observe(event *Event)
}

// OperationStarter augments StatusManager with structured operation metadata.
type OperationStarter interface {
	StartOperation(path string, meta OperationMetadata)
}

// Tracker keeps snapshot lifecycle updates consistent across indexing entrypoints.
type Tracker struct {
	status StatusManager
	path   string
	meta   OperationMetadata

	startOnce sync.Once

	mu                sync.Mutex
	progressInterval  time.Duration
	now               func() time.Time
	newTimer          progressTimerFactory
	lastProgressWrite time.Time
	pendingProgress   *Progress
	progressTimer     progressTimer
	progressTimerSeq  uint64
	terminal          bool
}

// NewTracker creates a tracker for a single codebase operation.
func NewTracker(status StatusManager, path string, meta OperationMetadata) *Tracker {
	return &Tracker{
		status:           status,
		path:             path,
		meta:             meta,
		progressInterval: defaultTrackerProgressInterval,
		now:              time.Now,
		newTimer: func(delay time.Duration, fn func()) progressTimer {
			return time.AfterFunc(delay, fn)
		},
	}
}

// Start records operation metadata and optionally the first visible step.
func (t *Tracker) Start(step string) {
	t.startOperation()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	if step != "" {
		t.status.SetStep(t.path, step)
	}
}

// Step updates the current step, auto-starting the operation if needed.
func (t *Tracker) Step(step string) {
	t.startOperation()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	t.status.SetStep(t.path, step)
}

// Progress updates pipeline counters, auto-starting the operation if needed.
func (t *Tracker) Progress(progress Progress) {
	t.startOperation()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	t.recordProgressLocked(progress, t.timeNow())
}

// Flush persists any pending progress update synchronously.
func (t *Tracker) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	t.stopProgressTimerLocked()
	t.flushPendingProgressLocked(t.timeNow())
}

// ProgressCallback adapts the tracker to pipeline progress hooks.
func (t *Tracker) ProgressCallback() func(filesDone, filesTotal, chunksTotal, chunksInserted int) {
	return func(filesDone, filesTotal, chunksTotal, chunksInserted int) {
		t.Progress(Progress{
			FilesDone:      filesDone,
			FilesTotal:     filesTotal,
			ChunksTotal:    chunksTotal,
			ChunksInserted: chunksInserted,
		})
	}
}

// Indexed marks the operation complete.
func (t *Tracker) Indexed(files, chunks int) {
	t.startOperation()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	t.stopProgressTimerLocked()
	t.flushPendingProgressLocked(t.timeNow())
	t.terminal = true
	t.status.SetIndexed(t.path, files, chunks)
}

// Failed marks the operation failed.
func (t *Tracker) Failed(errMsg string) {
	t.startOperation()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.terminal {
		return
	}

	t.stopProgressTimerLocked()
	t.flushPendingProgressLocked(t.timeNow())
	t.terminal = true
	t.status.SetFailed(t.path, errMsg)
}

func (t *Tracker) timeNow() time.Time {
	if t != nil && t.now != nil {
		return t.now()
	}

	return time.Now()
}

func (t *Tracker) newProgressTimer(delay time.Duration, fn func()) progressTimer {
	if t.newTimer != nil {
		return t.newTimer(delay, fn)
	}

	return time.AfterFunc(delay, fn)
}

func (t *Tracker) startOperation() {
	t.startOnce.Do(func() {
		if starter, ok := t.status.(OperationStarter); ok {
			starter.StartOperation(t.path, t.meta)
		}
	})
}

func (t *Tracker) progressIntervalOrDefault() time.Duration {
	if t.progressInterval <= 0 {
		return defaultTrackerProgressInterval
	}

	return t.progressInterval
}

func (t *Tracker) recordProgressLocked(progress Progress, now time.Time) {
	interval := t.progressIntervalOrDefault()

	if t.lastProgressWrite.IsZero() || now.Sub(t.lastProgressWrite) >= interval {
		t.stopProgressTimerLocked()
		t.persistProgressLocked(progress, now)

		return
	}

	t.pendingProgress = &progress

	if t.progressTimer != nil {
		return
	}

	delay := max(interval-now.Sub(t.lastProgressWrite), 0)

	t.progressTimerSeq++
	seq := t.progressTimerSeq

	t.progressTimer = t.newProgressTimer(delay, func() {
		t.flushPendingProgress(seq)
	})
}

func (t *Tracker) flushPendingProgress(seq uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.progressTimerSeq != seq {
		return
	}

	t.progressTimer = nil

	if t.terminal {
		return
	}

	t.flushPendingProgressLocked(t.timeNow())
}

func (t *Tracker) flushPendingProgressLocked(now time.Time) {
	if t.pendingProgress == nil {
		return
	}

	progress := *t.pendingProgress
	t.persistProgressLocked(progress, now)
}

func (t *Tracker) persistProgressLocked(progress Progress, now time.Time) {
	t.pendingProgress = nil
	t.lastProgressWrite = now
	t.status.SetProgress(t.path, progress)
}

func (t *Tracker) stopProgressTimerLocked() {
	if t.progressTimer == nil {
		return
	}

	t.progressTimer.Stop()
	t.progressTimer = nil
	t.progressTimerSeq++
}
