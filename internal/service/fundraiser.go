package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

type ReplaceFundraiserProfileInput struct {
	WalletAddress string
	Profile       domain.FundraiserProfileReplacement
}

// ObjectDeleter removes an object from profile-image storage.
type ObjectDeleter interface {
	Delete(ctx context.Context, objectKey string) error
}

type FundraiserService struct {
	repository    repository.FundraiserRepository
	generateID    IDGenerator
	objectDeleter ObjectDeleter
}

func NewFundraiserService(
	repo repository.FundraiserRepository,
	generateID IDGenerator,
	objectDeleters ...ObjectDeleter,
) *FundraiserService {
	if generateID == nil {
		generateID = uuid.NewV7
	}
	var objectDeleter ObjectDeleter
	if len(objectDeleters) > 0 {
		objectDeleter = objectDeleters[0]
	}

	return &FundraiserService{
		repository:    repo,
		generateID:    generateID,
		objectDeleter: objectDeleter,
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

func (s *FundraiserService) ReplaceProfile(ctx context.Context, input ReplaceFundraiserProfileInput) error {
	result, found, err := s.repository.ReplaceProfile(
		ctx,
		strings.TrimSpace(input.WalletAddress),
		normalizeFundraiserProfileReplacement(input.Profile),
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			return ErrEmailAlreadyRegistered
		default:
			return fmt.Errorf("service: replace fundraiser profile: %w", err)
		}
	}
	if !found {
		return ErrProfileNotFound
	}

	if result.DeleteOldImageFile && result.OldImageObjectKey != nil && s.objectDeleter != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.objectDeleter.Delete(cleanupContext, *result.OldImageObjectKey); err != nil {
			slog.Warn("delete unreferenced fundraiser profile image", "object_key", *result.OldImageObjectKey, "error", err)
		}
	}

	return nil
}

func (s *FundraiserService) DeleteProfile(ctx context.Context, walletAddress string) error {
	result, found, err := s.repository.DeleteProfile(ctx, strings.TrimSpace(walletAddress))
	if err != nil {
		if errors.Is(err, repository.ErrFundraiserHasActiveCampaigns) {
			return ErrActiveCampaignsExist
		}
		return fmt.Errorf("service: delete fundraiser profile: %w", err)
	}
	if !found {
		return ErrProfileNotFound
	}

	if result.DeleteImageObjectFile && result.ImageObjectKey != nil && s.objectDeleter != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.objectDeleter.Delete(cleanupContext, *result.ImageObjectKey); err != nil {
			slog.Warn("delete fundraiser profile image", "object_key", *result.ImageObjectKey, "error", err)
		}
	}

	return nil
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

func normalizeFundraiserProfileReplacement(
	profile domain.FundraiserProfileReplacement,
) domain.FundraiserProfileReplacement {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Email = strings.ToLower(strings.TrimSpace(profile.Email))
	profile.ContactName = strings.TrimSpace(profile.ContactName)
	profile.ContactPhone = strings.TrimSpace(profile.ContactPhone)
	profile.SocialURL = strings.TrimSpace(profile.SocialURL)
	profile.Country = strings.TrimSpace(profile.Country)
	profile.ZipCode = strings.TrimSpace(profile.ZipCode)
	if profile.ImageObjectKey.Set {
		profile.ImageObjectKey.Value = normalizeProfileString(profile.ImageObjectKey.Value, false)
	}
	return profile
}

func normalizeProfileString(value *string, lowercase bool) *string {
	if value == nil {
		return nil
	}

	normalized := strings.TrimSpace(*value)
	if lowercase {
		normalized = strings.ToLower(normalized)
	}
	return &normalized
}
