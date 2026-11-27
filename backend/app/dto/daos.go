package dto

import (
	"encoding/json"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
)

// DaoSummary is an item of GET /api/v1/daos. Member counts and the
// electorate are live data, not in the list.
type DaoSummary struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Address      string     `json:"address"`
	Admin        string     `json:"admin"`
	AdminLabel   *Label     `json:"admin_label"`
	SelfGoverned bool       `json:"self_governed"`
	CreatedTime  *time.Time `json:"created_time"`
	// PendingAdmin is the address nominated as the next admin; null when no
	// nomination is pending.
	PendingAdmin *string `json:"pending_admin"`
	// ProposalsOpen is the number of the DAO's proposals in voting.
	ProposalsOpen int64 `json:"proposals_open"`
}

// DaoList is the response of GET /api/v1/daos. Supported is false on a node
// that has not upgraded to chain v8.3.0 (no x/dao yet): the list is then
// empty, not broken.
type DaoList struct {
	Items      []DaoSummary `json:"items"`
	NextCursor *string      `json:"next_cursor"`
	Supported  bool         `json:"supported"`
	// Total is the number of DAOs the filters match, across every page. Set
	// only when a page was asked for.
	Total *int64 `json:"total,omitempty"`
}

// NewDaoSummary builds a list item.
func NewDaoSummary(d repository.Dao) DaoSummary {
	s := DaoSummary{
		ID: d.ID, Name: d.Name, Type: d.Type, Address: d.Address, Admin: d.Admin, AdminLabel: newLabel(d.AdminLabel),
		SelfGoverned: d.SelfGoverned, PendingAdmin: d.PendingAdmin, ProposalsOpen: d.ProposalsOpen,
	}
	if d.CreatedTime != nil {
		t := d.CreatedTime.UTC()
		s.CreatedTime = &t
	}
	return s
}

// DaoConfig is a DAO's voting rules: ratios are decimal strings, durations
// seconds, coins with the denom's display data.
type DaoConfig struct {
	MinQuorum              string   `json:"min_quorum"`
	Threshold              string   `json:"threshold"`
	VotingPeriodSeconds    int64    `json:"voting_period_seconds"`
	AllowExpedite          bool     `json:"allow_expedite"`
	ExpeditedPeriodSeconds int64    `json:"expedited_period_seconds"`
	ExpediteQuorum         string   `json:"expedite_quorum"`
	ExpediteThreshold      string   `json:"expedite_threshold"`
	OpenSubmission         bool     `json:"open_submission"`
	ProposalCost           *Balance `json:"proposal_cost"`
	ProposalDeposit        *Balance `json:"proposal_deposit"`
	DepositRefundPolicy    string   `json:"deposit_refund_policy"`
	CostDestination        string   `json:"cost_destination"`
	KeptDepositDestination string   `json:"kept_deposit_destination"`
}

// DaoElectorate names the stake sources of a stake DAO.
type DaoElectorate struct {
	SrIDs   []string `json:"sr_ids"`
	DrDenom string   `json:"dr_denom"`
}

// DaoStakeSource is one stake source of a stake DAO's electorate: a staking
// reward (sr_id) or a denom reward (dr_denom), with what is staked in it now.
type DaoStakeSource struct {
	SrID         *string `json:"sr_id"`
	DrDenom      *string `json:"dr_denom"`
	StakingDenom string  `json:"staking_denom"`
	StakedAmount Balance `json:"staked_amount"`
}

// DaoElectorateStake is the stake of a stake DAO's electorate: VotingStake is
// the sum of its sources, the chain's electorateStake.
type DaoElectorateStake struct {
	Sources     []DaoStakeSource `json:"sources"`
	VotingStake Balance          `json:"voting_stake"`
}

// DaoLive is the DAO as the chain holds it now. Without Available only that
// flag is set.
type DaoLive struct {
	Available       bool                `json:"available"`
	ID              *int64              `json:"id,omitempty"`
	Address         *string             `json:"address,omitempty"`
	Type            *string             `json:"type,omitempty"`
	Name            *string             `json:"name,omitempty"`
	AdminSeq        *uint64             `json:"admin_seq,omitempty"`
	PendingAdminSeq *uint64             `json:"pending_admin_seq,omitempty"`
	Admin           *string             `json:"admin,omitempty"`
	PendingAdmin    *string             `json:"pending_admin,omitempty"`
	Description     *string             `json:"description,omitempty"`
	Config          *DaoConfig          `json:"config,omitempty"`
	Members         []string            `json:"members,omitempty"`
	Electorate      *DaoElectorate      `json:"electorate,omitempty"`
	CreatedAt       *time.Time          `json:"created_at,omitempty"`
	ElectorateStake *DaoElectorateStake `json:"electorate_stake,omitempty"`
}

// DaoController is the account that ultimately controls a DAO, following its
// admin chain.
type DaoController struct {
	Address string   `json:"address"`
	Label   *Label   `json:"label"`
	Reason  string   `json:"end_reason"`
	Path    []string `json:"path"`
}

// Dao is GET /api/v1/daos/{id}: the indexed row, the chain's DAO (Live), its
// controller and the treasury's balances. Live.Available is false when the
// node failed or does not run x/dao; Controller is then null and Treasury
// empty.
type Dao struct {
	DaoSummary
	AddressLabel  *Label         `json:"address_label"`
	CreatedHeight *int64         `json:"created_height"`
	CreatedTxHash *string        `json:"created_tx_hash"`
	Creator       *string        `json:"creator"`
	CreatorLabel  *Label         `json:"creator_label"`
	UpdatedHeight *int64         `json:"updated_height"`
	Live          DaoLive        `json:"live"`
	Controller    *DaoController `json:"controller"`
	Treasury      []Balance      `json:"treasury"`

	// ProposalsTotal is the number of the DAO's proposals the index holds,
	// the cleaned-up ones included (0 for a DAO only the chain has yet).
	ProposalsTotal int64 `json:"proposals_total"`
}

// DaoLookups are the display data a DAO answer needs: the denoms and the
// labels of the addresses it names.
type DaoLookups struct {
	Denoms map[string]repository.Denom
	Labels map[string]repository.Label
}

func (l DaoLookups) balance(denom, amount string) Balance {
	b := Balance{Denom: denom, Amount: amount}
	if d, ok := l.Denoms[denom]; ok {
		exp := d.Exponent
		b.Symbol, b.Exponent = d.Symbol, &exp
	}
	return b
}

func (l DaoLookups) label(address string) *Label {
	if lab, ok := l.Labels[address]; ok {
		return &Label{Name: lab.Name, Kind: lab.Kind}
	}
	return nil
}

// NewDao builds the DAO answer. live and treasury are nil when the node's
// part is unavailable.
func NewDao(row repository.Dao, live *daostate.Live, treasury []chainstate.Coin, lk DaoLookups) Dao {
	out := Dao{
		DaoSummary: NewDaoSummary(row), AddressLabel: lk.label(row.Address), CreatedHeight: row.CreatedHeight,
		CreatedTxHash: row.CreatedTxHash, Creator: row.Creator, CreatorLabel: newLabel(row.CreatorLabel),
		UpdatedHeight: row.UpdatedHeight, Treasury: []Balance{},
	}
	if live == nil {
		return out
	}
	d := live.Dao
	id := int64(d.ID)
	out.Live = DaoLive{
		Available: true, ID: &id, Address: &d.Address, Type: &d.Type, Name: &d.Name,
		AdminSeq: &d.AdminSeq, PendingAdminSeq: &d.PendingAdminSeq,
		Admin: &d.Admin, Description: &d.Description, Members: d.Members,
		Config: &DaoConfig{
			MinQuorum: d.Config.MinQuorum, Threshold: d.Config.Threshold, VotingPeriodSeconds: d.Config.VotingPeriodS,
			AllowExpedite: d.Config.AllowExpedite, ExpeditedPeriodSeconds: d.Config.ExpeditedPeriodS,
			ExpediteQuorum: d.Config.ExpediteQuorum, ExpediteThreshold: d.Config.ExpediteThreshold,
			OpenSubmission: d.Config.OpenSubmission, DepositRefundPolicy: d.Config.DepositRefundPolicy,
			CostDestination: d.Config.CostDestination, KeptDepositDestination: d.Config.KeptDepositDestination,
		},
	}
	if d.PendingAdmin != "" {
		out.Live.PendingAdmin = &d.PendingAdmin
	}
	if c := d.Config.ProposalCost; c != nil {
		b := lk.balance(c.Denom, c.Amount)
		out.Live.Config.ProposalCost = &b
	}
	if c := d.Config.ProposalDeposit; c != nil {
		b := lk.balance(c.Denom, c.Amount)
		out.Live.Config.ProposalDeposit = &b
	}
	if d.CreatedAt > 0 {
		t := time.Unix(d.CreatedAt, 0).UTC()
		out.Live.CreatedAt = &t
	}
	if e := d.Electorate; e != nil {
		out.Live.Electorate = &DaoElectorate{SrIDs: nonNil(e.SrIDs), DrDenom: e.DrDenom}
	}
	if s := live.Stake; s != nil {
		st := DaoElectorateStake{Sources: make([]DaoStakeSource, 0, len(s.Sources)),
			VotingStake: lk.balance(s.VotingStake.Denom, s.VotingStake.Amount)}
		for _, src := range s.Sources {
			item := DaoStakeSource{StakingDenom: src.StakingDenom, StakedAmount: lk.balance(src.StakedAmount.Denom, src.StakedAmount.Amount)}
			if src.SrID != "" {
				id := src.SrID
				item.SrID = &id
			}
			if src.DrDenom != "" {
				dr := src.DrDenom
				item.DrDenom = &dr
			}
			st.Sources = append(st.Sources, item)
		}
		out.Live.ElectorateStake = &st
	}
	out.Controller = &DaoController{Address: live.Controller.Address, Label: lk.label(live.Controller.Address),
		Reason: live.Controller.EndReason, Path: nonNil(live.Controller.Path)}
	for _, c := range treasury {
		out.Treasury = append(out.Treasury, lk.balance(c.Denom, c.Amount))
	}
	return out
}

// DaoEvent is an item of GET /api/v1/daos/{id}/history: one change of the
// DAO. Attrs are the event's attributes as the chain emitted them (decoded
// JSON), TxHash is null for a change governance made at block level.
type DaoEvent struct {
	Height   int64           `json:"height"`
	TxIndex  int64           `json:"tx_index"`
	MsgIndex int64           `json:"msg_index"`
	Seq      int64           `json:"seq"`
	Type     string          `json:"type"`
	Attrs    json.RawMessage `json:"attrs"`
	TxHash   *string         `json:"tx_hash"`
	Time     time.Time       `json:"time"`
}

// NewDaoEvent builds a history item.
func NewDaoEvent(e repository.DaoEvent) DaoEvent {
	return DaoEvent{Height: e.Height, TxIndex: e.TxIndex, MsgIndex: e.MsgIndex, Seq: e.Seq, Type: e.Type,
		Attrs: e.Attrs, TxHash: e.TxHash, Time: e.Time.UTC()}
}

// EncodeDaoEventCursor is the cursor after a DAO history row. The indexes
// shift by +1 because block-level rows hold -1 and cursor keys are not
// negative.
func EncodeDaoEventCursor(k repository.DaoEventKey) string {
	return EncodeCursor(k.Height, k.TxIndex+1, k.MsgIndex+1, k.Seq)
}

// DecodeDaoEventCursor reverses EncodeDaoEventCursor.
func DecodeDaoEventCursor(cursor string) (*repository.DaoEventKey, error) {
	keys, err := DecodeCursor(cursor, 4)
	if err != nil {
		return nil, err
	}
	return &repository.DaoEventKey{Height: keys[0], TxIndex: keys[1] - 1, MsgIndex: keys[2] - 1, Seq: keys[3]}, nil
}
