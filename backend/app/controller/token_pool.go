package controller

import (
	"context"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// PoolPriceReader reads the price state of the pool whose LP denom is given;
// nil when there is no such pool. Implemented by poolprice.Service.
type PoolPriceReader interface {
	Pool(ctx context.Context, lpDenom string) (*dto.PoolPrice, error)
}

// poolPrice is the token page's pool object: LP denoms only. The page does
// not fail when the node cannot answer; the object is left null.
func (h *TokenController) poolPrice(ctx context.Context, t *repository.Token) *dto.PoolPrice {
	if h.Pools == nil || t.Kind != "lp" {
		return nil
	}
	pool, err := h.Pools.Pool(ctx, t.Denom)
	if err != nil {
		return nil
	}
	return pool
}
