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

type RegisterSupporterInput struct {
	Name           string
	Email          string
	WalletAddress  string
	ImageObjectKey *string
}

type SupporterService struct {
	repository repository.SupporterRepository
	generateID IDGenerator
}

func NewSupporterService(repo repository.SupporterRepository, generateID IDGenerator) *SupporterService {
	if generateID == nil {
		generateID = uuid.NewV7
	}

	return &SupporterService{
		repository: repo,
		generateID: generateID,
	}
}

func (s *SupporterService) Register(ctx context.Context, input RegisterSupporterInput) (domain.Supporter, error) {
	id, err := s.generateID()
	if err != nil {
		return domain.Supporter{}, fmt.Errorf("service: generate supporter ID: %w", err)
	}

	supporter := domain.Supporter{
		User: domain.User{
			ID:            id,
			Role:          domain.UserRoleSupporter,
			Email:         strings.ToLower(strings.TrimSpace(input.Email)),
			WalletAddress: strings.TrimSpace(input.WalletAddress),
		},
		Name:           strings.TrimSpace(input.Name),
		ImageObjectKey: input.ImageObjectKey,
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

	return created, nil
}
