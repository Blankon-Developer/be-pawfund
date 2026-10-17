package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/blockchain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

type campaignDeploymentRepositoryStub struct {
	campaign      domain.Campaign
	findErr       error
	completeCalls int
}

func (s *campaignDeploymentRepositoryStub) FindByIDForDeployment(
	_ context.Context,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	if s.findErr != nil {
		return domain.Campaign{}, s.findErr
	}
	if s.campaign.ID != campaignID {
		return domain.Campaign{}, repository.ErrCampaignNotFound
	}
	return s.campaign, nil
}

func (s *campaignDeploymentRepositoryStub) CompleteDeployment(
	_ context.Context,
	_ uuid.UUID,
	_ domain.BlockchainEvent,
	_ string,
) error {
	s.completeCalls++
	return nil
}

func TestCampaignDeploymentWorkerHandleCompletesPendingCampaign(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198b123-4567-7abc-8123-456789abcdef")
	endAt := time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)

	store := &campaignDeploymentRepositoryStub{
		campaign: domain.Campaign{
			ID:           campaignID,
			FundraiserID: fundraiserID,
			GoalAmount:   10_000_000_000,
			EndAt:        endAt,
			Title:        "Emergency Rescue",
			Status:       domain.CampaignStatusPending,
		},
	}

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	worker := NewCampaignDeploymentWorker(
		84532,
		store,
		blockchain.NewStubCampaignDeployer(),
		func() (uuid.UUID, error) { return uuid.MustParse("0198c123-4567-7abc-8123-456789abcdef"), nil },
		logger,
	)

	task := CampaignDeploymentTask{
		CampaignID:       campaignID,
		FundraiserID:     fundraiserID,
		FundraiserWallet: "0xFundraiser",
		GoalAmount:       10_000_000_000,
		EndAt:            endAt,
		ChainID:          84532,
	}
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", store.completeCalls)
	}
	if logBuffer.Len() == 0 {
		t.Fatal("expected deployment log entry")
	}
}

func TestCampaignDeploymentWorkerHandleChainMismatchAcks(t *testing.T) {
	campaignID := uuid.New()
	worker := NewCampaignDeploymentWorker(
		84532,
		&campaignDeploymentRepositoryStub{},
		blockchain.NewStubCampaignDeployer(),
		uuid.NewV7,
		slog.Default(),
	)
	task := CampaignDeploymentTask{CampaignID: campaignID, ChainID: 1}
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() mismatch error = %v", err)
	}
}

func TestCampaignDeploymentWorkerHandleMissingCampaignAcks(t *testing.T) {
	worker := NewCampaignDeploymentWorker(
		84532,
		&campaignDeploymentRepositoryStub{findErr: repository.ErrCampaignNotFound},
		blockchain.NewStubCampaignDeployer(),
		uuid.NewV7,
		slog.Default(),
	)
	task := CampaignDeploymentTask{CampaignID: uuid.New(), ChainID: 84532}
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}
