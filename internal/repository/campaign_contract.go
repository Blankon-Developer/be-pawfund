package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrCampaignFundraiserNotFound  = errors.New("campaign fundraiser not found")
	ErrCampaignIdempotencyConflict = errors.New("campaign idempotency key reused with different payload")
	ErrCampaignEndAtTooSoon        = errors.New("campaign end time is too soon")
	ErrCampaignNotFound            = errors.New("campaign not found")
)

type CampaignRepository interface {
	ListPublic(ctx context.Context, options domain.CampaignListOptions) ([]domain.PublicCampaignListItem, int64, error)
	FindPublicByContractAddress(ctx context.Context, contractAddress string) (domain.PublicCampaignDetail, error)
	ListPublicDonorsByContractAddress(
		ctx context.Context,
		contractAddress string,
		options domain.CampaignDonorListOptions,
	) ([]domain.PublicCampaignDonor, int64, error)
	ListForFundraiser(
		ctx context.Context,
		walletAddress string,
		options domain.CampaignListOptions,
	) ([]domain.Campaign, int64, error)
	FindByIDForFundraiser(
		ctx context.Context,
		walletAddress string,
		campaignID uuid.UUID,
	) (domain.Campaign, error)
	CreatePending(
		ctx context.Context,
		walletAddress string,
		campaign domain.Campaign,
		minimumEndAt time.Time,
	) (domain.Campaign, error)
}
