package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
)

const registryVDL = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"

// assetTables is a source of two denoms whose vdl name is settable.
type assetTables struct {
	vdlName string
	err     error
}

func (s *assetTables) Load(context.Context) (*assets.Tables, error) {
	if s.err != nil {
		return nil, s.err
	}
	md, _ := json.Marshal(map[string]string{"name": s.vdlName})
	return &assets.Tables{Denoms: []assets.Denom{
		{Denom: "ubze", Kind: "native"},
		{Denom: registryVDL, Kind: "factory", Metadata: md},
	}}, nil
}

func assetServer(t *testing.T, src assets.Source, cors ...string) (*assetHTTP, *assets.List, *logtest.Hook) {
	t.Helper()
	lg, hook := logtest.NewNullLogger()
	lg.SetLevel(log.DebugLevel)
	list := assets.New(assets.Config{}, assets.Deps{Source: src, ChainID: "beezee-1", Log: lg})
	return &assetHTTP{server.New(server.Deps{AssetList: list, CORSAllowedOrigins: cors, Log: lg})}, list, hook
}

// assetHTTP wraps the server with request helpers.
type assetHTTP struct{ http.Handler }

func (e *assetHTTP) get(path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestAssetListIsUnavailableBeforeTheFirstBuild(t *testing.T) {
	e, _, _ := assetServer(t, &assetTables{err: errors.New("db not up")})
	for _, path := range []string{"/api/v1/registry/assets", "/api/v1/registry/assets/ubze"} {
		rec := e.get(path)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
		assert.JSONEq(t, `{"error":{"code":"upstream_error","message":"the asset list is not built yet"}}`, rec.Body.String())
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		assert.Empty(t, rec.Header().Get("ETag"))
	}
}

func TestAssetListETagRoundTrip(t *testing.T) {
	src := &assetTables{vdlName: "Vidulum"}
	e, list, _ := assetServer(t, src)
	require.NoError(t, list.Rebuild(context.Background()))

	rec := e.get("/api/v1/registry/assets")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "public, max-age=30", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body struct {
		Version string         `json:"version"`
		Assets  []assets.Asset `json:"assets"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Assets, 2)
	etag := rec.Header().Get("ETag")
	assert.Equal(t, `"`+body.Version+`"`, etag)

	for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		again := e.get("/api/v1/registry/assets", "If-None-Match", inm)
		assert.Equal(t, http.StatusNotModified, again.Code, inm)
		assert.Empty(t, again.Body.Bytes())
		assert.Equal(t, etag, again.Header().Get("ETag"))
		assert.Equal(t, "public, max-age=30", again.Header().Get("Cache-Control"))
	}

	// An identical rebuild keeps the version: still 304.
	require.NoError(t, list.Rebuild(context.Background()))
	assert.Equal(t, http.StatusNotModified, e.get("/api/v1/registry/assets", "If-None-Match", etag).Code)

	// A changed field answers the new list.
	src.vdlName = "Vidulum Renamed"
	require.NoError(t, list.Rebuild(context.Background()))
	changed := e.get("/api/v1/registry/assets", "If-None-Match", etag)
	assert.Equal(t, http.StatusOK, changed.Code)
	assert.NotEqual(t, etag, changed.Header().Get("ETag"))
	assert.Contains(t, changed.Body.String(), "Vidulum Renamed")
}

func TestOneAssetByEncodedOrPlainDenom(t *testing.T) {
	e, list, _ := assetServer(t, &assetTables{vdlName: "Vidulum"})
	require.NoError(t, list.Rebuild(context.Background()))
	version, _, ok := list.Current().Asset(registryVDL)
	require.True(t, ok)

	for _, path := range []string{"/api/v1/registry/assets/" + url.PathEscape(registryVDL), "/api/v1/registry/assets/" + registryVDL} {
		rec := e.get(path)
		require.Equal(t, http.StatusOK, rec.Code, path)
		var a assets.Asset
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &a))
		assert.Equal(t, registryVDL, a.Denom)
		assert.Equal(t, "Vidulum", a.Name)
		assert.Equal(t, `"`+version+`"`, rec.Header().Get("ETag"), "the asset's own version")
		assert.Equal(t, "public, max-age=30", rec.Header().Get("Cache-Control"))
		assert.Equal(t, http.StatusNotModified, e.get(path, "If-None-Match", `"`+version+`"`).Code)
	}
}

func TestAnUnknownDenomIsLoggedAndNotFound(t *testing.T) {
	e, list, hook := assetServer(t, &assetTables{vdlName: "Vidulum"})
	require.NoError(t, list.Rebuild(context.Background()))
	hook.Reset()

	rec := e.get("/api/v1/registry/assets/"+url.PathEscape("factory/bze1nobody/unone"), appmw.RequestIDHeader, "req-7")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var body appmw.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not_found", body.Error.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var logged *log.Entry
	for _, entry := range hook.AllEntries() {
		if entry.Message == "asset list: unknown denom" {
			logged = entry
		}
	}
	require.NotNil(t, logged)
	assert.Equal(t, log.InfoLevel, logged.Level)
	assert.Equal(t, "factory/bze1nobody/unone", logged.Data["denom"])
	assert.Equal(t, "req-7", logged.Data["request_id"])
}

// The asset list answers every origin, whatever CORS_ALLOWED_ORIGINS
// says; the other /api/v1 routes keep the configured origins.
func TestAssetListCORSIsOpen(t *testing.T) {
	e, list, _ := assetServer(t, &assetTables{vdlName: "Vidulum"}, "https://explorer.example")
	require.NoError(t, list.Rebuild(context.Background()))

	rec := e.get("/api/v1/registry/assets", "Origin", "https://dex.example")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	rec = e.get("/api/v1/registry/assets/ubze", "Origin", "https://dex.example")
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))

	rec = e.get("/health", "Origin", "https://dex.example")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	rec = e.get("/api/v1/status", "Origin", "https://explorer.example")
	assert.Equal(t, "https://explorer.example", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestNoAssetListNoRoutes(t *testing.T) {
	e := &assetHTTP{server.New(server.Deps{})}
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/registry/assets").Code)
}
