package keybase_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/keybase"
)

// lookup serves the user lookup with body for every identity, recording the
// queries.
func lookup(t *testing.T, status int, body string) (*keybase.Client, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_/api/1.0/user/lookup.json" {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return keybase.New(srv.Client(), srv.URL+"/_/api/1.0/user/lookup.json"), &queries
}

func TestPictureURLReadsThePrimaryPicture(t *testing.T) {
	c, queries := lookup(t, http.StatusOK, `{"status":{"code":0,"name":"OK"},"them":[{"id":"x",
		"pictures":{"primary":{"url":"https://s3.amazonaws.com/keybase_processed_uploads/a_360_360.jpg","source":null}}}]}`)
	url, err := c.PictureURL(context.Background(), "1857FF1BC2EA6A69")
	require.NoError(t, err)
	assert.Equal(t, "https://s3.amazonaws.com/keybase_processed_uploads/a_360_360.jpg", url)
	assert.Equal(t, []string{"fields=pictures&key_suffix=1857FF1BC2EA6A69"}, *queries)
}

func TestPictureURLIsEmptyWithoutUserOrPicture(t *testing.T) {
	for _, body := range []string{
		`{"status":{"code":0,"name":"OK"},"them":[]}`,
		`{"status":{"code":0,"name":"OK"},"them":[{"id":"x"}]}`,
	} {
		c, _ := lookup(t, http.StatusOK, body)
		url, err := c.PictureURL(context.Background(), "0000000000000000")
		require.NoError(t, err, body)
		assert.Empty(t, url, body)
	}
}

func TestPictureURLFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"http":   {http.StatusTooManyRequests, `{}`},
		"status": {http.StatusOK, `{"status":{"code":100,"name":"INPUT_ERROR"}}`},
		"body":   {http.StatusOK, `not json`},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := lookup(t, tc.status, tc.body)
			_, err := c.PictureURL(context.Background(), "1857FF1BC2EA6A69")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "keybase 1857FF1BC2EA6A69")
		})
	}
	_, err := keybase.New(http.DefaultClient, "http://127.0.0.1:1/lookup.json").PictureURL(context.Background(), "x")
	require.Error(t, err, "unreachable")
}
