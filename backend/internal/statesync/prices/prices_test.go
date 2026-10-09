package prices_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
)

type fakeSource struct {
	prices map[string]string
	err    error
}

func (s *fakeSource) Prices(context.Context) (map[string]string, error) { return s.prices, s.err }

// fakeStore keeps the last prices written, as the table keeps them.
type fakeStore struct {
	saved   map[string]string
	chainID string
	writes  int
}

func (s *fakeStore) Save(_ context.Context, chainID string, p map[string]string) error {
	s.saved, s.chainID = p, chainID
	s.writes++
	return nil
}

func TestPricesAreRefreshedAndKeptWhenTheSourceFails(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.000168060000"}}
	store := &fakeStore{}
	s := prices.New(prices.Deps{Source: src, Store: store, ChainID: "beezee-1", Log: lg}, 0)
	assert.Equal(t, statesync.Prices, s.Name())
	assert.Equal(t, prices.DefaultInterval, s.Interval())
	ctx := context.Background()

	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, map[string]string{"bzedge": "0.000168060000"}, store.saved)
	assert.Equal(t, "beezee-1", store.chainID)

	// The aggregator fails for three minutes: nothing is written, one warning.
	src.err = errors.New("HTTP 502")
	for range 3 {
		err := s.FullResync(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "HTTP 502", "recorded in sync_jobs")
	}
	assert.Equal(t, 1, store.writes, "the previous prices are kept")
	warnings := 0
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			warnings++
		}
	}
	assert.Equal(t, 1, warnings, "logged once per change of state")

	// It answers again: one info line, the new prices written.
	src.err, src.prices = nil, map[string]string{"bzedge": "0.0002"}
	require.NoError(t, s.ResyncOne(ctx, "any"))
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
	assert.Equal(t, map[string]string{"bzedge": "0.0002"}, store.saved)
	assert.Len(t, hook.AllEntries(), 2)
}

func TestAFailureIsNotLoggedAgainByTheSyncer(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	s := prices.New(prices.Deps{Source: &fakeSource{err: errors.New("down")}, Store: &fakeStore{}, Log: lg}, 0)
	sync := statesync.New(statesync.Config{Log: lg}, noJobs{}, s)
	require.Error(t, sync.RunOnce(context.Background()))
	require.Error(t, sync.RunOnce(context.Background()))
	assert.Len(t, hook.AllEntries(), 1, "the set's own warning only")
}

type noJobs struct{}

func (noJobs) RecordRun(context.Context, statesync.Run) error { return nil }
