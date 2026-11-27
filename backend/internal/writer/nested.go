package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// A DAO proposal's messages run inside the MsgSettleProposal that executes
// it, whose body does not carry them: they are the proposal's stored msgs
// (dao_proposals.msgs, from the MsgSubmitProposal). The transformer, which
// does no I/O, derives what the settle's events tell (mints, burns, votes,
// transfers, typed events); SettledProposalActions adds the rows only the
// messages' bodies tell (a denom created, its metadata, branding or admin
// changed, a validator created or edited, an unjail), with the settle's
// height, transaction and time, after the flush that holds the second of
// the two facts: the settle that executed the proposal, or the submission
// that brought its msgs (the backfill walks down, so a settle is often
// written before its submission).

// settledActionsSQL reads the executed proposals among $1 whose settle
// transaction is indexed, with every other proposal the same transaction
// executed, numbered by id within it (the seq range of its rows), with the
// proposal's DAO, the settle's hash and the index of the settle message that
// ran it (0 when that transaction message is not stored, a settle run inside
// a wrapper: its rows then sit at the start of the transaction).
const settledActionsSQL = `WITH s AS (
	SELECT DISTINCT settled_height, settled_tx_hash FROM explorer.dao_proposals
	 WHERE id = ANY($1::bigint[]) AND status = 'executed' AND settled_tx_hash IS NOT NULL)
	SELECT p.id, COALESCE(p.msgs::text, ''), t.height, t.tx_index, t.time,
	       (row_number() OVER (PARTITION BY p.settled_height, p.settled_tx_hash ORDER BY p.id) - 1)::integer,
	       COALESCE(p.dao_id, 0), t.hash,
	       COALESCE((SELECT m.msg_index FROM explorer.messages m
	                  WHERE m.height = t.height AND m.tx_index = t.tx_index AND m.type_url = '/bze.dao.MsgSettleProposal'
	                    AND m.body ->> 'proposal_id' = p.id::text
	                  ORDER BY m.msg_index LIMIT 1), 0)
	  FROM explorer.dao_proposals p
	  JOIN s ON s.settled_height = p.settled_height AND s.settled_tx_hash = p.settled_tx_hash
	  JOIN explorer.transactions t ON t.height = p.settled_height AND t.hash = p.settled_tx_hash
	 WHERE p.status = 'executed'
	 ORDER BY p.id`

// SettledProposalActions returns the PostFlush hook that writes the token_events,
// validator_events and emergency_nested_actions rows of the messages the
// flush's executed DAO proposals ran (see above), in the flush's mode: running it again rewrites
// the same rows (transform.NestedActions numbers them from
// transform.NestedSeqBase by the proposal's rank in its settle), so a reindex
// or a second trigger changes nothing. The denoms and validators they change
// are marked in the flush's Dirty. A proposal whose rows overflow its seq
// range is written without the overflow, and log warns about it.
func SettledProposalActions(log logrus.FieldLogger) PostFlush {
	return func(ctx context.Context, tx pgx.Tx, f *Flush) error {
		return settledProposalActions(ctx, tx, f, log)
	}
}

func settledProposalActions(ctx context.Context, tx pgx.Tx, f *Flush, log logrus.FieldLogger) error {
	if f == nil || f.Entities == nil {
		return nil
	}
	var ids []int64
	for _, o := range f.Entities.DaoProposalOutcomes {
		if o.Executed != nil && *o.Executed {
			ids = append(ids, o.ID)
		}
	}
	for _, p := range f.Entities.DaoProposals {
		if p.HasBody {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	rows, err := tx.Query(ctx, settledActionsSQL, ids)
	if err != nil {
		return fmt.Errorf("settled proposal actions: %w", err)
	}
	var all transform.Entities
	var top int64
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var (
				id, height, daoID          int64
				msgs, hash                 string
				txIndex, ordinal, msgIndex int
				at                         time.Time
			)
			if err := rows.Scan(&id, &msgs, &height, &txIndex, &at, &ordinal, &daoID, &hash, &msgIndex); err != nil {
				return err
			}
			if msgs == "" {
				continue
			}
			ents, dropped := transform.NestedActions(json.RawMessage(msgs), height, txIndex, at, ordinal)
			if dropped > 0 {
				log.WithFields(logrus.Fields{"proposal_id": id, "height": height, "tx_index": txIndex, "dropped": dropped}).
					Warn("settled proposal actions: more rows than the proposal's seq range holds, the rest not stored")
			}
			all.TokenEvents = append(all.TokenEvents, ents.TokenEvents...)
			all.ValidatorEvents = append(all.ValidatorEvents, ents.ValidatorEvents...)
			all.NestedEmergencyActions = append(all.NestedEmergencyActions, transform.SettledEmergencyActions(json.RawMessage(msgs),
				transform.SettledProposal{ID: id, DaoID: daoID, Height: height, TxIndex: txIndex, MsgIndex: msgIndex,
					TxHash: hash, Time: at, Ordinal: ordinal})...)
			for set, keys := range ents.Dirty {
				for key := range keys {
					f.Entities.Dirty.Mark(set, key)
				}
			}
			top = max(top, height)
		}
		return rows.Err()
	}()
	if err != nil {
		return fmt.Errorf("settled proposal actions: %w", err)
	}
	if len(all.TokenEvents) == 0 && len(all.ValidatorEvents) == 0 && len(all.NestedEmergencyActions) == 0 {
		return nil
	}
	tokenRows, err := tokenEventRows(top, all.TokenEvents)
	if err != nil {
		return err
	}
	valRows, err := validatorEventRows(top, all.ValidatorEvents)
	if err != nil {
		return err
	}
	tokens, err := chunked(top, "token_events", tokenEventsTable, f.Mode, tokenRows, nil)
	if err != nil {
		return err
	}
	vals, err := chunked(top, "validator_events", validatorEventsTable, f.Mode, valRows, nil)
	if err != nil {
		return err
	}
	nested, err := emergencyNestedStatements(top, all.NestedEmergencyActions, f.Mode)
	if err != nil {
		return err
	}
	for _, st := range slices.Concat(tokens, vals, nested) {
		if err := runStatement(ctx, tx, st, &f.Inserted); err != nil {
			return fmt.Errorf("settled proposal actions: %s: %w", st.table, err)
		}
	}
	return nil
}
