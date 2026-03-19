package main

import (
	"cfmantic-code/internal/config"
	"errors"
	"sync"
	"testing"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

type eventRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *eventRecorder) add(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *eventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.events...)
}

func stubMainDeps(t *testing.T, cfg *config.Config, serveErr error) *eventRecorder {
	t.Helper()

	rec := &eventRecorder{}

	oldLoadConfig := loadConfig
	oldServeStdio := serveStdio
	oldStartSyncManager := startSyncManager
	oldStopSyncManager := stopSyncManager

	loadConfig = func() (*config.Config, error) {
		rec.add("load-config")
		return cfg, nil
	}
	serveStdio = func(*server.MCPServer, ...server.StdioOption) error {
		rec.add("serve")
		return serveErr
	}
	startSyncManager = func(*filesync.Manager) {
		rec.add("start-sync")
	}
	stopSyncManager = func(*filesync.Manager) {
		rec.add("stop-sync")
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		serveStdio = oldServeStdio
		startSyncManager = oldStartSyncManager
		stopSyncManager = oldStopSyncManager
	})

	return rec
}

func testMainConfig() *config.Config {
	return &config.Config{
		WorkerURL:            "https://worker.example",
		AuthToken:            "token",
		EmbeddingDimension:   1024,
		ChunkSize:            1,
		ChunkOverlap:         0,
		ServerName:           "cfmantic-code",
		ServerVersion:        "0.1.0",
		SplitterType:         "text",
		RerankStrategy:       "workers_ai",
		SyncInterval:         1,
		IndexConcurrency:     1,
		InsertBatchSize:      1,
		InsertConcurrency:    1,
		DesktopNotifications: false,
	}
}

func TestRun_ServerErrorExitsNonZeroAfterCleanup(t *testing.T) {
	rec := stubMainDeps(t, testMainConfig(), errors.New("serve boom"))

	require.Equal(t, 1, run())
	require.Equal(t, []string{"load-config", "start-sync", "serve", "stop-sync"}, rec.snapshot())
}

func TestRun_CleanShutdownExitsZeroAfterCleanup(t *testing.T) {
	rec := stubMainDeps(t, testMainConfig(), nil)

	require.Equal(t, 0, run())
	require.Equal(t, []string{"load-config", "start-sync", "serve", "stop-sync"}, rec.snapshot())
}
