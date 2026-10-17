package app

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

func TestDeployerHandleDeliveryInvalidMessageAcks(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	deployer := &Deployer{
		worker: service.NewCampaignDeploymentWorker(84532, logger),
		logger: logger,
	}

	if err := deployer.handleDelivery(context.Background(), queue.Delivery{Body: []byte("{")}); err != nil {
		t.Fatalf("handleDelivery() error = %v, want nil for invalid payload", err)
	}
}

func TestDeployerHandleDeliveryValidMessage(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198b123-4567-7abc-8123-456789abcdef")

	body, err := campaignDeploymentBody(service.CampaignDeploymentTask{
		CampaignID:       campaignID,
		FundraiserID:     fundraiserID,
		FundraiserWallet: "0xFundraiser",
		GoalAmount:       1,
		EndAt:            mustParseRFC3339(t, "2026-10-25T04:00:00Z"),
		ChainID:          84532,
	})
	if err != nil {
		t.Fatalf("campaignDeploymentBody() error = %v", err)
	}

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	deployer := &Deployer{
		worker: service.NewCampaignDeploymentWorker(84532, logger),
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
