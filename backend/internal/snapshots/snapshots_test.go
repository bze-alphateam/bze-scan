package snapshots_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gogoproto "github.com/cosmos/gogoproto/proto"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// fakeKind snapshots its targets, failing those in fail.
type fakeKind struct {
	name    string
	targets []string
	fail    map[string]bool
	listErr error

	mu   sync.Mutex
	done []string
	ids  []string
}

func (k *fakeKind) Name() string { return k.name }

func (k *fakeKind) Targets(_ context.Context, ids []string) ([]snapshots.Target, error) {
	k.ids = ids
	if k.listErr != nil {
		return nil, k.listErr
	}
	var out []snapshots.Target
	for _, t := range k.targets {
		out = append(out, snapshots.Target{ID: t})
	}
	return out, nil
}

func (k *fakeKind) Snapshot(_ context.Context, _ snapshots.Reader, t snapshots.Target) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.done = append(k.done, t.ID)
	if k.fail[t.ID] {
		return "", errors.New("archive down")
	}
	return "ok", nil
}

type fakeJobs struct{ runs []statesync.Run }

func (j *fakeJobs) RecordRun(_ context.Context, r statesync.Run) error {
	j.runs = append(j.runs, r)
	return nil
}

func runner(jobs *fakeJobs, kinds ...snapshots.Kind) (*snapshots.Runner, *test.Hook) {
	logger, hook := test.NewNullLogger()
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	return snapshots.New(snapshots.Deps{Kinds: kinds, Jobs: jobs, Log: logger, Now: func() time.Time { return now }}, 2), hook
}

func TestRunnerContinuesPastAFailedTargetAndReportsIt(t *testing.T) {
	k := &fakeKind{name: "proposals", targets: []string{"41", "42", "43", "44"}, fail: map[string]bool{"42": true, "44": true}}
	jobs := &fakeJobs{}
	r, hook := runner(jobs, k)

	res, err := r.Run(context.Background(), snapshots.Selection{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"41", "42", "43", "44"}, k.done, "every target is tried")
	assert.Equal(t, 4, res.Targets)
	assert.Equal(t, []string{"proposals 42", "proposals 44"}, res.Failed)
	assert.Equal(t, "snapshots: 4 targets, 2 failed", res.Summary())

	require.Len(t, jobs.runs, 1)
	assert.Equal(t, "snapshot:proposals", jobs.runs[0].Job)
	require.Error(t, jobs.runs[0].Err)
	assert.Equal(t, "2 of 4 targets failed: 42, 44", jobs.runs[0].Err.Error())

	var lines, errs int
	for _, e := range hook.AllEntries() {
		if e.Data["kind"] == "proposals" && e.Data["id"] != nil {
			lines++
			if e.Level == log.ErrorLevel {
				errs++
			}
		}
	}
	assert.Equal(t, 4, lines, "one log line per target")
	assert.Equal(t, 2, errs)
}

func TestRunnerRecordsASuccessfulRun(t *testing.T) {
	k := &fakeKind{name: "proposals", targets: []string{"47"}}
	jobs := &fakeJobs{}
	r, _ := runner(jobs, k)
	res, err := r.Run(context.Background(), snapshots.Selection{Kinds: []string{"proposals"}, IDs: []string{"47"}})
	require.NoError(t, err)
	assert.Empty(t, res.Failed)
	assert.Equal(t, []string{"47"}, k.ids, "the ids go to the kind")
	require.Len(t, jobs.runs, 1)
	assert.NoError(t, jobs.runs[0].Err)
}

func TestRunnerSelectsKinds(t *testing.T) {
	a := &fakeKind{name: "proposals", targets: []string{"1"}}
	b := &fakeKind{name: "params", targets: []string{"x"}}
	jobs := &fakeJobs{}
	r, _ := runner(jobs, a, b)
	assert.Equal(t, []string{"proposals", "params"}, r.Names())

	_, err := r.Run(context.Background(), snapshots.Selection{Kinds: []string{"params"}})
	require.NoError(t, err)
	assert.Empty(t, a.done)
	assert.Equal(t, []string{"x"}, b.done)

	_, err = r.Run(context.Background(), snapshots.Selection{Kinds: []string{"votes"}})
	require.ErrorContains(t, err, `unknown kind "votes" (kinds: proposals, params)`)
}

func TestRunnerCountsAKindThatCannotListItsTargets(t *testing.T) {
	broken := &fakeKind{name: "proposals", listErr: errors.New("database down")}
	other := &fakeKind{name: "params", targets: []string{"x"}}
	jobs := &fakeJobs{}
	r, _ := runner(jobs, broken, other)
	res, err := r.Run(context.Background(), snapshots.Selection{})
	require.NoError(t, err)
	assert.Equal(t, []string{"proposals"}, res.Failed)
	assert.Equal(t, []string{"x"}, other.done, "the other kinds still run")
	require.Len(t, jobs.runs, 2)
	assert.EqualError(t, jobs.runs[0].Err, "database down")
}

// countingLimiter counts its waits.
type countingLimiter struct{ waits int }

func (l *countingLimiter) Wait(context.Context) error {
	l.waits++
	return nil
}

type stubNode struct{}

func (stubNode) Query(context.Context, string, gogoproto.Message, gogoproto.Message, int64) error {
	return nil
}

func (stubNode) Status(context.Context) (*node.Status, []byte, error) {
	return &node.Status{LatestBlockHeight: 9}, nil, nil
}

func (stubNode) Block(_ context.Context, h int64) (*node.Block, []byte, error) {
	return &node.Block{Height: h}, nil, nil
}

func (stubNode) BlockResults(_ context.Context, h int64) (*node.BlockResults, []byte, error) {
	return &node.BlockResults{Height: h}, nil, nil
}

func TestArchivePacesEveryRequest(t *testing.T) {
	l := &countingLimiter{}
	a := snapshots.NewArchive(stubNode{}, l)
	ctx := context.Background()
	require.NoError(t, a.Query(ctx, "/x/Y", nil, nil, 1))
	st, err := a.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(9), st.LatestBlockHeight)
	b, err := a.Block(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(5), b.Height)
	_, err = a.BlockResults(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, 4, l.waits)
}
