// Package config loads and validates the backend's configuration from
// environment variables (a .env file in the working directory is honoured via
// godotenv). Load fails fast: an invalid value aborts startup with an error
// listing every problem found.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

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

	// LogLevel is a logrus level name (trace, debug, info, warn, error, fatal, panic).
	LogLevel string
	// LogFormat is "text" or "json".
	LogFormat string
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
		HTTPAddr:  envString("HTTP_ADDR", ":8080"),
		LogLevel:  strings.ToLower(envString("LOG_LEVEL", "info")),
		LogFormat: strings.ToLower(envString("LOG_FORMAT", LogFormatText)),
	}

	if _, err := log.ParseLevel(cfg.LogLevel); err != nil {
		fail("LOG_LEVEL must be one of: trace, debug, info, warn, error, fatal, panic (got %q)", cfg.LogLevel)
	}
	switch cfg.LogFormat {
	case LogFormatText, LogFormatJSON:
	default:
		fail("LOG_FORMAT must be one of: text, json (got %q)", cfg.LogFormat)
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
