package domain

import (
	"time"

	"github.com/google/uuid"
)

type CampaignStatus string

const (
	CampaignStatusActive    CampaignStatus = "active"
	CampaignStatusCompleted CampaignStatus = "completed"
	CampaignStatusCancelled CampaignStatus = "cancelled"
)

type CampaignListSort string

const (
	CampaignListSortRandom      CampaignListSort = "random"
	CampaignListSortNewest      CampaignListSort = "newest"
	CampaignListSortOldest      CampaignListSort = "oldest"
	CampaignListSortCloseToGoal CampaignListSort = "close-to-goal"
	CampaignListSortMostDonated CampaignListSort = "most-donated"
)

type CampaignListOptions struct {
	Search   string
	Sort     CampaignListSort
	Status   *CampaignStatus
	Page     int64
	PageSize int64
}

type CampaignDeploymentStatus string

const (
	CampaignDeploymentStatusPending   CampaignDeploymentStatus = "pending"
	CampaignDeploymentStatusSubmitted CampaignDeploymentStatus = "submitted"
	CampaignDeploymentStatusDeployed  CampaignDeploymentStatus = "deployed"
	CampaignDeploymentStatusFailed    CampaignDeploymentStatus = "failed"
)

type Campaign struct {
	ID               uuid.UUID
	FundraiserID     uuid.UUID
	EventID          *uuid.UUID
	Title            string
	ShortDescription string
	Story            string
	GoalAmount       int64
	RaisedAmount     int64
	DonorCount       int64
	ContractAddress  *string
	CreatedAt        time.Time
	EndAt            time.Time
	ImageObjectKey   string
	Country          string
	ZipCode          string
	Status           CampaignStatus
	DeploymentStatus CampaignDeploymentStatus
	IdempotencyKey   string
}

// PublicCampaignListItem contains the campaign fields that are safe to expose
// publicly, together with the optional fundraiser profile image.
type PublicCampaignListItem struct {
	Campaign
	FundraiserImageObjectKey *string
}

// PublicCampaignDetail contains the campaign fields that are safe to expose
// publicly, together with the fundraiser identity shown on the campaign page.
type PublicCampaignDetail struct {
	Campaign
	FundraiserName           string
	FundraiserWalletAddress  string
	FundraiserImageObjectKey *string
}
