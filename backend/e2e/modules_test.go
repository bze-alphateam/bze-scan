//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
)

func TestModulesListsTheSeededModuleAccounts(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serveAPI(t)
	r := fetch(t, base+"/modules")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "public, max-age=60", r.header.Get("Cache-Control"))
	list := into[dto.ModuleList](t, r)

	want := make([]dto.ModuleAccount, 0, len(labels.Modules()))
	for _, m := range labels.Modules() {
		want = append(want, dto.ModuleAccount{Address: m.Address, Name: m.Name, Module: m.Module})
	}
	assert.ElementsMatch(t, want, list.Items, "every seeded module account; no validator owner or escrow")
	var order []string
	for _, m := range list.Items {
		order = append(order, m.Address)
	}
	rows, err := e.db.Query(`SELECT address FROM explorer.labels WHERE kind = 'module' ORDER BY name, address`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var byName []string // queryStrings would sort them
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		byName = append(byName, a)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, byName, order, "by name, as the database collates")
}
