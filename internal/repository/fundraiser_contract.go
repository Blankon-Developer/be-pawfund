package repository

import (
	"context"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

type FundraiserRepository interface {
	Create(ctx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error)
}
