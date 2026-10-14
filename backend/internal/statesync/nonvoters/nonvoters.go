// Package nonvoters is the proposal_non_voters set of the state sync: for
// every proposal whose voting ended in a tally (passed, rejected, failed), the
// validators of the set the tally counted that did not vote. Only the
// non-voters are stored, with the height and size of the set.
//
// The tally set of a proposal resolved at height H is the CometBFT validator
// set at archive.TallySetHeight(H) (H+1, or H+2 in the generations whose
// staking EndBlocker ran before gov's) minus the validators jailed at H: the
// slashing BeginBlocker jails them before gov's EndBlocker tallies, which
// drops them from the staking power index the tally iterates, while
// CometBFT still lists them until their power update applies. A member
// voted when the account of its operator (the same key bytes) has a vote.
//
// The set waits for the votes to be complete: every height from the
// submission to the resolution indexed, or the votes read from the archive
// by the snapshots command (tally_source archive). It reads the set from the
// local node, or the archive once the local node pruned the height, so the
// same run serves a proposal that resolved a minute ago and one of years
// ago. Once written, a proposal is not computed again unless asked
// (sync-state --job proposal_non_voters --id N).
package nonvoters

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/archive"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is how often the set looks for proposals to compute.
const DefaultInterval = time.Minute

// powerReduction is the bonded tokens per unit of consensus power.
const powerReduction = 1_000_000

// TallySourceArchive is the tally_source of a proposal whose votes the
// snapshots command read from the archive.
const TallySourceArchive = "archive"

// Proposal is a closed proposal as the set reads it.
type Proposal struct {
	ID int64
	// SubmitHeight is nil for a proposal submitted below the indexed range.
	SubmitHeight   *int64
	ResolvedHeight int64
	// TallySource is "sync", "archive" or "" (none yet).
	TallySource string
	// BondedTokens is the bonded tokens the tally was measured against, ""
	// when unknown.
	BondedTokens string
}

// NonVoter is a validator of the tally set that did not vote.
type NonVoter struct {
	ConsensusAddress string
	// Operator is empty when no validator is known with the address.
	Operator    string
	VotingPower int64
	// PowerPct is the share of the tally set's power, percent with five
	// decimals.
	PowerPct string
}

// Result is what the set writes for a proposal.
type Result struct {
	ProposalID int64
	// SetHeight is the height of the /validators set read.
	SetHeight int64
	// SetSize is how many validators the tally counted.
	SetSize   int
	NonVoters []NonVoter
}

// Store reads the proposals and votes and writes the results.
type Store interface {
	// Pending lists the proposals whose voting ended in a tally, with a
	// resolution height and no non-voters written, by id.
	Pending(ctx context.Context) ([]Proposal, error)
	// Proposal returns proposal id when its voting ended in a tally and it
	// has a resolution height; false otherwise.
	Proposal(ctx context.Context, id int64) (Proposal, bool, error)
	// Indexed reports whether every height of [lo, hi] is indexed.
	Indexed(ctx context.Context, lo, hi int64) (bool, error)
	// Voters lists the accounts with a vote on proposal id.
	Voters(ctx context.Context, id int64) ([]string, error)
	// Save replaces the proposal's non-voters and records the set's height
	// and size, in one transaction.
	Save(ctx context.Context, r Result) error
}

// Node reads the tally's block; *activeset.Routed satisfies it.
type Node interface {
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
}

// Deps of the set.
type Deps struct {
	// Set reads the CometBFT set at a height.
	Set   activeset.Reader
	Node  Node
	Store Store
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Set is the proposal_non_voters set.
type Set struct {
	deps     Deps
	interval time.Duration
}

var _ statesync.Set = (*Set)(nil)

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{deps: deps, interval: interval}
}

// Name is statesync.ProposalNonVoters.
func (s *Set) Name() string { return statesync.ProposalNonVoters }

// Interval is how often the set looks for proposals to compute.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync computes every pending proposal whose votes are complete; the
// others wait for a later run. A proposal that fails is retried on the next
// run; the failures are returned joined.
func (s *Set) FullResync(ctx context.Context) error {
	pending, err := s.deps.Store.Pending(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range pending {
		complete, err := s.complete(ctx, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !complete {
			s.deps.Log.WithField("proposal", p.ID).Debug("proposal non-voters: votes not complete yet")
			continue
		}
		if err := s.compute(ctx, p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ResyncOne computes proposal key (its id) again, written or not. It fails
// while the proposal's votes are not complete.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("proposal non-voters: %q is not a proposal id", key)
	}
	p, ok, err := s.deps.Store.Proposal(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("proposal %d: no tally to read (not passed, rejected or failed, or no resolution height yet)", id)
	}
	complete, err := s.complete(ctx, p)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("proposal %d: votes not complete yet (heights %s to %d not all indexed and no archive snapshot)",
			id, heightOrUnknown(p.SubmitHeight), p.ResolvedHeight)
	}
	return s.compute(ctx, p)
}

func heightOrUnknown(h *int64) string {
	if h == nil {
		return "the unknown submission"
	}
	return strconv.FormatInt(*h, 10)
}

// complete reports whether every vote of p is in the database: read from
// the archive, or every height from its submission to its resolution
// indexed (no vote comes before the submission).
func (s *Set) complete(ctx context.Context, p Proposal) (bool, error) {
	if p.TallySource == TallySourceArchive {
		return true, nil
	}
	if p.SubmitHeight == nil {
		return false, nil
	}
	return s.deps.Store.Indexed(ctx, *p.SubmitHeight, p.ResolvedHeight)
}

// compute writes the non-voters of p.
func (s *Set) compute(ctx context.Context, p Proposal) error {
	setHeight := archive.TallySetHeight(p.ResolvedHeight)
	members, err := s.deps.Set.At(ctx, setHeight)
	if err != nil {
		return fmt.Errorf("proposal %d: %w", p.ID, err)
	}
	lg := s.deps.Log.WithFields(log.Fields{"proposal": p.ID, "resolved_height": p.ResolvedHeight, "set_height": setHeight})
	s.crossCheck(lg, p, members)

	jailed, err := s.jailedAt(ctx, p.ResolvedHeight)
	if err != nil {
		return fmt.Errorf("proposal %d: %w", p.ID, err)
	}
	voters, err := s.deps.Store.Voters(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("proposal %d: %w", p.ID, err)
	}
	voted := map[string]bool{}
	for _, v := range voters {
		voted[v] = true
	}

	var tally []activeset.Member
	var total int64
	for _, m := range members {
		if jailed[m.ConsensusAddress] {
			continue
		}
		tally = append(tally, m)
		total += m.VotingPower
	}
	res := Result{ProposalID: p.ID, SetHeight: setHeight, SetSize: len(tally)}
	for _, m := range tally {
		if m.Operator == "" {
			lg.WithField("consensus_address", m.ConsensusAddress).
				Warn("proposal non-voters: no validator known with this consensus address, stored without an operator")
		} else if voted[account(m.Operator)] {
			continue
		}
		res.NonVoters = append(res.NonVoters, NonVoter{
			ConsensusAddress: m.ConsensusAddress, Operator: m.Operator, VotingPower: m.VotingPower,
			PowerPct: pct(m.VotingPower, total),
		})
	}
	if err := s.deps.Store.Save(ctx, res); err != nil {
		return fmt.Errorf("proposal %d: %w", p.ID, err)
	}
	lg.WithFields(log.Fields{"set_size": res.SetSize, "jailed": len(members) - len(tally), "non_voters": len(res.NonVoters)}).
		Info("proposal non-voters: written")
	return nil
}

// crossCheck warns when the set's power does not add up to the bonded tokens
// the tally was measured against, within one unit of power per validator
// (each truncates its tokens). It reads the whole set, the validators
// jailed at the tally included: their tokens are still bonded then.
func (s *Set) crossCheck(lg log.FieldLogger, p Proposal, members []activeset.Member) {
	bonded, ok := new(big.Int).SetString(p.BondedTokens, 10)
	if !ok {
		return
	}
	var power int64
	for _, m := range members {
		power += m.VotingPower
	}
	diff := new(big.Int).Sub(bonded, new(big.Int).Mul(big.NewInt(power), big.NewInt(powerReduction)))
	tolerance := big.NewInt(int64(len(members)) * powerReduction)
	if diff.CmpAbs(tolerance) > 0 {
		lg.WithFields(log.Fields{"set_power": power, "tally_bonded_tokens": p.BondedTokens, "tally_source": p.TallySource}).
			Warn("proposal non-voters: the set's power does not add up to the tally's bonded tokens")
	}
}

// jailedAt returns the consensus addresses (upper-case hex) of the
// validators slashed in the block at height: the slashing module jails
// every validator it slashes, in the BeginBlocker, before gov tallies.
func (s *Set) jailedAt(ctx context.Context, height int64) (map[string]bool, error) {
	res, _, err := s.deps.Node.BlockResults(ctx, height)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, events := range [][]node.Event{res.FinalizeBlockEvents, res.BeginBlockEvents} {
		for _, ev := range events {
			if ev.Type != "slash" {
				continue
			}
			for _, key := range []string{"address", "jailed"} {
				v, _ := ev.Get(key)
				if h := consHex(v); h != "" {
					out[h] = true
				}
			}
		}
	}
	return out, nil
}

// consHex turns a bech32 consensus address into upper-case hex; empty when
// it is not one.
func consHex(addr string) string {
	hrp, b, err := bech32.DecodeAndConvert(addr)
	if err != nil || !strings.HasSuffix(hrp, "valcons") {
		return ""
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

// account is the account address of an operator address: the same bytes
// under the account prefix; empty when op is not bech32.
func account(op string) string {
	_, b, err := bech32.DecodeAndConvert(op)
	if err != nil {
		return ""
	}
	acc, err := bech32.ConvertAndEncode(chain.Bech32Prefix, b)
	if err != nil {
		return ""
	}
	return acc
}

// pct is part / whole in percent, rounded half up to five decimals; whole
// is positive.
func pct(part, whole int64) string {
	if whole <= 0 {
		return "0.00000"
	}
	q := strconv.FormatInt((part*10_000_000*2+whole)/(2*whole), 10)
	if len(q) <= 5 {
		q = strings.Repeat("0", 6-len(q)) + q
	}
	return q[:len(q)-5] + "." + q[len(q)-5:]
}
