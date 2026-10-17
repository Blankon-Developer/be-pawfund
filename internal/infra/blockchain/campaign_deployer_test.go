package blockchain

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestStubCampaignDeployerDeterministicAddress(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	deployer := NewStubCampaignDeployer()

	first, err := deployer.DeployCampaign(context.Background(), CampaignDeploymentParams{
		CampaignID: campaignID,
	})
	if err != nil {
		t.Fatalf("DeployCampaign() error = %v", err)
	}
	second, err := deployer.DeployCampaign(context.Background(), CampaignDeploymentParams{
		CampaignID: campaignID,
	})
	if err != nil {
		t.Fatalf("DeployCampaign() replay error = %v", err)
	}
	if first.ContractAddress != second.ContractAddress || first.TxHash != second.TxHash {
		t.Fatalf("results = %#v vs %#v", first, second)
	}
	if first.ContractAddress == "" || first.TxHash == "" {
		t.Fatalf("empty deployment result = %#v", first)
	}
}
