package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/config"
)

var configVars = []string{"HTTP_ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "NODE_RPC_URL", "CHAIN_ID", "INDEXER_ENABLED", "CORS_ALLOWED_ORIGINS",
	"NODE_GRPC_ADDR", "NODE_GRPC_TLS",
	"ARCHIVE_RPC_URL", "ARCHIVE_RPC_RETRY_URL", "STATUS_INTERVAL", "STATUS_HEIGHT_TOLERANCE",
	"RAW_CACHE_MAX_ENTRIES", "RAW_CACHE_TTL",
	"BACKFILL_ENABLED", "BACKFILL_FLOOR", "BACKFILL_WORKERS", "BACKFILL_BATCH", "BACKFILL_QUIET", "BACKFILL_RATE_LIMIT",
	"AGGREGATOR_URL", "CHAIN_REGISTRY_API_URL", "CHAIN_REGISTRY_RAW_URL"}

// isolate runs the test in an empty working directory (so no stray .env is
// picked up) with every config variable unset; both are restored afterwards.
func isolate(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, k := range configVars {
		t.Setenv(k, "") // registers the restore of the original value
		require.NoError(t, os.Unsetenv(k))
	}
}

func TestLoadDefaults(t *testing.T) {
	isolate(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, &config.Config{HTTPAddr: ":8080", LogLevel: "info", LogFormat: "text",
		NodeRPCURL: "http://127.0.0.1:26657", ChainID: "beezee-1", IndexerEnabled: true,
		NodeGRPCAddr:  "127.0.0.1:9090",
		ArchiveRPCURL: "https://rpc.getbze.com", StatusInterval: time.Minute, StatusHeightTolerance: 5,
		RawCacheMaxEntries: 300, RawCacheTTL: 20 * time.Minute,
		BackfillFloorHeight: 1, BackfillWorkers: 10, BackfillBatch: 50, BackfillQuiet: 2 * time.Second,
		BackfillRateLimit:   20,
		ChainRegistryAPIURL: "https://api.github.com/repos/cosmos/chain-registry/contents",
		ChainRegistryRawURL: "https://raw.githubusercontent.com/cosmos/chain-registry/master"}, cfg)
}

func TestLoadFromEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("LOG_LEVEL", " DEBUG ")
	t.Setenv("LOG_FORMAT", "json")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, &config.Config{HTTPAddr: "127.0.0.1:9090", LogLevel: "debug", LogFormat: "json",
		NodeRPCURL: "http://127.0.0.1:26657", ChainID: "beezee-1", IndexerEnabled: true,
		NodeGRPCAddr:  "127.0.0.1:9090",
		ArchiveRPCURL: "https://rpc.getbze.com", StatusInterval: time.Minute, StatusHeightTolerance: 5,
		RawCacheMaxEntries: 300, RawCacheTTL: 20 * time.Minute,
		BackfillFloorHeight: 1, BackfillWorkers: 10, BackfillBatch: 50, BackfillQuiet: 2 * time.Second,
		BackfillRateLimit:   20,
		ChainRegistryAPIURL: "https://api.github.com/repos/cosmos/chain-registry/contents",
		ChainRegistryRawURL: "https://raw.githubusercontent.com/cosmos/chain-registry/master"}, cfg)
}

func TestLoadDatabaseURL(t *testing.T) {
	isolate(t)
	t.Setenv("DATABASE_URL", " postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable ")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable", cfg.DatabaseURL)
	assert.NoError(t, cfg.RequireDatabase())
}

func TestLoadRejectsInvalidDatabaseURLWithoutEchoingIt(t *testing.T) {
	isolate(t)
	t.Setenv("DATABASE_URL", "postgres://bze:s3cret@127.0.0.1:notaport/bze_index")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestRequireDatabase(t *testing.T) {
	isolate(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.DatabaseURL)
	assert.ErrorIs(t, cfg.RequireDatabase(), config.ErrDatabaseURLRequired)
}

func TestLoadBlankValuesUseDefaults(t *testing.T) {
	isolate(t)
	t.Setenv("HTTP_ADDR", "  ")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_LEVEL", "loud")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
	assert.Contains(t, err.Error(), `"loud"`)
}

func TestLoadRejectsInvalidLogFormat(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_FORMAT", "xml")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_FORMAT")
	assert.Contains(t, err.Error(), `"xml"`)
}

func TestLoadListsEveryProblem(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_LEVEL", "loud")
	t.Setenv("LOG_FORMAT", "xml")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
	assert.Contains(t, err.Error(), "LOG_FORMAT")
}

func TestLoadHonoursDotEnv(t *testing.T) {
	isolate(t)
	dotenv := "HTTP_ADDR=:7070\nLOG_LEVEL=warn\nLOG_FORMAT=json\n"
	require.NoError(t, os.WriteFile(filepath.Join(".", ".env"), []byte(dotenv), 0o600))

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, &config.Config{HTTPAddr: ":7070", LogLevel: "warn", LogFormat: "json",
		NodeRPCURL: "http://127.0.0.1:26657", ChainID: "beezee-1", IndexerEnabled: true,
		NodeGRPCAddr:  "127.0.0.1:9090",
		ArchiveRPCURL: "https://rpc.getbze.com", StatusInterval: time.Minute, StatusHeightTolerance: 5,
		RawCacheMaxEntries: 300, RawCacheTTL: 20 * time.Minute,
		BackfillFloorHeight: 1, BackfillWorkers: 10, BackfillBatch: 50, BackfillQuiet: 2 * time.Second,
		BackfillRateLimit:   20,
		ChainRegistryAPIURL: "https://api.github.com/repos/cosmos/chain-registry/contents",
		ChainRegistryRawURL: "https://raw.githubusercontent.com/cosmos/chain-registry/master"}, cfg)
}

func TestLoadEnvironmentWinsOverDotEnv(t *testing.T) {
	isolate(t)
	require.NoError(t, os.WriteFile(".env", []byte("LOG_LEVEL=warn\n"), 0o600))
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "error", cfg.LogLevel)
}

func TestLoadIndexerSettings(t *testing.T) {
	isolate(t)
	t.Setenv("NODE_RPC_URL", "https://rpc.example.org:443/")
	t.Setenv("CHAIN_ID", "beezee-testnet")
	t.Setenv("INDEXER_ENABLED", "false")
	t.Setenv("NODE_GRPC_ADDR", "grpc.example.org:443")
	t.Setenv("NODE_GRPC_TLS", "true")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://rpc.example.org:443", cfg.NodeRPCURL)
	assert.Equal(t, "beezee-testnet", cfg.ChainID)
	assert.False(t, cfg.IndexerEnabled)
	assert.Equal(t, "grpc.example.org:443", cfg.NodeGRPCAddr)
	assert.True(t, cfg.NodeGRPCTLS)
}

func TestLoadRejectsInvalidIndexerSettings(t *testing.T) {
	cases := map[string]string{
		"INDEXER_ENABLED": "maybe",
		"NODE_RPC_URL":    "127.0.0.1:26657",
		"NODE_GRPC_ADDR":  "http://127.0.0.1:9090",
		"NODE_GRPC_TLS":   "sometimes",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			isolate(t)
			t.Setenv(key, value)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), key)
		})
	}
}

func TestLoadCORSAllowedOrigins(t *testing.T) {
	isolate(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", " https://scan.getbze.com, http://localhost:3000 ,,")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"https://scan.getbze.com", "http://localhost:3000"}, cfg.CORSAllowedOrigins)

	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	cfg, err = config.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"*"}, cfg.CORSAllowedOrigins)
}

func TestLoadRejectsInvalidCORSOrigins(t *testing.T) {
	for _, v := range []string{"scan.getbze.com", "ftp://scan.getbze.com", "https://scan.getbze.com/path", "https://"} {
		isolate(t)
		t.Setenv("CORS_ALLOWED_ORIGINS", v)

		_, err := config.Load()
		require.Error(t, err, v)
		assert.Contains(t, err.Error(), "CORS_ALLOWED_ORIGINS", v)
	}
}

func TestLoadStatusSettings(t *testing.T) {
	isolate(t)
	t.Setenv("ARCHIVE_RPC_URL", "https://archive.example.org/")
	t.Setenv("ARCHIVE_RPC_RETRY_URL", "http://10.0.0.2:26657/")
	t.Setenv("STATUS_INTERVAL", "100ms")
	t.Setenv("STATUS_HEIGHT_TOLERANCE", "0")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://archive.example.org", cfg.ArchiveRPCURL)
	assert.Equal(t, "http://10.0.0.2:26657", cfg.ArchiveRPCRetryURL)
	assert.Equal(t, 100*time.Millisecond, cfg.StatusInterval)
	assert.Equal(t, int64(0), cfg.StatusHeightTolerance)
}

func TestLoadRejectsInvalidStatusSettings(t *testing.T) {
	cases := map[string][]string{
		"ARCHIVE_RPC_URL":         {"rpc.getbze.com"},
		"ARCHIVE_RPC_RETRY_URL":   {"ftp://rpc.getbze.com"},
		"STATUS_INTERVAL":         {"60", "0s", "-1s"},
		"STATUS_HEIGHT_TOLERANCE": {"five", "-1"},
	}
	for key, values := range cases {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				isolate(t)
				t.Setenv(key, value)

				_, err := config.Load()
				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoadRawCacheSettings(t *testing.T) {
	isolate(t)
	t.Setenv("RAW_CACHE_MAX_ENTRIES", "50")
	t.Setenv("RAW_CACHE_TTL", "90s")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 50, cfg.RawCacheMaxEntries)
	assert.Equal(t, 90*time.Second, cfg.RawCacheTTL)
}

func TestLoadRejectsInvalidRawCacheSettings(t *testing.T) {
	cases := map[string][]string{
		"RAW_CACHE_MAX_ENTRIES": {"many", "0", "-1"},
		"RAW_CACHE_TTL":         {"20", "0s", "-1m"},
	}
	for key, values := range cases {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				isolate(t)
				t.Setenv(key, value)

				_, err := config.Load()
				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoadBackfillSettings(t *testing.T) {
	isolate(t)
	t.Setenv("BACKFILL_ENABLED", "true")
	t.Setenv("BACKFILL_FLOOR", "19560001")
	t.Setenv("BACKFILL_WORKERS", "3")
	t.Setenv("BACKFILL_BATCH", "4")
	t.Setenv("BACKFILL_QUIET", "500ms")
	t.Setenv("BACKFILL_RATE_LIMIT", "2.5")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.BackfillEnabled)
	assert.Equal(t, int64(19560001), cfg.BackfillFloorHeight)
	assert.True(t, cfg.BackfillFloorDate.IsZero())
	assert.Equal(t, 3, cfg.BackfillWorkers)
	assert.Equal(t, 4, cfg.BackfillBatch)
	assert.Equal(t, 500*time.Millisecond, cfg.BackfillQuiet)
	assert.InDelta(t, 2.5, cfg.BackfillRateLimit, 0)
}

func TestLoadBackfillFloorForms(t *testing.T) {
	isolate(t)
	t.Setenv("BACKFILL_FLOOR", "GENESIS")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, int64(1), cfg.BackfillFloorHeight)
	assert.True(t, cfg.BackfillFloorDate.IsZero())

	t.Setenv("BACKFILL_FLOOR", "2024-10-08")
	cfg, err = config.Load()
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 10, 8, 0, 0, 0, 0, time.UTC), cfg.BackfillFloorDate)
	assert.Zero(t, cfg.BackfillFloorHeight)
}

func TestLoadRejectsInvalidBackfillSettings(t *testing.T) {
	cases := map[string][]string{
		"BACKFILL_ENABLED":    {"maybe"},
		"BACKFILL_FLOOR":      {"0", "-5", "yesterday", "2024-13-01", "08.10.2024"},
		"BACKFILL_WORKERS":    {"many", "0", "-1"},
		"BACKFILL_BATCH":      {"1.5", "0"},
		"BACKFILL_QUIET":      {"2", "0s"},
		"BACKFILL_RATE_LIMIT": {"fast", "0", "-1", "Inf"},
	}
	for key, values := range cases {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				isolate(t)
				t.Setenv(key, value)

				_, err := config.Load()
				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoadTickerJobSettings(t *testing.T) {
	isolate(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.AggregatorURL, "no prices job by default")
	assert.Equal(t, "https://api.github.com/repos/cosmos/chain-registry/contents", cfg.ChainRegistryAPIURL)
	assert.Equal(t, "https://raw.githubusercontent.com/cosmos/chain-registry/master", cfg.ChainRegistryRawURL)

	t.Setenv("AGGREGATOR_URL", "https://getbze.com/")
	t.Setenv("CHAIN_REGISTRY_API_URL", "http://127.0.0.1:9000/api/")
	t.Setenv("CHAIN_REGISTRY_RAW_URL", "http://127.0.0.1:9000/raw")
	cfg, err = config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://getbze.com", cfg.AggregatorURL)
	assert.Equal(t, "http://127.0.0.1:9000/api", cfg.ChainRegistryAPIURL)
	assert.Equal(t, "http://127.0.0.1:9000/raw", cfg.ChainRegistryRawURL)
}

func TestLoadRejectsInvalidTickerJobSettings(t *testing.T) {
	for _, key := range []string{"AGGREGATOR_URL", "CHAIN_REGISTRY_API_URL", "CHAIN_REGISTRY_RAW_URL"} {
		t.Run(key, func(t *testing.T) {
			isolate(t)
			t.Setenv(key, "getbze.com")
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), key)
		})
	}
}
