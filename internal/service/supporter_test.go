package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

type stubSupporterRepository struct {
	create     func(context.Context, domain.Supporter) (domain.Supporter, error)
	find       func(context.Context, string) (domain.Supporter, bool, error)
	called     int
	findCalled int
}

func (s *stubSupporterRepository) Create(ctx context.Context, supporter domain.Supporter) (domain.Supporter, error) {
	s.called++
	return s.create(ctx, supporter)
}

func (s *stubSupporterRepository) FindByWalletAddress(
	ctx context.Context,
	walletAddress string,
) (domain.Supporter, bool, error) {
	s.findCalled++
	return s.find(ctx, walletAddress)
}

func TestSupporterServiceRegister(t *testing.T) {
	fixedID := uuid.MustParse("0198f1a8-c0c0-7e1a-a604-d2b6942fc011")
	fixedTime := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	idFailure := errors.New("ID failure")
	repositoryFailure := errors.New("repository failure")
	imageKey := "profiles/cat.png"

	tests := []struct {
		name               string
		idError            error
		repositoryError    error
		wantError          error
		wantRepositoryCall bool
	}{
		{
			name:               "creates normalized supporter",
			wantRepositoryCall: true,
		},
		{
			name:      "returns ID generation error",
			idError:   idFailure,
			wantError: idFailure,
		},
		{
			name:               "maps duplicate email",
			repositoryError:    repository.ErrEmailAlreadyExists,
			wantError:          ErrEmailAlreadyRegistered,
			wantRepositoryCall: true,
		},
		{
			name:               "maps duplicate wallet",
			repositoryError:    repository.ErrWalletAlreadyExists,
			wantError:          ErrWalletAlreadyRegistered,
			wantRepositoryCall: true,
		},
		{
			name:               "wraps unexpected repository error",
			repositoryError:    repositoryFailure,
			wantError:          repositoryFailure,
			wantRepositoryCall: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			repo := &stubSupporterRepository{
				create: func(gotCtx context.Context, supporter domain.Supporter) (domain.Supporter, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if supporter.ID != fixedID {
						t.Errorf("ID = %s, want %s", supporter.ID, fixedID)
					}
					if supporter.Role != domain.UserRoleSupporter {
						t.Errorf("role = %q", supporter.Role)
					}
					if supporter.Name != "Cat Lover" || supporter.Email != "cat@example.com" {
						t.Errorf("normalized supporter = %#v", supporter)
					}
					if supporter.WalletAddress != "0xWallet" {
						t.Errorf("wallet address = %q", supporter.WalletAddress)
					}
					if supporter.ImageObjectKey == nil || *supporter.ImageObjectKey != imageKey {
						t.Errorf("image object key = %#v", supporter.ImageObjectKey)
					}

					if test.repositoryError != nil {
						return domain.Supporter{}, test.repositoryError
					}
					supporter.CreatedAt = fixedTime
					return supporter, nil
				},
			}
			generator := func() (uuid.UUID, error) {
				if test.idError != nil {
					return uuid.Nil, test.idError
				}
				return fixedID, nil
			}
			supporterService := NewSupporterService(repo, generator)

			created, err := supporterService.Register(ctx, RegisterSupporterInput{
				Name:           " Cat Lover ",
				Email:          " CAT@EXAMPLE.COM ",
				WalletAddress:  " 0xWallet ",
				ImageObjectKey: &imageKey,
			})

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Register() error = %v, want %v", err, test.wantError)
				}
			} else {
				if err != nil {
					t.Fatalf("Register() unexpected error: %v", err)
				}
				if created.CreatedAt != fixedTime {
					t.Errorf("CreatedAt = %v, want %v", created.CreatedAt, fixedTime)
				}
			}

			if (repo.called > 0) != test.wantRepositoryCall {
				t.Errorf("repository calls = %d, want called = %v", repo.called, test.wantRepositoryCall)
			}
		})
	}
}

func TestSupporterServiceGetProfile(t *testing.T) {
	profile := domain.Supporter{
		User: domain.User{
			Role:          domain.UserRoleSupporter,
			Email:         "cat@example.com",
			WalletAddress: "0xWalletChecksum",
		},
		Name: "Cat Lover",
	}
	repositoryFailure := errors.New("repository failure")

	tests := []struct {
		name        string
		found       bool
		repoError   error
		wantError   error
		wantProfile domain.Supporter
	}{
		{
			name:        "returns supporter profile",
			found:       true,
			wantProfile: profile,
		},
		{
			name:      "returns not found",
			wantError: ErrProfileNotFound,
		},
		{
			name:      "wraps repository failure",
			repoError: repositoryFailure,
			wantError: repositoryFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			repo := &stubSupporterRepository{
				find: func(gotCtx context.Context, walletAddress string) (domain.Supporter, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					return profile, test.found, test.repoError
				},
			}
			supporterService := NewSupporterService(repo, nil)

			got, err := supporterService.GetProfile(ctx, " 0xWalletChecksum ")
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("GetProfile() error = %v, want %v", err, test.wantError)
				}
				if test.repoError != nil && !strings.Contains(err.Error(), "get supporter profile") {
					t.Errorf("GetProfile() error lacks operation context: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("GetProfile() unexpected error: %v", err)
				}
				if got != test.wantProfile {
					t.Errorf("GetProfile() = %#v, want %#v", got, test.wantProfile)
				}
			}
			if repo.findCalled != 1 {
				t.Errorf("repository calls = %d, want 1", repo.findCalled)
			}
		})
	}
}
