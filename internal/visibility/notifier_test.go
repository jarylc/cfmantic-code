package visibility

import (
	"cfmantic-code/internal/snapshot"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePublisher struct {
	messages []Message
	err      error
}

func (f *fakePublisher) Publish(message Message) error {
	f.messages = append(f.messages, message)

	return f.err
}

type fakeSink struct {
	events []*snapshot.Event
	err    error
}

func (f *fakeSink) Notify(event *snapshot.Event) error {
	f.events = append(f.events, event)

	return f.err
}

func TestMCPSink_PublishesLifecycleAndStepEvents(t *testing.T) {
	publisher := &fakePublisher{}
	sink := NewMCPSink(publisher)
	eventTime := time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC)
	path := "/tmp/project"

	for _, event := range []snapshot.Event{
		{
			Type:      snapshot.EventOperationStarted,
			Timestamp: eventTime,
			Path:      path,
			Info:      snapshot.CodebaseInfo{Source: "manual", Mode: "full"},
		},
		{
			Type:      snapshot.EventStepUpdated,
			Timestamp: eventTime,
			Path:      path,
			Info:      snapshot.CodebaseInfo{Step: "Walking files"},
		},
		{
			Type:      snapshot.EventProgressUpdated,
			Timestamp: eventTime,
			Path:      path,
			Info:      snapshot.CodebaseInfo{FilesDone: 1, FilesTotal: 2},
		},
		{
			Type:      snapshot.EventOperationCompleted,
			Timestamp: eventTime,
			Path:      path,
			Info:      snapshot.CodebaseInfo{IndexedFiles: 2, TotalChunks: 8},
		},
	} {
		sink.Notify(&event)
	}

	require.Len(t, publisher.messages, 3)
	assert.Equal(t, Message{
		Level:  "info",
		Logger: loggerName,
		Text:   "Indexing started for /tmp/project (manual, full)",
	}, publisher.messages[0])
	assert.Equal(t, Message{
		Level:  "info",
		Logger: loggerName,
		Text:   "Indexing step for /tmp/project: Walking files",
	}, publisher.messages[1])
	assert.Equal(t, Message{
		Level:  "notice",
		Logger: loggerName,
		Text:   "Indexing completed for /tmp/project (2 files, 8 chunks)",
	}, publisher.messages[2])
}

func TestNotifier_ContinuesAfterSinkError(t *testing.T) {
	failing := &fakeSink{err: errors.New("boom")}
	recording := &fakeSink{}

	var logs []string

	notifier := NewNotifier(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}, nil, failing, recording)

	event := &snapshot.Event{
		Type: snapshot.EventOperationCompleted,
		Path: "/tmp/project",
		Info: snapshot.CodebaseInfo{IndexedFiles: 2, TotalChunks: 8},
	}
	notifier.Observe(event)

	require.Len(t, failing.events, 1)
	require.Len(t, recording.events, 1)
	assert.Same(t, event, failing.events[0])
	assert.Same(t, event, recording.events[0])
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0], "visibility: boom")
}

func TestMCPPublisher_Publish(t *testing.T) {
	require.NoError(t, (*MCPPublisher)(nil).Publish(Message{}))

	publisher := NewMCPPublisher(mcpserver.NewMCPServer("cfmantic-code", "test"))

	require.NotNil(t, publisher)
	require.NoError(t, publisher.Publish(Message{
		Level:  mcp.LoggingLevelInfo,
		Logger: loggerName,
		Text:   "hello",
	}))
}

func TestMCPSink_Notify(t *testing.T) {
	t.Run("nil sink", func(t *testing.T) {
		var sink *MCPSink

		require.NoError(t, sink.Notify(&snapshot.Event{Type: snapshot.EventOperationStarted}))
	})

	t.Run("nil publisher", func(t *testing.T) {
		require.NoError(t, NewMCPSink(nil).Notify(&snapshot.Event{Type: snapshot.EventOperationStarted}))
	})

	t.Run("publisher error", func(t *testing.T) {
		err := NewMCPSink(&fakePublisher{err: errors.New("boom")}).Notify(&snapshot.Event{
			Type: snapshot.EventOperationFailed,
			Info: snapshot.CodebaseInfo{ErrorMessage: "boom"},
		})

		require.ErrorContains(t, err, "publish MCP message: boom")
	})
}

func TestBuildMCPMessage(t *testing.T) {
	t.Run("nil event", func(t *testing.T) {
		message, ok := buildMCPMessage(nil)

		assert.False(t, ok)
		assert.Equal(t, Message{}, message)
	})

	t.Run("started without path", func(t *testing.T) {
		message, ok := buildMCPMessage(&snapshot.Event{
			Type: snapshot.EventOperationStarted,
			Info: snapshot.CodebaseInfo{Source: "manual", Mode: "full"},
		})

		require.True(t, ok)
		assert.Equal(t, Message{
			Level:  mcp.LoggingLevelInfo,
			Logger: loggerName,
			Text:   "Indexing started (manual, full)",
		}, message)
	})

	t.Run("step without text", func(t *testing.T) {
		message, ok := buildMCPMessage(&snapshot.Event{
			Type: snapshot.EventStepUpdated,
		})

		assert.False(t, ok)
		assert.Equal(t, Message{}, message)
	})

	t.Run("failed event", func(t *testing.T) {
		message, ok := buildMCPMessage(&snapshot.Event{
			Type: snapshot.EventOperationFailed,
			Path: "/tmp/project",
			Info: snapshot.CodebaseInfo{ErrorMessage: "boom"},
		})

		require.True(t, ok)
		assert.Equal(t, Message{
			Level:  mcp.LoggingLevelError,
			Logger: loggerName,
			Text:   "Indexing failed for /tmp/project: boom",
		}, message)
	})
}
