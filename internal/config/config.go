package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for config validation.
var (
	ErrWorkerURLRequired              = errors.New("WORKER_URL is required")
	ErrAuthTokenRequired              = errors.New("AUTH_TOKEN is required")
	ErrInvalidSplitterType            = errors.New("SPLITTER_TYPE must be \"ast\" or \"text\"")
	ErrInvalidEmbeddingDim            = errors.New("EMBEDDING_DIMENSION must be a positive integer")
	ErrInvalidChunkSize               = errors.New("CHUNK_SIZE must be a positive integer")
	ErrInvalidChunkOverlap            = errors.New("CHUNK_OVERLAP must be >= 0 and less than CHUNK_SIZE")
	ErrInvalidRerankStrategy          = errors.New("RERANK_STRATEGY must be one of \"workers_ai\" or \"rrf\"")
	ErrInvalidSearchMinRerankScore    = errors.New("SEARCH_MIN_RERANK_SCORE must be a finite number between 0 and 1")
	ErrSearchMinRerankScoreRequiresAI = errors.New("SEARCH_MIN_RERANK_SCORE requires RERANK_STRATEGY=workers_ai")
	ErrSyncIntervalNegative           = errors.New("SYNC_INTERVAL must be >= 0 (0 = disabled)")
	ErrInvalidIncrementalSyncTimeout  = errors.New("INCREMENTAL_SYNC_TIMEOUT_SECONDS must be a positive integer")
	ErrInvalidIndexConcurrency        = errors.New("INDEX_CONCURRENCY must be a positive integer")
	ErrInvalidInsertBatchSize         = errors.New("INSERT_BATCH_SIZE must be a positive integer")
	ErrInvalidInsertConcurrency       = errors.New("INSERT_CONCURRENCY must be a positive integer")
	ErrInvalidDesktopNotify           = errors.New("DESKTOP_NOTIFICATIONS must be a boolean")
)

const defaultRerankStrategy = "workers_ai"

const defaultIncrementalSyncTimeoutSeconds = 600

var buildVersion = "0.1.0"

// Config holds all runtime configuration for the MCP server.
type Config struct {
	WorkerURL              string
	AuthToken              string
	EmbeddingDimension     int
	ChunkSize              int
	ChunkOverlap           int
	CustomIgnore           []string
	ServerName             string
	ServerVersion          string
	SplitterType           string        // SPLITTER_TYPE env var: "ast" (default) or "text"
	RerankStrategy         string        // RERANK_STRATEGY env var: Milvus hybrid rerank strategy ("workers_ai" or "rrf", default workers_ai)
	SearchMinRerankScore   *float64      // SEARCH_MIN_RERANK_SCORE env var: optional inclusive Workers AI score threshold [0,1]
	SyncInterval           int           // SYNC_INTERVAL env var: seconds between sync cycles (default 60)
	IncrementalSyncTimeout time.Duration // INCREMENTAL_SYNC_TIMEOUT_SECONDS env var: maximum duration for one incremental sync (default 600 seconds)
	IndexConcurrency       int           // INDEX_CONCURRENCY env var: parallel workers for indexing (default: NumCPU)
	InsertBatchSize        int           // INSERT_BATCH_SIZE env var: entities per insert request (default 192)
	InsertConcurrency      int           // INSERT_CONCURRENCY env var: concurrent HTTP insert calls to worker (default 2)
	DesktopNotifications   bool          // DESKTOP_NOTIFICATIONS env var: enable best-effort OS notifications (default false)
}

func defaultIndexConcurrency(cpuCount int) int {
	return max(1, cpuCount/2)
}

// Load reads configuration from environment variables and validates required fields.
func Load() (*Config, error) {
	workerURL := os.Getenv("WORKER_URL")
	if workerURL == "" {
		return nil, ErrWorkerURLRequired
	}

	authToken := os.Getenv("AUTH_TOKEN")
	if authToken == "" {
		return nil, ErrAuthTokenRequired
	}

	embeddingDimension := 1024

	if v := os.Getenv("EMBEDDING_DIMENSION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("EMBEDDING_DIMENSION must be an integer: %w", err)
		}

		if n < 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidEmbeddingDim, v)
		}

		embeddingDimension = n
	}

	chunkSize := 4000

	if v := os.Getenv("CHUNK_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("CHUNK_SIZE must be an integer: %w", err)
		}

		if n < 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidChunkSize, v)
		}

		chunkSize = n
	}

	chunkOverlap := 200

	if v := os.Getenv("CHUNK_OVERLAP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("CHUNK_OVERLAP must be an integer: %w", err)
		}

		if n < 0 || n >= chunkSize {
			return nil, fmt.Errorf("%w: %s", ErrInvalidChunkOverlap, v)
		}

		chunkOverlap = n
	}

	splitCSV := func(env string) []string {
		v := os.Getenv(env)
		if v == "" {
			return nil
		}

		parts := strings.Split(v, ",")

		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}

		return out
	}

	serverName := "cfmantic-code"
	serverVersion := buildVersion

	splitterType := os.Getenv("SPLITTER_TYPE")
	if splitterType == "" {
		splitterType = "ast"
	} else if splitterType != "ast" && splitterType != "text" {
		return nil, fmt.Errorf("%w, got %q", ErrInvalidSplitterType, splitterType)
	}

	rerankStrategy := os.Getenv("RERANK_STRATEGY")
	if rerankStrategy == "" {
		rerankStrategy = defaultRerankStrategy
	} else if rerankStrategy != "workers_ai" && rerankStrategy != "rrf" {
		return nil, fmt.Errorf("%w, got %q", ErrInvalidRerankStrategy, rerankStrategy)
	}

	var searchMinRerankScore *float64

	if v := os.Getenv("SEARCH_MIN_RERANK_SCORE"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidSearchMinRerankScore, v)
		}

		if rerankStrategy != defaultRerankStrategy {
			return nil, ErrSearchMinRerankScoreRequiresAI
		}

		searchMinRerankScore = &n
	}

	syncInterval := 60

	if v := os.Getenv("SYNC_INTERVAL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SYNC_INTERVAL must be an integer: %w", err)
		}

		if n < 0 {
			return nil, ErrSyncIntervalNegative
		}

		syncInterval = n
	}

	incrementalSyncTimeoutSeconds := defaultIncrementalSyncTimeoutSeconds

	if v := os.Getenv("INCREMENTAL_SYNC_TIMEOUT_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || int64(n) > math.MaxInt64/int64(time.Second) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidIncrementalSyncTimeout, v)
		}

		incrementalSyncTimeoutSeconds = n
	}

	indexConcurrency := defaultIndexConcurrency(runtime.NumCPU())

	if v := os.Getenv("INDEX_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidIndexConcurrency, v)
		}

		indexConcurrency = n
	}

	insertBatchSize := 192

	if v := os.Getenv("INSERT_BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidInsertBatchSize, v)
		}

		insertBatchSize = n
	}

	insertConcurrency := 4

	if v := os.Getenv("INSERT_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidInsertConcurrency, v)
		}

		insertConcurrency = n
	}

	desktopNotifications := false

	if v := os.Getenv("DESKTOP_NOTIFICATIONS"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidDesktopNotify, v)
		}

		desktopNotifications = enabled
	}

	return &Config{
		WorkerURL:              workerURL,
		AuthToken:              authToken,
		EmbeddingDimension:     embeddingDimension,
		ChunkSize:              chunkSize,
		ChunkOverlap:           chunkOverlap,
		CustomIgnore:           splitCSV("CUSTOM_IGNORE_PATTERNS"),
		ServerName:             serverName,
		ServerVersion:          serverVersion,
		SplitterType:           splitterType,
		RerankStrategy:         rerankStrategy,
		SearchMinRerankScore:   searchMinRerankScore,
		SyncInterval:           syncInterval,
		IncrementalSyncTimeout: time.Duration(incrementalSyncTimeoutSeconds) * time.Second,
		IndexConcurrency:       indexConcurrency,
		InsertBatchSize:        insertBatchSize,
		InsertConcurrency:      insertConcurrency,
		DesktopNotifications:   desktopNotifications,
	}, nil
}
