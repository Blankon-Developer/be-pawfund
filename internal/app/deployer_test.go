package app

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/blockchain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

type deployerRepositoryStub struct {
	campaign domain.Campaign
}

func (s *deployerRepositoryStub) FindByIDForDeployment(
	_ context.Context,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	if s.campaign.ID != campaignID {
		return domain.Campaign{}, repository.ErrCampaignNotFound
	}
	return s.campaign, nil
}

func (s *deployerRepositoryStub) CompleteDeployment(
	_ context.Context,
	_ uuid.UUID,
	_ domain.BlockchainEvent,
	_ string,
) error {
	return nil
}

func TestDeployerHandleDeliveryInvalidMessageAcks(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	deployer := &Deployer{
		worker: service.NewCampaignDeploymentWorker(
			84532,
			&deployerRepositoryStub{},
			blockchain.NewStubCampaignDeployer(),
			uuid.NewV7,
			logger,
		),
		logger: logger,
	}

	if err := deployer.handleDelivery(context.Background(), queue.Delivery{Body: []byte("{")}); err != nil {
		t.Fatalf("handleDelivery() error = %v, want nil for invalid payload", err)
	}
}

func TestDeployerHandleDeliveryValidMessage(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198b123-4567-7abc-8123-456789abcdef")
	endAt := mustParseRFC3339(t, "2026-10-25T04:00:00Z")

	body, err := campaignDeploymentBody(service.CampaignDeploymentTask{
		CampaignID:       campaignID,
		FundraiserID:     fundraiserID,
		FundraiserWallet: "0xFundraiser",
		GoalAmount:       10_000_000_000,
		EndAt:            endAt,
		ChainID:          84532,
	})
	if err != nil {
		t.Fatalf("campaignDeploymentBody() error = %v", err)
	}

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	deployer := &Deployer{
		worker: service.NewCampaignDeploymentWorker(
			84532,
			&deployerRepositoryStub{
				campaign: domain.Campaign{
					ID:           campaignID,
					FundraiserID: fundraiserID,
					GoalAmount:   10_000_000_000,
					EndAt:        endAt,
					Title:        "Emergency Rescue",
					Status:       domain.CampaignStatusPending,
				},
			},
			blockchain.NewStubCampaignDeployer(),
			uuid.NewV7,
			logger,
		),
		logger: logger,
	}
	if err := deployer.handleDelivery(context.Background(), queue.Delivery{Body: body}); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if logBuffer.Len() == 0 {
		t.Fatal("expected worker log entry")
	}
}

func mustParseRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
