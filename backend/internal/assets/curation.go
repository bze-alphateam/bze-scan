package assets

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// MainnetChainID is the chain whose curation is the mainnet section; every
// other chain id reads the testnet section.
const MainnetChainID = "beezee-1"

//go:embed curation.json
var curationJSON []byte

// Notice explains an asset being wound down.
type Notice struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	// URL is an optional "learn more" link.
	URL string `json:"url,omitempty"`
}

// Curation is the hand-kept part of the asset list, one section of
// curation.json. Every list holds BZE denoms.
type Curation struct {
	// Verified is the only source of a factory token's verified flag.
	Verified []string `json:"verified"`
	// Unverified clears the verified flag whatever the other rules say.
	Unverified       []string          `json:"unverified"`
	Stable           []string          `json:"stable"`
	Excluded         []string          `json:"excluded"`
	DepositExcluded  []string          `json:"deposit_excluded"`
	WithdrawExcluded []string          `json:"withdraw_excluded"`
	Notices          map[string]Notice `json:"notices"`
}

// CurationFor returns the embedded curation of chainID: the mainnet
// section for MainnetChainID, the testnet one otherwise.
func CurationFor(chainID string) (Curation, error) {
	return ParseCuration(curationJSON, chainID)
}

// ParseCuration reads the section of chainID from a curation file.
func ParseCuration(raw []byte, chainID string) (Curation, error) {
	var file struct {
		Mainnet *Curation `json:"mainnet"`
		Testnet *Curation `json:"testnet"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return Curation{}, fmt.Errorf("asset curation: %w", err)
	}
	section, name := file.Testnet, "testnet"
	if chainID == MainnetChainID {
		section, name = file.Mainnet, "mainnet"
	}
	if section == nil {
		return Curation{}, fmt.Errorf("asset curation: no %s section", name)
	}
	return *section, nil
}

// flags is a Curation as sets, for the build.
type flags struct {
	verified, unverified, stable, excluded, noDeposit, noWithdraw map[string]bool
	notices                                                       map[string]Notice
}

func (c Curation) flags() flags {
	set := func(denoms []string) map[string]bool {
		out := make(map[string]bool, len(denoms))
		for _, d := range denoms {
			out[d] = true
		}
		return out
	}
	return flags{
		verified: set(c.Verified), unverified: set(c.Unverified), stable: set(c.Stable),
		excluded: set(c.Excluded), noDeposit: set(c.DepositExcluded), noWithdraw: set(c.WithdrawExcluded),
		notices: c.Notices,
	}
}
