package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// IBCTransfer is an ibc_transfers row with its counterparty chain's
// registry names.
type IBCTransfer struct {
	ChannelID      string
	Sequence       int64
	Direction      string
	Status         string
	Sender         string
	Receiver       string
	Denom          string
	Amount         string
	Memo           *string
	CreatedHeight  int64
	CreatedTxIndex int64
	CreatedTxHash  string
	CreatedTime    time.Time
	TimeoutHeight  *string
	TimeoutTime    *time.Time
	ResultHeight   *int64
	ResultTxHash   *string
	ResultTime     *time.Time
	AckError       *string
	// Expired is a pending transfer past its timeout time.
	Expired      bool
	Counterparty Chain
}

// Chain is a counterparty chain as the registry cache names it; every
// field but the chain id is nil when the registry does not know it, and
// the chain id is nil when the channel's chain is not known.
type Chain struct {
	ChainID            *string
	PrettyName         *string
	LogoURL            *string
	ExplorerAccountURL *string
	ExplorerTxURL      *string
}

// IBCChannel is an ibc_channels row with its counterparty chain.
type IBCChannel struct {
	ChannelID             string
	PortID                string
	ClientID              *string
	ConnectionID          *string
	CounterpartyChannelID *string
	State                 *string
	// ClientStatus is the light client's status: active, expired, frozen.
	ClientStatus *string
	UpdatedAt    time.Time
	Counterparty Chain
}

// IBCKey is the keyset position of a transfer in an account's list: its
// creating transaction, then its channel number and sequence.
type IBCKey struct {
	Height   int64
	TxIndex  int64
	Channel  int64
	Sequence int64
}

// IBC directions of an account's list.
const (
	IBCDirectionOut = "out"
	IBCDirectionIn  = "in"
)

const ibcCols = `t.channel_id, t.sequence, t.direction, t.status, t.sender, t.receiver, t.denom, t.amount::text,
		t.memo, t.created_height, t.created_tx_index, t.created_tx_hash, t.created_time, t.timeout_height,
		t.timeout_time, t.result_height, t.result_tx_hash, t.result_time, t.ack_error,
		(t.status = 'pending' AND t.timeout_time IS NOT NULL AND t.timeout_time < now()) AS expired,
		t.counterparty_chain_id, ch.pretty_name, ch.logo_url, ch.explorer_account_url, ch.explorer_tx_url`

// ibcChannelNumber orders channels by their number: channel-10 after
// channel-9.
const ibcChannelNumber = `split_part(t.channel_id, '-', 2)::bigint`

func scanIBCTransfer(row pgx.CollectableRow) (IBCTransfer, error) {
	var t IBCTransfer
	c := &t.Counterparty
	err := row.Scan(&t.ChannelID, &t.Sequence, &t.Direction, &t.Status, &t.Sender, &t.Receiver, &t.Denom, &t.Amount,
		&t.Memo, &t.CreatedHeight, &t.CreatedTxIndex, &t.CreatedTxHash, &t.CreatedTime, &t.TimeoutHeight,
		&t.TimeoutTime, &t.ResultHeight, &t.ResultTxHash, &t.ResultTime, &t.AckError, &t.Expired,
		&c.ChainID, &c.PrettyName, &c.LogoURL, &c.ExplorerAccountURL, &c.ExplorerTxURL)
	return t, err
}

func (r *Explorer) ibcTransfers(ctx context.Context, what, sql string, args ...any) ([]IBCTransfer, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scanIBCTransfer)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// TxIBCTransfers returns the cross-chain transfers the transaction hash
// created (a MsgTransfer, a MsgRecvPacket) or settled (an acknowledgement,
// a timeout), in channel and sequence order.
func (r *Explorer) TxIBCTransfers(ctx context.Context, hash string) ([]IBCTransfer, error) {
	return r.ibcTransfers(ctx, "cross-chain transfers of "+hash, `SELECT `+ibcCols+`
		FROM explorer.ibc_transfers t LEFT JOIN explorer.chains ch ON ch.chain_id = t.counterparty_chain_id
		WHERE t.created_tx_hash = $1 OR t.result_tx_hash = $1
		ORDER BY `+ibcChannelNumber+`, t.sequence, t.direction`, hash)
}

// AccountIBCTransfers returns up to limit cross-chain transfers of address,
// newest first (creating transaction, then channel number and sequence,
// descending): the ones it sent (direction out), the ones it received (in),
// or both when direction is empty, after before when it is not nil. Each
// direction reads its own index.
func (r *Explorer) AccountIBCTransfers(ctx context.Context, address, direction string, before *IBCKey,
	limit int) ([]IBCTransfer, error) {
	var bh, bt, bc, bs *int64
	if before != nil {
		bh, bt, bc, bs = &before.Height, &before.TxIndex, &before.Channel, &before.Sequence
	}
	order := `t.created_height DESC, t.created_tx_index DESC, ` + ibcChannelNumber + ` DESC, t.sequence DESC`
	side := func(dir, col string) string {
		return `(SELECT ` + ibcCols + `, ` + ibcChannelNumber + ` AS channel_number
			FROM explorer.ibc_transfers t LEFT JOIN explorer.chains ch ON ch.chain_id = t.counterparty_chain_id
			WHERE t.direction = '` + dir + `' AND t.` + col + ` = $1
			  AND ($2::bigint IS NULL OR (t.created_height, t.created_tx_index, ` + ibcChannelNumber + `, t.sequence)
			      < ($2::bigint, $3::integer, $4::bigint, $5::bigint))
			ORDER BY ` + order + ` LIMIT $6)`
	}
	var from string
	switch direction {
	case IBCDirectionOut:
		from = side(IBCDirectionOut, "sender")
	case IBCDirectionIn:
		from = side(IBCDirectionIn, "receiver")
	default:
		from = side(IBCDirectionOut, "sender") + ` UNION ALL ` + side(IBCDirectionIn, "receiver")
	}
	rows, err := r.db.Query(ctx, `SELECT channel_id, sequence, direction, status, sender, receiver, denom, amount,
			memo, created_height, created_tx_index, created_tx_hash, created_time, timeout_height, timeout_time,
			result_height, result_tx_hash, result_time, ack_error, expired, counterparty_chain_id, pretty_name,
			logo_url, explorer_account_url, explorer_tx_url
		FROM (`+from+`) u
		ORDER BY created_height DESC, created_tx_index DESC, channel_number DESC, sequence DESC LIMIT $6`,
		address, bh, bt, bc, bs, limit)
	if err != nil {
		return nil, fmt.Errorf("cross-chain transfers of %s: %w", address, err)
	}
	out, err := pgx.CollectRows(rows, scanIBCTransfer)
	if err != nil {
		return nil, fmt.Errorf("cross-chain transfers of %s: %w", address, err)
	}
	return out, nil
}

// IBCChannels returns every channel with its counterparty chain, by channel
// number.
func (r *Explorer) IBCChannels(ctx context.Context) ([]IBCChannel, error) {
	rows, err := r.db.Query(ctx, `SELECT t.channel_id, t.port_id, t.client_id, t.connection_id,
			t.counterparty_channel_id, t.state, t.client_status, t.updated_at,
			t.counterparty_chain_id, ch.pretty_name, ch.logo_url, ch.explorer_account_url, ch.explorer_tx_url
		FROM explorer.ibc_channels t LEFT JOIN explorer.chains ch ON ch.chain_id = t.counterparty_chain_id
		ORDER BY `+ibcChannelNumber+`, t.port_id`)
	if err != nil {
		return nil, fmt.Errorf("ibc channels: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IBCChannel, error) {
		var c IBCChannel
		cp := &c.Counterparty
		err := row.Scan(&c.ChannelID, &c.PortID, &c.ClientID, &c.ConnectionID, &c.CounterpartyChannelID, &c.State,
			&c.ClientStatus, &c.UpdatedAt, &cp.ChainID, &cp.PrettyName, &cp.LogoURL, &cp.ExplorerAccountURL, &cp.ExplorerTxURL)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("ibc channels: %w", err)
	}
	return out, nil
}
