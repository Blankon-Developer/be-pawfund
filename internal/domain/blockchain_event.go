package domain

import (
	"time"

	"github.com/google/uuid"
)

type BlockchainEventType string

const BlockchainEventTypeCampaignCreated BlockchainEventType = "campaign_created"

// BlockchainEvent records an on-chain log the application has observed.
type BlockchainEvent struct {
	ID          uuid.UUID
	TxHash      string
	LogIndex    int
	Type        BlockchainEventType
	BlockNumber int
	CreatedAt   time.Time
}
