package config

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allConfigEnvVars lists every env var that Load() reads so tests can clear
// them all before setting only the ones relevant to each sub-test.
var allConfigEnvVars = []string{
	"WORKER_URL",
	"AUTH_TOKEN",
	"RERANK_STRATEGY",
	"EMBEDDING_DIMENSION",
	"CHUNK_SIZE",
	"CHUNK_OVERLAP",
	"CUSTOM_IGNORE_PATTERNS",
	"SPLITTER_TYPE",
	"SYNC_INTERVAL",
	"INDEX_CONCURRENCY",
	"INSERT_BATCH_SIZE",
	"INSERT_CONCURRENCY",
	"DESKTOP_NOTIFICATIONS",
}

func TestConfig_DoesNotExposeCustomExtensions(t *testing.T) {
	_, ok := reflect.TypeFor[Config]().FieldByName("CustomExtensions")
	assert.False(t, ok)
}

// clearConfigEnv unsets all config env vars so each sub-test starts clean.
// It relies on t.Setenv to restore original values on cleanup.
func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range allConfigEnvVars {
		t.Setenv(key, "")
	}
}

// setRequired sets the two mandatory env vars so optional-var tests can focus
// on a single field without worrying about required-var errors.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("WORKER_URL", "https://worker.example.com")
	t.Setenv("AUTH_TOKEN", "secret-token")
}

func withBuildVersion(t *testing.T, version string) {
	t.Helper()

	prev := buildVersion
	buildVersion = version

	t.Cleanup(func() {
		buildVersion = prev
	})
}

// TestLoad_HappyPath verifies that when every env var is provided Load returns
// the expected Config with no error.
func TestLoad_HappyPath(t *testing.T) {
	clearConfigEnv(t)
	withBuildVersion(t, "2.3.4")
	t.Setenv("WORKER_URL", "https://worker.example.com")
	t.Setenv("AUTH_TOKEN", "my-token")
	t.Setenv("RERANK_STRATEGY", "rrf")
	t.Setenv("EMBEDDING_DIMENSION", "512")
	t.Setenv("CHUNK_SIZE", "1000")
	t.Setenv("CHUNK_OVERLAP", "100")
	t.Setenv("CUSTOM_IGNORE_PATTERNS", "*.tmp,*.log")
	t.Setenv("SPLITTER_TYPE", "text")
	t.Setenv("SYNC_INTERVAL", "600")
	t.Setenv("INDEX_CONCURRENCY", "4")
	t.Setenv("INSERT_BATCH_SIZE", "192")
	t.Setenv("INSERT_CONCURRENCY", "3")
	t.Setenv("DESKTOP_NOTIFICATIONS", "true")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "https://worker.example.com", cfg.WorkerURL)
	assert.Equal(t, "my-token", cfg.AuthToken)
	assert.Equal(t, "rrf", cfg.RerankStrategy)
	assert.Equal(t, 512, cfg.EmbeddingDimension)
	assert.Equal(t, 1000, cfg.ChunkSize)
	assert.Equal(t, 100, cfg.ChunkOverlap)
	assert.Equal(t, []string{"*.tmp", "*.log"}, cfg.CustomIgnore)
	assert.Equal(t, "cfmantic-code", cfg.ServerName)
	assert.Equal(t, "2.3.4", cfg.ServerVersion)
	assert.Equal(t, "text", cfg.SplitterType)
	assert.Equal(t, 600, cfg.SyncInterval)
	assert.Equal(t, 4, cfg.IndexConcurrency)
	assert.Equal(t, 192, cfg.InsertBatchSize)
	assert.Equal(t, 3, cfg.InsertConcurrency)
	assert.True(t, cfg.DesktopNotifications)
}

// TestLoad_RequiredVarsMissing checks that missing required vars return errors.
func TestLoad_RequiredVarsMissing(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		wantErr string
	}{
		{
			name: "WORKER_URL missing",
			setup: func(t *testing.T) {
				t.Helper()
				// AUTH_TOKEN set but WORKER_URL empty (cleared by clearConfigEnv)
				t.Setenv("AUTH_TOKEN", "tok")
			},
			wantErr: "WORKER_URL is required",
		},
		{
			name: "AUTH_TOKEN missing",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("WORKER_URL", "https://worker.example.com")
				// AUTH_TOKEN empty (cleared by clearConfigEnv)
			},
			wantErr: "AUTH_TOKEN is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			tc.setup(t)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestLoad_Defaults verifies every field's default value when only required
// env vars are set.
func TestLoad_Defaults(t *testing.T) {
	clearConfigEnv(t)
	setRequired(t)
	withBuildVersion(t, "2.3.4")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	wantIndexConcurrency := defaultIndexConcurrency(runtime.NumCPU())

	assert.Equal(t, 1024, cfg.EmbeddingDimension, "default EMBEDDING_DIMENSION")
	assert.Equal(t, 4000, cfg.ChunkSize, "default CHUNK_SIZE")
	assert.Equal(t, 200, cfg.ChunkOverlap, "default CHUNK_OVERLAP")
	assert.Equal(t, "cfmantic-code", cfg.ServerName, "fixed server name")
	assert.Equal(t, "2.3.4", cfg.ServerVersion, "server version comes from buildVersion")
	assert.Equal(t, "ast", cfg.SplitterType, "default SPLITTER_TYPE")
	assert.Equal(t, 60, cfg.SyncInterval, "default SYNC_INTERVAL")
	assert.Equal(t, wantIndexConcurrency, cfg.IndexConcurrency, "default INDEX_CONCURRENCY")
	assert.Equal(t, 192, cfg.InsertBatchSize, "default INSERT_BATCH_SIZE")
	assert.Equal(t, 4, cfg.InsertConcurrency, "default INSERT_CONCURRENCY")
	assert.Equal(t, "workers_ai", cfg.RerankStrategy, "default RERANK_STRATEGY")
	assert.False(t, cfg.DesktopNotifications, "default DESKTOP_NOTIFICATIONS")
	assert.Nil(t, cfg.CustomIgnore, "default CUSTOM_IGNORE_PATTERNS")
}

func TestLoad_RerankStrategyOverride(t *testing.T) {
	clearConfigEnv(t)
	setRequired(t)
	t.Setenv("RERANK_STRATEGY", "rrf")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "rrf", cfg.RerankStrategy)
}

func TestDefaultIndexConcurrency(t *testing.T) {
	tests := []struct {
		name     string
		cpuCount int
		want     int
	}{
		{name: "zero cpus still returns one", cpuCount: 0, want: 1},
		{name: "single cpu stays at one", cpuCount: 1, want: 1},
		{name: "two cpus halves to one", cpuCount: 2, want: 1},
		{name: "odd cpu count rounds down", cpuCount: 7, want: 3},
		{name: "even cpu count halves", cpuCount: 12, want: 6},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, defaultIndexConcurrency(tc.cpuCount))
		})
	}
}

func TestLoad_DesktopNotifications(t *testing.T) {
	t.Run("enabled via boolean env", func(t *testing.T) {
		clearConfigEnv(t)
		setRequired(t)
		t.Setenv("DESKTOP_NOTIFICATIONS", "true")

		cfg, err := Load()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.True(t, cfg.DesktopNotifications)
	})

	t.Run("invalid boolean returns error", func(t *testing.T) {
		clearConfigEnv(t)
		setRequired(t)
		t.Setenv("DESKTOP_NOTIFICATIONS", "definitely")

		cfg, err := Load()
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "DESKTOP_NOTIFICATIONS must be a boolean")
	})
}

// TestLoad_InvalidIntegers checks that non-numeric values for integer fields
// produce the appropriate parse error.
func TestLoad_InvalidIntegers(t *testing.T) {
	tests := []struct {
		envKey  string
		wantErr string
	}{
		{"EMBEDDING_DIMENSION", "EMBEDDING_DIMENSION must be an integer"},
		{"CHUNK_SIZE", "CHUNK_SIZE must be an integer"},
		{"CHUNK_OVERLAP", "CHUNK_OVERLAP must be an integer"},
		{"SYNC_INTERVAL", "SYNC_INTERVAL must be an integer"},
		{"INDEX_CONCURRENCY", "INDEX_CONCURRENCY must be a positive integer"},
		{"INSERT_BATCH_SIZE", "INSERT_BATCH_SIZE must be a positive integer"},
		{"INSERT_CONCURRENCY", "INSERT_CONCURRENCY must be a positive integer"},
	}

	for _, tc := range tests {
		t.Run(tc.envKey+"_invalid", func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv(tc.envKey, "not-a-number")

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoad_EmbeddingDimensionValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("EMBEDDING_DIMENSION", tc.value)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "EMBEDDING_DIMENSION must be a positive integer")
		})
	}
}

func TestLoad_ChunkSizeAndOverlapValidation(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize string
		chunkOver string
		wantErr   string
	}{
		{name: "chunk size zero", chunkSize: "0", chunkOver: "0", wantErr: "CHUNK_SIZE must be a positive integer"},
		{name: "chunk size negative", chunkSize: "-1", chunkOver: "0", wantErr: "CHUNK_SIZE must be a positive integer"},
		{name: "chunk overlap negative", chunkSize: "8000", chunkOver: "-1", wantErr: "CHUNK_OVERLAP must be >= 0 and less than CHUNK_SIZE"},
		{name: "chunk overlap equals chunk size", chunkSize: "8000", chunkOver: "8000", wantErr: "CHUNK_OVERLAP must be >= 0 and less than CHUNK_SIZE"},
		{name: "chunk overlap greater than chunk size", chunkSize: "8000", chunkOver: "8001", wantErr: "CHUNK_OVERLAP must be >= 0 and less than CHUNK_SIZE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("CHUNK_SIZE", tc.chunkSize)
			t.Setenv("CHUNK_OVERLAP", tc.chunkOver)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoad_RerankStrategyValidation(t *testing.T) {
	tests := []string{"linear", "RRF", "workers-ai"}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("RERANK_STRATEGY", value)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "RERANK_STRATEGY must be one of")
		})
	}
}

// TestLoad_IndexConcurrencyValidation checks boundary conditions for
// INDEX_CONCURRENCY: zero and negative values must be rejected.
func TestLoad_IndexConcurrencyValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("INDEX_CONCURRENCY", tc.value)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "INDEX_CONCURRENCY must be a positive integer")
		})
	}
}

// TestLoad_InsertBatchSizeValidation checks boundary conditions for
// INSERT_BATCH_SIZE: zero and negative values must be rejected.
func TestLoad_InsertBatchSizeValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("INSERT_BATCH_SIZE", tc.value)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "INSERT_BATCH_SIZE must be a positive integer")
		})
	}
}

// TestLoad_InsertConcurrencyValidation checks boundary conditions for
// INSERT_CONCURRENCY: zero and negative values must be rejected.
func TestLoad_InsertConcurrencyValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("INSERT_CONCURRENCY", tc.value)

			cfg, err := Load()
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), "INSERT_CONCURRENCY must be a positive integer")
		})
	}
}

// TestLoad_SyncIntervalValidation verifies SYNC_INTERVAL boundary conditions:
// 0 is valid (disables sync), negative values must be rejected.
func TestLoad_SyncIntervalValidation(t *testing.T) {
	t.Run("zero is valid (disables sync)", func(t *testing.T) {
		clearConfigEnv(t)
		setRequired(t)
		t.Setenv("SYNC_INTERVAL", "0")

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 0, cfg.SyncInterval)
	})

	t.Run("negative is invalid", func(t *testing.T) {
		clearConfigEnv(t)
		setRequired(t)
		t.Setenv("SYNC_INTERVAL", "-1")

		cfg, err := Load()
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "SYNC_INTERVAL must be >= 0")
	})
}

// TestLoad_SplitterType checks all accepted values and rejects unknown ones.
func TestLoad_SplitterType(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		wantErr      bool
		wantSplitter string
	}{
		{"ast is valid", "ast", false, "ast"},
		{"text is valid", "text", false, "text"},
		{"invalid value", "foobar", true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("SPLITTER_TYPE", tc.value)

			cfg, err := Load()
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.Contains(t, err.Error(), "SPLITTER_TYPE must be")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantSplitter, cfg.SplitterType)
			}
		})
	}
}

// TestLoad_CustomIgnorePatterns covers the CSV parsing for CUSTOM_IGNORE_PATTERNS.
func TestLoad_CustomIgnorePatterns(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantSlice []string
	}{
		{"comma-separated patterns", "*.tmp,*.log", []string{"*.tmp", "*.log"}},
		{"empty string", "", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("CUSTOM_IGNORE_PATTERNS", tc.value)

			cfg, err := Load()
			require.NoError(t, err)
			assert.Equal(t, tc.wantSlice, cfg.CustomIgnore)
		})
	}
}

// TestLoad_CustomIgnorePatterns_SplitCSVEdgeCases exercises splitCSV edge
// cases via CUSTOM_IGNORE_PATTERNS.
func TestLoad_CustomIgnorePatterns_SplitCSVEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantSlice []string
	}{
		{
			name:      "whitespace is trimmed",
			value:     " go , rs , py ",
			wantSlice: []string{"go", "rs", "py"},
		},
		{
			name:      "trailing comma is ignored",
			value:     "go,rs,",
			wantSlice: []string{"go", "rs"},
		},
		{
			name:      "leading comma is ignored",
			value:     ",go,rs",
			wantSlice: []string{"go", "rs"},
		},
		{
			// splitCSV only returns nil when the env var itself is empty string;
			// a non-empty value of all commas produces a non-nil empty slice.
			name:      "only commas returns empty slice",
			value:     ",,",
			wantSlice: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			setRequired(t)
			t.Setenv("CUSTOM_IGNORE_PATTERNS", tc.value)

			cfg, err := Load()
			require.NoError(t, err)
			assert.Equal(t, tc.wantSlice, cfg.CustomIgnore)
		})
	}
}
