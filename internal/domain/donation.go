package domain

import "time"

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
