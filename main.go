package main

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/visibility"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	mcpserver "cfmantic-code/internal/server"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/server"
)

type stdioListener interface {
	Listen(ctx context.Context, stdin io.Reader, stdout io.Writer) error
}

type phaseStartupError struct {
	err error
}

func (e *phaseStartupError) Error() string {
	return e.err.Error()
}

func (e *phaseStartupError) Unwrap() error {
	return e.err
}

var (
	errNilMilvusClient    = errors.New("nil Milvus client")
	errNilSnapshotManager = errors.New("nil snapshot manager")
	errNilSyncManager     = errors.New("nil sync manager")
	errNilMCPServer       = errors.New("nil MCP server")

	loadConfig      = config.Load
	newMilvusClient = func(cfg *config.Config) (milvus.VectorClient, error) {
		mc := milvus.NewClient(cfg.WorkerURL, cfg.AuthToken)
		if mc == nil {
			return nil, errNilMilvusClient
		}

		mc.SetRerankStrategy(cfg.RerankStrategy)

		return mc, nil
	}
	newSnapshotManager = func() (*snapshot.Manager, error) {
		sm := snapshot.NewManager()
		if sm == nil {
			return nil, errNilSnapshotManager
		}

		return sm, nil
	}
	newSyncManager = func(mc milvus.VectorClient, sm *snapshot.Manager, sp splitter.Splitter, cfg *config.Config, intervalSeconds int) (*filesync.Manager, error) {
		syncMgr := filesync.NewManager(mc, sm, sp, cfg, intervalSeconds)
		if syncMgr == nil {
			return nil, errNilSyncManager
		}

		return syncMgr, nil
	}
	newMCPServer = func(cfg *config.Config, h *handler.Handler) (*server.MCPServer, error) {
		s := mcpserver.New(cfg, h)
		if s == nil {
			return nil, errNilMCPServer
		}

		return s, nil
	}
	newStdioServer = func(s *server.MCPServer) stdioListener {
		return server.NewStdioServer(s)
	}
	signalNotifyContext = signal.NotifyContext
	startSyncManager    = func(syncMgr *filesync.Manager) {
		syncMgr.Start()
	}
	stopSyncManager = func(syncMgr *filesync.Manager) {
		syncMgr.Stop()
	}
)

func main() {
	os.Exit(run())
}

func run() int {
	log.SetOutput(os.Stderr)

	ctx, stop := signalNotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runWithContext(ctx); err != nil {
		if isStartupPhaseError(err) {
			writeBootDiagnostics(err, os.Stderr)
		} else {
			fmt.Fprintf(os.Stderr, "cfmantic-code error: %v\n", err)
		}

		return 1
	}

	return 0
}

func runWithContext(ctx context.Context) error {
	log.SetOutput(os.Stderr)

	cfg, err := loadConfig()
	if err != nil {
		return startupPhaseError("config load", err)
	}

	var sp splitter.Splitter
	if cfg.SplitterType == "ast" {
		sp = splitter.NewASTSplitter(cfg.ChunkSize, cfg.ChunkOverlap)
	} else {
		sp = splitter.NewTextSplitter(cfg.ChunkSize, cfg.ChunkOverlap)
	}

	mc, err := newMilvusClient(cfg)
	if err != nil {
		return startupPhaseError("Milvus client construction", err)
	}

	sm, err := newSnapshotManager()
	if err != nil {
		return startupPhaseError("snapshot manager construction", err)
	}

	var syncMgr *filesync.Manager
	if cfg.SyncInterval > 0 {
		syncMgr, err = newSyncManager(mc, sm, sp, cfg, cfg.SyncInterval)
		if err != nil {
			return startupPhaseError("sync manager construction", err)
		}
	}

	h := handler.New(mc, sm, cfg, sp, syncMgr)

	s, err := newMCPServer(cfg, h)
	if err != nil {
		return startupPhaseError("MCP server construction", err)
	}

	desktopClient := visibility.BeeepClient{}
	sm.AddObserver(visibility.NewNotifier(
		log.Printf,
		visibility.NewMCPSink(visibility.NewMCPPublisher(s)),
		visibility.NewDesktopSink(cfg.DesktopNotifications, desktopClient, visibility.DesktopAvailable),
	))

	log.Printf("Starting %s v%s", cfg.ServerName, cfg.ServerVersion)

	if syncMgr != nil {
		syncMgr.AutoTrackWorkingDirectory(handler.CanonicalizePath)

		startSyncManager(syncMgr)
		defer stopSyncManager(syncMgr)

		log.Printf("Background sync enabled (interval: %ds)", cfg.SyncInterval)
	}

	// Run MCP server in goroutine
	errCh := make(chan error, 1)
	stdioServer := newStdioServer(s)

	go func() {
		errCh <- stdioServer.Listen(ctx, os.Stdin, os.Stdout)
	}()

	if err := visibility.NotifyDesktopStartup(cfg.DesktopNotifications, desktopClient, visibility.DesktopAvailable, visibility.StartupInfo{
		WorkingDirectory: resolveStartupWorkingDirectory(handler.CanonicalizePath),
		SyncEnabled:      syncMgr != nil,
		SyncInterval:     cfg.SyncInterval,
	}); err != nil {
		log.Printf("visibility: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && (!errors.Is(err, context.Canceled) || ctx.Err() == nil) {
			return fmt.Errorf("server failed: %w", err)
		}
	case <-ctx.Done():
		log.Printf("Shutdown requested, shutting down")

		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("server failed during shutdown: %w", err)
		}
	}

	return nil
}

func resolveStartupWorkingDirectory(canonicalize func(string) (string, error)) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	path, err := canonicalize(cwd)
	if err != nil {
		return cwd
	}

	return path
}

func startupPhaseError(phase string, err error) error {
	return &phaseStartupError{err: fmt.Errorf("startup failed during %s: %w", phase, err)}
}

func isStartupPhaseError(err error) bool {
	var target *phaseStartupError

	return errors.As(err, &target)
}

func writeBootDiagnostics(err error, stderr io.Writer) {
	fmt.Fprintf(stderr, "cfmantic-code startup error: %v\n", err)
}
