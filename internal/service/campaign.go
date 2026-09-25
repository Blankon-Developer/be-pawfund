package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

const CampaignDeploymentLeadTime = 5 * time.Minute

var (
	ErrCampaignIdempotencyConflict = errors.New("campaign idempotency key conflict")
	ErrCampaignEndAtTooSoon        = errors.New("campaign end time is too soon")
	ErrCampaignNotFound            = errors.New("campaign not found")
)

type CreateCampaignInput struct {
	WalletAddress    string
	IdempotencyKey   string
	Title            string
	ShortDescription string
	Story            string
	GoalAmount       int64
	EndAt            time.Time
	ImageObjectKey   string
	Country          string
	ZipCode          string
}

// CampaignDeploymentTask is the deployment work item published after a campaign
// is stored. The deployer treats CampaignID as its idempotency key.
type CampaignDeploymentTask struct {
	CampaignID       uuid.UUID
	FundraiserID     uuid.UUID
	FundraiserWallet string
	GoalAmount       int64
	EndAt            time.Time
	ChainID          int
}

// CampaignDeploymentPublisher enqueues a campaign deployment task.
type CampaignDeploymentPublisher interface {
	PublishCampaignDeployment(ctx context.Context, task CampaignDeploymentTask) error
}

type CampaignService struct {
	repository          repository.CampaignRepository
	generateID          IDGenerator
	objectPromoter      ObjectPromoter
	deploymentPublisher CampaignDeploymentPublisher
	chainID             int
	now                 func() time.Time
}

func NewCampaignService(
	repo repository.CampaignRepository,
	generateID IDGenerator,
	objectPromoter ObjectPromoter,
	deploymentPublisher CampaignDeploymentPublisher,
	chainID int,
) *CampaignService {
	if generateID == nil {
		generateID = uuid.NewV7
	}
	return &CampaignService{
		repository:          repo,
		generateID:          generateID,
		objectPromoter:      objectPromoter,
		deploymentPublisher: deploymentPublisher,
		chainID:             chainID,
		now:                 time.Now,
	}
}

func (s *CampaignService) ListPublicCampaigns(
	ctx context.Context,
	options domain.CampaignListOptions,
) ([]domain.PublicCampaignListItem, int64, error) {
	options.Search = strings.TrimSpace(options.Search)
	campaigns, total, err := s.repository.ListPublic(ctx, options)
	if err != nil {
		return nil, 0, fmt.Errorf("service: list public campaigns: %w", err)
	}
	return campaigns, total, nil
}

func (s *CampaignService) GetPublicCampaignDetail(
	ctx context.Context,
	contractAddress string,
) (domain.PublicCampaignDetail, error) {
	campaign, err := s.repository.FindPublicByContractAddress(ctx, strings.TrimSpace(contractAddress))
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return domain.PublicCampaignDetail{}, ErrCampaignNotFound
		}
		return domain.PublicCampaignDetail{}, fmt.Errorf("service: get public campaign detail: %w", err)
	}
	return campaign, nil
}

func (s *CampaignService) ListPublicCampaignDonors(
	ctx context.Context,
	contractAddress string,
	options domain.CampaignDonorListOptions,
) ([]domain.PublicCampaignDonor, int64, error) {
	donors, total, err := s.repository.ListPublicDonorsByContractAddress(
		ctx,
		strings.TrimSpace(contractAddress),
		options,
	)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return nil, 0, ErrCampaignNotFound
		}
		return nil, 0, fmt.Errorf("service: list public campaign donors: %w", err)
	}
	return donors, total, nil
}

func (s *CampaignService) ListMyCampaigns(
	ctx context.Context,
	walletAddress string,
	options domain.CampaignListOptions,
) ([]domain.Campaign, int64, error) {
	options.Search = strings.TrimSpace(options.Search)
	campaigns, total, err := s.repository.ListForFundraiser(ctx, strings.TrimSpace(walletAddress), options)
	if err != nil {
		return nil, 0, fmt.Errorf("service: list fundraiser campaigns: %w", err)
	}
	return campaigns, total, nil
}

func (s *CampaignService) GetMyCampaignDetail(
	ctx context.Context,
	walletAddress string,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	campaign, err := s.repository.FindByIDForFundraiser(ctx, strings.TrimSpace(walletAddress), campaignID)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			return domain.Campaign{}, ErrCampaignNotFound
		}
		return domain.Campaign{}, fmt.Errorf("service: get fundraiser campaign detail: %w", err)
	}
	return campaign, nil
}

func (s *CampaignService) Create(ctx context.Context, input CreateCampaignInput) (domain.Campaign, error) {
	id, err := s.generateID()
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("service: generate campaign ID: %w", err)
	}

	stagingKey := strings.TrimSpace(input.ImageObjectKey)
	imageObjectKey, err := promoteImageObjectKey(
		ctx,
		s.objectPromoter,
		stagingKey,
		CampaignImageDirectory,
	)
	if err != nil {
		return domain.Campaign{}, err
	}

	campaign := domain.Campaign{
		ID:               id,
		Title:            strings.TrimSpace(input.Title),
		ShortDescription: strings.TrimSpace(input.ShortDescription),
		Story:            strings.TrimSpace(input.Story),
		GoalAmount:       input.GoalAmount,
		EndAt:            input.EndAt.UTC().Truncate(time.Microsecond),
		ImageObjectKey:   imageObjectKey,
		Country:          strings.TrimSpace(input.Country),
		ZipCode:          strings.TrimSpace(input.ZipCode),
		Status:           domain.CampaignStatusPending,
		IdempotencyKey:   strings.TrimSpace(input.IdempotencyKey),
	}

	created, err := s.repository.CreatePending(
		ctx,
		strings.TrimSpace(input.WalletAddress),
		campaign,
		s.now().UTC().Add(CampaignDeploymentLeadTime),
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrCampaignFundraiserNotFound):
			return domain.Campaign{}, ErrProfileNotFound
		case errors.Is(err, repository.ErrCampaignIdempotencyConflict):
			return domain.Campaign{}, ErrCampaignIdempotencyConflict
		case errors.Is(err, repository.ErrCampaignEndAtTooSoon):
			return domain.Campaign{}, ErrCampaignEndAtTooSoon
		default:
			return domain.Campaign{}, fmt.Errorf("service: create pending campaign: %w", err)
		}
	}

	// Drop staging before publish. A failed publish still leaves the saved campaign retryable.
	discardStagingImage(ctx, s.objectPromoter, &stagingKey)
	if err := s.publishCampaignDeployment(ctx, strings.TrimSpace(input.WalletAddress), created); err != nil {
		return domain.Campaign{}, err
	}
	return created, nil
}

func (s *CampaignService) publishCampaignDeployment(ctx context.Context, walletAddress string, campaign domain.Campaign) error {
	if !needsCampaignDeployment(campaign) {
		return nil
	}
	if s.chainID <= 0 {
		return fmt.Errorf("service: publish campaign deployment %s: chain ID is required", campaign.ID)
	}
	if s.deploymentPublisher == nil {
		return fmt.Errorf("service: publish campaign deployment %s: publisher is required", campaign.ID)
	}

	err := s.deploymentPublisher.PublishCampaignDeployment(ctx, CampaignDeploymentTask{
		CampaignID:       campaign.ID,
		FundraiserID:     campaign.FundraiserID,
		FundraiserWallet: walletAddress,
		GoalAmount:       campaign.GoalAmount,
		EndAt:            campaign.EndAt.UTC(),
		ChainID:          s.chainID,
	})
	if err != nil {
		return fmt.Errorf("service: publish campaign deployment %s: %w", campaign.ID, err)
	}
	return nil
}

func needsCampaignDeployment(campaign domain.Campaign) bool {
	if campaign.Status != domain.CampaignStatusPending {
		return false
	}
	if campaign.ContractAddress == nil {
		return true
	}
	return strings.TrimSpace(*campaign.ContractAddress) == ""
}
