package server_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/supply"
)

// The recorded mainnet state of the fake gRPC fixtures: ubze's supply and
// the community pool's ubze.
const (
	recordedUbzeSupply = "281824500438386"
	recordedUbzePool   = "34939146569152.357342022929412238"
	honey              = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
)

// supplyNode answers SupplyOf from supplies (zero for the others) and
// CommunityPool with pool, until told to fail, and records the requests.
type supplyNode struct {
	mu        sync.Mutex
	supplies  map[string]string
	pool      sdk.DecCoins
	fail      bool
	asked     []string
	poolCalls int
}

func (n *supplyNode) SupplyOf(_ context.Context, in *banktypes.QuerySupplyOfRequest, _ ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.asked = append(n.asked, in.Denom)
	if n.fail {
		return nil, errors.New("connection refused")
	}
	amount := sdkmath.ZeroInt()
	if s, ok := n.supplies[in.Denom]; ok {
		amount, _ = sdkmath.NewIntFromString(s)
	}
	return &banktypes.QuerySupplyOfResponse{Amount: sdk.Coin{Denom: in.Denom, Amount: amount}}, nil
}

func (n *supplyNode) CommunityPool(context.Context, *distrtypes.QueryCommunityPoolRequest, ...grpc.CallOption) (*distrtypes.QueryCommunityPoolResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.poolCalls++
	if n.fail {
		return nil, errors.New("connection refused")
	}
	return &distrtypes.QueryCommunityPoolResponse{Pool: n.pool}, nil
}

// supplyDenoms is explorer.denoms' exponents.
type supplyDenoms struct {
	exponents map[string]int
	fail      bool
}

func (d *supplyDenoms) Exponent(_ context.Context, denom string) (int, bool, error) {
	if d.fail {
		return 0, false, errors.New("database is down")
	}
	e, ok := d.exponents[denom]
	return e, ok, nil
}

func ubzePool(t *testing.T, amount string) sdk.DecCoins {
	t.Helper()
	dec, err := sdkmath.LegacyNewDecFromStr(amount)
	require.NoError(t, err)
	return sdk.DecCoins{sdk.NewDecCoinFromDec("ubze", dec)}
}

func recordedNode(t *testing.T) *supplyNode {
	return &supplyNode{
		supplies: map[string]string{"ubze": recordedUbzeSupply, honey: "1500000000000"},
		pool:     ubzePool(t, recordedUbzePool),
	}
}

func syncedDenoms() *supplyDenoms {
	return &supplyDenoms{exponents: map[string]int{"ubze": 6, honey: 6}}
}

func supplyServer(node *supplyNode, denoms *supplyDenoms, clock *compatClock) *echo.Echo {
	lg, _ := logtest.NewNullLogger()
	return server.New(server.Deps{CompatSupply: supply.New(supply.Deps{
		Bank: node, Distribution: node, Denoms: denoms, Clock: clock, Log: lg,
	})})
}

// plain asserts a plain-text answer, as the aggregator's ctx.String sends it.
func plain(t *testing.T, e *echo.Echo, path string, code int, body string) {
	t.Helper()
	rec := serve(t, e, http.MethodGet, path)
	assert.Equal(t, code, rec.Code, path)
	assert.Equal(t, body, rec.Body.String(), path)
	assert.Equal(t, echo.MIMETextPlainCharsetUTF8, rec.Header().Get(echo.HeaderContentType), path)
	assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl), "the aggregator sends none")
}

func TestCompatSupplyOfBZE(t *testing.T) {
	node := recordedNode(t)
	e := supplyServer(node, syncedDenoms(), &compatClock{now: time.Unix(1000, 0)})

	// ubze is the default denom.
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824500.44")
	plain(t, e, "/aggregator/api/supply/total?denom=ubze", http.StatusOK, "281824500.44")
	// Total minus the community pool only, exactly.
	plain(t, e, "/aggregator/api/supply/circulating", http.StatusOK, "246885353.87")
	plain(t, e, "/aggregator/api/supply/circulating?denom=ubze", http.StatusOK, "246885353.87")
	assert.Equal(t, []string{"ubze", "ubze"}, node.asked, "one read per route, then the cache")
	assert.Equal(t, 1, node.poolCalls)

	rec := corsRequest(e, http.MethodGet, "/aggregator/api/supply/total", "https://getbze.com")
	assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}

func TestCompatCirculatingSupplyOfAnotherDenomIsItsTotal(t *testing.T) {
	node := recordedNode(t)
	e := supplyServer(node, syncedDenoms(), &compatClock{now: time.Unix(1000, 0)})

	plain(t, e, "/aggregator/api/supply/total?denom="+honey, http.StatusOK, "1500000.00")
	plain(t, e, "/aggregator/api/supply/circulating?denom="+honey, http.StatusOK, "1500000.00")
	assert.Zero(t, node.poolCalls, "the community pool counts only for BZE")
}

func TestCompatSupplyRoundsHalfUp(t *testing.T) {
	const denom = "factory/bze1x/uround"
	for supplied, want := range map[string]string{
		"12345":    "12.35", // half to even would give 12.34
		"12355":    "12.36",
		"12344999": "12345.00", // the carry reaches the whole part
		"12344":    "12.34",
		"0":        "0.00",
	} {
		node := &supplyNode{supplies: map[string]string{denom: supplied}}
		e := supplyServer(node, &supplyDenoms{exponents: map[string]int{denom: 3}}, &compatClock{now: time.Unix(1000, 0)})
		plain(t, e, "/aggregator/api/supply/total?denom="+denom, http.StatusOK, want)
	}
}

// Allowed difference: the aggregator subtracts the pool from the rounded
// total in floats; the explorer rounds the exact difference once, which can
// differ by 0.01.
func TestCompatCirculatingSupplyIsExact(t *testing.T) {
	node := &supplyNode{supplies: map[string]string{"ubze": "1000005000"}, pool: ubzePool(t, "1000")}
	e := supplyServer(node, syncedDenoms(), &compatClock{now: time.Unix(1000, 0)})

	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "1000.01")
	plain(t, e, "/aggregator/api/supply/circulating", http.StatusOK, "1000.00") // the aggregator: 1000.01 - 0.001 = "1000.01"
}

func TestCompatSupplyExponentBounds(t *testing.T) {
	node := &supplyNode{supplies: map[string]string{
		"uzero": "7", "uatto": "1500000000000000000", "uover": "1", "uneg": "1",
	}}
	e := supplyServer(node, &supplyDenoms{exponents: map[string]int{
		"uzero": 0, "uatto": 18, "uover": 19, "uneg": -1,
	}}, &compatClock{now: time.Unix(1000, 0)})

	plain(t, e, "/aggregator/api/supply/total?denom=uzero", http.StatusOK, "7.00")
	plain(t, e, "/aggregator/api/supply/total?denom=uatto", http.StatusOK, "1.50")
	plain(t, e, "/aggregator/api/supply/total?denom=uover", http.StatusBadRequest, "uover has an unsupported display exponent")
	plain(t, e, "/aggregator/api/supply/circulating?denom=uneg", http.StatusBadRequest, "uneg has an unsupported display exponent")
	assert.Equal(t, []string{"uzero", "uatto"}, node.asked, "a bad exponent asks no node")
}

func TestCompatSupplyIsCachedThenBackedUp(t *testing.T) {
	node := recordedNode(t)
	start := time.Unix(1000, 0)
	clock := &compatClock{now: start}
	e := supplyServer(node, syncedDenoms(), clock)
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824500.44")
	plain(t, e, "/aggregator/api/supply/circulating", http.StatusOK, "246885353.87")

	// Within 600 s the node is not asked again.
	node.supplies["ubze"] = "281824600000000"
	clock.now = start.Add(supply.FreshFor - time.Second)
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824500.44")
	assert.Len(t, node.asked, 2)

	// After 600 s: a new read.
	clock.now = start.Add(supply.FreshFor)
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824600.00")
	readAt := clock.now

	// Allowed difference: where the aggregator answers "0" on a node
	// failure, the last read is served for 24 hours, the node asked on
	// every request meanwhile…
	node.fail = true
	clock.now = readAt.Add(supply.FreshFor)
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824600.00")
	plain(t, e, "/aggregator/api/supply/circulating", http.StatusOK, "246885353.87")
	clock.now = readAt.Add(supply.BackupFor - time.Second)
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824600.00")
	asked := len(node.asked)
	assert.Equal(t, 6, asked)

	// …then a 503.
	clock.now = readAt.Add(supply.BackupFor)
	plain(t, e, "/aggregator/api/supply/total", http.StatusServiceUnavailable, "supply is unavailable, try again later")
	plain(t, e, "/aggregator/api/supply/circulating", http.StatusServiceUnavailable, "supply is unavailable, try again later")

	// The node answers again.
	node.fail = false
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824600.00")
}

func TestCompatSupplyWithNothingReadIsUnavailable(t *testing.T) {
	node := recordedNode(t)
	node.fail = true
	e := supplyServer(node, syncedDenoms(), &compatClock{now: time.Unix(1000, 0)})
	plain(t, e, "/aggregator/api/supply/total", http.StatusServiceUnavailable, "supply is unavailable, try again later")
	plain(t, e, "/aggregator/api/supply/circulating?denom="+honey, http.StatusServiceUnavailable, "supply is unavailable, try again later")

	// The database failing is no different.
	node.fail = false
	e = supplyServer(node, &supplyDenoms{fail: true}, &compatClock{now: time.Unix(1000, 0)})
	plain(t, e, "/aggregator/api/supply/total", http.StatusServiceUnavailable, "supply is unavailable, try again later")
}

// Allowed difference: the aggregator answers an unknown denom with a 500.
func TestCompatSupplyOfAnUnknownDenomIsABadRequest(t *testing.T) {
	node := recordedNode(t)
	e := supplyServer(node, syncedDenoms(), &compatClock{now: time.Unix(1000, 0)})

	plain(t, e, "/aggregator/api/supply/total?denom=unope", http.StatusBadRequest, "denom unope not found in registry")
	plain(t, e, "/aggregator/api/supply/circulating?denom=unope", http.StatusBadRequest, "denom unope not found in registry")
	// Not a denom at all: the node is not asked.
	plain(t, e, "/aggregator/api/supply/total?denom=%21%21", http.StatusBadRequest, "denom !! not found in registry")
	assert.Equal(t, []string{"unope", "unope"}, node.asked)

	// A denom the chain has minted but the explorer has not synced yet is
	// served with exponent 0; ubze always has exponent 6.
	node.supplies["unew"] = "42"
	plain(t, e, "/aggregator/api/supply/total?denom=unew", http.StatusOK, "42.00")
	e = supplyServer(node, &supplyDenoms{}, &compatClock{now: time.Unix(1000, 0)})
	plain(t, e, "/aggregator/api/supply/total", http.StatusOK, "281824500.44")
}

// Allowed difference: the aggregator reads one page of 5000 supplies and
// misses the denoms beyond it; the explorer asks the node for the one denom.
func TestCompatSupplyAsksForTheOneDenom(t *testing.T) {
	const late = "factory/bze1zzz/ulast"
	node := &supplyNode{supplies: map[string]string{late: "5000000"}}
	e := supplyServer(node, &supplyDenoms{exponents: map[string]int{late: 6}}, &compatClock{now: time.Unix(1000, 0)})

	plain(t, e, "/aggregator/api/supply/total?denom="+late, http.StatusOK, "5.00")
	assert.Equal(t, []string{late}, node.asked)
}

func TestCompatSupplyNeedsAReader(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, serve(t, server.New(server.Deps{}), http.MethodGet, "/aggregator/api/supply/total").Code)
}
