package classify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer runs a statement; *pgxpool.Pool and pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Mirror rewrites explorer.message_kinds and explorer.block_event_kinds from
// the Go tables (upsert every entry, delete the rows without one), then runs
// explorer.reclassify_unknown() so activity stored as "other" picks up the
// entries added since. Idempotent; `migrate up` runs it after the SQL
// migrations.
func Mirror(ctx context.Context, db Execer) error {
	var (
		urls, kinds, signers, participants, counterparties []string
	)
	for _, m := range Messages() {
		urls = append(urls, m.TypeURL)
		kinds = append(kinds, m.Kind)
		signers = append(signers, m.SignerCategory)
		participants = append(participants, m.ParticipantCategory)
		counterparties = append(counterparties, m.CounterpartyAttr)
	}
	if _, err := db.Exec(ctx, `INSERT INTO explorer.message_kinds
			(type_url, kind, signer_category, participant_category, counterparty_attr)
		SELECT u, k, s, p, NULLIF(c, '')
		  FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) AS t(u, k, s, p, c)
		ON CONFLICT (type_url) DO UPDATE
		SET kind = EXCLUDED.kind, signer_category = EXCLUDED.signer_category,
		    participant_category = EXCLUDED.participant_category, counterparty_attr = EXCLUDED.counterparty_attr`,
		urls, kinds, signers, participants, counterparties); err != nil {
		return fmt.Errorf("message_kinds: %w", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM explorer.message_kinds WHERE type_url <> ALL($1::text[])`, urls); err != nil {
		return fmt.Errorf("message_kinds: delete stale: %w", err)
	}

	var types, eventKinds, categories []string
	var attrs []string // one JSON array per event: unnest cannot take a ragged text[][]
	for _, e := range BlockEvents() {
		types = append(types, e.EventType)
		eventKinds = append(eventKinds, e.Kind)
		categories = append(categories, e.Category)
		attrs = append(attrs, jsonArray(e.AddressAttrs))
	}
	if _, err := db.Exec(ctx, `INSERT INTO explorer.block_event_kinds (event_type, kind, category, address_attrs)
		SELECT t, k, c, ARRAY(SELECT jsonb_array_elements_text(a::jsonb))
		  FROM unnest($1::text[], $2::text[], $3::text[], $4::text[]) AS x(t, k, c, a)
		ON CONFLICT (event_type) DO UPDATE
		SET kind = EXCLUDED.kind, category = EXCLUDED.category, address_attrs = EXCLUDED.address_attrs`,
		types, eventKinds, categories, attrs); err != nil {
		return fmt.Errorf("block_event_kinds: %w", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM explorer.block_event_kinds WHERE event_type <> ALL($1::text[])`, types); err != nil {
		return fmt.Errorf("block_event_kinds: delete stale: %w", err)
	}

	if _, err := db.Exec(ctx, `SELECT explorer.reclassify_unknown()`); err != nil {
		return fmt.Errorf("reclassify_unknown: %w", err)
	}
	return nil
}

func jsonArray(items []string) string {
	b, _ := json.Marshal(items) // []string never fails to marshal
	return string(b)
}
