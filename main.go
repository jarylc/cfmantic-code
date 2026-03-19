package main

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/visibility"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	mcpserver "cfmantic-code/internal/server"

	filesync "cfmantic-code/internal/sync"

	"github.com/mark3labs/mcp-go/server"
)

var (
	loadConfig       = config.Load
	serveStdio       = server.ServeStdio
	startSyncManager = func(syncMgr *filesync.Manager) {
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

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		return 1
	}

	var sp splitter.Splitter
	if cfg.SplitterType == "ast" {
		sp = splitter.NewASTSplitter(cfg.ChunkSize, cfg.ChunkOverlap)
	} else {
		sp = splitter.NewTextSplitter(cfg.ChunkSize, cfg.ChunkOverlap)
	}

	mc := milvus.NewClient(cfg.WorkerURL, cfg.AuthToken)
	mc.SetRerankStrategy(cfg.RerankStrategy)

	sm := snapshot.NewManager()

	var syncMgr *filesync.Manager
	if cfg.SyncInterval > 0 {
		syncMgr = filesync.NewManager(mc, sm, sp, cfg, cfg.SyncInterval)
	}

	h := handler.New(mc, sm, cfg, sp, syncMgr)
	s := mcpserver.New(cfg, h)
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
		log.Printf("Background sync enabled (interval: %ds)", cfg.SyncInterval)
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Run MCP server in goroutine
	errCh := make(chan error, 1)

	go func() {
		errCh <- serveStdio(s)
	}()

	if err := visibility.NotifyDesktopStartup(cfg.DesktopNotifications, desktopClient, visibility.DesktopAvailable, visibility.StartupInfo{
		WorkingDirectory: resolveStartupWorkingDirectory(handler.CanonicalizePath),
		SyncEnabled:      syncMgr != nil,
		SyncInterval:     cfg.SyncInterval,
	}); err != nil {
		log.Printf("visibility: %v", err)
	}

	// Wait for signal or server error
	exitCode := 0

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)

			exitCode = 1
		}
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down", sig)
	}

	// Cleanup
	if syncMgr != nil {
		stopSyncManager(syncMgr)
	}

	return exitCode
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
