package visibility

import (
	"cfmantic-code/internal/snapshot"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/gen2brain/beeep"
)

var (
	beeepNotify = beeep.Notify
	desktopGOOS = runtime.GOOS
	getenv      = os.Getenv
)

// DesktopNotification represents a platform notification payload.
type DesktopNotification struct {
	Title string
	Body  string
}

// StartupInfo describes server startup state for desktop notifications.
type StartupInfo struct {
	WorkingDirectory string
	SyncEnabled      bool
	SyncInterval     int
}

// DesktopClient emits a single best-effort desktop notification.
type DesktopClient interface {
	Notify(notification DesktopNotification) error
}

// BeeepClient emits OS notifications through beeep.
type BeeepClient struct{}

// Notify sends a best-effort desktop notification.
func (BeeepClient) Notify(notification DesktopNotification) error {
	if err := beeepNotify(notification.Title, notification.Body, ""); err != nil {
		return fmt.Errorf("beeep notify: %w", err)
	}

	return nil
}

// DesktopSink translates major snapshot lifecycle events into desktop notifications.
type DesktopSink struct {
	enabled   bool
	client    DesktopClient
	available func() bool
}

// NewDesktopSink creates a best-effort desktop notification sink.
func NewDesktopSink(enabled bool, client DesktopClient, available func() bool) *DesktopSink {
	return &DesktopSink{enabled: enabled, client: client, available: available}
}

// DesktopAvailable reports whether the current environment looks notification-capable.
func DesktopAvailable() bool {
	if desktopGOOS != "linux" {
		return true
	}

	return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
}

// Notify emits milestone-only desktop notifications.
func (s *DesktopSink) Notify(event *snapshot.Event) error {
	notification, ok := buildDesktopNotification(event)
	if !ok {
		return nil
	}

	return notifyDesktop(s.enabled, s.client, s.available, notification)
}

// NotifyDesktopStartup emits a best-effort startup notification.
func NotifyDesktopStartup(enabled bool, client DesktopClient, available func() bool, info StartupInfo) error {
	return notifyDesktop(enabled, client, available, buildDesktopStartupNotification(info))
}

func notifyDesktop(enabled bool, client DesktopClient, available func() bool, notification DesktopNotification) error {
	if !enabled || client == nil {
		return nil
	}

	if available != nil && !available() {
		return nil
	}

	if err := client.Notify(notification); err != nil {
		return fmt.Errorf("desktop notify: %w", err)
	}

	return nil
}

func buildDesktopNotification(event *snapshot.Event) (DesktopNotification, bool) {
	if event == nil {
		return DesktopNotification{}, false
	}

	switch event.Type {
	case snapshot.EventOperationStarted:
		body := fmt.Sprintf("Indexing started (%s)", event.Info.Mode)
		if event.Path != "" {
			body = fmt.Sprintf("%s indexing started (%s)", event.Path, event.Info.Mode)
		}

		return DesktopNotification{
			Title: "cfmantic-code indexing started",
			Body:  body,
		}, true
	case snapshot.EventOperationCompleted:
		body := fmt.Sprintf("Indexed successfully (%d files, %d chunks)", event.Info.IndexedFiles, event.Info.TotalChunks)
		if event.Path != "" {
			body = fmt.Sprintf("%s indexed successfully (%d files, %d chunks)", event.Path, event.Info.IndexedFiles, event.Info.TotalChunks)
		}

		return DesktopNotification{
			Title: "cfmantic-code indexing complete",
			Body:  body,
		}, true
	case snapshot.EventOperationFailed:
		body := "Indexing failed: " + event.Info.ErrorMessage
		if event.Path != "" {
			body = fmt.Sprintf("%s failed: %s", event.Path, event.Info.ErrorMessage)
		}

		return DesktopNotification{
			Title: "cfmantic-code indexing failed",
			Body:  body,
		}, true
	default:
		return DesktopNotification{}, false
	}
}

func buildDesktopStartupNotification(info StartupInfo) DesktopNotification {
	parts := []string{"Server ready."}
	if info.WorkingDirectory != "" {
		parts = append(parts, fmt.Sprintf("Working directory: %s.", info.WorkingDirectory))
	}

	body := "Background sync disabled."
	if info.SyncEnabled {
		body = fmt.Sprintf("Background sync enabled (interval: %ds).", info.SyncInterval)
	}

	parts = append(parts, body)

	return DesktopNotification{
		Title: "cfmantic-code server ready",
		Body:  strings.Join(parts, " "),
	}
}
