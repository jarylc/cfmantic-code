package visibility

import (
	"cfmantic-code/internal/snapshot"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDesktopClient struct {
	notifications []DesktopNotification
	err           error
}

func (f *fakeDesktopClient) Notify(notification DesktopNotification) error {
	f.notifications = append(f.notifications, notification)

	return f.err
}

func TestDesktopSink_IsGatedAndOnlyPublishesMilestones(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		client := &fakeDesktopClient{}
		sink := NewDesktopSink(false, client, func() bool { return true })

		sink.Notify(&snapshot.Event{Type: snapshot.EventOperationStarted, Path: "/tmp/project"})

		assert.Empty(t, client.notifications)
	})

	t.Run("enabled milestone only", func(t *testing.T) {
		client := &fakeDesktopClient{}
		sink := NewDesktopSink(true, client, func() bool { return true })
		path := "/tmp/project"

		for _, event := range []snapshot.Event{
			{Type: snapshot.EventOperationStarted, Path: path, Info: snapshot.CodebaseInfo{Mode: "auto-sync"}},
			{Type: snapshot.EventStepUpdated, Path: path, Info: snapshot.CodebaseInfo{Step: "Walking files"}},
			{Type: snapshot.EventOperationFailed, Path: path, Info: snapshot.CodebaseInfo{ErrorMessage: "boom"}},
		} {
			sink.Notify(&event)
		}

		require.Len(t, client.notifications, 2)
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code indexing started",
			Body:  "/tmp/project indexing started (auto-sync)",
		}, client.notifications[0])
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code indexing failed",
			Body:  "/tmp/project failed: boom",
		}, client.notifications[1])
	})
}

func TestNotifyDesktopStartup_IsGatedAndDescribesSyncState(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		client := &fakeDesktopClient{}

		err := NotifyDesktopStartup(false, client, func() bool { return true }, StartupInfo{
			SyncEnabled:  true,
			SyncInterval: 300,
		})

		require.NoError(t, err)
		assert.Empty(t, client.notifications)
	})

	t.Run("unavailable desktop", func(t *testing.T) {
		client := &fakeDesktopClient{}

		err := NotifyDesktopStartup(true, client, func() bool { return false }, StartupInfo{
			SyncEnabled:  true,
			SyncInterval: 300,
		})

		require.NoError(t, err)
		assert.Empty(t, client.notifications)
	})

	t.Run("background sync enabled", func(t *testing.T) {
		client := &fakeDesktopClient{}

		err := NotifyDesktopStartup(true, client, func() bool { return true }, StartupInfo{
			WorkingDirectory: "/tmp/project",
			SyncEnabled:      true,
			SyncInterval:     300,
		})

		require.NoError(t, err)
		require.Len(t, client.notifications, 1)
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code server ready",
			Body:  "Server ready. Working directory: /tmp/project. Background sync enabled (interval: 300s).",
		}, client.notifications[0])
	})

	t.Run("background sync disabled", func(t *testing.T) {
		client := &fakeDesktopClient{}

		err := NotifyDesktopStartup(true, client, func() bool { return true }, StartupInfo{WorkingDirectory: "/tmp/project"})

		require.NoError(t, err)
		require.Len(t, client.notifications, 1)
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code server ready",
			Body:  "Server ready. Working directory: /tmp/project. Background sync disabled.",
		}, client.notifications[0])
	})

	t.Run("working directory unavailable", func(t *testing.T) {
		client := &fakeDesktopClient{}

		err := NotifyDesktopStartup(true, client, func() bool { return true }, StartupInfo{
			SyncEnabled:  true,
			SyncInterval: 300,
		})

		require.NoError(t, err)
		require.Len(t, client.notifications, 1)
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code server ready",
			Body:  "Server ready. Background sync enabled (interval: 300s).",
		}, client.notifications[0])
	})

	t.Run("client error", func(t *testing.T) {
		client := &fakeDesktopClient{err: errors.New("boom")}

		err := NotifyDesktopStartup(true, client, func() bool { return true }, StartupInfo{})

		require.ErrorContains(t, err, "desktop notify: boom")
	})
}

func TestBeeepClient_Notify(t *testing.T) {
	originalNotify := beeepNotify

	t.Cleanup(func() {
		beeepNotify = originalNotify
	})

	t.Run("success", func(t *testing.T) {
		var got DesktopNotification

		beeepNotify = func(title, body string, appIcon any) error {
			got = DesktopNotification{Title: title, Body: body}

			return nil
		}

		err := (BeeepClient{}).Notify(DesktopNotification{Title: "ready", Body: "done"})

		require.NoError(t, err)
		assert.Equal(t, DesktopNotification{Title: "ready", Body: "done"}, got)
	})

	t.Run("error", func(t *testing.T) {
		beeepNotify = func(title, body string, appIcon any) error {
			return errors.New("boom")
		}

		err := (BeeepClient{}).Notify(DesktopNotification{Title: "ready", Body: "done"})

		require.ErrorContains(t, err, "beeep notify: boom")
	})
}

func TestDesktopAvailable(t *testing.T) {
	originalGOOS := desktopGOOS
	originalGetenv := getenv

	t.Cleanup(func() {
		desktopGOOS = originalGOOS
		getenv = originalGetenv
	})

	t.Run("non linux", func(t *testing.T) {
		desktopGOOS = "darwin"
		getenv = func(string) string {
			return ""
		}

		assert.True(t, DesktopAvailable())
	})

	t.Run("linux without display", func(t *testing.T) {
		desktopGOOS = "linux"
		getenv = func(string) string {
			return ""
		}

		assert.False(t, DesktopAvailable())
	})

	t.Run("linux with display", func(t *testing.T) {
		desktopGOOS = "linux"
		getenv = func(key string) string {
			if key == "DISPLAY" {
				return ":0"
			}

			return ""
		}

		assert.True(t, DesktopAvailable())
	})
}

func TestBuildDesktopNotification(t *testing.T) {
	t.Run("nil event", func(t *testing.T) {
		notification, ok := buildDesktopNotification(nil)

		assert.False(t, ok)
		assert.Equal(t, DesktopNotification{}, notification)
	})

	t.Run("completed event", func(t *testing.T) {
		notification, ok := buildDesktopNotification(&snapshot.Event{
			Type: snapshot.EventOperationCompleted,
			Path: "/tmp/project",
			Info: snapshot.CodebaseInfo{IndexedFiles: 2, TotalChunks: 8},
		})

		require.True(t, ok)
		assert.Equal(t, DesktopNotification{
			Title: "cfmantic-code indexing complete",
			Body:  "/tmp/project indexed successfully (2 files, 8 chunks)",
		}, notification)
	})
}
