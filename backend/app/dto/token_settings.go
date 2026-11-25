package dto

import "github.com/bze-alphateam/bze-scan/backend/app/repository"

// TokenSettings is the factory-token part of the token page (chain
// v8.3.0), embedded in Token so its keys sit at the top level.
type TokenSettings struct {
	// Mintable is false once minting is turned off for good; null when not
	// a factory token or not read from a v8.3.0 node yet.
	Mintable *bool `json:"mintable"`
	// Config is null when not a factory token or unknown.
	Config *TokenConfig `json:"config"`
}

// TokenConfig is a factory token's DenomConfig; the ratios are decimal
// strings and the community pool is null for none.
type TokenConfig struct {
	CommunityPool      *string `json:"community_pool"`
	CommunityPoolLabel *Label  `json:"community_pool_label"`
	TransferFee        string  `json:"transfer_fee"`
	GasSurcharge       string  `json:"gas_surcharge"`
}

// NewTokenSettings maps the settings; nil gives the unknown shape.
func NewTokenSettings(s *repository.TokenSettings) TokenSettings {
	if s == nil {
		return TokenSettings{}
	}
	out := TokenSettings{Mintable: s.Mintable}
	if c := s.Config; c != nil {
		out.Config = &TokenConfig{CommunityPool: c.CommunityPool, CommunityPoolLabel: newLabel(c.CommunityPoolLabel),
			TransferFee: c.TransferFee, GasSurcharge: c.GasSurcharge}
	}
	return out
}
