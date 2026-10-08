// Package config loads and validates the backend's configuration from
// environment variables (a .env file in the working directory is honoured via
// godotenv). Load fails fast: an invalid value aborts startup with an error
// listing every problem found.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

// Log formats accepted in LOG_FORMAT.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Config holds every runtime setting of the backend. See .env.dist for the
// reference with defaults and comments.
type Config struct {
	// HTTPAddr is the listen address of the HTTP API.
	HTTPAddr string
	// CORSAllowedOrigins are the origins the API answers CORS requests for
	// ("*" for any). Empty sends no CORS headers at all.
	CORSAllowedOrigins []string

	// LogLevel is a logrus level name (trace, debug, info, warn, error, fatal, panic).
	LogLevel string
	// LogFormat is "text" or "json".
	LogFormat string

	// DatabaseURL is the PostgreSQL URL of the node's database (the CometBFT
	// psql sink plus the explorer schema). Optional here; the commands that
	// use the database (serve and migrate) require it. Carries a password:
	// never log it.
	DatabaseURL string

	// NodeRPCURL is the CometBFT RPC endpoint of the local node, read by
	// height only.
	NodeRPCURL string
	// ChainID is the chain the node must report in /status node_info.network;
	// serve refuses to start the indexer on another chain.
	ChainID string
	// IndexerEnabled runs the live indexer in serve. false runs the API only:
	// the one way to run a second process against the same database.
	IndexerEnabled bool

	// ArchiveRPCURL is the CometBFT RPC of an archive node: the status
	// checker compares its tip with the local node's, and the raw-JSON routes
	// fetch the heights they have not cached from it.
	ArchiveRPCURL string
	// ArchiveRPCRetryURL is tried when ArchiveRPCURL fails; empty means
	// ArchiveRPCURL again.
	ArchiveRPCRetryURL string
	// StatusInterval is the period of the status checker's ticks.
	StatusInterval time.Duration
	// StatusHeightTolerance is the largest spread, in blocks, between the
	// explorer, the local node and the archive that still counts as healthy.
	StatusHeightTolerance int64

	// RawCacheMaxEntries bounds each raw-JSON route's in-memory LRU.
	RawCacheMaxEntries int
	// RawCacheTTL is how long a raw-JSON entry lives after it was stored.
	RawCacheTTL time.Duration
}

// Load reads the environment (and a .env file when present), applies
// defaults and validates. It returns an error describing every invalid
// variable at once.
func Load() (*Config, error) {
	// Best effort: no .env file is the normal production case. Variables
	// already set in the environment win over the file.
	_ = godotenv.Load()

	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	cfg := &Config{
		HTTPAddr:    envString("HTTP_ADDR", ":8080"),
		LogLevel:    strings.ToLower(envString("LOG_LEVEL", "info")),
		LogFormat:   strings.ToLower(envString("LOG_FORMAT", LogFormatText)),
		DatabaseURL: envString("DATABASE_URL", ""),
		NodeRPCURL:  strings.TrimRight(envString("NODE_RPC_URL", "http://127.0.0.1:26657"), "/"),
		ChainID:     envString("CHAIN_ID", "beezee-1"),

		ArchiveRPCURL:      strings.TrimRight(envString("ARCHIVE_RPC_URL", "https://rpc.getbze.com"), "/"),
		ArchiveRPCRetryURL: strings.TrimRight(envString("ARCHIVE_RPC_RETRY_URL", ""), "/"),
	}

	for _, o := range strings.Split(envString("CORS_ALLOWED_ORIGINS", ""), ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if u, err := url.Parse(o); o != "*" && (err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "") {
			fail("CORS_ALLOWED_ORIGINS must list * or origins like https://scan.getbze.com (got %q)", o)
			continue
		}
		cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
	}

	indexer := envString("INDEXER_ENABLED", "true")
	if v, err := strconv.ParseBool(indexer); err != nil {
		fail("INDEXER_ENABLED must be true or false (got %q)", indexer)
	} else {
		cfg.IndexerEnabled = v
	}

	if u, err := url.Parse(cfg.NodeRPCURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fail("NODE_RPC_URL must be an http(s) URL (got %q)", cfg.NodeRPCURL)
	}

	for key, v := range map[string]string{"ARCHIVE_RPC_URL": cfg.ArchiveRPCURL, "ARCHIVE_RPC_RETRY_URL": cfg.ArchiveRPCRetryURL} {
		if v == "" && key == "ARCHIVE_RPC_RETRY_URL" {
			continue
		}
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fail("%s must be an http(s) URL (got %q)", key, v)
		}
	}

	interval := envString("STATUS_INTERVAL", "60s")
	if d, err := time.ParseDuration(interval); err != nil || d <= 0 {
		fail("STATUS_INTERVAL must be a positive duration like 60s (got %q)", interval)
	} else {
		cfg.StatusInterval = d
	}

	tolerance := envString("STATUS_HEIGHT_TOLERANCE", "5")
	if n, err := strconv.ParseInt(tolerance, 10, 64); err != nil || n < 0 {
		fail("STATUS_HEIGHT_TOLERANCE must be a non-negative integer (got %q)", tolerance)
	} else {
		cfg.StatusHeightTolerance = n
	}

	maxEntries := envString("RAW_CACHE_MAX_ENTRIES", "300")
	if n, err := strconv.Atoi(maxEntries); err != nil || n <= 0 {
		fail("RAW_CACHE_MAX_ENTRIES must be a positive integer (got %q)", maxEntries)
	} else {
		cfg.RawCacheMaxEntries = n
	}

	ttl := envString("RAW_CACHE_TTL", "20m")
	if d, err := time.ParseDuration(ttl); err != nil || d <= 0 {
		fail("RAW_CACHE_TTL must be a positive duration like 20m (got %q)", ttl)
	} else {
		cfg.RawCacheTTL = d
	}

	if _, err := log.ParseLevel(cfg.LogLevel); err != nil {
		fail("LOG_LEVEL must be one of: trace, debug, info, warn, error, fatal, panic (got %q)", cfg.LogLevel)
	}
	switch cfg.LogFormat {
	case LogFormatText, LogFormatJSON:
	default:
		fail("LOG_FORMAT must be one of: text, json (got %q)", cfg.LogFormat)
	}

	if cfg.DatabaseURL != "" {
		// The parse error is not included: it can echo the URL and its password.
		if _, err := pgx.ParseConfig(cfg.DatabaseURL); err != nil {
			fail("DATABASE_URL is not a valid PostgreSQL connection URL")
		}
	}

	if len(problems) > 0 {
		return nil, errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}

// envString returns the trimmed value of key, or def when it is unset or blank.
func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// ErrDatabaseURLRequired is returned by the commands that need the database
// when DATABASE_URL is not set.
var ErrDatabaseURLRequired = errors.New("invalid configuration: DATABASE_URL is required")

// RequireDatabase returns ErrDatabaseURLRequired when DATABASE_URL is empty.
func (c *Config) RequireDatabase() error {
	if c.DatabaseURL == "" {
		return ErrDatabaseURLRequired
	}
	return nil
}
