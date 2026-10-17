package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCampaignDeploymentWorkerHandle(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198b123-4567-7abc-8123-456789abcdef")
	endAt := time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	worker := NewCampaignDeploymentWorker(84532, logger)

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
	if logBuffer.Len() == 0 {
		t.Fatal("expected deployment log entry")
	}

	mismatchWorker := NewCampaignDeploymentWorker(1, slog.Default())
	if err := mismatchWorker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() mismatch error = %v", err)
	}
}
