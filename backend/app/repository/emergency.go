package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
)

// EmergencyKey is the keyset position of a row of the emergency actions
// list: the block and the position inside it (see EmergencyAction.Order).
type EmergencyKey struct {
	Height int64
	Order  int64
}

// blockOrderBase puts every block-level row after the transactions of its
// block: governance executes its messages in the end block.
const blockOrderBase = int64(1_000_000_000_000)

// msgOrderStride spaces the transactions of a block in the order key:
// tx_index * stride + msg_index.
const msgOrderStride = int64(10_000)

// EmergencyAction is a stored message or block event that pulled an
// emergency switch or released one.
type EmergencyAction struct {
	Height int64
	// Order is the position inside the block: tx_index*10,000 + msg_index
	// for a message, 1,000,000,000,000 + seq for a block event.
	Order int64
	Time  time.Time
	// TxHash is empty for a block-level row.
	TxHash string
	// Kind is the message type URL or the event type.
	Kind string
	// BlockLevel is true for a block event.
	BlockLevel bool
	// Signer is the message's first signer; empty for a block event, whose
	// signer or authority is in Details.
	Signer  string
	Details json.RawMessage
}

// EmergencyActions returns up to limit emergency actions, newest first
// (height, then position, descending), after before when it is not nil:
// the successful messages of the guardian module, the circuit breaker's
// reset and tradebin's halt and unhalt, plus the guardian events of the
// blocks. Each branch reads its own index ((type_url, height) and (type,
// height)) with the keyset bound on the height.
func (r *Explorer) EmergencyActions(ctx context.Context, before *EmergencyKey, limit int) ([]EmergencyAction, error) {
	var bh, bo *int64
	if before != nil {
		bh, bo = &before.Height, &before.Order
	}
	rows, err := r.db.Query(ctx, `
		(SELECT m.height, m.tx_index::bigint * $6 + m.msg_index AS ord, t.time, t.hash, m.type_url,
				false AS block_level, coalesce(m.sender, ''), coalesce(m.body, '{}'::jsonb)
		 FROM explorer.messages m
		 JOIN explorer.transactions t ON t.height = m.height AND t.tx_index = m.tx_index
		 WHERE m.type_url = ANY($1) AND t.success
		   AND ($2::bigint IS NULL OR (m.height <= $2::bigint
		        AND (m.height, m.tx_index::bigint * $6 + m.msg_index) < ($2::bigint, $3::bigint)))
		 ORDER BY m.height DESC, ord DESC LIMIT $4)
		UNION ALL
		(SELECT e.height, $7::bigint + e.seq AS ord, b.time, '', e.type, true, '', e.attrs
		 FROM explorer.block_events e
		 JOIN explorer.blocks b ON b.height = e.height
		 WHERE e.type = ANY($5)
		   AND ($2::bigint IS NULL OR (e.height <= $2::bigint
		        AND (e.height, $7::bigint + e.seq) < ($2::bigint, $3::bigint)))
		 ORDER BY e.height DESC, e.seq DESC LIMIT $4)
		ORDER BY 1 DESC, 2 DESC LIMIT $4`,
		emergency.MessageTypes(), bh, bo, limit, emergency.EventTypes(), msgOrderStride, blockOrderBase)
	if err != nil {
		return nil, fmt.Errorf("emergency actions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EmergencyAction, error) {
		var a EmergencyAction
		err := row.Scan(&a.Height, &a.Order, &a.Time, &a.TxHash, &a.Kind, &a.BlockLevel, &a.Signer, &a.Details)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("emergency actions: %w", err)
	}
	return out, nil
}

// EmergencyDenom is what the denoms table knows of a denom.
type EmergencyDenom struct {
	Symbol   *string
	Exponent int
	LogoURL  *string
}

// EmergencyDenoms returns the denoms table's rows of denoms by denom; a
// denom the table does not know is absent.
func (r *Explorer) EmergencyDenoms(ctx context.Context, denoms []string) (map[string]EmergencyDenom, error) {
	out := map[string]EmergencyDenom{}
	if len(denoms) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT denom, symbol, exponent, logo_url FROM explorer.denoms WHERE denom = ANY($1)`, denoms)
	if err != nil {
		return nil, fmt.Errorf("emergency denoms: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var denom string
		var d EmergencyDenom
		if err := rows.Scan(&denom, &d.Symbol, &d.Exponent, &d.LogoURL); err != nil {
			return nil, fmt.Errorf("emergency denoms: %w", err)
		}
		out[denom] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emergency denoms: %w", err)
	}
	return out, nil
}

// EmergencyChannel is the counterparty of a transfer channel.
type EmergencyChannel struct {
	CounterpartyChainID   *string
	CounterpartyChainName *string
}

// EmergencyChannels returns the counterparty of each channel of ids the
// ibc_channels table knows, by channel id.
func (r *Explorer) EmergencyChannels(ctx context.Context, ids []string) (map[string]EmergencyChannel, error) {
	out := map[string]EmergencyChannel{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT c.channel_id, c.counterparty_chain_id, ch.pretty_name
		FROM explorer.ibc_channels c
		LEFT JOIN explorer.chains ch ON ch.chain_id = c.counterparty_chain_id
		WHERE c.channel_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("emergency channels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var c EmergencyChannel
		if err := rows.Scan(&id, &c.CounterpartyChainID, &c.CounterpartyChainName); err != nil {
			return nil, fmt.Errorf("emergency channels: %w", err)
		}
		out[id] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emergency channels: %w", err)
	}
	return out, nil
}

// EmergencyLabel returns the label name of address, or nil when it has none.
func (r *Explorer) EmergencyLabel(ctx context.Context, address string) (*string, error) {
	var name string
	err := r.db.QueryRow(ctx, `SELECT name FROM explorer.labels WHERE address = $1`, address).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("emergency label: %w", err)
	}
	return &name, nil
}
