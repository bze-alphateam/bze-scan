package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// The order keys of the rows of one block, ascending: the transactions'
// messages, then the block events, then the messages of the proposals that
// passed in it (governance executes in the end block). Every key is unique,
// so the (height, order) keyset pages without skipping or repeating a row.
const (
	// msgOrderStride spaces the transactions of a block: tx_index * stride +
	// msg_index.
	msgOrderStride = int64(10_000)
	// blockOrderBase + seq orders the block events.
	blockOrderBase = int64(1_000_000_000_000)
	// proposalOrderBase + proposal id * proposalOrderStride + message index
	// orders the messages of passed proposals.
	proposalOrderBase   = int64(2_000_000_000_000)
	proposalOrderStride = int64(1_000)
)

// The sources of an EmergencyAction.
const (
	EmergencyFromTx       = "tx"
	EmergencyFromEvent    = "event"
	EmergencyFromProposal = "proposal"
)

// EmergencyAction is a stored message or block event that pulled an
// emergency switch or released one, or a message a passed proposal executed.
type EmergencyAction struct {
	Height int64
	// Order is the position inside the block (see the order bases).
	Order int64
	Time  time.Time
	// TxHash is empty unless Source is tx.
	TxHash string
	// Kind is the message type URL or the event type.
	Kind string
	// Source is EmergencyFromTx, EmergencyFromEvent or EmergencyFromProposal.
	Source string
	// Signer is the message's first signer; empty for an event, whose
	// signer or authority is in Details, and for a proposal's message.
	Signer  string
	Details json.RawMessage
	// ProposalID is set when Source is proposal.
	ProposalID *int64
}

// EmergencyActions returns up to limit emergency actions, newest first
// (height, then position, descending), after before when it is not nil,
// from three sources: the successful messages of the guardian module and
// the circuit breaker's reset and tradebin's halt and unhalt, signed in a
// transaction; the guardian events of the blocks (guardian messages
// governance executed); and the messages of passed proposals, for the types
// only governance can run (which are never transactions' messages). Each
// branch reads its own index and carries the keyset bound on the height, so
// the partitions above the cursor are pruned.
func (r *Explorer) EmergencyActions(ctx context.Context, before *EmergencyKey, limit int) ([]EmergencyAction, error) {
	bh, bo := int64(math.MaxInt64), int64(math.MaxInt64)
	if before != nil {
		bh, bo = before.Height, before.Order
	}
	rows, err := r.db.Query(ctx, `
		(SELECT m.height, m.tx_index::bigint * $6 + m.msg_index AS ord, t.time, t.hash, m.type_url,
				'tx' AS src, coalesce(m.sender, ''), coalesce(m.body, '{}'::jsonb), NULL::bigint AS proposal_id
		 FROM explorer.messages m
		 JOIN explorer.transactions t ON t.height = m.height AND t.tx_index = m.tx_index
		 WHERE m.type_url = ANY($1) AND t.success
		   AND m.height <= $2 AND (m.height, m.tx_index::bigint * $6 + m.msg_index) < ($2, $3)
		 ORDER BY m.height DESC, ord DESC LIMIT $4)
		UNION ALL
		(SELECT e.height, $7::bigint + e.seq AS ord, b.time, '', e.type, 'event', '', e.attrs, NULL::bigint
		 FROM explorer.block_events e
		 JOIN explorer.blocks b ON b.height = e.height
		 WHERE e.type = ANY($5)
		   AND e.height <= $2 AND (e.height, $7::bigint + e.seq) < ($2, $3)
		 ORDER BY e.height DESC, e.seq DESC LIMIT $4)
		UNION ALL
		(SELECT p.resolved_height, $8::bigint + p.id * $9 + (x.idx - 1) AS ord,
				coalesce(b.time, p.voting_end_time, p.updated_at), '', x.msg ->> '@type', 'proposal', '',
				x.msg || jsonb_build_object('proposal_id', p.id), p.id
		 FROM explorer.proposals p
		 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(p.messages) = 'array' THEN p.messages ELSE '[]'::jsonb END)
				WITH ORDINALITY AS x(msg, idx)
		 LEFT JOIN explorer.blocks b ON b.height = p.resolved_height
		 WHERE p.status = 'passed' AND p.resolved_height IS NOT NULL AND p.message_types && $10
		   AND x.msg ->> '@type' = ANY($10)
		   AND p.resolved_height <= $2 AND (p.resolved_height, $8::bigint + p.id * $9 + (x.idx - 1)) < ($2, $3)
		 ORDER BY p.resolved_height DESC, ord DESC LIMIT $4)
		ORDER BY 1 DESC, 2 DESC LIMIT $4`,
		emergency.MessageTypes(), bh, bo, limit, emergency.EventTypes(), msgOrderStride, blockOrderBase,
		proposalOrderBase, proposalOrderStride, emergency.GovernanceMessageTypes())
	if err != nil {
		return nil, fmt.Errorf("emergency actions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EmergencyAction, error) {
		var a EmergencyAction
		err := row.Scan(&a.Height, &a.Order, &a.Time, &a.TxHash, &a.Kind, &a.Source, &a.Signer, &a.Details, &a.ProposalID)
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

// EmergencyLabel is the label of the guardian address: its name and kind.
type EmergencyLabel struct {
	Name string
	Kind string
}

// EmergencyLabel returns the label of address, or nil when it has none.
func (r *Explorer) EmergencyLabel(ctx context.Context, address string) (*EmergencyLabel, error) {
	var l EmergencyLabel
	err := r.db.QueryRow(ctx, `SELECT name, kind FROM explorer.labels WHERE address = $1`, address).Scan(&l.Name, &l.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("emergency label: %w", err)
	}
	return &l, nil
}
