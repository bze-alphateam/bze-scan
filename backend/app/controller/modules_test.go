package controller_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestModulesListsTheModuleAccounts(t *testing.T) {
	f := &fakeReader{modules: []repository.ModuleAccount{
		{Address: "bze1qyqszqgpqyqszqgpqyqszqgpqyqszqgpjnp7du", Name: "Burner", Module: "burner"},
		{Address: "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp", Name: "DEX", Module: "tradebin"},
	}}
	rec := get(t, newAPI(f), "/modules")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheIndexed, rec.Header().Get(echo.HeaderCacheControl), "reference data, cached")
	assert.JSONEq(t, `{"items":[
		{"address":"bze1qyqszqgpqyqszqgpqyqszqgpqyqszqgpjnp7du","name":"Burner","module":"burner"},
		{"address":"bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp","name":"DEX","module":"tradebin"}]}`, rec.Body.String())
}

func TestModulesIsAnEmptyListWithoutLabels(t *testing.T) {
	rec := get(t, newAPI(&fakeReader{}), "/modules")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"items":[]}`, rec.Body.String())
}

func TestModulesFailure(t *testing.T) {
	rec := get(t, newAPI(&fakeReader{err: errors.New("db down")}), "/modules")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), "errors are never cached")
}
