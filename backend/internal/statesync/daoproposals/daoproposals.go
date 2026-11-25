// Package daoproposals is the DAO proposals set of the state sync (chain
// v8.3.0, x/dao). It writes into explorer.dao_proposals what only the chain
// knows while it still has a proposal: the running tally of the voting ones,
// whether an expedited one can settle early, a failed execution's retry
// deadline and the config pinned at submission (the API's checks need it).
//
// Its first run lists every proposal still on chain in VOTING or
// EXECUTION_FAILED, DAO by DAO; later runs (every Interval) refresh the
// stored voting ones. The transformer marks a proposal dirty on each of its
// events. A proposal the chain no longer has (cleaned up) keeps its indexed
// values. A node without x/dao answers Unimplemented: "not supported", no
// error, no rows, retried at the next run.
package daoproposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the period of the refresh of the voting proposals.
const DefaultInterval = time.Minute

// PageLimit is the page size of the DAO and proposal lists.
const PageLimit = 100

// Client is the part of the x/dao query client the set uses;
// daotypes.QueryClient satisfies it.
type Client interface {
	Daos(ctx context.Context, in *daotypes.QueryDaosRequest, opts ...grpc.CallOption) (*daotypes.QueryDaosResponse, error)
	Proposals(ctx context.Context, in *daotypes.QueryProposalsRequest, opts ...grpc.CallOption) (*daotypes.QueryProposalsResponse, error)
	Proposal(ctx context.Context, in *daotypes.QueryProposalRequest, opts ...grpc.CallOption) (*daotypes.QueryProposalResponse, error)
}

// JSON marshals a proto message to proto JSON; *chain.Codec satisfies it.
type JSON interface {
	ProtoJSON(msg gogoproto.Message) ([]byte, error)
}

// Proposal is a proposal as the chain describes it now.
type Proposal struct {
	ID       uint64
	DaoID    uint64
	Proposer string
	Title    string
	Summary  string
	// Msgs is the proposal's messages as proto JSON (a JSON array, the
	// shape of the MsgSubmitProposal body's msgs).
	Msgs         json.RawMessage
	Expedited    bool
	SubmitHeight int64
	SubmitTime   time.Time
	VotingEnd    time.Time
	Denominator  string
	// PinnedConfig is the DaoConfig pinned at submission, as proto JSON.
	PinnedConfig json.RawMessage
	// Status is the lower-case chain status (voting, rejected, executed,
	// execution_failed, expired, cancelled).
	Status           string
	Yes, No, Abstain string
	// TallyAt is when the tally was read.
	TallyAt            time.Time
	ExpediteSettleable bool
	// RetryDeadline is set for an execution_failed proposal.
	RetryDeadline *time.Time
}

// Store persists the set.
type Store interface {
	// Save upserts proposals in one transaction.
	Save(ctx context.Context, ps []Proposal) error
	// Voting returns the ids of the stored proposals in voting.
	Voting(ctx context.Context) ([]uint64, error)
}

// Deps of the set.
type Deps struct {
	Client Client
	JSON   JSON
	Store  Store
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Set is the dao_proposals set.
type Set struct {
	deps     Deps
	interval time.Duration

	mu sync.Mutex
	// walked is set once a run has listed the open proposals of every DAO.
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

// Name is statesync.DaoProposals.
func (s *Set) Name() string { return statesync.DaoProposals }

// Interval is the period of the voting refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// Key is the set key of proposal id.
func Key(id uint64) string { return strconv.FormatUint(id, 10) }

// errUnsupported ends a run on a node without x/dao.
var errUnsupported = errors.New("x/dao not supported")

// FullResync lists the open proposals of every DAO on its first run, then
// refreshes the stored voting ones.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []uint64
	var err error
	if s.walked {
		if ids, err = s.deps.Store.Voting(ctx); err != nil {
			return fmt.Errorf("dao proposals: stored voting proposals: %w", err)
		}
	} else if ids, err = s.open(ctx); err != nil {
		if errors.Is(err, errUnsupported) {
			return nil
		}
		return err
	}
	rows := make([]Proposal, 0, len(ids))
	for _, id := range ids {
		row, ok, err := s.one(ctx, id)
		if errors.Is(err, errUnsupported) {
			return nil
		}
		if err != nil {
			return err
		}
		if ok {
			rows = append(rows, row)
		}
	}
	if len(rows) > 0 {
		if err := s.deps.Store.Save(ctx, rows); err != nil {
			return err
		}
	}
	s.walked = true
	return nil
}

// ResyncOne rewrites proposal key (its id). A proposal the chain no longer
// has keeps its row.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return fmt.Errorf("dao proposals: key %q is not a proposal id", key)
	}
	row, ok, err := s.one(ctx, id)
	if errors.Is(err, errUnsupported) || (err == nil && !ok) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.deps.Store.Save(ctx, []Proposal{row})
}

// open lists the ids of every proposal in VOTING or EXECUTION_FAILED, DAO by
// DAO.
func (s *Set) open(ctx context.Context) ([]uint64, error) {
	var daoIDs []uint64
	var next []byte
	for {
		resp, err := s.deps.Client.Daos(ctx, &daotypes.QueryDaosRequest{Pagination: &query.PageRequest{Key: next, Limit: PageLimit}})
		if status.Code(err) == codes.Unimplemented {
			return nil, errUnsupported
		}
		if err != nil {
			return nil, fmt.Errorf("dao proposals: daos: %w", err)
		}
		for _, d := range resp.Daos {
			daoIDs = append(daoIDs, d.Id)
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}
	var ids []uint64
	for _, daoID := range daoIDs {
		next = nil
		for {
			resp, err := s.deps.Client.Proposals(ctx, &daotypes.QueryProposalsRequest{
				DaoId:      daoID,
				Statuses:   []daotypes.ProposalStatus{daotypes.ProposalStatus_VOTING, daotypes.ProposalStatus_EXECUTION_FAILED},
				Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
			})
			if status.Code(err) == codes.Unimplemented {
				return nil, errUnsupported
			}
			if err != nil {
				return nil, fmt.Errorf("dao proposals: dao %d: %w", daoID, err)
			}
			for _, p := range resp.Proposals {
				ids = append(ids, p.Id)
			}
			if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
				break
			}
			next = resp.Pagination.NextKey
		}
	}
	return ids, nil
}

// one reads proposal id; false when the chain does not have it.
func (s *Set) one(ctx context.Context, id uint64) (Proposal, bool, error) {
	resp, err := s.deps.Client.Proposal(ctx, &daotypes.QueryProposalRequest{Id: id})
	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound:
		return Proposal{}, false, nil
	case codes.Unimplemented:
		return Proposal{}, false, errUnsupported
	default:
		return Proposal{}, false, fmt.Errorf("dao proposals: proposal %d: %w", id, err)
	}
	row, err := s.row(resp)
	return row, err == nil, err
}

// row turns the node's answer into its row.
func (s *Set) row(resp *daotypes.QueryProposalResponse) (Proposal, error) {
	p := resp.Proposal
	doc, err := s.deps.JSON.ProtoJSON(&p)
	if err != nil {
		return Proposal{}, fmt.Errorf("dao proposals: proposal %d: %w", p.Id, err)
	}
	var fields struct {
		Msgs         json.RawMessage `json:"msgs"`
		PinnedConfig json.RawMessage `json:"pinned_config"`
	}
	if err := json.Unmarshal(doc, &fields); err != nil {
		return Proposal{}, fmt.Errorf("dao proposals: proposal %d: %w", p.Id, err)
	}
	row := Proposal{
		ID: p.Id, DaoID: p.DaoId, Proposer: p.Proposer, Title: p.Title, Summary: p.Summary, Msgs: fields.Msgs,
		Expedited: p.Expedited, SubmitHeight: p.SubmitHeight, SubmitTime: time.Unix(p.SubmitTime, 0).UTC(),
		VotingEnd: time.Unix(p.VotingEnd, 0).UTC(), Denominator: intOrZero(p.PinnedDenominator.String()),
		PinnedConfig: fields.PinnedConfig, Status: StatusOf(p.Status),
		Yes: intOrZero(p.Yes.String()), No: intOrZero(p.No.String()), Abstain: intOrZero(p.Abstain.String()),
		TallyAt: s.deps.Now().UTC(), ExpediteSettleable: resp.ExpediteSettleable,
	}
	if len(row.Msgs) == 0 || string(row.Msgs) == "null" {
		row.Msgs = json.RawMessage(`[]`)
	}
	if p.Status == daotypes.ProposalStatus_EXECUTION_FAILED && resp.RetryDeadline > 0 {
		t := time.Unix(resp.RetryDeadline, 0).UTC()
		row.RetryDeadline = &t
	}
	return row, nil
}

// StatusOf is the status column value of a chain status.
func StatusOf(st daotypes.ProposalStatus) string {
	return strings.ToLower(st.String())
}

// intOrZero maps the String of a nil math.Int ("<nil>") to "0".
func intOrZero(s string) string {
	if s == "" || s == "<nil>" {
		return "0"
	}
	return s
}
