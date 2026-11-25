package writer

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// daoEventsTable is explorer.dao_events: the history of the DAOs, written
// like the other partitioned history tables (reindex updates in place).
var daoEventsTable = table{
	name: "explorer.dao_events",
	keys: []string{"height", "tx_index", "msg_index", "seq"},
	cols: []column{
		{name: "height", typ: "bigint"},
		{name: "tx_index", typ: "integer"},
		{name: "msg_index", typ: "integer"},
		{name: "seq", typ: "integer"},
		{name: "dao_id", typ: "bigint"},
		{name: "type", typ: "text"},
		{name: "attrs", typ: "jsonb"},
		{name: "tx_hash", typ: "text"},
		{name: "time", typ: "timestamptz"},
	},
}

type daoEventRow struct {
	Height   int64           `json:"height"`
	TxIndex  int             `json:"tx_index"`
	MsgIndex int             `json:"msg_index"`
	Seq      int             `json:"seq"`
	DaoID    int64           `json:"dao_id"`
	Type     string          `json:"type"`
	Attrs    json.RawMessage `json:"attrs"`
	TxHash   *string         `json:"tx_hash"`
	Time     time.Time       `json:"time"`
}

type daoRow struct {
	ID            int64     `json:"id"`
	Address       string    `json:"address"`
	Type          string    `json:"type"`
	Name          string    `json:"name"`
	Admin         string    `json:"admin"`
	SelfGoverned  bool      `json:"self_governed"`
	CreatedHeight int64     `json:"created_height"`
	CreatedTime   time.Time `json:"created_time"`
	TxHash        *string   `json:"created_tx_hash"`
	Creator       *string   `json:"creator"`
}

// daoCreateSQL upserts the DAOs the heights created. A DAO the state sync
// inserted first (NULL created_*) gets its creation facts; the name and
// admin the sync read from the chain stay (they are newer than the
// creation's), except an empty name.
const daoCreateSQL = `INSERT INTO explorer.daos AS d (id, address, type, name, admin, self_governed,
		created_height, created_time, created_tx_hash, creator, updated_height)
	SELECT r.id, r.address, r.type, r.name, r.admin, r.self_governed, r.created_height, r.created_time,
	       r.created_tx_hash, r.creator, r.created_height
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, address text, type text, name text, admin text,
	       self_governed boolean, created_height bigint, created_time timestamptz, created_tx_hash text, creator text)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		created_height = EXCLUDED.created_height, created_time = EXCLUDED.created_time,
		created_tx_hash = EXCLUDED.created_tx_hash, creator = COALESCE(EXCLUDED.creator, d.creator),
		name = CASE WHEN d.name = '' THEN EXCLUDED.name ELSE d.name END,
		updated_height = GREATEST(d.updated_height, EXCLUDED.updated_height)
	WHERE (d.created_height, d.created_time, d.created_tx_hash, d.creator, d.name) IS DISTINCT FROM
	      (EXCLUDED.created_height, EXCLUDED.created_time, EXCLUDED.created_tx_hash,
	       COALESCE(EXCLUDED.creator, d.creator), CASE WHEN d.name = '' THEN EXCLUDED.name ELSE d.name END)`

// daoTouchSQL records the latest height a DAO changed at.
const daoTouchSQL = `UPDATE explorer.daos d SET updated_height = e.h
	  FROM (SELECT r.dao_id, max(r.height) AS h
	          FROM jsonb_to_recordset($1::jsonb) AS r(height bigint, dao_id bigint) GROUP BY r.dao_id) e
	 WHERE d.id = e.dao_id AND (d.updated_height IS NULL OR d.updated_height < e.h)`

// daoStatements returns the writes of the DAOs and their events of ents:
// the creations, the events, then the DAOs' updated_height.
func daoStatements(top int64, ents *transform.Entities, mode Mode) ([]statement, error) {
	if len(ents.Daos) == 0 && len(ents.DaoEvents) == 0 {
		return nil, nil
	}
	created := make([]daoRow, 0, len(ents.Daos))
	for _, d := range ents.Daos {
		created = append(created, daoRow{
			ID: d.ID, Address: d.Address, Type: d.Type, Name: d.Name, Admin: d.Admin, SelfGoverned: d.Admin == d.Address,
			CreatedHeight: d.Height, CreatedTime: d.Time, TxHash: nullIfEmpty(d.TxHash), Creator: nullIfEmpty(d.Creator),
		})
	}
	events := make([]daoEventRow, 0, len(ents.DaoEvents))
	touched := make([]map[string]int64, 0, len(ents.DaoEvents))
	for _, e := range ents.DaoEvents {
		attrs, err := json.Marshal(e.Attrs)
		if err != nil {
			return nil, fmt.Errorf("write %d: dao_events: attrs: %w", top, err)
		}
		events = append(events, daoEventRow{
			Height: e.Height, TxIndex: e.TxIndex, MsgIndex: e.MsgIndex, Seq: e.Seq, DaoID: e.DaoID, Type: e.Type,
			Attrs: attrs, TxHash: nullIfEmpty(e.TxHash), Time: e.Time,
		})
		touched = append(touched, map[string]int64{"height": e.Height, "dao_id": e.DaoID})
	}
	creates, err := plainStatements(top, "daos", daoCreateSQL, created)
	if err != nil {
		return nil, err
	}
	hist, err := chunked(top, "dao_events", daoEventsTable, mode, events, nil)
	if err != nil {
		return nil, err
	}
	touch, err := plainStatements(top, "daos updated_height", daoTouchSQL, touched)
	if err != nil {
		return nil, err
	}
	return append(append(creates, hist...), touch...), nil
}
