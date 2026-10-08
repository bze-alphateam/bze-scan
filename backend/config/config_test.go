package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var configVars = []string{"HTTP_ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL"}

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

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, &Config{HTTPAddr: ":8080", LogLevel: "info", LogFormat: "text"}, cfg)
}

func TestLoadFromEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("LOG_LEVEL", " DEBUG ")
	t.Setenv("LOG_FORMAT", "json")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, &Config{HTTPAddr: "127.0.0.1:9090", LogLevel: "debug", LogFormat: "json"}, cfg)
}

func TestLoadDatabaseURL(t *testing.T) {
	isolate(t)
	t.Setenv("DATABASE_URL", " postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable ")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable", cfg.DatabaseURL)
	assert.NoError(t, cfg.RequireDatabase())
}

func TestLoadRejectsInvalidDatabaseURLWithoutEchoingIt(t *testing.T) {
	isolate(t)
	t.Setenv("DATABASE_URL", "postgres://bze:s3cret@127.0.0.1:notaport/bze_index")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestRequireDatabase(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.DatabaseURL)
	assert.ErrorIs(t, cfg.RequireDatabase(), ErrDatabaseURLRequired)
}

func TestLoadBlankValuesUseDefaults(t *testing.T) {
	isolate(t)
	t.Setenv("HTTP_ADDR", "  ")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_LEVEL", "loud")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
	assert.Contains(t, err.Error(), `"loud"`)
}

func TestLoadRejectsInvalidLogFormat(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_FORMAT", "xml")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_FORMAT")
	assert.Contains(t, err.Error(), `"xml"`)
}

func TestLoadListsEveryProblem(t *testing.T) {
	isolate(t)
	t.Setenv("LOG_LEVEL", "loud")
	t.Setenv("LOG_FORMAT", "xml")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
	assert.Contains(t, err.Error(), "LOG_FORMAT")
}

func TestLoadHonoursDotEnv(t *testing.T) {
	isolate(t)
	dotenv := "HTTP_ADDR=:7070\nLOG_LEVEL=warn\nLOG_FORMAT=json\n"
	require.NoError(t, os.WriteFile(filepath.Join(".", ".env"), []byte(dotenv), 0o600))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, &Config{HTTPAddr: ":7070", LogLevel: "warn", LogFormat: "json"}, cfg)
}

func TestLoadEnvironmentWinsOverDotEnv(t *testing.T) {
	isolate(t)
	require.NoError(t, os.WriteFile(".env", []byte("LOG_LEVEL=warn\n"), 0o600))
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "error", cfg.LogLevel)
}
