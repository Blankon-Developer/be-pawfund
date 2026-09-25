package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

const (
	campaignDeploymentExchange   = "pawfund.tasks"
	campaignDeploymentRoutingKey = "campaign.deploy"
	campaignDeploymentQueue      = "campaign.deploy"
)

// CampaignDeploymentTopology is the durable route for campaign deployment tasks.
func CampaignDeploymentTopology() queue.Topology {
	return queue.Topology{
		Exchange:     campaignDeploymentExchange,
		ExchangeKind: "topic",
		Queue:        campaignDeploymentQueue,
		BindingKey:   campaignDeploymentRoutingKey,
		Durable:      true,
	}
}

type campaignDeploymentMessage struct {
	CampaignID       string `json:"campaignId"`
	FundraiserID     string `json:"fundraiserId"`
	FundraiserWallet string `json:"fundraiserWallet"`
	GoalAmount       int64  `json:"goalAmount"`
	EndAt            string `json:"endAt"`
	ChainID          int    `json:"chainId"`
}

// CampaignDeploymentPublisher publishes campaign deployment tasks.
type CampaignDeploymentPublisher struct {
	client    *queue.QueueClient
	publisher *queue.Publisher
}

func NewCampaignDeploymentPublisher(client *queue.QueueClient) *CampaignDeploymentPublisher {
	return &CampaignDeploymentPublisher{
		client:    client,
		publisher: queue.NewPublisher(client),
	}
}

func (p *CampaignDeploymentPublisher) PublishCampaignDeployment(ctx context.Context, task service.CampaignDeploymentTask) error {
	if p == nil || p.client == nil || p.publisher == nil {
		return fmt.Errorf("app: publish campaign deployment: publisher is required")
	}
	// Declare on each publish so a retry can recreate the durable topology after reconnect.
	if err := queue.DeclareOn(p.client, CampaignDeploymentTopology()); err != nil {
		return fmt.Errorf("app: declare campaign deployment topology: %w", err)
	}

	body, err := campaignDeploymentBody(task)
	if err != nil {
		return err
	}
	topology := CampaignDeploymentTopology()
	if err := p.publisher.Publish(ctx, queue.Message{
		Exchange:    topology.Exchange,
		RoutingKey:  topology.BindingKey,
		Body:        body,
		ContentType: "application/json",
		Persistent:  true,
	}); err != nil {
		return fmt.Errorf("app: publish campaign deployment: %w", err)
	}
	return nil
}

func campaignDeploymentBody(task service.CampaignDeploymentTask) ([]byte, error) {
	body, err := json.Marshal(campaignDeploymentMessage{
		CampaignID:       task.CampaignID.String(),
		FundraiserID:     task.FundraiserID.String(),
		FundraiserWallet: task.FundraiserWallet,
		GoalAmount:       task.GoalAmount,
		EndAt:            task.EndAt.UTC().Format(time.RFC3339),
		ChainID:          task.ChainID,
	})
	if err != nil {
		return nil, fmt.Errorf("app: encode campaign deployment: %w", err)
	}
	return body, nil
}
