package transform

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// msgCreateDao is the message that creates a DAO (chain v8.3.0, x/dao).
const msgCreateDao = "/bze.dao.MsgCreateDao"

// daoEventPrefix starts the type of every x/dao typed event.
const daoEventPrefix = "bze.dao."

// DAO kinds, as the daos.type column stores them.
const (
	DaoTypeStaticList = "static_list"
	DaoTypeStake      = "stake"
)

// daoHistoryEvents are the x/dao events that change a DAO itself: the rows
// of its History tab. The proposal and ballot events are the proposals
// tables' (their own story).
var daoHistoryEvents = map[string]bool{
	daoEventPrefix + "EventDaoCreated":               true,
	daoEventPrefix + "EventAdminExecuted":            true,
	daoEventPrefix + "EventAdminNominated":           true,
	daoEventPrefix + "EventAdminChanged":             true,
	daoEventPrefix + "EventAdminNominationWithdrawn": true,
	daoEventPrefix + "EventDaoConfigUpdated":         true,
	daoEventPrefix + "EventDaoMetadataUpdated":       true,
	daoEventPrefix + "EventDaoElectorateUpdated":     true,
	daoEventPrefix + "EventDaoMembersUpdated":        true,
}

// DaoCreated is an explorer.daos row an EventDaoCreated writes: what only
// the creation shows (who, when, in which transaction). The name and the
// current admin are the state sync's afterwards.
type DaoCreated struct {
	ID      int64
	Address string
	// Type is DaoTypeStaticList or DaoTypeStake.
	Type string
	// Name is from the MsgCreateDao; empty when the DAO was created by a
	// message the explorer does not read (gov at block level).
	Name string
	// Admin is the event's admin: the DAO's own address when it owns itself.
	Admin string
	// Creator is the MsgCreateDao signer; empty at block level.
	Creator string
	Height  int64
	Time    time.Time
	// TxHash is empty at block level.
	TxHash string
}

// DaoEvent is one explorer.dao_events row.
type DaoEvent struct {
	Height int64
	// TxIndex and MsgIndex are -1 for a block-level event.
	TxIndex  int
	MsgIndex int
	// Seq is the event's position among the message's DAO events, or among
	// the block's events.
	Seq   int
	DaoID int64
	// Type is the event type without the bze.dao. prefix.
	Type   string
	Attrs  map[string]any
	TxHash string
	Time   time.Time
}

// txDaoEvents appends the DAO rows of a successful transaction: its DAO
// events carry the message index of the message that ran them (also when a
// MsgAdminExecute ran them for the DAO).
func txDaoEvents(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg, events []node.Event) {
	seqs := map[int]int{}
	for _, ev := range events {
		if !daoHistoryEvents[ev.Type] {
			continue
		}
		msgIndex := -1
		if v, ok := ev.Get("msg_index"); ok {
			if j, err := strconv.Atoi(v); err == nil {
				msgIndex = j
			}
		}
		row, ok := daoRow(ev)
		if !ok {
			continue
		}
		row.Height, row.TxIndex, row.MsgIndex, row.Seq = b.Height, txIndex, msgIndex, seqs[msgIndex]
		row.TxHash, row.Time = txHash, b.Time
		seqs[msgIndex]++
		ents.DaoEvents = append(ents.DaoEvents, row)
		ents.Dirty.Mark(statesync.Daos, strconv.FormatInt(row.DaoID, 10))
		if ev.Type == daoEventPrefix+"EventDaoCreated" {
			created := daoCreated(row)
			if msgIndex >= 0 && msgIndex < len(msgs) && msgs[msgIndex].TypeURL == msgCreateDao {
				var body map[string]any
				_ = json.Unmarshal(msgs[msgIndex].Body, &body) // an undecodable body leaves the name empty
				created.Name = str(body, "name")
				created.Creator = msgs[msgIndex].Signer
			}
			created.Height, created.Time, created.TxHash = b.Height, b.Time, txHash
			ents.Daos = append(ents.Daos, created)
		}
	}
}

// blockDaoEvents appends the DAO rows of the block's finalize events:
// governance running a DAO message in its EndBlocker.
func blockDaoEvents(ents *Entities, b Block, events []node.Event) {
	for i, ev := range events {
		if !daoHistoryEvents[ev.Type] {
			continue
		}
		row, ok := daoRow(ev)
		if !ok {
			continue
		}
		row.Height, row.TxIndex, row.MsgIndex, row.Seq, row.Time = b.Height, -1, -1, i, b.Time
		ents.DaoEvents = append(ents.DaoEvents, row)
		ents.Dirty.Mark(statesync.Daos, strconv.FormatInt(row.DaoID, 10))
		if ev.Type == daoEventPrefix+"EventDaoCreated" {
			created := daoCreated(row)
			created.Height, created.Time = b.Height, b.Time
			ents.Daos = append(ents.Daos, created)
		}
	}
}

// daoRow reads the DAO an event names; false without a usable id.
func daoRow(ev node.Event) (DaoEvent, bool) {
	attrs := storedEvent(ev).Attrs
	id, err := int64Attr(attrs["id"])
	if err != nil || id <= 0 {
		return DaoEvent{}, false
	}
	return DaoEvent{DaoID: id, Type: strings.TrimPrefix(ev.Type, daoEventPrefix), Attrs: attrs}, true
}

// daoCreated builds the daos row of an EventDaoCreated row.
func daoCreated(row DaoEvent) DaoCreated {
	return DaoCreated{
		ID: row.DaoID, Address: anyString(row.Attrs["address"]), Admin: anyString(row.Attrs["admin"]),
		Type: daoType(row.Attrs["type"]),
	}
}

// daoType maps a DaoType (proto JSON writes the enum name; a number is
// taken too) to its column value.
func daoType(v any) string {
	switch x := v.(type) {
	case string:
		if x == "DAO_TYPE_STAKE" || x == "1" {
			return DaoTypeStake
		}
	case json.Number:
		if x.String() == "1" {
			return DaoTypeStake
		}
	}
	return DaoTypeStaticList
}
