// Package testutil holds helpers shared by the acceptance tests.
package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// DefaultDatabaseURL is the PostgreSQL of docker/compose.yml.
const DefaultDatabaseURL = "postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable"

// DatabaseURL returns E2E_DATABASE_URL, or DefaultDatabaseURL when unset.
func DatabaseURL() string {
	if url := os.Getenv("E2E_DATABASE_URL"); url != "" {
		return url
	}
	return DefaultDatabaseURL
}

// Migrate runs `migrate up` (migrations and post-migration steps) on the
// database at DatabaseURL and returns that URL. It is idempotent, so every
// test that needs the explorer schema can call it.
func Migrate(t testing.TB) string {
	t.Helper()
	url := DatabaseURL()
	ctx := context.Background()
	mg, err := migrations.Open(ctx, url)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer func() { _ = mg.Close() }()
	if _, err := mg.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return url
}
