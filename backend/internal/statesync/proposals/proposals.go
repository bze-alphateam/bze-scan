// Package proposals is the proposals set of the state sync: it rewrites
// explorer.proposals from the local node's gov queries. Status, times,
// deposits and the final tally always come from the node; while a proposal is
// in its voting period the set also stores the running tally and the bonded
// tokens it is measured against, for turnout.
package proposals

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the period of the set's refresh of the proposals in
// their voting period.
const DefaultInterval = time.Minute

// PageLimit is the page size of the proposals list.
const PageLimit = 100

// Statuses of a proposal, as the status column stores them.
const (
	StatusDeposit  = "deposit_period"
	StatusVoting   = "voting_period"
	StatusPassed   = "passed"
	StatusRejected = "rejected"
	StatusFailed   = "failed"
	// StatusCanceled is a proposal its proposer withdrew (MsgCancelProposal):
	// the chain deletes it, only the explorer remembers it.
	StatusCanceled = "canceled"
)

// Kinds of proposal, as the kind column stores them.
const (
	KindSoftwareUpgrade    = "software_upgrade"
	KindCommunityPoolSpend = "community_pool_spend"
	KindParameterChange    = "parameter_change"
	KindCointrunkPublisher = "cointrunk_publisher"
	KindIBCClientUpdate    = "ibc_client_update"
	KindText               = "text"
	KindOther              = "other"
)

// kindOf maps the type URL of a proposal message, or of the legacy content a
// MsgExecLegacyContent carries, to its kind. Any module's MsgUpdateParams is
// a parameter change too (see kindOfType).
var kindOf = map[string]string{
	"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade":              KindSoftwareUpgrade,
	"/cosmos.upgrade.v1beta1.MsgCancelUpgrade":                KindSoftwareUpgrade,
	"/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal":         KindSoftwareUpgrade,
	"/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal":   KindSoftwareUpgrade,
	"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend":      KindCommunityPoolSpend,
	"/cosmos.distribution.v1beta1.CommunityPoolSpendProposal": KindCommunityPoolSpend,
	"/cosmos.params.v1beta1.ParameterChangeProposal":          KindParameterChange,
	"/bze.cointrunk.MsgSavePublisher":                         KindCointrunkPublisher,
	"/bze.cointrunk.MsgAcceptDomain":                          KindCointrunkPublisher,
	"/bze.cointrunk.v1.PublisherProposal":                     KindCointrunkPublisher,
	"/bze.cointrunk.v1.AcceptedDomainProposal":                KindCointrunkPublisher,
	"/ibc.core.client.v1.MsgRecoverClient":                    KindIBCClientUpdate,
	"/ibc.core.client.v1.MsgIBCSoftwareUpgrade":               KindIBCClientUpdate,
	"/ibc.core.client.v1.ClientUpdateProposal":                KindIBCClientUpdate,
	"/ibc.core.client.v1.UpgradeProposal":                     KindIBCClientUpdate,
	"/cosmos.gov.v1beta1.TextProposal":                        KindText,
}

func kindOfType(typeURL string) string {
	if k, ok := kindOf[typeURL]; ok {
		return k
	}
	if strings.HasSuffix(typeURL, ".MsgUpdateParams") {
		return KindParameterChange
	}
	return KindOther
}

// Kind classifies a proposal by its messages' type URLs (contentTypes being,
// per message, the type of the legacy content it carries or ""): the kind of
// the first message that has one, text without messages, other otherwise.
func Kind(messageTypes, contentTypes []string) string {
	if len(messageTypes) == 0 {
		return KindText
	}
	for i, t := range messageTypes {
		if i < len(contentTypes) && contentTypes[i] != "" {
			t = contentTypes[i]
		}
		if k := kindOfType(t); k != KindOther {
			return k
		}
	}
	return KindOther
}

// Messages reads the type URLs of a proposal's messages as proto JSON (the
// "messages" of a MsgSubmitProposal or of a gov Proposal) and, per message,
// the type of the legacy content it carries ("" when none). It answers nil
// for anything that is not a JSON array of objects.
func Messages(raw json.RawMessage) (messageTypes, contentTypes []string) {
	var msgs []struct {
		Type    string `json:"@type"`
		Content *struct {
			Type string `json:"@type"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &msgs) != nil {
		return nil, nil
	}
	for _, m := range msgs {
		messageTypes = append(messageTypes, m.Type)
		ct := ""
		if m.Content != nil {
			ct = m.Content.Type
		}
		contentTypes = append(contentTypes, ct)
	}
	return messageTypes, contentTypes
}

// Gov is the part of the gov v1 query client the set uses;
// govv1.QueryClient satisfies it.
type Gov interface {
	Proposals(ctx context.Context, in *govv1.QueryProposalsRequest, opts ...grpc.CallOption) (*govv1.QueryProposalsResponse, error)
	Proposal(ctx context.Context, in *govv1.QueryProposalRequest, opts ...grpc.CallOption) (*govv1.QueryProposalResponse, error)
	TallyResult(ctx context.Context, in *govv1.QueryTallyResultRequest, opts ...grpc.CallOption) (*govv1.QueryTallyResultResponse, error)
}

// Staking is the part of the staking query client the set uses;
// stakingtypes.QueryClient satisfies it.
type Staking interface {
	Pool(ctx context.Context, in *stakingtypes.QueryPoolRequest, opts ...grpc.CallOption) (*stakingtypes.QueryPoolResponse, error)
}

// JSON marshals a proto message to proto JSON; *chain.Codec satisfies it.
type JSON interface {
	ProtoJSON(msg gogoproto.Message) ([]byte, error)
}

// Tally is a vote count in base units of the bond denom.
type Tally struct {
	Yes, No, Abstain, Veto string
}

// Proposal is one explorer.proposals row as the node describes it. The
// columns only events know (submit height and transaction, resolution
// height) are the store's.
type Proposal struct {
	ID           uint64
	Title        string
	Summary      string
	Metadata     string
	Proposer     string
	Kind         string
	MessageTypes []string
	// Messages are the proposal's messages as proto JSON; nil when the node
	// returns none (a failed proposal's are cleared).
	Messages        json.RawMessage
	Status          string
	Expedited       bool
	SubmitTime      time.Time
	DepositEndTime  *time.Time
	VotingStartTime *time.Time
	VotingEndTime   *time.Time
	// TotalDeposit is a JSON array of coins.
	TotalDeposit json.RawMessage
	// Tally is the running tally in the voting period, the final one once
	// resolved; nil when the node has none.
	Tally *Tally
	// BondedTokens is the bonded supply the running tally was measured
	// against; empty for a resolved proposal (the store keeps the last one).
	BondedTokens string
	// TallyAt is when the tally was taken: now for a running tally, the end
	// of the voting period for a final one.
	TallyAt *time.Time
}

// Resolved reports whether status is final.
func Resolved(status string) bool {
	return status == StatusPassed || status == StatusRejected || status == StatusFailed || status == StatusCanceled
}

// Store persists the set.
type Store interface {
	// Save upserts proposals in one transaction.
	Save(ctx context.Context, ps []Proposal) error
	// Voting returns the ids of the stored proposals in their voting period.
	Voting(ctx context.Context) ([]uint64, error)
}

// Deps of the set.
type Deps struct {
	Gov     Gov
	Staking Staking
	JSON    JSON
	Store   Store
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Set is the proposals set.
type Set struct {
	deps     Deps
	interval time.Duration

	mu sync.Mutex
	// walked is set once a run has listed every proposal.
	walked bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Proposals.
func (s *Set) Name() string { return statesync.Proposals }

// Interval is the period of the voting-period refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// Key is the proposals-set key of a proposal.
func Key(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// FullResync lists every proposal on its first run, so a proposal submitted
// before the live floor exists too. Later runs (every Interval) refresh the
// proposals in their voting period: the node's list of them, plus the stored
// ones it no longer lists (resolved since).
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filter := govv1.StatusNil
	if s.walked {
		filter = govv1.StatusVotingPeriod
	}
	listed, err := s.list(ctx, filter)
	if err != nil {
		return err
	}
	pool, err := s.bonded(ctx, listed)
	if err != nil {
		return err
	}
	rows := make([]Proposal, 0, len(listed))
	seen := map[uint64]bool{}
	for _, p := range listed {
		row, err := s.row(ctx, p, pool)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		seen[p.Id] = true
	}
	if filter == govv1.StatusVotingPeriod {
		stored, err := s.deps.Store.Voting(ctx)
		if err != nil {
			return fmt.Errorf("proposals: stored voting proposals: %w", err)
		}
		for _, id := range stored {
			if seen[id] {
				continue
			}
			row, ok, err := s.one(ctx, id)
			if err != nil {
				return err
			}
			if ok {
				rows = append(rows, row)
			}
		}
	}
	if err := s.deps.Store.Save(ctx, rows); err != nil {
		return err
	}
	s.walked = true
	return nil
}

// ResyncOne rewrites the proposal key (its id). A proposal the node no longer
// has (dropped for lack of deposit, canceled) keeps the row its events wrote.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return fmt.Errorf("proposals: key %q is not a proposal id", key)
	}
	row, ok, err := s.one(ctx, id)
	if err != nil || !ok {
		return err
	}
	return s.deps.Store.Save(ctx, []Proposal{row})
}

// one reads proposal id; false when the node does not have it.
func (s *Set) one(ctx context.Context, id uint64) (Proposal, bool, error) {
	resp, err := s.deps.Gov.Proposal(ctx, &govv1.QueryProposalRequest{ProposalId: id})
	if status.Code(err) == codes.NotFound {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, fmt.Errorf("proposals: proposal %d: %w", id, err)
	}
	if resp.Proposal == nil {
		return Proposal{}, false, nil
	}
	pool, err := s.bonded(ctx, []*govv1.Proposal{resp.Proposal})
	if err != nil {
		return Proposal{}, false, err
	}
	row, err := s.row(ctx, resp.Proposal, pool)
	return row, err == nil, err
}

// list pages through the proposals with status (every one for StatusNil).
func (s *Set) list(ctx context.Context, st govv1.ProposalStatus) ([]*govv1.Proposal, error) {
	var out []*govv1.Proposal
	var next []byte
	for {
		resp, err := s.deps.Gov.Proposals(ctx, &govv1.QueryProposalsRequest{
			ProposalStatus: st,
			Pagination:     &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return nil, fmt.Errorf("proposals: list: %w", err)
		}
		out = append(out, resp.Proposals...)
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			return out, nil
		}
		next = resp.Pagination.NextKey
	}
}

// bonded reads the bonded tokens when one of ps is in its voting period, ""
// otherwise.
func (s *Set) bonded(ctx context.Context, ps []*govv1.Proposal) (string, error) {
	if !slices.ContainsFunc(ps, func(p *govv1.Proposal) bool { return p.Status == govv1.StatusVotingPeriod }) {
		return "", nil
	}
	resp, err := s.deps.Staking.Pool(ctx, &stakingtypes.QueryPoolRequest{})
	if err != nil {
		return "", fmt.Errorf("proposals: staking pool: %w", err)
	}
	return resp.Pool.BondedTokens.String(), nil
}

// row turns a node proposal into its row: a voting-period proposal gets the
// running tally against bonded, a resolved one its final tally.
func (s *Set) row(ctx context.Context, p *govv1.Proposal, bonded string) (Proposal, error) {
	doc, err := s.deps.JSON.ProtoJSON(p)
	if err != nil {
		return Proposal{}, fmt.Errorf("proposals: proposal %d: %w", p.Id, err)
	}
	var fields struct {
		Messages     json.RawMessage `json:"messages"`
		TotalDeposit json.RawMessage `json:"total_deposit"`
	}
	if err := json.Unmarshal(doc, &fields); err != nil {
		return Proposal{}, fmt.Errorf("proposals: proposal %d: %w", p.Id, err)
	}
	row := Proposal{
		ID: p.Id, Title: p.Title, Summary: p.Summary, Metadata: p.Metadata, Proposer: p.Proposer,
		Status: StatusOf(p.Status), Expedited: p.Expedited, DepositEndTime: p.DepositEndTime,
		VotingStartTime: p.VotingStartTime, VotingEndTime: p.VotingEndTime, TotalDeposit: fields.TotalDeposit,
	}
	if p.SubmitTime != nil {
		row.SubmitTime = *p.SubmitTime
	}
	if len(p.Messages) > 0 {
		row.Messages = fields.Messages
	}
	types, contents := Messages(fields.Messages)
	row.MessageTypes = types
	row.Kind = Kind(types, contents)
	if row.MessageTypes == nil {
		row.MessageTypes = []string{}
	}
	if len(row.TotalDeposit) == 0 || string(row.TotalDeposit) == "null" {
		row.TotalDeposit = json.RawMessage(`[]`)
	}

	switch {
	case p.Status == govv1.StatusVotingPeriod:
		resp, err := s.deps.Gov.TallyResult(ctx, &govv1.QueryTallyResultRequest{ProposalId: p.Id})
		if err != nil {
			return Proposal{}, fmt.Errorf("proposals: tally %d: %w", p.Id, err)
		}
		now := s.deps.Now().UTC()
		row.Tally, row.BondedTokens, row.TallyAt = tallyOf(resp.Tally), bonded, &now
	case Resolved(row.Status) && p.FinalTallyResult != nil:
		row.Tally, row.TallyAt = tallyOf(p.FinalTallyResult), p.VotingEndTime
	}
	return row, nil
}

func tallyOf(t *govv1.TallyResult) *Tally {
	if t == nil {
		return nil
	}
	return &Tally{Yes: zero(t.YesCount), No: zero(t.NoCount), Abstain: zero(t.AbstainCount), Veto: zero(t.NoWithVetoCount)}
}

func zero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// StatusOf is the status column value of a gov status.
func StatusOf(st govv1.ProposalStatus) string {
	switch st {
	case govv1.StatusDepositPeriod:
		return StatusDeposit
	case govv1.StatusVotingPeriod:
		return StatusVoting
	case govv1.StatusPassed:
		return StatusPassed
	case govv1.StatusRejected:
		return StatusRejected
	case govv1.StatusFailed:
		return StatusFailed
	default:
		return strings.ToLower(strings.TrimPrefix(st.String(), "PROPOSAL_STATUS_"))
	}
}
