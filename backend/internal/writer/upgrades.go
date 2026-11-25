package writer

import (
	"cmp"
	"slices"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// emergencyHaltRow is the JSON shape of an emergency upgrades write.
type emergencyHaltRow struct {
	Height          int64   `json:"height"`
	Name            string  `json:"name"`
	Reason          *string `json:"reason"`
	ScheduledBy     *string `json:"scheduled_by"`
	ScheduledTxHash *string `json:"scheduled_tx_hash"`
}

// emergencyHaltSQL upserts the emergency halts. A governance row at the
// same height keeps its proposal but becomes the emergency halt: that is
// the plan the chain stops for.
const emergencyHaltSQL = `INSERT INTO explorer.upgrades AS u (height, name, kind, reason, scheduled_by, scheduled_tx_hash)
	SELECT r.height, r.name, 'emergency', r.reason, r.scheduled_by, r.scheduled_tx_hash
	  FROM jsonb_to_recordset($1::jsonb) AS r(height bigint, name text, reason text, scheduled_by text, scheduled_tx_hash text)
	 ORDER BY r.height
	ON CONFLICT (height) DO UPDATE SET kind = 'emergency', name = EXCLUDED.name, reason = EXCLUDED.reason,
		scheduled_by = EXCLUDED.scheduled_by, scheduled_tx_hash = EXCLUDED.scheduled_tx_hash, superseded_by_height = NULL
	WHERE (u.kind, u.name, u.reason, u.scheduled_by, u.scheduled_tx_hash, u.superseded_by_height) IS DISTINCT FROM
	      ('emergency', EXCLUDED.name, EXCLUDED.reason, EXCLUDED.scheduled_by, EXCLUDED.scheduled_tx_hash, NULL::bigint)`

// upgradeStatements returns the writes of the emergency halts of ents: the
// rows, then the governance upgrades they superseded. Both are upserts, so
// the mode does not matter.
func upgradeStatements(top int64, ents *transform.Entities) ([]statement, error) {
	if len(ents.EmergencyHalts) == 0 {
		return nil, nil
	}
	stmts, err := plainStatements(top, "upgrades", emergencyHaltSQL, emergencyHaltRows(ents.EmergencyHalts))
	if err != nil {
		return nil, err
	}
	return append(stmts, statement{table: "upgrades superseded", sql: upgrades.SupersedeSQL}), nil
}

// emergencyHaltRows keeps one row per height (one statement cannot upsert a
// key twice): the latest scheduling, a block-level one (governance's
// EndBlocker) over the transactions of its block, in height order.
func emergencyHaltRows(halts []transform.EmergencyHalt) []emergencyHaltRow {
	latest := map[int64]transform.EmergencyHalt{}
	for _, h := range halts {
		cur, ok := latest[h.Height]
		if !ok || h.ScheduledHeight > cur.ScheduledHeight || (h.ScheduledHeight == cur.ScheduledHeight && (h.TxHash == "" || cur.TxHash != "")) {
			latest[h.Height] = h
		}
	}
	out := make([]emergencyHaltRow, 0, len(latest))
	for _, h := range latest {
		out = append(out, emergencyHaltRow{
			Height: h.Height, Name: h.Name, Reason: nullIfEmpty(h.Reason), ScheduledBy: nullIfEmpty(h.ScheduledBy),
			ScheduledTxHash: nullIfEmpty(h.TxHash),
		})
	}
	slices.SortFunc(out, func(a, b emergencyHaltRow) int { return cmp.Compare(a.Height, b.Height) })
	return out
}
