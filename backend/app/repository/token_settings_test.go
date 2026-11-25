package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func settingsRow(mintable bool, pool string, fee, gas string, labelName, labelKind *string) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(**bool) = &mintable
		if pool != "" {
			*dest[1].(**string) = &pool
		}
		*dest[2].(**string), *dest[3].(**string) = &fee, &gas
		*dest[4].(**string), *dest[5].(**string) = labelName, labelKind
		return nil
	}
}

func str(s string) *string { return &s }

func TestTokenSettingsKeepsAUserCommunityPoolAndItsLabel(t *testing.T) {
	db := &scriptedDB{rows: []func(dest ...any) error{settingsRow(true, "bze1pool", "0.01", "0.5", str("Pool"), str("dao"))}}
	got, err := repository.NewExplorer(db).TokenSettings(t.Context(), "factory/x/uz")
	require.NoError(t, err)
	require.NotNil(t, got.Config)
	assert.Equal(t, "bze1pool", *got.Config.CommunityPool)
	assert.Equal(t, &repository.Label{Address: "bze1pool", Name: "Pool", Kind: "dao"}, got.Config.CommunityPoolLabel)
	assert.Equal(t, "0.01", got.Config.TransferFee)
	assert.Contains(t, db.sql[0], "trim_scale", "the ratios have no trailing zeros")
}

func TestTokenSettingsCountsAModuleAccountPoolAsNone(t *testing.T) {
	db := &scriptedDB{rows: []func(dest ...any) error{settingsRow(false, "bze1module", "0.02", "0", str("Burner"), str("module"))}}
	got, err := repository.NewExplorer(db).TokenSettings(t.Context(), "factory/x/uz")
	require.NoError(t, err)
	require.NotNil(t, got.Config)
	assert.Nil(t, got.Config.CommunityPool, "the chain blocks a module account from receiving: no pool")
	assert.Nil(t, got.Config.CommunityPoolLabel)
	assert.Equal(t, "0.02", got.Config.TransferFee)
}

func TestTokenSettingsAreUnknownWithoutRatios(t *testing.T) {
	db := &scriptedDB{rows: []func(dest ...any) error{func(dest ...any) error { return nil }}}
	got, err := repository.NewExplorer(db).TokenSettings(t.Context(), "ubze")
	require.NoError(t, err)
	assert.Nil(t, got.Config)
	assert.Nil(t, got.Mintable)
}
