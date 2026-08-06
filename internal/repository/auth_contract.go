package repository

import (
	"context"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

type AuthRepository interface {
	FindProfileByWalletAddress(ctx context.Context, walletAddress string) (domain.AuthProfile, bool, error)
}
