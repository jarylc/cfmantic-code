package snapshot

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackerClock struct {
	times []time.Time
}

func (c *trackerClock) Now() time.Time {
	if len(c.times) == 0 {
		return time.Time{}
	}

	now := c.times[0]
	c.times = c.times[1:]

	return now
}

type manualProgressTimer struct {
	mu      sync.Mutex
	stopped bool
	fn      func()
}

func (t *manualProgressTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	wasStopped := t.stopped
	t.stopped = true

	return !wasStopped
}

func (t *manualProgressTimer) Fire() {
	t.mu.Lock()
	if t.stopped || t.fn == nil {
		t.mu.Unlock()

		return
	}

	fn := t.fn
	t.mu.Unlock()

	fn()
}

type trackerStatusRecorder struct {
	mu sync.Mutex

	startCalls   int
	startPath    string
	startMeta    OperationMetadata
	startHold    chan struct{}
	startEntered chan struct{}

	steps      []string
	progresses []Progress
	indexed    []struct {
		files  int
		chunks int
	}
	failures []string
}

func (r *trackerStatusRecorder) GetStatus(string) Status { return StatusNotFound }

func (r *trackerStatusRecorder) GetInfo(string) *CodebaseInfo { return nil }

func (r *trackerStatusRecorder) SetStep(_, step string) {
	r.mu.Lock()
	r.steps = append(r.steps, step)
	r.mu.Unlock()
}

func (r *trackerStatusRecorder) SetProgress(_ string, progress Progress) {
	r.mu.Lock()
	r.progresses = append(r.progresses, progress)
	r.mu.Unlock()
}

func (r *trackerStatusRecorder) SetIndexed(_ string, files, chunks int) {
	r.mu.Lock()
	r.indexed = append(r.indexed, struct {
		files  int
		chunks int
	}{files: files, chunks: chunks})
	r.mu.Unlock()
}

func (r *trackerStatusRecorder) SetFailed(_, errMsg string) {
	r.mu.Lock()
	r.failures = append(r.failures, errMsg)
	r.mu.Unlock()
}

func (r *trackerStatusRecorder) Remove(string) {}

func (r *trackerStatusRecorder) IsIndexing(string) bool { return false }

func (r *trackerStatusRecorder) StartOperation(path string, meta OperationMetadata) {
	r.mu.Lock()
	r.startCalls++
	r.startPath = path
	r.startMeta = meta
	hold := r.startHold
	entered := r.startEntered
	r.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}

	if hold != nil {
		<-hold
	}
}

func (r *trackerStatusRecorder) startCallsSnapshot() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.startCalls
}

func (r *trackerStatusRecorder) progressSnapshot() []Progress {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Progress(nil), r.progresses...)
}

func (r *trackerStatusRecorder) indexedSnapshot() []struct {
	files  int
	chunks int
} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]struct {
		files  int
		chunks int
	}(nil), r.indexed...)
}

func (r *trackerStatusRecorder) failuresSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.failures...)
}

func TestTracker_StartsOperationOnceAndForwardsStepAndProgress(t *testing.T) {
	status := &trackerStatusRecorder{}
	meta := OperationMetadata{Operation: "indexing", Source: "manual", Mode: "full"}
	tracker := NewTracker(status, "/tmp/project", meta)

	tracker.Start("Walking files")
	tracker.Step("Splitting")
	tracker.Progress(Progress{FilesDone: 1, FilesTotal: 2, ChunksTotal: 3, ChunksInserted: 4})

	require.Equal(t, 1, status.startCallsSnapshot())
	assert.Equal(t, "/tmp/project", status.startPath)
	assert.Equal(t, meta, status.startMeta)
	assert.Equal(t, []string{"Walking files", "Splitting"}, status.steps)
	assert.Equal(t, []Progress{{FilesDone: 1, FilesTotal: 2, ChunksTotal: 3, ChunksInserted: 4}}, status.progresses)
}

func TestTracker_ProgressCallbackAutoStarts(t *testing.T) {
	status := &trackerStatusRecorder{}
	tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})

	tracker.ProgressCallback()(2, 5, 8, 3)

	require.Equal(t, 1, status.startCallsSnapshot())
	assert.Equal(t, []Progress{{FilesDone: 2, FilesTotal: 5, ChunksTotal: 8, ChunksInserted: 3}}, status.progresses)
}

func TestTracker_TerminalUpdatesAutoStart(t *testing.T) {
	t.Run("indexed", func(t *testing.T) {
		status := &trackerStatusRecorder{}
		tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})

		tracker.Indexed(7, 11)

		require.Equal(t, 1, status.startCallsSnapshot())
		assert.Equal(t, []struct {
			files  int
			chunks int
		}{{files: 7, chunks: 11}}, status.indexed)
	})

	t.Run("failed", func(t *testing.T) {
		status := &trackerStatusRecorder{}
		tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})

		tracker.Failed("boom")

		require.Equal(t, 1, status.startCallsSnapshot())
		assert.Equal(t, []string{"boom"}, status.failures)
	})
}

func TestTracker_StartOperationIsThreadSafe(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	status := &trackerStatusRecorder{startHold: hold, startEntered: entered}
	tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing", Source: "manual", Mode: "full"})

	const workers = 16

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		go func(i int) {
			defer wg.Done()

			tracker.Progress(Progress{FilesDone: i + 1, FilesTotal: workers, ChunksTotal: i, ChunksInserted: i})
		}(i)
	}

	<-entered
	close(hold)
	wg.Wait()

	require.Equal(t, 1, status.startCallsSnapshot())
	assert.Equal(t, "/tmp/project", status.startPath)
	assert.Equal(t, OperationMetadata{Operation: "indexing", Source: "manual", Mode: "full"}, status.startMeta)
}

func TestTracker_ProgressCoalescesWithinInterval(t *testing.T) {
	status := &trackerStatusRecorder{}
	tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})
	tracker.progressInterval = 50 * time.Millisecond
	clock := &trackerClock{times: []time.Time{time.Unix(100, 0), time.Unix(100, 0), time.Unix(100, 0)}}
	tracker.now = clock.Now

	var timer *manualProgressTimer

	tracker.newTimer = func(time.Duration, func()) progressTimer {
		timer = &manualProgressTimer{}
		timer.fn = func() { tracker.flushPendingProgress(1) }

		return timer
	}

	first := Progress{FilesDone: 1, FilesTotal: 10, ChunksTotal: 2, ChunksInserted: 0}
	latest := Progress{FilesDone: 3, FilesTotal: 10, ChunksTotal: 6, ChunksInserted: 4}

	tracker.Progress(first)
	tracker.Progress(Progress{FilesDone: 2, FilesTotal: 10, ChunksTotal: 4, ChunksInserted: 1})
	tracker.Progress(latest)

	progresses := status.progressSnapshot()
	require.Len(t, progresses, 1)
	assert.Equal(t, first, progresses[0])

	require.NotNil(t, timer)
	timer.Fire()

	progresses = status.progressSnapshot()
	require.Len(t, progresses, 2)
	assert.Equal(t, latest, progresses[1])
}

func TestTracker_FlushPersistsPendingProgress(t *testing.T) {
	status := &trackerStatusRecorder{}
	tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})
	tracker.progressInterval = time.Hour

	first := Progress{FilesDone: 1, FilesTotal: 10, ChunksTotal: 2, ChunksInserted: 0}
	latest := Progress{FilesDone: 3, FilesTotal: 10, ChunksTotal: 6, ChunksInserted: 4}

	tracker.Progress(first)
	tracker.Progress(latest)

	progresses := status.progressSnapshot()
	require.Len(t, progresses, 1)
	assert.Equal(t, first, progresses[0])

	tracker.Flush()

	progresses = status.progressSnapshot()
	require.Len(t, progresses, 2)
	assert.Equal(t, latest, progresses[1])
}

func TestTracker_TerminalUpdatesBlockLateProgress(t *testing.T) {
	t.Run("indexed", func(t *testing.T) {
		status := &trackerStatusRecorder{}
		tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})
		tracker.progressInterval = 30 * time.Millisecond
		tracker.now = func() time.Time { return time.Unix(200, 0) }

		var timer *manualProgressTimer

		tracker.newTimer = func(time.Duration, func()) progressTimer {
			timer = &manualProgressTimer{}
			timer.fn = func() { tracker.flushPendingProgress(1) }

			return timer
		}

		tracker.Progress(Progress{FilesDone: 1, FilesTotal: 10, ChunksTotal: 2, ChunksInserted: 0})
		tracker.Progress(Progress{FilesDone: 2, FilesTotal: 10, ChunksTotal: 4, ChunksInserted: 1})
		tracker.Indexed(7, 11)

		before := status.progressSnapshot()
		require.NotEmpty(t, before)

		require.NotNil(t, timer)
		timer.Fire()

		after := status.progressSnapshot()
		assert.Equal(t, before, after)
		require.Len(t, status.indexedSnapshot(), 1)
	})

	t.Run("failed", func(t *testing.T) {
		status := &trackerStatusRecorder{}
		tracker := NewTracker(status, "/tmp/project", OperationMetadata{Operation: "indexing"})
		tracker.progressInterval = 30 * time.Millisecond
		tracker.now = func() time.Time { return time.Unix(300, 0) }

		var timer *manualProgressTimer

		tracker.newTimer = func(time.Duration, func()) progressTimer {
			timer = &manualProgressTimer{}
			timer.fn = func() { tracker.flushPendingProgress(1) }

			return timer
		}

		tracker.Progress(Progress{FilesDone: 1, FilesTotal: 10, ChunksTotal: 2, ChunksInserted: 0})
		tracker.Progress(Progress{FilesDone: 2, FilesTotal: 10, ChunksTotal: 4, ChunksInserted: 1})
		tracker.Failed("boom")

		before := status.progressSnapshot()
		require.NotEmpty(t, before)

		require.NotNil(t, timer)
		timer.Fire()

		after := status.progressSnapshot()
		assert.Equal(t, before, after)
		require.Len(t, status.failuresSnapshot(), 1)
	})
}
