package channels

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore writes the set into explorer.ibc_channels, explorer.labels and
// explorer.ibc_transfers.
type PGStore struct {
	db DB
}

var _ Store = (*PGStore)(nil)

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// channelSQL upserts a channel; a counterparty chain or client status the
// node did not answer keeps the stored one, and updated_at moves only when a value
// changes.
const channelSQL = `INSERT INTO explorer.ibc_channels AS c (channel_id, port_id, client_id, connection_id,
		counterparty_chain_id, counterparty_channel_id, state, client_status, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
	ON CONFLICT (channel_id) DO UPDATE SET
		port_id = EXCLUDED.port_id, client_id = COALESCE(EXCLUDED.client_id, c.client_id),
		connection_id = EXCLUDED.connection_id,
		counterparty_chain_id = COALESCE(EXCLUDED.counterparty_chain_id, c.counterparty_chain_id),
		counterparty_channel_id = EXCLUDED.counterparty_channel_id, state = EXCLUDED.state,
		client_status = COALESCE(EXCLUDED.client_status, c.client_status), updated_at = now()
	WHERE (c.port_id, c.client_id, c.connection_id, c.counterparty_chain_id, c.counterparty_channel_id, c.state,
	       c.client_status)
	      IS DISTINCT FROM
	      (EXCLUDED.port_id, COALESCE(EXCLUDED.client_id, c.client_id), EXCLUDED.connection_id,
	       COALESCE(EXCLUDED.counterparty_chain_id, c.counterparty_chain_id), EXCLUDED.counterparty_channel_id,
	       EXCLUDED.state, COALESCE(EXCLUDED.client_status, c.client_status))`

// escrowLabelSQL names a channel's escrow account after its counterparty
// chain: the registry's pretty name, else the chain id, else the channel.
// Labels of another source (seed, manual) are never overwritten.
const escrowLabelSQL = `INSERT INTO explorer.labels AS l (address, name, kind, module, source, updated_at)
	SELECT $1, 'Cross-chain escrow (' || COALESCE(ch.pretty_name, c.counterparty_chain_id, c.channel_id) || ')',
		'` + labels.KindIBCEscrow + `', 'transfer', '` + labels.SourceSync + `', now()
	  FROM explorer.ibc_channels c
	  LEFT JOIN explorer.chains ch ON ch.chain_id = c.counterparty_chain_id
	 WHERE c.channel_id = $2
	ON CONFLICT (address) DO UPDATE SET name = EXCLUDED.name, kind = EXCLUDED.kind, module = EXCLUDED.module,
		updated_at = now()
	WHERE l.source = '` + labels.SourceSync + `'
	  AND (l.name, l.kind, l.module) IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.kind, EXCLUDED.module)`

// transfersChainSQL fills the counterparty chain of the transfers stored
// before their channel's chain was known.
const transfersChainSQL = `UPDATE explorer.ibc_transfers t SET counterparty_chain_id = c.counterparty_chain_id
	  FROM explorer.ibc_channels c
	 WHERE c.channel_id = $1 AND c.counterparty_chain_id IS NOT NULL
	   AND t.channel_id = c.channel_id AND t.counterparty_chain_id IS NULL`

// confirmSQL writes a channel's confirmation; updated_at is the channel's,
// it does not move with a check.
const confirmSQL = `UPDATE explorer.ibc_channels SET confirmed_via = $2, confirmed_at = $3, confirm_checked_at = $4,
		confirm_note = $5
	WHERE channel_id = $1
	  AND (confirmed_via, confirmed_at, confirm_checked_at, confirm_note)
	      IS DISTINCT FROM ($2::text, $3::timestamptz, $4::timestamptz, $5::text)`

// Save writes the channels, their confirmations, their escrow labels and
// the missing transfer chains, in one transaction.
func (s *PGStore) Save(ctx context.Context, rows []Row) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("channels save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(channelSQL, r.ChannelID, r.PortID, null(r.ClientID), null(r.ConnectionID),
			null(r.CounterpartyChainID), null(r.CounterpartyChannelID), null(r.State), null(r.ClientStatus))
		if c := r.Confirmation; c != nil {
			batch.Queue(confirmSQL, r.ChannelID, null(c.Via), nullTime(c.At), nullTime(c.CheckedAt), null(c.Note))
		}
		batch.Queue(escrowLabelSQL, r.Escrow, r.ChannelID)
		batch.Queue(transfersChainSQL, r.ChannelID)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("channels save: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("channels save: %w", err)
	}
	return nil
}

// UnknownChains returns the ids absent from explorer.chains, sorted.
func (s *PGStore) UnknownChains(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id FROM unnest($1::text[]) AS id
		WHERE NOT EXISTS (SELECT 1 FROM explorer.chains WHERE chain_id = id) ORDER BY id`, ids)
	if err != nil {
		return nil, fmt.Errorf("unknown chains: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("unknown chains: %w", err)
	}
	return out, nil
}

// Stored returns every stored channel by channel id.
func (s *PGStore) Stored(ctx context.Context) (map[string]Stored, error) {
	rows, err := s.db.Query(ctx, `SELECT channel_id, coalesce(client_id, ''), coalesce(connection_id, ''),
			coalesce(counterparty_chain_id, ''), coalesce(counterparty_channel_id, ''), coalesce(confirmed_via, ''),
			confirmed_at, confirm_checked_at, coalesce(confirm_note, '')
		FROM explorer.ibc_channels`)
	if err != nil {
		return nil, fmt.Errorf("stored channels: %w", err)
	}
	out := map[string]Stored{}
	var id string
	var st Stored
	var at, checked *time.Time
	_, err = pgx.ForEachRow(rows, []any{&id, &st.ClientID, &st.ConnectionID, &st.CounterpartyChainID,
		&st.CounterpartyChannelID, &st.Confirmation.Via, &at, &checked, &st.Confirmation.Note}, func() error {
		st.Confirmation.At, st.Confirmation.CheckedAt = deref(at), deref(checked)
		out[id] = st
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("stored channels: %w", err)
	}
	return out, nil
}

// Chains returns the chains of ids the registry cache holds (fetched, with
// a registry name).
func (s *PGStore) Chains(ctx context.Context, ids []string) (map[string]ChainRef, error) {
	rows, err := s.db.Query(ctx, `SELECT chain_id, registry_name, coalesce(network_type, ''), rest_urls
		FROM explorer.chains
		WHERE chain_id = ANY($1) AND registry_fetched_at IS NOT NULL AND registry_name IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("channel chains: %w", err)
	}
	out := map[string]ChainRef{}
	var c ChainRef
	_, err = pgx.ForEachRow(rows, []any{&c.ChainID, &c.Name, &c.NetworkType, &c.REST}, func() error {
		out[c.ChainID] = ChainRef{ChainID: c.ChainID, Name: c.Name, NetworkType: c.NetworkType, REST: c.REST}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("channel chains: %w", err)
	}
	return out, nil
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
