package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

type ContactPerson struct {
	Name  string
	Phone string
}

type RegisterFundraiserInput struct {
	Name           string
	Email          string
	WalletAddress  string
	ContactPerson  ContactPerson
	SocialURL      string
	Country        string
	ZipCode        string
	ImageObjectKey *string
}

type FundraiserService struct {
	repository repository.FundraiserRepository
	generateID IDGenerator
}

func NewFundraiserService(repo repository.FundraiserRepository, generateID IDGenerator) *FundraiserService {
	if generateID == nil {
		generateID = uuid.NewV7
	}

	return &FundraiserService{
		repository: repo,
		generateID: generateID,
	}
}

func (s *FundraiserService) Register(ctx context.Context, input RegisterFundraiserInput) (domain.Fundraiser, error) {
	id, err := s.generateID()
	if err != nil {
		return domain.Fundraiser{}, fmt.Errorf("service: generate fundraiser ID: %w", err)
	}

	socialURL := strings.TrimSpace(input.SocialURL)
	fundraiser := domain.Fundraiser{
		User: domain.User{
			ID:            id,
			Role:          domain.UserRoleFundraiser,
			Email:         strings.ToLower(strings.TrimSpace(input.Email)),
			WalletAddress: strings.TrimSpace(input.WalletAddress),
		},
		Name:           strings.TrimSpace(input.Name),
		ImageObjectKey: normalizeOptionalString(input.ImageObjectKey),
		ContactName:    strings.TrimSpace(input.ContactPerson.Name),
		ContactPhone:   strings.TrimSpace(input.ContactPerson.Phone),
		SocialURL:      &socialURL,
		Country:        strings.TrimSpace(input.Country),
		ZipCode:        strings.TrimSpace(input.ZipCode),
	}

	created, err := s.repository.Create(ctx, fundraiser)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			return domain.Fundraiser{}, ErrEmailAlreadyRegistered
		case errors.Is(err, repository.ErrWalletAlreadyExists):
			return domain.Fundraiser{}, ErrWalletAlreadyRegistered
		default:
			return domain.Fundraiser{}, fmt.Errorf("service: register fundraiser: %w", err)
		}
	}

	return created, nil
}

func (s *FundraiserService) GetProfile(ctx context.Context, walletAddress string) (domain.Fundraiser, error) {
	fundraiser, found, err := s.repository.FindByWalletAddress(ctx, strings.TrimSpace(walletAddress))
	if err != nil {
		return domain.Fundraiser{}, fmt.Errorf("service: get fundraiser profile: %w", err)
	}
	if !found {
		return domain.Fundraiser{}, ErrProfileNotFound
	}

	return fundraiser, nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}
