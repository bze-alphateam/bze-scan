package upgrades_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
)

// The synthetic upgrade of the tests: planned at 100, consensus waits at
// 101. Four validators by power: A and B come back during the halt (70%
// of the power: the chain resumes), C signs 105, D never comes back.
const (
	planHeight int64 = 100
	haltHeight int64 = 101
	consA            = "AAAA00000000000000000000000000000000000A"
	consB            = "BBBB00000000000000000000000000000000000B"
	consC            = "CCCC00000000000000000000000000000000000C"
	consD            = "DDDD00000000000000000000000000000000000D"
)

var (
	base = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	set  = []activeset.Member{
		{ConsensusAddress: consA, VotingPower: 40, Operator: "bzevaloper1a"},
		{ConsensusAddress: consB, VotingPower: 30, Operator: "bzevaloper1b"},
		{ConsensusAddress: consC, VotingPower: 20, Operator: "bzevaloper1c"},
		{ConsensusAddress: consD, VotingPower: 10},
	}
)

// blockTime is the time of block h in the synthetic chain: six seconds a
// block, and the halt lasts from block 101's time to block 102's (ten
// minutes).
func blockTime(h int64) time.Time {
	t := base.Add(time.Duration(h-haltHeight) * 6 * time.Second)
	if h > haltHeight {
		t = t.Add(10 * time.Minute)
	}
	return t
}

// row is a member as the store holds it.
type row struct {
	operator    string
	power       int64
	back        *time.Time
	source      string
	voteTime    *time.Time
	firstSigned int64
	needed      int64
	secondsBack *int
}

// memStore is the Store in memory, with the PGStore's rules.
type memStore struct {
	mu       sync.Mutex
	upgrades map[int64]*upgrades.Upgrade
	computed map[int64]bool
	setSize  map[int64]int
	planned  []upgrades.Upgrade // what Discover finds
	rows     map[int64]map[string]*row
	tip      int64
	gaps     map[int64]bool // indexed heights missing below the tip
	scans    []upgrades.Scan
	failSave error
}

// resumed reports whether blocks U+1 and U+2 are indexed.
func (s *memStore) resumed(u upgrades.Upgrade) bool {
	return s.tip >= u.Height+2 && !s.gaps[u.Height+1] && !s.gaps[u.Height+2]
}

func newMemStore(tip int64) *memStore {
	return &memStore{
		upgrades: map[int64]*upgrades.Upgrade{}, computed: map[int64]bool{}, setSize: map[int64]int{},
		rows: map[int64]map[string]*row{}, tip: tip,
		planned: []upgrades.Upgrade{{Height: planHeight, ProposalID: 48, Name: "v9.0.0"}},
	}
}

func (s *memStore) Discover(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.planned {
		if _, ok := s.upgrades[u.Height]; !ok {
			u := u
			s.upgrades[u.Height] = &u
		}
	}
	return nil
}

func (s *memStore) Pending(context.Context) ([]upgrades.Upgrade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []upgrades.Upgrade
	for h, u := range s.upgrades {
		if !s.computed[h] {
			c := *u
			c.Resumed = s.resumed(c)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height < out[j].Height })
	return out, nil
}

func (s *memStore) Upgrade(_ context.Context, height int64) (upgrades.Upgrade, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.upgrades[height]
	if !ok {
		return upgrades.Upgrade{}, false, nil
	}
	c := *u
	c.Resumed = s.resumed(c)
	return c, true, nil
}

func (s *memStore) Tip(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tip, s.tip > 0, nil
}

func (s *memStore) Members(_ context.Context, height int64) ([]upgrades.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []upgrades.Member
	for cons, r := range s.rows[height] {
		out = append(out, upgrades.Member{ConsensusAddress: cons, Back: r.back != nil})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConsensusAddress < out[j].ConsensusAddress })
	return out, nil
}

func (s *memStore) AddMembers(_ context.Context, height int64, members []activeset.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows[height] == nil {
		s.rows[height] = map[string]*row{}
	}
	for _, m := range members {
		if r, ok := s.rows[height][m.ConsensusAddress]; ok {
			if m.Operator != "" {
				r.operator = m.Operator
			}
			continue
		}
		s.rows[height][m.ConsensusAddress] = &row{operator: m.Operator, power: m.VotingPower}
	}
	return nil
}

func (s *memStore) HaltVotes(_ context.Context, height int64, votes []upgrades.HaltVote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range votes {
		r, ok := s.rows[height][v.ConsensusAddress]
		if !ok || r.source == upgrades.SourceHaltVote {
			continue
		}
		back, at := v.BackTime, v.VoteTime
		*r = row{operator: r.operator, power: r.power, back: &back, source: upgrades.SourceHaltVote, voteTime: &at}
	}
	if u, ok := s.upgrades[height]; ok {
		u.Watched = true
	}
	return nil
}

func (s *memStore) SaveScan(_ context.Context, sc upgrades.Scan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSave != nil {
		return s.failSave
	}
	s.scans = append(s.scans, sc)
	for _, b := range sc.Backs {
		r, ok := s.rows[sc.Height][b.ConsensusAddress]
		if !ok || r.back != nil {
			continue
		}
		back, at := b.BackTime, b.VoteTime
		r.back, r.source, r.voteTime, r.firstSigned, r.needed = &back, upgrades.SourceCommit, &at, b.FirstSignedHeight, b.BlocksNeeded
	}
	u := s.upgrades[sc.Height]
	if sc.HaltTime != nil {
		u.HaltTime = sc.HaltTime
	}
	if sc.ResumeTime != nil {
		u.ResumeTime = sc.ResumeTime
	}
	u.ScannedTo = sc.ScannedTo
	s.setSize[sc.Height] = sc.SetSize
	s.computed[sc.Height] = sc.Complete
	for _, r := range s.rows[sc.Height] {
		if r.back != nil && u.HaltTime != nil {
			secs := max(0, int(r.back.Sub(*u.HaltTime)/time.Second))
			r.secondsBack = &secs
		}
	}
	return nil
}

func (s *memStore) Reset(_ context.Context, height int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows[height] {
		if r.source != upgrades.SourceHaltVote {
			*r = row{operator: r.operator, power: r.power}
		}
	}
	u := s.upgrades[height]
	u.HaltTime, u.ResumeTime, u.ScannedTo = nil, nil, 0
	s.computed[height] = false
	return nil
}

// recovery is the members of the upgrade as "cons source back first needed
// seconds", "-" for what is unset, sorted.
func (s *memStore) recovery(height int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for cons, r := range s.rows[height] {
		line := cons[:4]
		if r.back == nil {
			line += " -"
		} else {
			line += fmt.Sprintf(" %s %s", r.source, r.back.Format(time.TimeOnly))
			if r.source == upgrades.SourceCommit {
				line += fmt.Sprintf(" %d %d", r.firstSigned, r.needed)
			}
			if r.secondsBack != nil {
				line += fmt.Sprintf(" %ds", *r.secondsBack)
			}
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func (s *memStore) upgrade(height int64) (upgrades.Upgrade, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.upgrades[height], s.computed[height]
}

// fakeSet is the activeset.Reader of the tests.
type fakeSet struct {
	mu      sync.Mutex
	members []activeset.Member
	err     error
	calls   []int64
}

func (f *fakeSet) At(_ context.Context, height int64) ([]activeset.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, height)
	if f.err != nil {
		return nil, f.err
	}
	return f.members, nil
}

// fakeCommits answers the commits of the synthetic chain: signed(h) are the
// validators that signed h, after an absent slot.
type fakeCommits struct {
	mu     sync.Mutex
	signed func(h int64) []string
	failAt map[int64]error
	reads  []int64
}

func (f *fakeCommits) Commit(_ context.Context, h int64) (*node.Commit, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, h)
	if err := f.failAt[h]; err != nil {
		return nil, nil, err
	}
	c := &node.Commit{Height: h, Time: blockTime(h)}
	c.Signatures = append(c.Signatures, node.CommitSig{BlockIDFlag: 1}) // an absent slot
	for _, cons := range f.signed(h) {
		c.Signatures = append(c.Signatures, node.CommitSig{
			BlockIDFlag: node.BlockIDFlagCommit, ValidatorAddress: cons, Timestamp: blockTime(h).Add(5 * time.Second),
		})
	}
	return c, nil, nil
}

// synthetic is who signs each height: A and B from 101, C from 105, D
// never.
func synthetic(h int64) []string {
	out := []string{consA, consB}
	if h >= 105 {
		out = append(out, consC)
	}
	return out
}

// countingLimiter counts the waits.
type countingLimiter struct {
	mu sync.Mutex
	n  int
}

func (l *countingLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	return ctx.Err()
}

var errDown = fmt.Errorf("commit: %w: HTTP 503", node.ErrUnavailable)
