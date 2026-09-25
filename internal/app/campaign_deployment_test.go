package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

func TestCampaignDeploymentTopology(t *testing.T) {
	topology := CampaignDeploymentTopology()
	if topology.Exchange != "pawfund.tasks" || topology.ExchangeKind != "topic" || topology.Queue != "campaign.deploy" || topology.BindingKey != "campaign.deploy" || !topology.Durable || topology.Exclusive || topology.AutoDelete {
		t.Fatalf("topology = %#v", topology)
	}
}

func TestCampaignDeploymentBody(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198b123-4567-7abc-8123-456789abcdef")
	endAt := time.Date(2026, 10, 25, 11, 0, 0, 0, time.FixedZone("WIB", 7*60*60))

	body, err := campaignDeploymentBody(service.CampaignDeploymentTask{
		CampaignID:       campaignID,
		FundraiserID:     fundraiserID,
		FundraiserWallet: "0xFundraiserChecksum",
		GoalAmount:       10_000_000_000,
		EndAt:            endAt,
		ChainID:          84532,
	})
	if err != nil {
		t.Fatalf("campaignDeploymentBody() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(payload) != 6 {
		t.Fatalf("fields = %#v", payload)
	}

	var message campaignDeploymentMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if message.CampaignID != campaignID.String() || message.FundraiserID != fundraiserID.String() || message.FundraiserWallet != "0xFundraiserChecksum" || message.GoalAmount != 10_000_000_000 || message.EndAt != "2026-10-25T04:00:00Z" || message.ChainID != 84532 {
		t.Fatalf("message = %#v", message)
	}
}
