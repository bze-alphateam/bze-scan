package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
)

func serve(t *testing.T, e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHealthRoute(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
	assert.NotEmpty(t, rec.Header().Get(appmw.RequestIDHeader))
}

type pendingStatus struct{}

func (pendingStatus) Snapshot() status.Snapshot {
	return status.Snapshot{BackFill: status.BackFillFinished}
}

func TestStatusRoute(t *testing.T) {
	rec := serve(t, server.New(server.Deps{Status: pendingStatus{}}), http.MethodGet, "/api/v1/status")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"live_fill":{"healthy":false,"checked_at":null,"db_height":null,"node_height":null,"archive_height":null},
		"back_fill":{"status":"finished","oldest_height":null}}`, rec.Body.String())
}

// echoRaw answers every route with its name and height.
type echoRaw struct{}

func (echoRaw) Get(_ context.Context, route rawcache.Route, height int64) ([]byte, error) {
	b, _ := json.Marshal(map[string]any{"route": route, "height": height})
	return b, nil
}

func (echoRaw) Tx(context.Context, int64, int) (*rawcache.Tx, error) {
	return nil, errors.New("unused")
}

func TestRawRoutes(t *testing.T) {
	e := server.New(server.Deps{Raw: echoRaw{}})
	for _, route := range []string{"block", "block_results", "commit"} {
		rec := serve(t, e, http.MethodGet, "/api/v1/raw/"+route+"/42")
		assert.Equal(t, http.StatusOK, rec.Code, route)
		assert.JSONEq(t, `{"route":"`+route+`","height":42}`, rec.Body.String())
	}

	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/api/v1/raw/block/42")
	assert.Equal(t, http.StatusNotFound, rec.Code, "no raw reader, no routes")
}

// noAccounts knows no account and no denom.
type noAccounts struct{}

func (noAccounts) Account(_ context.Context, address string) (*repository.Account, error) {
	return &repository.Account{Address: address}, nil
}

func (noAccounts) Denoms(context.Context, []string) (map[string]repository.Denom, error) {
	return nil, nil
}

func (noAccounts) Monikers(context.Context, []string) (map[string]string, error) {
	return nil, nil
}

func TestAccountRoute(t *testing.T) {
	const addr = "bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls"
	rec := serve(t, server.New(server.Deps{Accounts: noAccounts{}}), http.MethodGet, "/api/v1/accounts/"+addr)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"live":{"available":false}`, "no account state, no live part")

	rec = serve(t, server.New(server.Deps{}), http.MethodGet, "/api/v1/accounts/"+addr)
	assert.Equal(t, http.StatusNotFound, rec.Code, "no account reader, no route")
}

func TestUnknownPathAnswersNotFoundEnvelope(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/nope")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
	assert.JSONEq(t, `{"error":{"code":"not_found","message":"Not Found"}}`, rec.Body.String())
}

func TestWrongMethodAnswersEnvelope(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodPost, "/health")

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	var body appmw.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "method_not_allowed", body.Error.Code)
}

func TestRunServesAndShutsDownCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.New(server.Deps{}), "127.0.0.1:0", func(a net.Addr) { addrCh <- a })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start listening")
	}

	resp, err := http.Get("http://" + addr.String() + "/health")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(server.ShutdownTimeout + 5*time.Second):
		t.Fatal("server did not shut down")
	}

	resp, err = http.Get("http://" + addr.String() + "/health")
	if err == nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err, "listener must be closed after shutdown")
}

func TestRunDrainsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := server.New(server.Deps{})
	entered := make(chan struct{})
	release := make(chan struct{})
	e.GET("/slow", func(c *echo.Context) error {
		close(entered)
		<-release
		return c.String(http.StatusOK, "done")
	})

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, e, "127.0.0.1:0", func(a net.Addr) { addrCh <- a }) }()
	addr := <-addrCh

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr.String() + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), err: err}
	}()

	<-entered
	cancel() // shutdown starts while /slow is in flight
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Run returned before the in-flight request finished: %v", err)
	default:
	}
	close(release)

	res := <-resCh
	require.NoError(t, res.err)
	assert.Equal(t, "done", res.body)
	assert.NoError(t, <-done)
}

func TestRunFailsWhenAddressIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	err = server.Run(context.Background(), server.New(server.Deps{}), ln.Addr().String(), nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}

func TestAPIRoutesLiveUnderTheVersionPrefix(t *testing.T) {
	e := server.New(server.Deps{})
	for _, path := range []string{"/blocks", "/txs", "/validators", "/search?q=1"} {
		assert.Equal(t, http.StatusNotFound, serve(t, e, http.MethodGet, path).Code, path)
	}
	// Validation answers before any repository call, so a nil repository is
	// enough to prove the routes exist.
	for _, path := range []string{"/blocks/x", "/txs/x", "/blocks?limit=0", "/txs?limit=0", "/validators?status=x", "/search"} {
		rec := serve(t, e, http.MethodGet, server.APIPrefix+path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestCORSHeadersOnlyWhenConfigured(t *testing.T) {
	request := func(e *echo.Echo, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, server.APIPrefix+"/blocks/x", nil)
		req.Header.Set(echo.HeaderOrigin, "https://scan.getbze.com")
		if method == http.MethodOptions {
			req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodGet)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	rec := request(server.New(server.Deps{}), http.MethodGet)
	assert.Empty(t, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))

	e := server.New(server.Deps{CORSAllowedOrigins: []string{"https://scan.getbze.com"}})
	rec = request(e, http.MethodGet)
	assert.Equal(t, "https://scan.getbze.com", rec.Header().Get(echo.HeaderAccessControlAllowOrigin), "errors carry CORS headers too")
	rec = request(e, http.MethodOptions)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderAccessControlAllowMethods), http.MethodGet)

	e = server.New(server.Deps{CORSAllowedOrigins: []string{"*"}})
	assert.Equal(t, "*", request(e, http.MethodGet).Header().Get(echo.HeaderAccessControlAllowOrigin))
}

// noTokens knows the native denom only.
type noTokens struct{}

func (noTokens) Tokens(context.Context, *string, *repository.TokenKey, int) ([]repository.TokenSummary, error) {
	return nil, nil
}

func (noTokens) Token(_ context.Context, denom string) (*repository.Token, error) {
	if denom != "factory/bze1x/uhoney" {
		return nil, repository.ErrNotFound
	}
	return &repository.Token{TokenSummary: repository.TokenSummary{Denom: denom, Kind: "factory"}}, nil
}

func (noTokens) TokenEvents(context.Context, string, *repository.EventKey, int) ([]repository.TokenEvent, error) {
	return nil, nil
}

func (noTokens) DenomTransfers(context.Context, string, *repository.EventKey, int) ([]repository.DenomTransfer, error) {
	return nil, nil
}

// A URL-encoded denom stays one path segment, on the detail route and on the
// routes below it.
func TestTokenRoutes(t *testing.T) {
	e := server.New(server.Deps{Tokens: noTokens{}})
	for _, path := range []string{
		"/api/v1/tokens",
		"/api/v1/tokens/factory%2Fbze1x%2Fuhoney",
		"/api/v1/tokens/factory%2Fbze1x%2Fuhoney/events",
		"/api/v1/tokens/factory%2Fbze1x%2Fuhoney/transfers",
		"/api/v1/token?denom=factory/bze1x/uhoney",
		"/api/v1/token/events?denom=factory%2Fbze1x%2Fuhoney",
	} {
		assert.Equal(t, http.StatusOK, serve(t, e, http.MethodGet, path).Code, path)
	}
	assert.Equal(t, http.StatusNotFound, serve(t, e, http.MethodGet, "/api/v1/tokens/factory/bze1x/uhoney").Code,
		"an unencoded denom is three segments")
	assert.Equal(t, http.StatusNotFound, serve(t, server.New(server.Deps{}), http.MethodGet, "/api/v1/tokens").Code,
		"no token reader, no route")
}

// noProposals knows proposal 47 only.
type noProposals struct{}

func (noProposals) Proposals(context.Context, *string, *int64, int) ([]repository.ProposalSummary, error) {
	return nil, nil
}

func (noProposals) Proposal(_ context.Context, id int64) (*repository.Proposal, error) {
	if id != 47 {
		return nil, repository.ErrNotFound
	}
	return &repository.Proposal{ProposalSummary: repository.ProposalSummary{ID: id}}, nil
}

func (noProposals) ProposalVotes(context.Context, int64, *string, *repository.VoteKey, int) ([]repository.ProposalVote, error) {
	return nil, nil
}

func (noProposals) ProposalDeposits(context.Context, int64, *repository.DepositKey, int) ([]repository.ProposalDeposit, error) {
	return nil, nil
}

func TestProposalRoutes(t *testing.T) {
	e := server.New(server.Deps{Proposals: noProposals{}})
	for _, path := range []string{"/api/v1/proposals", "/api/v1/proposals/47", "/api/v1/proposals/47/votes", "/api/v1/proposals/47/deposits"} {
		assert.Equal(t, http.StatusOK, serve(t, e, http.MethodGet, path).Code, path)
	}
	assert.Equal(t, http.StatusNotFound, serve(t, e, http.MethodGet, "/api/v1/proposals/48/votes").Code)
	assert.Equal(t, http.StatusNotFound, serve(t, server.New(server.Deps{}), http.MethodGet, "/api/v1/proposals").Code,
		"no proposal reader, no route")
}
