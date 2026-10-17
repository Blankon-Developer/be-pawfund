//go:build integration

package integration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/blockchain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

func TestCampaignDeploymentWorkerActivatesPendingCampaign(t *testing.T) {
	const walletAddress = "0xFundraiserChecksum"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)

	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })
	fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	endAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	created, err := campaignRepo.CreatePending(
		t.Context(),
		walletAddress,
		newPendingCampaign(endAt, "worker-deploy-1"),
		time.Now().UTC().Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("CreatePending() unexpected error: %v", err)
	}

	worker := service.NewCampaignDeploymentWorker(
		campaignIntegrationChainID,
		campaignRepo,
		blockchain.NewStubCampaignDeployer(),
		uuid.NewV7,
		nil,
	)
	if err := worker.Handle(t.Context(), service.CampaignDeploymentTask{
		CampaignID:       created.ID,
		FundraiserID:     fundraiser.ID,
		FundraiserWallet: walletAddress,
		GoalAmount:       created.GoalAmount,
		EndAt:            created.EndAt,
		ChainID:          campaignIntegrationChainID,
	}); err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}

	found, err := campaignRepo.FindByIDForDeployment(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("FindByIDForDeployment() unexpected error: %v", err)
	}
	if found.Status != domain.CampaignStatusActive || found.ContractAddress == nil || strings.TrimSpace(*found.ContractAddress) == "" {
		t.Fatalf("campaign after deployment = %#v", found)
	}
	if found.EventID == nil {
		t.Fatal("expected blockchain event id on campaign")
	}

	if err := worker.Handle(t.Context(), service.CampaignDeploymentTask{
		CampaignID:       created.ID,
		FundraiserID:     fundraiser.ID,
		FundraiserWallet: walletAddress,
		GoalAmount:       created.GoalAmount,
		EndAt:            created.EndAt,
		ChainID:          campaignIntegrationChainID,
	}); err != nil {
		t.Fatalf("Handle() replay error = %v", err)
	}
}
