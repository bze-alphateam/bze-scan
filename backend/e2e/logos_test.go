//go:build e2e

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainToolsIdentity is ChainTools' recorded description identity; the fake
// Keybase gives it a picture and no other identity one.
const (
	chainToolsIdentity = "1857FF1BC2EA6A69"
	chainToolsLogo     = "https://s3.amazonaws.com/keybase_processed_uploads/chaintools_360_360.jpg"
	recordedIdentities = 48 // recorded validators with an identity
	recordedKeys       = 45 // of them Keybase key suffixes; "0", "00" and "URI_VALIDATOR" are never looked up
)

// fakeKeybase stands in for the Keybase user lookup, counting the lookups.
type fakeKeybase struct {
	URL string

	mu      sync.Mutex
	lookups int
	down    bool
}

func newFakeKeybase(t *testing.T) *fakeKeybase {
	t.Helper()
	k := &fakeKeybase{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		k.lookups++
		down := k.down
		k.mu.Unlock()
		switch {
		case r.URL.Path != "/_/api/1.0/user/lookup.json" || r.URL.Query().Get("fields") != "pictures":
			http.NotFound(w, r)
		case down:
			http.Error(w, "bad gateway", http.StatusBadGateway)
		case r.URL.Query().Get("key_suffix") == chainToolsIdentity:
			_, _ = w.Write([]byte(`{"status":{"code":0,"name":"OK"},"them":[{"id":"x","pictures":{"primary":{"url":"` + chainToolsLogo + `"}}}]}`))
		default:
			_, _ = w.Write([]byte(`{"status":{"code":0,"name":"OK"},"them":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	k.URL = srv.URL + "/_/api/1.0/user/lookup.json"
	return k
}

func (k *fakeKeybase) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lookups
}

func (k *fakeKeybase) setDown(down bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.down = down
}

func TestValidatorLogosComeFromKeybaseOncePerDay(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, recordedKeys, e.kb.count(), "one lookup per key suffix")
	logo := `SELECT concat_ws(' ', coalesce(logo_url, '-'), logo_checked_at IS NOT NULL) FROM explorer.validators WHERE operator_address = $1`
	assert.Equal(t, []string{chainToolsLogo + " true"}, queryStrings(t, e.db, logo, chainTools))
	assert.Equal(t, recordedIdentities, e.count(t, `SELECT count(*) FROM explorer.validators WHERE logo_checked_at IS NOT NULL`))
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.validators WHERE logo_url IS NOT NULL`))

	// Checked less than a day ago: the next run asks nothing and the
	// validators upsert keeps the logo.
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, recordedKeys, e.kb.count())
	assert.Equal(t, []string{chainToolsLogo + " true"}, queryStrings(t, e.db, logo, chainTools))

	// A day later Keybase is down: the logo and its check time stay.
	_, err := e.db.Exec(`UPDATE explorer.validators SET logo_checked_at = now() - interval '25 hours' WHERE operator_address = $1`, chainTools)
	require.NoError(t, err)
	e.kb.setDown(true)
	code, _ = e.syncState(t)
	require.Equal(t, 0, code, "a failed lookup never fails the sync")
	assert.Equal(t, recordedKeys+1, e.kb.count())
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.validators
		WHERE operator_address = $1 AND logo_url = $2 AND logo_checked_at < now() - interval '24 hours'`, chainTools, chainToolsLogo))
}

func TestTheValidatorsAPICarriesTheLogo(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, h316)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.validators WHERE logo_url IS NOT NULL`) == 1
	}, "the logo")
	page := getValidators(t, base, "?limit=100")
	for _, v := range page.Items {
		if v.OperatorAddress == chainTools {
			require.NotNil(t, v.LogoURL)
			assert.Equal(t, chainToolsLogo, *v.LogoURL)
		} else {
			assert.Nil(t, v.LogoURL, v.Moniker)
		}
	}
}
