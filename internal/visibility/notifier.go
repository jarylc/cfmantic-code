package visibility

import (
	"cfmantic-code/internal/snapshot"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const loggerName = "cfmantic-code/indexing"

// Message is a best-effort MCP log notification payload.
type Message struct {
	Level  mcp.LoggingLevel
	Logger string
	Text   string
}

// Publisher delivers MCP log-style messages.
type Publisher interface {
	Publish(message Message) error
}

// Sink consumes snapshot events and may publish secondary notifications.
type Sink interface {
	Notify(event *snapshot.Event) error
}

// Notifier fans out snapshot events to multiple best-effort sinks.
type Notifier struct {
	logf  func(string, ...any)
	sinks []Sink
}

// NewNotifier creates a notifier fanout.
func NewNotifier(logf func(string, ...any), sinks ...Sink) *Notifier {
	return &Notifier{logf: logf, sinks: sinks}
}

// Observe dispatches an event to every configured sink.
func (n *Notifier) Observe(event *snapshot.Event) {
	for _, sink := range n.sinks {
		if sink == nil {
			continue
		}

		if err := sink.Notify(event); err != nil && n.logf != nil {
			n.logf("visibility: %v", err)
		}
	}
}

// MCPPublisher sends best-effort log notifications to connected MCP clients.
type MCPPublisher struct {
	server *mcpserver.MCPServer
}

// NewMCPPublisher creates a best-effort publisher for MCP log notifications.
func NewMCPPublisher(server *mcpserver.MCPServer) *MCPPublisher {
	return &MCPPublisher{server: server}
}

// Publish sends a logging notification to all active clients.
func (p *MCPPublisher) Publish(message Message) error {
	if p == nil || p.server == nil {
		return nil
	}

	p.server.SendNotificationToAllClients("notifications/message", map[string]any{
		"level":  message.Level,
		"logger": message.Logger,
		"data":   message.Text,
	})

	return nil
}

// MCPSink translates snapshot events into MCP logging notifications.
type MCPSink struct {
	publisher Publisher
}

// NewMCPSink creates an MCP sink.
func NewMCPSink(publisher Publisher) *MCPSink {
	return &MCPSink{publisher: publisher}
}

// Notify publishes major lifecycle and step updates.
func (s *MCPSink) Notify(event *snapshot.Event) error {
	if s == nil || s.publisher == nil {
		return nil
	}

	message, ok := buildMCPMessage(event)
	if !ok {
		return nil
	}

	if err := s.publisher.Publish(message); err != nil {
		return fmt.Errorf("publish MCP message: %w", err)
	}

	return nil
}

func buildMCPMessage(event *snapshot.Event) (Message, bool) {
	if event == nil {
		return Message{}, false
	}

	switch event.Type {
	case snapshot.EventOperationStarted:
		text := fmt.Sprintf("Indexing started (%s, %s)", event.Info.Source, event.Info.Mode)
		if event.Path != "" {
			text = fmt.Sprintf("Indexing started for %s (%s, %s)", event.Path, event.Info.Source, event.Info.Mode)
		}

		return Message{
			Level:  mcp.LoggingLevelInfo,
			Logger: loggerName,
			Text:   text,
		}, true
	case snapshot.EventStepUpdated:
		if event.Info.Step == "" {
			return Message{}, false
		}

		text := "Indexing step: " + event.Info.Step
		if event.Path != "" {
			text = fmt.Sprintf("Indexing step for %s: %s", event.Path, event.Info.Step)
		}

		return Message{
			Level:  mcp.LoggingLevelInfo,
			Logger: loggerName,
			Text:   text,
		}, true
	case snapshot.EventOperationCompleted:
		text := fmt.Sprintf("Indexing completed (%d files, %d chunks)", event.Info.IndexedFiles, event.Info.TotalChunks)
		if event.Path != "" {
			text = fmt.Sprintf("Indexing completed for %s (%d files, %d chunks)", event.Path, event.Info.IndexedFiles, event.Info.TotalChunks)
		}

		return Message{
			Level:  mcp.LoggingLevelNotice,
			Logger: loggerName,
			Text:   text,
		}, true
	case snapshot.EventOperationFailed:
		text := "Indexing failed: " + event.Info.ErrorMessage
		if event.Path != "" {
			text = fmt.Sprintf("Indexing failed for %s: %s", event.Path, event.Info.ErrorMessage)
		}

		return Message{
			Level:  mcp.LoggingLevelError,
			Logger: loggerName,
			Text:   text,
		}, true
	default:
		return Message{}, false
	}
}
