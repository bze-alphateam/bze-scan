// Package transform turns the node's answers for one height (/block,
// /block_results, /commit) into the rows the explorer stores. It is the only
// place where chain data is interpreted, shared by the live indexer and,
// later, the backfill and the reindex command. It does no I/O.
package transform

import (
	"encoding/json"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Input is everything the node answered for one height.
type Input struct {
	Block   *node.Block
	Results *node.BlockResults
	Commit  *node.Commit
}

// Entities are the rows derived from one or more heights, one slice per
// table. Later work adds a slice per table it fills.
type Entities struct {
	Blocks []Block
}

// Block is one explorer.blocks row. BlockTimeMs is not here: the writer
// derives it from the previous block's row.
type Block struct {
	Height              int64
	Time                time.Time
	TxCount             int
	TxFailedCount       int
	Hash                string
	ProposerConsAddress string
	SizeBytes           int
	// Minted is the mint event's amount (ubze), nil without a mint event.
	Minted *string
	// Inflation is the mint event's inflation, nil without a mint event.
	Inflation *string
	// FeesDistributed is the sum of the transfers from the fee collector to
	// the distribution module in the finalize-block events, nil when there is
	// none.
	FeesDistributed sdk.Coins
	SignaturesCount int
	// SignaturesPowerPct stays nil: the voting power at the height is not in
	// /commit (filled later from the validators table).
	SignaturesPowerPct *string
}

// FeesDistributedJSON is the fees_distributed column value: a JSON array of
// {"denom","amount"} objects, or nil.
func (b *Block) FeesDistributedJSON() ([]byte, error) {
	if b.FeesDistributed == nil {
		return nil, nil
	}
	return json.Marshal(b.FeesDistributed)
}

// Transformer holds the chain facts the rules need.
type Transformer struct {
	feeCollector string
	distribution string
}

// New returns a transformer for the BZE chain.
func New() *Transformer {
	return &Transformer{
		feeCollector: chain.ModuleAddress(chain.FeeCollector),
		distribution: chain.ModuleAddress(chain.Distribution),
	}
}

// Transform derives the entities of one height.
func (t *Transformer) Transform(in Input) (*Entities, error) {
	if in.Block == nil || in.Results == nil || in.Commit == nil {
		return nil, fmt.Errorf("transform: incomplete input")
	}
	h := in.Block.Height
	if in.Results.Height != h || in.Commit.Height != h {
		return nil, fmt.Errorf("transform %d: inputs of heights %d/%d/%d", h, h, in.Results.Height, in.Commit.Height)
	}
	if len(in.Results.TxsResults) != len(in.Block.Txs) {
		return nil, fmt.Errorf("transform %d: %d transactions but %d results", h, len(in.Block.Txs), len(in.Results.TxsResults))
	}

	b := Block{
		Height:              h,
		Time:                in.Block.Time.UTC(),
		TxCount:             len(in.Block.Txs),
		Hash:                in.Block.Hash,
		ProposerConsAddress: in.Block.ProposerAddress,
		SizeBytes:           len(in.Block.Raw),
	}
	for _, r := range in.Results.TxsResults {
		if r.Code != 0 {
			b.TxFailedCount++
		}
	}
	for _, s := range in.Commit.Signatures {
		if s.BlockIDFlag == node.BlockIDFlagCommit {
			b.SignaturesCount++
		}
	}

	for _, ev := range in.Results.FinalizeBlockEvents {
		switch ev.Type {
		case "mint":
			if b.Minted != nil {
				continue
			}
			if err := mintSummary(&b, ev); err != nil {
				return nil, fmt.Errorf("transform %d: %w", h, err)
			}
		case "transfer":
			sender, _ := ev.Get("sender")
			recipient, _ := ev.Get("recipient")
			if sender != t.feeCollector || recipient != t.distribution {
				continue
			}
			raw, _ := ev.Get("amount")
			coins, err := sdk.ParseCoinsNormalized(raw)
			if err != nil {
				return nil, fmt.Errorf("transform %d: fee distribution amount %q: %w", h, raw, err)
			}
			b.FeesDistributed = b.FeesDistributed.Add(coins...)
		}
	}

	return &Entities{Blocks: []Block{b}}, nil
}

func mintSummary(b *Block, ev node.Event) error {
	if raw, ok := ev.Get("amount"); ok {
		if _, ok := sdkmath.NewIntFromString(raw); !ok {
			return fmt.Errorf("mint amount %q is not an integer", raw)
		}
		b.Minted = &raw
	}
	if raw, ok := ev.Get("inflation"); ok {
		if _, err := sdkmath.LegacyNewDecFromStr(raw); err != nil {
			return fmt.Errorf("mint inflation %q: %w", raw, err)
		}
		b.Inflation = &raw
	}
	return nil
}
