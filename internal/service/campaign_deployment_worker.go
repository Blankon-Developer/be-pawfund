package service

import (
	"context"
	"fmt"
	"log/slog"
)

// CampaignDeploymentWorker handles campaign deployment tasks from the queue.
// Phase 1 only logs received work; chain deploy and persistence follow later.
type CampaignDeploymentWorker struct {
	chainID int
	logger  *slog.Logger
}

func NewCampaignDeploymentWorker(chainID int, logger *slog.Logger) *CampaignDeploymentWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CampaignDeploymentWorker{
		chainID: chainID,
		logger:  logger,
	}
}

func (w *CampaignDeploymentWorker) Handle(ctx context.Context, task CampaignDeploymentTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("service: handle campaign deployment: worker is required")
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

	w.logger.Info(
		"campaign deployment task received",
		"campaignId", task.CampaignID,
		"fundraiserId", task.FundraiserID,
		"fundraiserWallet", task.FundraiserWallet,
		"goalAmount", task.GoalAmount,
		"endAt", task.EndAt.UTC(),
		"chainId", task.ChainID,
	)
	return nil
}
