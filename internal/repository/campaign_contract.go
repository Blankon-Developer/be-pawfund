package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

var (
	ErrCampaignFundraiserNotFound  = errors.New("campaign fundraiser not found")
	ErrCampaignIdempotencyConflict = errors.New("campaign idempotency key reused with different payload")
	ErrCampaignEndAtTooSoon        = errors.New("campaign end time is too soon")
)

type CampaignRepository interface {
	CreatePending(
		ctx context.Context,
		walletAddress string,
		campaign domain.Campaign,
		minimumEndAt time.Time,
	) (domain.Campaign, error)
}
