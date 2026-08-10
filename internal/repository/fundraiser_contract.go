package repository

import (
	"context"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

type FundraiserRepository interface {
	Create(ctx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error)
	FindByWalletAddress(ctx context.Context, walletAddress string) (domain.Fundraiser, bool, error)
	ReplaceProfile(
		ctx context.Context,
		walletAddress string,
		profile domain.FundraiserProfileReplacement,
	) (ReplaceFundraiserProfileResult, bool, error)
}

// ReplaceFundraiserProfileResult describes storage cleanup that is safe after a
// successful profile-update transaction commits.
type ReplaceFundraiserProfileResult struct {
	OldImageObjectKey  *string
	DeleteOldImageFile bool
}
