package main

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"testing"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

type stdioListenerFunc func(context.Context, io.Reader, io.Writer) error

func (f stdioListenerFunc) Listen(ctx context.Context, in io.Reader, out io.Writer) error {
	return f(ctx, in, out)
}

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

func stubMainDeps(t *testing.T, cfg *config.Config, listenErr error) *eventRecorder {
	t.Helper()

	rec := &eventRecorder{}

	oldLoadConfig := loadConfig
	oldNewStdioServer := newStdioServer
	oldStartSyncManager := startSyncManager
	oldStopSyncManager := stopSyncManager

	loadConfig = func() (*config.Config, error) {
		rec.add("load-config")
		return cfg, nil
	}
	newStdioServer = func(*server.MCPServer) stdioListener {
		rec.add("new-stdio")

		return stdioListenerFunc(func(context.Context, io.Reader, io.Writer) error {
			rec.add("listen")
			return listenErr
		})
	}
	startSyncManager = func(*filesync.Manager) {
		rec.add("start-sync")
	}
	stopSyncManager = func(*filesync.Manager) {
		rec.add("stop-sync")
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		newStdioServer = oldNewStdioServer
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
	require.Equal(t, []string{"load-config", "start-sync", "new-stdio", "listen", "stop-sync"}, rec.snapshot())
}

func TestRun_CleanShutdownExitsZeroAfterCleanup(t *testing.T) {
	rec := stubMainDeps(t, testMainConfig(), nil)

	require.Equal(t, 0, run())
	require.Equal(t, []string{"load-config", "start-sync", "new-stdio", "listen", "stop-sync"}, rec.snapshot())
}

func TestRun_SignalContextCancellationStopsServerAndCleansUp(t *testing.T) {
	cfg := testMainConfig()
	rec := &eventRecorder{}
	ctx, cancel := context.WithCancel(context.Background())

	oldLoadConfig := loadConfig
	oldNewStdioServer := newStdioServer
	oldStartSyncManager := startSyncManager
	oldStopSyncManager := stopSyncManager
	oldSignalNotifyContext := signalNotifyContext

	loadConfig = func() (*config.Config, error) {
		rec.add("load-config")
		return cfg, nil
	}
	newStdioServer = func(*server.MCPServer) stdioListener {
		rec.add("new-stdio")

		return stdioListenerFunc(func(ctx context.Context, _ io.Reader, _ io.Writer) error {
			rec.add("listen")
			cancel()
			<-ctx.Done()
			rec.add("context-canceled")

			return ctx.Err()
		})
	}
	startSyncManager = func(*filesync.Manager) {
		rec.add("start-sync")
	}
	stopSyncManager = func(*filesync.Manager) {
		rec.add("stop-sync")
	}
	signalNotifyContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, func() {}
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		newStdioServer = oldNewStdioServer
		startSyncManager = oldStartSyncManager
		stopSyncManager = oldStopSyncManager
		signalNotifyContext = oldSignalNotifyContext
	})

	require.Equal(t, 0, run())
	require.Equal(t, []string{"load-config", "start-sync", "new-stdio", "listen", "context-canceled", "stop-sync"}, rec.snapshot())
}

func TestRunWithContext_ContextCancellationReturnsCleanly(t *testing.T) {
	cfg := testMainConfig()
	rec := &eventRecorder{}
	ctx, cancel := context.WithCancel(context.Background())

	oldLoadConfig := loadConfig
	oldNewStdioServer := newStdioServer
	oldStartSyncManager := startSyncManager
	oldStopSyncManager := stopSyncManager

	loadConfig = func() (*config.Config, error) {
		rec.add("load-config")
		return cfg, nil
	}
	newStdioServer = func(*server.MCPServer) stdioListener {
		rec.add("new-stdio")

		return stdioListenerFunc(func(ctx context.Context, _ io.Reader, _ io.Writer) error {
			rec.add("listen")
			cancel()
			<-ctx.Done()
			rec.add("context-canceled")

			return ctx.Err()
		})
	}
	startSyncManager = func(*filesync.Manager) {
		rec.add("start-sync")
	}
	stopSyncManager = func(*filesync.Manager) {
		rec.add("stop-sync")
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		newStdioServer = oldNewStdioServer
		startSyncManager = oldStartSyncManager
		stopSyncManager = oldStopSyncManager
	})

	require.NoError(t, runWithContext(ctx))
	require.Equal(t, []string{"load-config", "start-sync", "new-stdio", "listen", "context-canceled", "stop-sync"}, rec.snapshot())
}

func TestRun_StartupErrorLogsToStderrOnly(t *testing.T) {
	oldLoadConfig := loadConfig
	loadConfig = func() (*config.Config, error) {
		return nil, errors.New("missing worker url")
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
	})

	code, stdout, stderr := captureRunOutput(t, run)
	require.Equal(t, 1, code)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "startup failed during config load: missing worker url")
	require.NotContains(t, stderr, "startup log")
}

func TestRun_StartupConstructorErrorsHavePhaseContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   string
		arrange func(error)
	}{
		{
			name:  "milvus",
			phase: "Milvus client construction",
			arrange: func(err error) {
				newMilvusClient = func(*config.Config) (milvus.VectorClient, error) {
					return nil, err
				}
			},
		},
		{
			name:  "snapshot",
			phase: "snapshot manager construction",
			arrange: func(err error) {
				newSnapshotManager = func() (*snapshot.Manager, error) {
					return nil, err
				}
			},
		},
		{
			name:  "sync",
			phase: "sync manager construction",
			arrange: func(err error) {
				newSyncManager = func(milvus.VectorClient, *snapshot.Manager, splitter.Splitter, *config.Config, int) (*filesync.Manager, error) {
					return nil, err
				}
			},
		},
		{
			name:  "mcp-server",
			phase: "MCP server construction",
			arrange: func(err error) {
				newMCPServer = func(*config.Config, *handler.Handler) (*server.MCPServer, error) {
					return nil, err
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetMainConstructorStubs(t)

			bootErr := errors.New("boom")
			tc.arrange(bootErr)

			code, stdout, stderr := captureRunOutput(t, run)
			require.Equal(t, 1, code)
			require.Empty(t, stdout)

			want := "startup failed during " + tc.phase + ": boom"
			require.Contains(t, stderr, want)
			require.NotContains(t, stderr, "startup log")
		})
	}
}

func resetMainConstructorStubs(t *testing.T) {
	t.Helper()

	oldLoadConfig := loadConfig
	oldNewMilvusClient := newMilvusClient
	oldNewSnapshotManager := newSnapshotManager
	oldNewSyncManager := newSyncManager
	oldNewMCPServer := newMCPServer

	loadConfig = func() (*config.Config, error) {
		return testMainConfig(), nil
	}

	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		newMilvusClient = oldNewMilvusClient
		newSnapshotManager = oldNewSnapshotManager
		newSyncManager = oldNewSyncManager
		newMCPServer = oldNewMCPServer
	})
}

func captureRunOutput(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = outW
	os.Stderr = errW
	log.SetOutput(errW)

	code := fn()

	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())

	os.Stdout = oldStdout
	os.Stderr = oldStderr
	log.SetOutput(oldStderr)

	out, err := io.ReadAll(outR)
	require.NoError(t, err)
	stderr, err := io.ReadAll(errR)
	require.NoError(t, err)

	return code, string(out), string(stderr)
}
