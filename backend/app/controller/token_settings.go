package controller

import (
	"context"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// tokenSettings reads the factory-token settings of denom.
func (h *TokenController) tokenSettings(ctx context.Context, denom string) (dto.TokenSettings, error) {
	s, err := h.repo.TokenSettings(ctx, denom)
	if err != nil {
		return dto.TokenSettings{}, err
	}
	return dto.NewTokenSettings(s), nil
}
