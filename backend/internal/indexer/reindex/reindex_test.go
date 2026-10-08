package reindex_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/reindex"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

func ptr(v int64) *int64 { return &v }

func TestParse(t *testing.T) {
	cases := []struct {
		name  string
		flags reindex.Flags
		want  reindex.Selection
		err   string
	}{
		{"heights sorted down, deduplicated", reindex.Flags{Heights: "100, 300,200,300"},
			reindex.Selection{Kind: reindex.KindHeights, Heights: []int64{300, 200, 100}}, ""},
		{"one height", reindex.Flags{Heights: "7"}, reindex.Selection{Kind: reindex.KindHeights, Heights: []int64{7}}, ""},
		{"range", reindex.Flags{From: ptr(10), To: ptr(20)}, reindex.Selection{Kind: reindex.KindRange, From: 10, To: 20}, ""},
		{"range of one", reindex.Flags{From: ptr(5), To: ptr(5)}, reindex.Selection{Kind: reindex.KindRange, From: 5, To: 5}, ""},
		{"failed", reindex.Flags{Failed: true}, reindex.Selection{Kind: reindex.KindFailed}, ""},
		{"failed of a source", reindex.Flags{Failed: true, Source: "live"},
			reindex.Selection{Kind: reindex.KindFailed, Source: "live"}, ""},

		{"nothing", reindex.Flags{}, reindex.Selection{}, "is required"},
		{"heights and range", reindex.Flags{Heights: "1", From: ptr(1), To: ptr(2)}, reindex.Selection{}, "mutually exclusive"},
		{"heights and failed", reindex.Flags{Heights: "1", Failed: true}, reindex.Selection{}, "mutually exclusive"},
		{"range and failed", reindex.Flags{From: ptr(1), To: ptr(2), Failed: true}, reindex.Selection{}, "mutually exclusive"},
		{"from alone", reindex.Flags{From: ptr(1)}, reindex.Selection{}, "go together"},
		{"to alone", reindex.Flags{To: ptr(1)}, reindex.Selection{}, "go together"},
		{"inverted range", reindex.Flags{From: ptr(20), To: ptr(10)}, reindex.Selection{}, "above --to"},
		{"range from zero", reindex.Flags{From: ptr(0), To: ptr(10)}, reindex.Selection{}, "at least 1"},
		{"bad height", reindex.Flags{Heights: "1,x"}, reindex.Selection{}, `"x"`},
		{"empty item", reindex.Flags{Heights: "1,,2"}, reindex.Selection{}, `""`},
		{"zero height", reindex.Flags{Heights: "0"}, reindex.Selection{}, `"0"`},
		{"source without failed", reindex.Flags{Heights: "1", Source: "live"}, reindex.Selection{}, "--failed only"},
		{"unknown source", reindex.Flags{Failed: true, Source: "manual"}, reindex.Selection{}, "live, backfill, reindex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reindex.Parse(tc.flags)
			if tc.err != "" {
				require.ErrorIs(t, err, reindex.ErrSelection)
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// mockNode serves every height up to tip and records the heights asked for
// Block; pruned answers the pruned error below it.
type mockNode struct {
	mu        sync.Mutex
	tip       int64
	pruned    int64
	earliest  int64
	statusErr error
	blocks    []int64
}

func (n *mockNode) check(h int64) error {
	switch {
	case h > n.tip:
		return errors.New("height above the node's tip")
	case h < n.pruned:
		return node.ErrPruned
	}
	return nil
}

func (n *mockNode) Block(_ context.Context, h int64) (*node.Block, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.blocks = append(n.blocks, h)
	if err := n.check(h); err != nil {
		return nil, nil, err
	}
	return &node.Block{Height: h}, nil, nil
}

func (n *mockNode) BlockResults(_ context.Context, h int64) (*node.BlockResults, []byte, error) {
	if err := n.check(h); err != nil {
		return nil, nil, err
	}
	return &node.BlockResults{Height: h}, nil, nil
}

func (n *mockNode) Commit(_ context.Context, h int64) (*node.Commit, []byte, error) {
	if err := n.check(h); err != nil {
		return nil, nil, err
	}
	return &node.Commit{Height: h}, nil, nil
}

func (n *mockNode) Status(context.Context) (*node.Status, []byte, error) {
	if n.statusErr != nil {
		return nil, nil, n.statusErr
	}
	return &node.Status{EarliestBlockHeight: n.earliest, LatestBlockHeight: n.tip}, nil, nil
}

func (n *mockNode) asked() []int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := slices.Clone(n.blocks)
	slices.Sort(out)
	return slices.Compact(out)
}

type mockTransformer struct{}

func (mockTransformer) Transform(in transform.Input) (*transform.Entities, error) {
	return &transform.Entities{Blocks: []transform.Block{{Height: in.Block.Height}}}, nil
}

type mockWriter struct {
	mu       sync.Mutex
	written  []int64
	modes    []writer.Mode
	failures []string
	writeErr error
}

func (w *mockWriter) Write(_ context.Context, batch []*transform.Entities, mode writer.Mode) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		return w.writeErr
	}
	for _, e := range batch {
		w.written = append(w.written, e.Blocks[0].Height)
	}
	w.modes = append(w.modes, mode)
	return nil
}

func (w *mockWriter) RecordFailure(_ context.Context, h int64, source string, _ int, _ error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failures = append(w.failures, source)
	_ = h
	return nil
}

type mockStore struct {
	mu       sync.Mutex
	failures map[string][]int64
	saves    []backfill.Checkpoint
	saveErr  error
}

func (s *mockStore) UnresolvedFailures(_ context.Context, source string) ([]int64, error) {
	return s.failures[source], nil
}

func (s *mockStore) SaveCheckpoint(_ context.Context, cp backfill.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saves = append(s.saves, cp)
	return nil
}

func (s *mockStore) last() backfill.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves[len(s.saves)-1]
}

var start = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type env struct {
	local, archive, retry *mockNode
	writer                *mockWriter
	store                 *mockStore
}

func newEnv() *env {
	return &env{
		local:   &mockNode{tip: 1000, earliest: 50, pruned: 50},
		archive: &mockNode{tip: 1000},
		retry:   &mockNode{tip: 1000},
		writer:  &mockWriter{},
		store:   &mockStore{failures: map[string][]int64{}},
	}
}

func (e *env) job(local reindex.LocalNode) *reindex.Job {
	clock := start
	return reindex.New(backfill.Config{
		Workers: 2, Batch: 3, RetryDelays: []time.Duration{0, 0},
		Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}, reindex.Deps{
		Store: e.store,
		Local: local,
		Pipeline: backfill.Deps{
			Archive: e.archive, ArchiveRetry: e.retry, Transformer: mockTransformer{}, Writer: e.writer,
		},
		Now: func() time.Time {
			clock = clock.Add(1500 * time.Millisecond)
			return clock
		},
	})
}

func (e *env) run(t *testing.T, local reindex.LocalNode, sel reindex.Selection) (reindex.Result, error) {
	t.Helper()
	j := e.job(local)
	p, err := j.Plan(context.Background(), sel)
	require.NoError(t, err)
	return j.Run(context.Background(), p)
}

func TestSourceSelectionByEarliestBlockHeight(t *testing.T) {
	e := newEnv()
	e.local.pruned = 55 // pruned further since /status answered
	res, err := e.run(t, e.local, reindex.Selection{Kind: reindex.KindRange, From: 45, To: 60})
	require.NoError(t, err)
	assert.Empty(t, res.Failed)

	assert.Equal(t, heights(50, 60), e.local.asked(), "at or above earliest_block_height: the local node")
	assert.Equal(t, heights(45, 54), e.archive.asked(),
		"below it the archive, and the heights the local node pruned meanwhile")
	assert.Empty(t, e.retry.asked(), "the retry endpoint only on errors")
}

func TestEveryHeightFromTheArchiveWithoutALocalNode(t *testing.T) {
	e := newEnv()
	e.local.statusErr = errors.New("connection refused")
	_, err := e.run(t, e.local, reindex.Selection{Kind: reindex.KindHeights, Heights: []int64{70, 60}})
	require.NoError(t, err)
	assert.Empty(t, e.local.asked(), "an unreachable local node is not asked")
	assert.Equal(t, []int64{60, 70}, e.archive.asked())

	e = newEnv()
	_, err = e.run(t, nil, reindex.Selection{Kind: reindex.KindHeights, Heights: []int64{70}})
	require.NoError(t, err)
	assert.Equal(t, []int64{70}, e.archive.asked())
}

func TestARunOverwritesAndEndsDone(t *testing.T) {
	e := newEnv()
	res, err := e.run(t, e.local, reindex.Selection{Kind: reindex.KindRange, From: 100, To: 107})
	require.NoError(t, err)

	assert.Equal(t, "reindex-2026-10-08T12:00:01Z", res.Job, "named after the start time")
	assert.Equal(t, int64(8), res.Heights)
	assert.Equal(t, int64(8), res.Written)
	assert.ElementsMatch(t, heights(107, 100), e.writer.written)
	for _, m := range e.writer.modes {
		assert.Equal(t, writer.ModeUpdate, m)
	}
	assert.Equal(t, "reindex done heights=8 failed=0 duration=1.5s", res.Summary())

	first, last := e.store.saves[0], e.store.last()
	assert.Equal(t, backfill.Checkpoint{Job: res.Job, Ceiling: 107, Floor: 100, LowestDispatched: 108,
		Status: backfill.StatusRunning}, first)
	assert.Equal(t, backfill.Checkpoint{Job: res.Job, Ceiling: 107, Floor: 100, LowestDispatched: 100,
		BlocksDone: 8, Status: backfill.StatusDone}, last)
	assert.Greater(t, len(e.store.saves), 3, "saved at every flush")
}

func TestFailedHeightsAreRecordedAndListed(t *testing.T) {
	e := newEnv()
	res, err := e.run(t, e.local, reindex.Selection{Kind: reindex.KindHeights, Heights: []int64{1002, 999, 1001}})
	require.NoError(t, err, "a failed height is not a run error")
	assert.Equal(t, []int64{1001, 1002}, res.Failed, "above the tip, ascending")
	assert.Equal(t, []string{writer.FailureSourceReindex, writer.FailureSourceReindex}, e.writer.failures)
	assert.Equal(t, []int64{999}, e.writer.written)
	assert.Equal(t, backfill.StatusDone, e.store.last().Status)
	assert.Contains(t, res.Summary(), "heights=3 failed=2")
}

func TestFailedSelectionReadsTheOpenFailures(t *testing.T) {
	e := newEnv()
	e.store.failures["live"] = []int64{300, 200}
	j := e.job(e.local)
	p, err := j.Plan(context.Background(), reindex.Selection{Kind: reindex.KindFailed, Source: "live"})
	require.NoError(t, err)
	assert.Equal(t, "reindex selection heights=2 lowest=200 highest=300\n300\n200\n", p.Describe())

	p, err = j.Plan(context.Background(), reindex.Selection{Kind: reindex.KindFailed})
	require.NoError(t, err)
	assert.Equal(t, "reindex selection heights=0\n", p.Describe())
	res, err := j.Run(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, "reindex done heights=0 failed=0 duration=0s", res.Summary())
	assert.Empty(t, e.store.saves, "nothing to do, no checkpoint row")
}

func TestARangeDescribesItself(t *testing.T) {
	p, err := newEnv().job(nil).Plan(context.Background(), reindex.Selection{Kind: reindex.KindRange, From: 10, To: 19})
	require.NoError(t, err)
	assert.Equal(t, "reindex selection heights=10 lowest=10 highest=19\n", p.Describe())
}

func TestADatabaseErrorEndsInError(t *testing.T) {
	e := newEnv()
	e.writer.writeErr = errors.New("database unavailable")
	_, err := e.run(t, e.local, reindex.Selection{Kind: reindex.KindRange, From: 100, To: 103})
	require.ErrorContains(t, err, "database unavailable")
	assert.Equal(t, backfill.StatusError, e.store.last().Status)
	assert.Contains(t, e.store.last().LastError, "database unavailable")

	e = newEnv()
	e.store.saveErr = errors.New("database unavailable")
	_, err = e.run(t, e.local, reindex.Selection{Kind: reindex.KindRange, From: 100, To: 103})
	require.ErrorContains(t, err, "database unavailable", "the first checkpoint save fails the run")
	assert.Empty(t, e.archive.asked())
}

func TestACancelledRunEndsInError(t *testing.T) {
	e := newEnv()
	j := e.job(e.local)
	p, err := j.Plan(context.Background(), reindex.Selection{Kind: reindex.KindRange, From: 100, To: 200})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = j.Run(ctx, p)
	require.ErrorIs(t, err, reindex.ErrInterrupted)
	assert.Equal(t, backfill.StatusError, e.store.last().Status)
}

func heights(from, to int64) []int64 {
	var out []int64
	step := int64(1)
	if to < from {
		step = -1
	}
	for h := from; ; h += step {
		out = append(out, h)
		if h == to {
			return out
		}
	}
}
