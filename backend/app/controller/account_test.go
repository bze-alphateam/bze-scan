package controller_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
)

// fakeAccounts is an in-memory AccountReader.
type fakeAccounts struct {
	accounts    map[string]*repository.Account
	denoms      map[string]repository.Denom
	monikers    map[string]string
	err         error
	denomsErr   error
	gotDenoms   []string
	gotMonikers []string
}

func (f *fakeAccounts) Account(_ context.Context, address string) (*repository.Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	if a, ok := f.accounts[address]; ok {
		return a, nil
	}
	return &repository.Account{Address: address}, nil
}

func (f *fakeAccounts) Denoms(_ context.Context, denoms []string) (map[string]repository.Denom, error) {
	f.gotDenoms = denoms
	out := map[string]repository.Denom{}
	for _, d := range denoms {
		if v, ok := f.denoms[d]; ok {
			out[d] = v
		}
	}
	return out, f.denomsErr
}

func (f *fakeAccounts) Monikers(_ context.Context, operators []string) (map[string]string, error) {
	f.gotMonikers = operators
	out := map[string]string{}
	for _, op := range operators {
		if m, ok := f.monikers[op]; ok {
			out[op] = m
		}
	}
	return out, nil
}

// fakeState answers every address with one account, or fails.
type fakeState struct {
	acc  *chainstate.Account
	err  error
	asks []string
}

func (f *fakeState) Account(_ context.Context, address string) (*chainstate.Account, error) {
	f.asks = append(f.asks, address)
	return f.acc, f.err
}

const (
	unbondingVal = "bzevaloper1unknown"
	uvdl         = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
)

var completion = time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)

func liveAccount() *chainstate.Account {
	return &chainstate.Account{
		Balances: []chainstate.Coin{{Denom: uvdl, Amount: "25065076620"}, {Denom: "ubze", Amount: "30063263"}},
		Delegations: []chainstate.Delegation{
			{Validator: valoper, Amount: chainstate.Coin{Denom: "ubze", Amount: "123881169576"}},
			{Validator: unbondingVal, Amount: chainstate.Coin{Denom: "ubze", Amount: "4"}},
		},
		Unbonding: []chainstate.Unbonding{
			{Validator: unbondingVal, Amount: chainstate.Coin{Denom: "ubze", Amount: "7"}, CompletionTime: completion},
		},
		Rewards: chainstate.Rewards{
			ByValidator: []chainstate.Reward{{Validator: valoper, Coins: []chainstate.Coin{{Denom: "ubze", Amount: "19010032"}}}},
			Total:       []chainstate.Coin{{Denom: "ubze", Amount: "19010032"}},
		},
	}
}

func seenAccount() *repository.Account {
	last := int64(25000894)
	return &repository.Account{
		Address:        account,
		FirstSeen:      &repository.Seen{Height: 24999209, Time: time.Date(2026, 10, 8, 18, 0, 0, 0, time.FixedZone("x", 3600))},
		LastSeenHeight: &last, TxCount: 2,
		Label: &repository.Label{Address: account, Name: "Vidulum", Kind: "validator_owner"},
	}
}

func newAccountAPI(r controller.AccountReader, s controller.AccountState) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/accounts/:address", controller.NewAccountController(r, s).Account)
	return e
}

func TestAccountMergesTheIndexAndTheLiveState(t *testing.T) {
	bze := "BZE"
	repo := &fakeAccounts{
		accounts: map[string]*repository.Account{account: seenAccount()},
		denoms:   map[string]repository.Denom{"ubze": {Symbol: &bze, Exponent: 6}},
		monikers: map[string]string{valoper: "Vidulum"},
	}
	state := &fakeState{acc: liveAccount()}
	rec := get(t, newAccountAPI(repo, state), "/accounts/"+strings.ToUpper(account))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, []string{account}, state.asks, "the canonical lower-case address")
	assert.Equal(t, []string{uvdl, "ubze"}, repo.gotDenoms)
	assert.Equal(t, []string{valoper, unbondingVal, unbondingVal}, repo.gotMonikers)

	assert.JSONEq(t, `{
		"address": "`+account+`",
		"label": {"name": "Vidulum", "kind": "validator_owner"},
		"first_seen": {"height": 24999209, "time": "2026-10-08T17:00:00Z"},
		"last_seen_height": 25000894,
		"tx_count": 2,
		"activity_count": 0,
		"balances": [
			{"denom": "`+uvdl+`", "amount": "25065076620", "symbol": null, "exponent": null},
			{"denom": "ubze", "amount": "30063263", "symbol": "BZE", "exponent": 6}
		],
		"delegations": [
			{"validator": "`+valoper+`", "moniker": "Vidulum", "amount": "123881169576"},
			{"validator": "`+unbondingVal+`", "moniker": null, "amount": "4"}
		],
		"unbonding": [
			{"validator": "`+unbondingVal+`", "moniker": null, "amount": "7", "completion_time": "2026-10-30T08:00:00Z"}
		],
		"rewards": [{"validator": "`+valoper+`", "coins": [{"denom": "ubze", "amount": "19010032"}]}],
		"total_staked": "123881169580",
		"total_rewards": [{"denom": "ubze", "amount": "19010032"}],
		"live": {"available": true}
	}`, rec.Body.String())
}

func TestAnAccountTheExplorerNeverSawStillReadsLive(t *testing.T) {
	other := zeroAddress("bze")
	rec := get(t, newAccountAPI(&fakeAccounts{}, &fakeState{acc: &chainstate.Account{
		Balances: []chainstate.Coin{{Denom: "ubze", Amount: "1"}},
	}}), "/accounts/"+other)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"address": "`+other+`", "label": null, "first_seen": null, "last_seen_height": null,
		"tx_count": 0, "activity_count": 0,
		"balances": [{"denom": "ubze", "amount": "1", "symbol": null, "exponent": null}],
		"delegations": [], "unbonding": [], "rewards": [], "total_staked": "0", "total_rewards": [],
		"live": {"available": true}
	}`, rec.Body.String())
}

func TestALiveFailureStillAnswersTheIndexedPart(t *testing.T) {
	repo := &fakeAccounts{accounts: map[string]*repository.Account{account: seenAccount()}}
	for name, state := range map[string]controller.AccountState{
		"node down": &fakeState{err: errors.New("connection refused")},
		"no node":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, newAccountAPI(repo, state), "/accounts/"+account)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, `{
				"address": "`+account+`",
				"label": {"name": "Vidulum", "kind": "validator_owner"},
				"first_seen": {"height": 24999209, "time": "2026-10-08T17:00:00Z"},
				"last_seen_height": 25000894, "tx_count": 2, "activity_count": 0,
				"balances": [], "delegations": [], "unbonding": [], "rewards": [],
				"total_staked": null, "total_rewards": [],
				"live": {"available": false}
			}`, rec.Body.String())
		})
	}
}

func TestAccountRejectsAnythingButABze1Address(t *testing.T) {
	e := newAccountAPI(&fakeAccounts{}, &fakeState{acc: &chainstate.Account{}})
	for _, a := range []string{"nope", valoper, bech32Of("cosmos", account), account[:len(account)-1] + "x", "25000894"} {
		rec := get(t, e, "/accounts/"+a)
		assert.Equal(t, http.StatusBadRequest, rec.Code, a)
		assert.Equal(t, "bad_request", errorCode(t, rec), a)
	}
}

func TestAccountDatabaseErrorsAre500(t *testing.T) {
	rec := get(t, newAccountAPI(&fakeAccounts{err: errors.New("db down")}, nil), "/accounts/"+account)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	rec = get(t, newAccountAPI(&fakeAccounts{denomsErr: errors.New("db down")}, &fakeState{acc: liveAccount()}),
		"/accounts/"+account)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
