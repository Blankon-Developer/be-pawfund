package repository

import (
	"context"
	"errors"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

var ErrFundraiserHasActiveCampaigns = errors.New("fundraiser has active campaigns")

type FundraiserRepository interface {
	Create(ctx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error)
	FindByWalletAddress(ctx context.Context, walletAddress string) (domain.Fundraiser, bool, error)
	ReplaceProfile(
		ctx context.Context,
		walletAddress string,
		profile domain.FundraiserProfileReplacement,
	) (ReplaceFundraiserProfileResult, bool, error)
	DeleteProfile(ctx context.Context, walletAddress string) (DeleteFundraiserProfileResult, bool, error)
}

// ReplaceFundraiserProfileResult describes storage cleanup that is safe after a
// successful profile-update transaction commits.
type ReplaceFundraiserProfileResult struct {
	OldImageObjectKey  *string
	DeleteOldImageFile bool
}

// DeleteFundraiserProfileResult describes storage cleanup that is safe after a
// successful profile-deletion transaction commits.
type DeleteFundraiserProfileResult struct {
	ImageObjectKey        *string
	DeleteImageObjectFile bool
}
