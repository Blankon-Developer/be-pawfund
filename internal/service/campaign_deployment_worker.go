package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/blockchain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

// CampaignDeploymentRepository loads campaigns and records successful deployments.
type CampaignDeploymentRepository interface {
	FindByIDForDeployment(ctx context.Context, campaignID uuid.UUID) (domain.Campaign, error)
	CompleteDeployment(
		ctx context.Context,
		campaignID uuid.UUID,
		event domain.BlockchainEvent,
		contractAddress string,
	) error
}

// CampaignDeploymentWorker handles campaign deployment tasks from the queue.
type CampaignDeploymentWorker struct {
	chainID    int
	repository CampaignDeploymentRepository
	deployer   blockchain.CampaignDeployer
	newEventID IDGenerator
	logger     *slog.Logger
}

func NewCampaignDeploymentWorker(
	chainID int,
	repository CampaignDeploymentRepository,
	deployer blockchain.CampaignDeployer,
	newEventID IDGenerator,
	logger *slog.Logger,
) *CampaignDeploymentWorker {
	if newEventID == nil {
		newEventID = uuid.NewV7
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &CampaignDeploymentWorker{
		chainID:    chainID,
		repository: repository,
		deployer:   deployer,
		newEventID: newEventID,
		logger:     logger,
	}
}

func (w *CampaignDeploymentWorker) Handle(ctx context.Context, task CampaignDeploymentTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("service: handle campaign deployment: worker is required")
	}
	if w.repository == nil {
		return fmt.Errorf("service: handle campaign deployment: repository is required")
	}
	if w.deployer == nil {
		return fmt.Errorf("service: handle campaign deployment: deployer is required")
	}
	if task.ChainID != w.chainID {
		w.logger.Warn(
			"campaign deployment task chain mismatch",
			"campaignId", task.CampaignID,
			"taskChainId", task.ChainID,
			"configuredChainId", w.chainID,
		)
		return nil
	}

	campaign, err := w.repository.FindByIDForDeployment(ctx, task.CampaignID)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotFound) {
			w.logger.Warn(
				"campaign deployment task references missing campaign",
				"campaignId", task.CampaignID,
			)
			return nil
		}
		return fmt.Errorf("service: load campaign %s for deployment: %w", task.CampaignID, err)
	}

	if campaignAlreadyDeployed(campaign) {
		w.logger.Info(
			"campaign deployment already completed",
			"campaignId", task.CampaignID,
			"contractAddress", campaign.ContractAddress,
		)
		return nil
	}

	if campaign.Status != domain.CampaignStatusPending {
		w.logger.Warn(
			"campaign deployment skipped for non-pending status",
			"campaignId", task.CampaignID,
			"status", campaign.Status,
		)
		return nil
	}

	if !deploymentTaskMatchesCampaign(task, campaign) {
		w.logger.Warn(
			"campaign deployment task does not match stored campaign",
			"campaignId", task.CampaignID,
			"taskFundraiserId", task.FundraiserID,
			"campaignFundraiserId", campaign.FundraiserID,
		)
		return nil
	}

	result, err := w.deployer.DeployCampaign(ctx, blockchain.CampaignDeploymentParams{
		CampaignID:       task.CampaignID,
		FundraiserID:     task.FundraiserID,
		FundraiserWallet: task.FundraiserWallet,
		GoalAmount:       task.GoalAmount,
		EndAt:            task.EndAt.UTC(),
		ChainID:          task.ChainID,
		Title:            campaign.Title,
	})
	if err != nil {
		return fmt.Errorf("service: deploy campaign %s: %w", task.CampaignID, err)
	}

	eventID, err := w.newEventID()
	if err != nil {
		return fmt.Errorf("service: generate campaign deployment event id %s: %w", task.CampaignID, err)
	}
	err = w.repository.CompleteDeployment(ctx, task.CampaignID, domain.BlockchainEvent{
		ID:          eventID,
		TxHash:      result.TxHash,
		LogIndex:    result.LogIndex,
		Type:        domain.BlockchainEventTypeCampaignCreated,
		BlockNumber: result.BlockNumber,
		CreatedAt:   result.EventCreatedAt.UTC(),
	}, result.ContractAddress)
	if err != nil {
		if errors.Is(err, repository.ErrCampaignNotPendingDeployment) {
			w.logger.Info(
				"campaign deployment completion skipped",
				"campaignId", task.CampaignID,
			)
			return nil
		}
		return fmt.Errorf("service: complete campaign deployment %s: %w", task.CampaignID, err)
	}

	w.logger.Info(
		"campaign deployment completed",
		"campaignId", task.CampaignID,
		"contractAddress", result.ContractAddress,
		"txHash", result.TxHash,
		"eventId", eventID,
	)
	return nil
}

func campaignAlreadyDeployed(campaign domain.Campaign) bool {
	if campaign.Status == domain.CampaignStatusActive {
		return campaign.ContractAddress != nil && strings.TrimSpace(*campaign.ContractAddress) != ""
	}
	return campaign.ContractAddress != nil && strings.TrimSpace(*campaign.ContractAddress) != ""
}

func deploymentTaskMatchesCampaign(task CampaignDeploymentTask, campaign domain.Campaign) bool {
	return campaign.FundraiserID == task.FundraiserID &&
		campaign.GoalAmount == task.GoalAmount &&
		campaign.EndAt.UTC().Equal(task.EndAt.UTC())
}
