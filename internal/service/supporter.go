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

type RegisterSupporterInput struct {
	Name           string
	Email          string
	WalletAddress  string
	ImageObjectKey *string
}

type ReplaceSupporterProfileInput struct {
	WalletAddress string
	Profile       domain.SupporterProfileReplacement
}

type SupporterService struct {
	repository     repository.SupporterRepository
	generateID     IDGenerator
	objectDeleter  ObjectDeleter
	objectPromoter ObjectPromoter
}

func NewSupporterService(
	repo repository.SupporterRepository,
	generateID IDGenerator,
	objectDeleter ObjectDeleter,
	objectPromoter ObjectPromoter,
) *SupporterService {
	if generateID == nil {
		generateID = uuid.NewV7
	}

	return &SupporterService{
		repository:     repo,
		generateID:     generateID,
		objectDeleter:  objectDeleter,
		objectPromoter: objectPromoter,
	}
}

func (s *SupporterService) Register(ctx context.Context, input RegisterSupporterInput) (domain.Supporter, error) {
	id, err := s.generateID()
	if err != nil {
		return domain.Supporter{}, fmt.Errorf("service: generate supporter ID: %w", err)
	}

	stagingKey := normalizeOptionalString(input.ImageObjectKey)
	imageObjectKey, err := promoteOptionalImageObjectKey(
		ctx,
		s.objectPromoter,
		stagingKey,
		ProfileImageDirectory,
	)
	if err != nil {
		return domain.Supporter{}, err
	}

	supporter := domain.Supporter{
		User: domain.User{
			ID:            id,
			Role:          domain.UserRoleSupporter,
			Email:         strings.ToLower(strings.TrimSpace(input.Email)),
			WalletAddress: strings.TrimSpace(input.WalletAddress),
		},
		Name:           strings.TrimSpace(input.Name),
		ImageObjectKey: imageObjectKey,
	}

	created, err := s.repository.Create(ctx, supporter)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			return domain.Supporter{}, ErrEmailAlreadyRegistered
		case errors.Is(err, repository.ErrWalletAlreadyExists):
			return domain.Supporter{}, ErrWalletAlreadyRegistered
		default:
			return domain.Supporter{}, fmt.Errorf("service: register supporter: %w", err)
		}
	}

	discardStagingImage(ctx, s.objectPromoter, stagingKey)
	return created, nil
}

func (s *SupporterService) GetProfile(ctx context.Context, walletAddress string) (domain.Supporter, error) {
	supporter, found, err := s.repository.FindByWalletAddress(ctx, strings.TrimSpace(walletAddress))
	if err != nil {
		return domain.Supporter{}, fmt.Errorf("service: get supporter profile: %w", err)
	}
	if !found {
		return domain.Supporter{}, ErrProfileNotFound
	}

	return supporter, nil
}

func (s *SupporterService) ListMyDonations(
	ctx context.Context,
	walletAddress string,
	options domain.DonationListOptions,
) ([]domain.Donation, error) {
	donations, found, err := s.repository.ListDonationsByWalletAddress(
		ctx,
		strings.TrimSpace(walletAddress),
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("service: list supporter donations: %w", err)
	}
	if !found {
		return nil, ErrProfileNotFound
	}

	return donations, nil
}

func (s *SupporterService) ReplaceProfile(ctx context.Context, input ReplaceSupporterProfileInput) error {
	profile := normalizeSupporterProfileReplacement(input.Profile)
	var stagingKey *string
	if profile.ImageObjectKey.Set && profile.ImageObjectKey.Value != nil {
		stagingKey = profile.ImageObjectKey.Value
		canonical, err := promoteImageObjectKey(
			ctx,
			s.objectPromoter,
			*stagingKey,
			ProfileImageDirectory,
		)
		if err != nil {
			return err
		}
		profile.ImageObjectKey.Value = &canonical
	}

	result, found, err := s.repository.ReplaceProfile(
		ctx,
		strings.TrimSpace(input.WalletAddress),
		profile,
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			return ErrEmailAlreadyRegistered
		default:
			return fmt.Errorf("service: replace supporter profile: %w", err)
		}
	}
	if !found {
		return ErrProfileNotFound
	}

	discardStagingImage(ctx, s.objectPromoter, stagingKey)
	if result.DeleteOldImageFile && result.OldImageObjectKey != nil && s.objectDeleter != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.objectDeleter.Delete(cleanupContext, *result.OldImageObjectKey); err != nil {
			slog.Warn("delete unreferenced supporter profile image", "object_key", *result.OldImageObjectKey, "error", err)
		}
	}

	return nil
}

func (s *SupporterService) DeleteProfile(ctx context.Context, walletAddress string) error {
	result, found, err := s.repository.DeleteProfile(ctx, strings.TrimSpace(walletAddress))
	if err != nil {
		return fmt.Errorf("service: delete supporter profile: %w", err)
	}
	if !found {
		return ErrProfileNotFound
	}

	if result.DeleteImageObjectFile && result.ImageObjectKey != nil && s.objectDeleter != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.objectDeleter.Delete(cleanupContext, *result.ImageObjectKey); err != nil {
			slog.Warn("delete supporter profile image", "object_key", *result.ImageObjectKey, "error", err)
		}
	}

	return nil
}

func normalizeSupporterProfileReplacement(
	profile domain.SupporterProfileReplacement,
) domain.SupporterProfileReplacement {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Email = strings.ToLower(strings.TrimSpace(profile.Email))
	if profile.ImageObjectKey.Set {
		profile.ImageObjectKey.Value = normalizeProfileString(profile.ImageObjectKey.Value, false)
	}
	return profile
}
