package repository

import (
	"context"
	"errors"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

var (
	ErrEmailAlreadyExists  = errors.New("email already exists")
	ErrWalletAlreadyExists = errors.New("wallet address already exists")
)

type SupporterRepository interface {
	Create(ctx context.Context, supporter domain.Supporter) (domain.Supporter, error)
	FindByWalletAddress(ctx context.Context, walletAddress string) (domain.Supporter, bool, error)
	ReplaceProfile(
		ctx context.Context,
		walletAddress string,
		profile domain.SupporterProfileReplacement,
	) (ReplaceSupporterProfileResult, bool, error)
}

// ReplaceSupporterProfileResult describes storage cleanup that is safe after a
// successful profile-update transaction commits.
type ReplaceSupporterProfileResult struct {
	OldImageObjectKey  *string
	DeleteOldImageFile bool
}
