package holders_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/holders"
)

// fakeBank pages each denom's owners two by two; a page key is "<denom>#<i>".
type fakeBank struct {
	owners map[string][]string // denom → balances, the owner of the i-th is addr<i>
	// failAt fails the call for this page key once.
	failAt string
	calls  []string
}

func (b *fakeBank) DenomOwners(_ context.Context, in *banktypes.QueryDenomOwnersRequest, _ ...grpc.CallOption) (*banktypes.QueryDenomOwnersResponse, error) {
	key := string(in.Pagination.Key)
	b.calls = append(b.calls, in.Denom+"@"+key)
	if b.failAt != "" && key == b.failAt {
		b.failAt = ""
		return nil, errors.New("node down")
	}
	start := 0
	if key != "" {
		_, _ = fmt.Sscanf(key[len(in.Denom)+1:], "%d", &start)
	}
	all := b.owners[in.Denom]
	end := min(start+2, len(all))
	resp := &banktypes.QueryDenomOwnersResponse{Pagination: &query.PageResponse{}}
	for i := start; i < end; i++ {
		amt, _ := sdkmath.NewIntFromString(all[i])
		resp.DenomOwners = append(resp.DenomOwners, &banktypes.DenomOwner{
			Address: fmt.Sprintf("addr%d", i), Balance: sdk.Coin{Denom: in.Denom, Amount: amt}})
	}
	if end < len(all) {
		resp.Pagination.NextKey = fmt.Appendf(nil, "%s#%d", in.Denom, end)
	}
	return resp, nil
}

// fakeStore keeps the snapshot like the table does.
type fakeStore struct {
	denoms   []string
	cursor   json.RawMessage
	rows     map[string]map[string]stampedBalance // denom → address → row
	finished []string
}

type stampedBalance struct {
	balance string
	stamp   time.Time
}

func (s *fakeStore) Denoms(context.Context) ([]string, error)        { return s.denoms, nil }
func (s *fakeStore) Cursor(context.Context) (json.RawMessage, error) { return s.cursor, nil }
func (s *fakeStore) Finish(_ context.Context, d string, st time.Time) error {
	for addr, r := range s.rows[d] {
		if r.stamp.Before(st) {
			delete(s.rows[d], addr)
		}
	}
	s.finished = append(s.finished, d)
	return nil
}

func (s *fakeStore) SavePage(_ context.Context, d string, st time.Time, owners []holders.Owner) error {
	if s.rows == nil {
		s.rows = map[string]map[string]stampedBalance{}
	}
	if s.rows[d] == nil {
		s.rows[d] = map[string]stampedBalance{}
	}
	for _, o := range owners {
		s.rows[d][o.Address] = stampedBalance{balance: o.Balance, stamp: st}
	}
	return nil
}

func (s *fakeStore) balances(d string) map[string]string {
	out := map[string]string{}
	for a, r := range s.rows[d] {
		out[a] = r.balance
	}
	return out
}

// clock moves a second per Now and records the pauses.
type clock struct {
	now    time.Time
	pauses int
}

func (c *clock) Now() time.Time {
	c.now = c.now.Add(time.Second)
	return c.now
}

func (c *clock) Sleep(ctx context.Context, _ time.Duration) error {
	c.pauses++
	return ctx.Err()
}

func TestSnapshotPagesEveryDenomAndDropsTheGoneOwners(t *testing.T) {
	bank := &fakeBank{owners: map[string][]string{"uaaa": {"5", "0", "7", "9", "1"}, "ubbb": {"3"}}}
	store := &fakeStore{denoms: []string{"uaaa", "ubbb"},
		rows: map[string]map[string]stampedBalance{"ubbb": {"gone": {balance: "4", stamp: time.Unix(1, 0)}}}}
	c := &clock{now: time.Unix(100, 0)}
	s := holders.New(holders.Deps{Bank: bank, Store: store, Clock: c}, 0)
	assert.Equal(t, statesync.Holders, s.Name())
	assert.Equal(t, holders.DefaultInterval, s.Interval())

	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"uaaa@", "uaaa@uaaa#2", "uaaa@uaaa#4", "ubbb@"}, bank.calls)
	assert.Equal(t, 2, c.pauses, "a pause between pages, none between denoms")
	assert.Equal(t, map[string]string{"addr0": "5", "addr2": "7", "addr3": "9", "addr4": "1"}, store.balances("uaaa"),
		"every non-zero balance")
	assert.Equal(t, map[string]string{"addr0": "3"}, store.balances("ubbb"), "an owner not listed any more is deleted")
	assert.Equal(t, []string{"uaaa", "ubbb"}, store.finished)
	assert.JSONEq(t, `{}`, string(s.Cursor()), "nothing left to resume")
}

func TestARestartResumesFromTheStoredCursor(t *testing.T) {
	bank := &fakeBank{owners: map[string][]string{"uaaa": {"1", "2"}, "ubbb": {"1", "2", "3", "4", "5"}, "uccc": {"9"}}, failAt: "ubbb#4"}
	store := &fakeStore{denoms: []string{"uaaa", "ubbb", "uccc"}}
	c := &clock{now: time.Unix(100, 0)}
	s := holders.New(holders.Deps{Bank: bank, Store: store, Clock: c}, 0)

	require.Error(t, s.FullResync(context.Background()))
	var cur struct {
		Denom string    `json:"denom"`
		Key   []byte    `json:"key"`
		Stamp time.Time `json:"stamp"`
	}
	require.NoError(t, json.Unmarshal(s.Cursor(), &cur))
	assert.Equal(t, "ubbb", cur.Denom)
	assert.Equal(t, "ubbb#4", string(cur.Key), "the page that failed")
	assert.Equal(t, []string{"uaaa"}, store.finished)

	// A new process reads the cursor sync_jobs recorded and goes on from
	// that page with the same stamp: the rows of the pages already written
	// stay.
	store.cursor = s.Cursor()
	bank.calls = nil
	restarted := holders.New(holders.Deps{Bank: bank, Store: store, Clock: c}, 0)
	require.NoError(t, restarted.FullResync(context.Background()))
	assert.Equal(t, []string{"ubbb@ubbb#4", "uccc@"}, bank.calls)
	assert.Len(t, store.balances("ubbb"), 5)
	assert.Equal(t, []string{"uaaa", "ubbb", "uccc"}, store.finished)
	assert.JSONEq(t, `{}`, string(restarted.Cursor()))
}

func TestAnUnreadableCursorStartsOver(t *testing.T) {
	bank := &fakeBank{owners: map[string][]string{"uaaa": {"1"}}}
	store := &fakeStore{denoms: []string{"uaaa"}, cursor: json.RawMessage(`"garbage"`)}
	s := holders.New(holders.Deps{Bank: bank, Store: store, Clock: &clock{}}, time.Minute)
	assert.Equal(t, time.Minute, s.Interval())
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"uaaa@"}, bank.calls)
}

func TestResyncOneKeepsAnUnfinishedRunsPosition(t *testing.T) {
	bank := &fakeBank{owners: map[string][]string{"uaaa": {"1"}, "ubbb": {"1", "2", "3"}}, failAt: "ubbb#2"}
	store := &fakeStore{denoms: []string{"uaaa", "ubbb"}}
	s := holders.New(holders.Deps{Bank: bank, Store: store, Clock: &clock{}}, 0)
	require.Error(t, s.FullResync(context.Background()))
	before := s.Cursor()

	require.NoError(t, s.ResyncOne(context.Background(), "uaaa"))
	assert.JSONEq(t, string(before), string(s.Cursor()))
}

func TestAStoppedRunReturnsDuringThePause(t *testing.T) {
	bank := &fakeBank{owners: map[string][]string{"uaaa": {"1", "2", "3"}}}
	store := &fakeStore{denoms: []string{"uaaa"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := holders.New(holders.Deps{Bank: bank, Store: store}, 0) // the real clock
	require.ErrorIs(t, s.FullResync(ctx), context.Canceled)
	assert.Empty(t, store.finished)
}
