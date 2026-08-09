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

type stubFundraiserRepository struct {
	create     func(context.Context, domain.Fundraiser) (domain.Fundraiser, error)
	find       func(context.Context, string) (domain.Fundraiser, bool, error)
	called     int
	findCalled int
}

func (s *stubFundraiserRepository) Create(ctx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error) {
	s.called++
	return s.create(ctx, fundraiser)
}

func (s *stubFundraiserRepository) FindByWalletAddress(
	ctx context.Context,
	walletAddress string,
) (domain.Fundraiser, bool, error) {
	s.findCalled++
	return s.find(ctx, walletAddress)
}

func TestFundraiserServiceRegister(t *testing.T) {
	fixedID := uuid.MustParse("0198f1a8-c0c0-7e1a-a604-d2b6942fc012")
	fixedTime := time.Date(2026, time.August, 8, 10, 0, 0, 0, time.UTC)
	idFailure := errors.New("ID failure")
	repositoryFailure := errors.New("repository failure")
	imageKey := " profiles/rescue.png "

	tests := []struct {
		name               string
		idError            error
		repositoryError    error
		imageObjectKey     *string
		wantImageObjectKey *string
		wantError          error
		wantRepositoryCall bool
	}{
		{
			name:               "creates normalized fundraiser",
			imageObjectKey:     &imageKey,
			wantImageObjectKey: serviceStringPointer("profiles/rescue.png"),
			wantRepositoryCall: true,
		},
		{
			name:               "keeps omitted image nil",
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
			repo := &stubFundraiserRepository{
				create: func(gotCtx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if fundraiser.ID != fixedID || fundraiser.Role != domain.UserRoleFundraiser {
						t.Errorf("fundraiser identity = %s/%q", fundraiser.ID, fundraiser.Role)
					}
					if fundraiser.Name != "Animal Rescue" || fundraiser.Email != "rescue@example.com" {
						t.Errorf("normalized fundraiser = %#v", fundraiser)
					}
					if fundraiser.WalletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", fundraiser.WalletAddress)
					}
					if fundraiser.ContactName != "Jane Doe" || fundraiser.ContactPhone != "+62 812 3456" {
						t.Errorf("contact fields = %q/%q", fundraiser.ContactName, fundraiser.ContactPhone)
					}
					if fundraiser.SocialURL == nil || *fundraiser.SocialURL != "https://example.com/rescue" || fundraiser.Country != "Indonesia" || fundraiser.ZipCode != "10110" {
						t.Errorf("profile fields = %#v", fundraiser)
					}
					if !serviceEqualStringPointers(fundraiser.ImageObjectKey, test.wantImageObjectKey) {
						t.Errorf("image object key = %#v, want %#v", fundraiser.ImageObjectKey, test.wantImageObjectKey)
					}

					if test.repositoryError != nil {
						return domain.Fundraiser{}, test.repositoryError
					}
					fundraiser.CreatedAt = fixedTime
					return fundraiser, nil
				},
			}
			generator := func() (uuid.UUID, error) {
				if test.idError != nil {
					return uuid.Nil, test.idError
				}
				return fixedID, nil
			}
			fundraiserService := NewFundraiserService(repo, generator)

			created, err := fundraiserService.Register(ctx, RegisterFundraiserInput{
				Name:          " Animal Rescue ",
				Email:         " RESCUE@EXAMPLE.COM ",
				WalletAddress: " 0xWalletChecksum ",
				ContactPerson: ContactPerson{
					Name:  " Jane Doe ",
					Phone: " +62 812 3456 ",
				},
				SocialURL:      " https://example.com/rescue ",
				Country:        " Indonesia ",
				ZipCode:        " 10110 ",
				ImageObjectKey: test.imageObjectKey,
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

func TestFundraiserServiceGetProfile(t *testing.T) {
	socialURL := "https://example.com/rescue"
	profile := domain.Fundraiser{
		User: domain.User{
			Role:          domain.UserRoleFundraiser,
			Email:         "rescue@example.com",
			WalletAddress: "0xWalletChecksum",
		},
		Name:         "Animal Rescue",
		ContactName:  "Jane Doe",
		ContactPhone: "+62 812 3456",
		SocialURL:    &socialURL,
		Country:      "Indonesia",
		ZipCode:      "10110",
	}
	repositoryFailure := errors.New("repository failure")

	tests := []struct {
		name        string
		found       bool
		repoError   error
		wantError   error
		wantProfile domain.Fundraiser
	}{
		{
			name:        "returns fundraiser profile",
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
			repo := &stubFundraiserRepository{
				find: func(gotCtx context.Context, walletAddress string) (domain.Fundraiser, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					return profile, test.found, test.repoError
				},
			}
			fundraiserService := NewFundraiserService(repo, nil)

			got, err := fundraiserService.GetProfile(ctx, " 0xWalletChecksum ")
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("GetProfile() error = %v, want %v", err, test.wantError)
				}
				if test.repoError != nil && !strings.Contains(err.Error(), "get fundraiser profile") {
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

func serviceStringPointer(value string) *string {
	return &value
}

func serviceEqualStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
