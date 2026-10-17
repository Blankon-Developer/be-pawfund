package blockchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CampaignDeploymentParams is the on-chain input for creating a campaign instance.
type CampaignDeploymentParams struct {
	CampaignID       uuid.UUID
	FundraiserID     uuid.UUID
	FundraiserWallet string
	GoalAmount       int64
	EndAt            time.Time
	ChainID          int
	Title            string
}

// CampaignDeploymentResult is the observed outcome of a successful deployment.
type CampaignDeploymentResult struct {
	ContractAddress string
	TxHash          string
	LogIndex        int
	BlockNumber     int
	EventCreatedAt  time.Time
}

// CampaignDeployer creates a campaign contract instance (for example via a factory).
type CampaignDeployer interface {
	DeployCampaign(ctx context.Context, params CampaignDeploymentParams) (CampaignDeploymentResult, error)
}

// StubCampaignDeployer simulates a factory deployment for development and tests.
type StubCampaignDeployer struct{}

func NewStubCampaignDeployer() *StubCampaignDeployer {
	return &StubCampaignDeployer{}
}

func (d *StubCampaignDeployer) DeployCampaign(
	ctx context.Context,
	params CampaignDeploymentParams,
) (CampaignDeploymentResult, error) {
	if d == nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: stub deploy campaign: deployer is required")
	}
	if err := ctx.Err(); err != nil {
		return CampaignDeploymentResult{}, err
	}
	if params.CampaignID == uuid.Nil {
		return CampaignDeploymentResult{}, fmt.Errorf("blockchain: stub deploy campaign: campaign id is required")
	}

	contractAddress := stubContractAddress(params.CampaignID)
	txHash := stubTxHash(params.CampaignID)
	now := time.Now().UTC()

	return CampaignDeploymentResult{
		ContractAddress: contractAddress,
		TxHash:          txHash,
		LogIndex:        0,
		BlockNumber:     1,
		EventCreatedAt:  now,
	}, nil
}

func stubContractAddress(campaignID uuid.UUID) string {
	hex := strings.ReplaceAll(campaignID.String(), "-", "")
	if len(hex) > 40 {
		hex = hex[len(hex)-40:]
	}
	return "0x" + strings.Repeat("0", 40-len(hex)) + hex
}

func stubTxHash(campaignID uuid.UUID) string {
	hex := strings.ReplaceAll(campaignID.String(), "-", "")
	if len(hex) > 64 {
		hex = hex[:64]
	}
	return "0x" + strings.Repeat("0", 64-len(hex)) + hex
}
