package domain

import "time"

type CampaignDonorListSort string

const (
	CampaignDonorListSortRecent CampaignDonorListSort = "recent"
	CampaignDonorListSortTop    CampaignDonorListSort = "top"
)

type CampaignDonorListOptions struct {
	Sort     CampaignDonorListSort
	Page     int64
	PageSize int64
}

type PublicCampaignDonor struct {
	Name           *string
	WalletAddress  string
	ImageObjectKey *string
	Amount         int64
	DonatedAt      time.Time
}

type DonationListOptions struct {
	Page     int64
	PageSize int64
}

type DonationCampaign struct {
	Title           string
	ContractAddress string
}

type Donation struct {
	Amount    int64
	Campaign  DonationCampaign
	DonatedAt time.Time
	TxHash    string
}
