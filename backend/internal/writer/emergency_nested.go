package writer

import (
	"encoding/json"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// emergencyNestedTable is explorer.emergency_nested_actions: the emergency
// messages run inside a MsgExec, a MsgAdminExecute or an executed DAO
// proposal (reindex updates in place).
var emergencyNestedTable = table{
	name: "explorer.emergency_nested_actions",
	keys: []string{"height", "tx_index", "msg_index", "seq"},
	cols: []column{
		{name: "height", typ: "bigint"},
		{name: "tx_index", typ: "integer"},
		{name: "msg_index", typ: "integer"},
		{name: "seq", typ: "integer"},
		{name: "type_url", typ: "text"},
		{name: "signer", typ: "text"},
		{name: "body", typ: "jsonb"},
		{name: "via_kind", typ: "text"},
		{name: "grantee", typ: "text"},
		{name: "dao_id", typ: "bigint"},
		{name: "proposal_id", typ: "bigint"},
		{name: "tx_hash", typ: "text"},
		{name: "time", typ: "timestamptz"},
	},
}

type emergencyNestedRow struct {
	Height     int64           `json:"height"`
	TxIndex    int             `json:"tx_index"`
	MsgIndex   int             `json:"msg_index"`
	Seq        int             `json:"seq"`
	TypeURL    string          `json:"type_url"`
	Signer     *string         `json:"signer"`
	Body       json.RawMessage `json:"body"`
	ViaKind    string          `json:"via_kind"`
	Grantee    *string         `json:"grantee"`
	DaoID      *int64          `json:"dao_id"`
	ProposalID *int64          `json:"proposal_id"`
	TxHash     string          `json:"tx_hash"`
	Time       time.Time       `json:"time"`
}

// emergencyNestedStatements returns the writes of the nested emergency
// actions of rows.
func emergencyNestedStatements(top int64, rows []transform.NestedEmergencyAction, mode Mode) ([]statement, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]emergencyNestedRow, 0, len(rows))
	for _, a := range rows {
		body := a.Body
		if len(body) == 0 {
			body = json.RawMessage(`{}`)
		}
		out = append(out, emergencyNestedRow{
			Height: a.Height, TxIndex: a.TxIndex, MsgIndex: a.MsgIndex, Seq: a.Seq, TypeURL: a.TypeURL,
			Signer: nullIfEmpty(a.Signer), Body: body, ViaKind: a.Via, Grantee: nullIfEmpty(a.Grantee),
			DaoID: nullIfZero(a.DaoID), ProposalID: nullIfZero(a.ProposalID), TxHash: a.TxHash, Time: a.Time,
		})
	}
	return chunked(top, "emergency_nested_actions", emergencyNestedTable, mode, out, nil)
}

// nullIfZero is nil for 0, else a pointer to v.
func nullIfZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}
