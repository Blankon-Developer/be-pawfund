package service

import "errors"

var (
	ErrEmailAlreadyRegistered  = errors.New("email already registered")
	ErrWalletAlreadyRegistered = errors.New("wallet address already registered")
	ErrProfileNotFound         = errors.New("profile not found")
	ErrActiveCampaignsExist    = errors.New("fundraiser has active campaigns")
)
