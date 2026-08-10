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
	create        func(context.Context, domain.Fundraiser) (domain.Fundraiser, error)
	find          func(context.Context, string) (domain.Fundraiser, bool, error)
	replace       func(context.Context, string, domain.FundraiserProfileReplacement) (repository.ReplaceFundraiserProfileResult, bool, error)
	delete        func(context.Context, string) (repository.DeleteFundraiserProfileResult, bool, error)
	called        int
	findCalled    int
	replaceCalled int
	deleteCalled  int
}

type stubFundraiserObjectDeleter struct {
	err       error
	calls     int
	objectKey string
}

func (s *stubFundraiserObjectDeleter) Delete(_ context.Context, objectKey string) error {
	s.calls++
	s.objectKey = objectKey
	return s.err
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

func (s *stubFundraiserRepository) ReplaceProfile(
	ctx context.Context,
	walletAddress string,
	profile domain.FundraiserProfileReplacement,
) (repository.ReplaceFundraiserProfileResult, bool, error) {
	s.replaceCalled++
	if s.replace == nil {
		return repository.ReplaceFundraiserProfileResult{}, false, nil
	}
	return s.replace(ctx, walletAddress, profile)
}

func (s *stubFundraiserRepository) DeleteProfile(
	ctx context.Context,
	walletAddress string,
) (repository.DeleteFundraiserProfileResult, bool, error) {
	s.deleteCalled++
	if s.delete == nil {
		return repository.DeleteFundraiserProfileResult{}, false, nil
	}
	return s.delete(ctx, walletAddress)
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

func TestFundraiserServiceReplaceProfile(t *testing.T) {
	oldImageObjectKey := "profiles/old.png"
	repositoryFailure := errors.New("repository failure")
	deleteFailure := errors.New("storage failure")

	tests := []struct {
		name               string
		found              bool
		repositoryError    error
		deleteError        error
		replaceResult      repository.ReplaceFundraiserProfileResult
		wantError          error
		wantDeleteCall     bool
		wantRepositoryCall bool
	}{
		{
			name:  "replaces normalized profile and removes old image",
			found: true,
			replaceResult: repository.ReplaceFundraiserProfileResult{
				OldImageObjectKey:  &oldImageObjectKey,
				DeleteOldImageFile: true,
			},
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{
			name:  "does not fail after a best effort image delete failure",
			found: true,
			replaceResult: repository.ReplaceFundraiserProfileResult{
				OldImageObjectKey:  &oldImageObjectKey,
				DeleteOldImageFile: true,
			},
			deleteError:        deleteFailure,
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{
			name:               "returns profile not found",
			wantError:          ErrProfileNotFound,
			wantRepositoryCall: true,
		},
		{
			name:               "maps duplicate email",
			found:              true,
			repositoryError:    repository.ErrEmailAlreadyExists,
			wantError:          ErrEmailAlreadyRegistered,
			wantRepositoryCall: true,
		},
		{
			name:               "wraps unexpected repository failure",
			found:              true,
			repositoryError:    repositoryFailure,
			wantError:          repositoryFailure,
			wantRepositoryCall: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			deleter := &stubFundraiserObjectDeleter{err: test.deleteError}
			repo := &stubFundraiserRepository{
				replace: func(gotCtx context.Context, walletAddress string, profile domain.FundraiserProfileReplacement) (repository.ReplaceFundraiserProfileResult, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					if profile.Name != "Animal Rescue" || profile.Email != "rescue@example.com" || profile.ContactName != "Jane Doe" || profile.ContactPhone != "+62 812 3456" || profile.SocialURL != "https://example.com/rescue" || profile.Country != "Indonesia" || profile.ZipCode != "10110" {
						t.Errorf("normalized replacement = %#v", profile)
					}
					if !profile.ImageObjectKey.Set || profile.ImageObjectKey.Value == nil || *profile.ImageObjectKey.Value != "profiles/new.png" {
						t.Errorf("image replacement = %#v", profile)
					}
					return test.replaceResult, test.found, test.repositoryError
				},
			}
			fundraiserService := NewFundraiserService(repo, nil, deleter)
			err := fundraiserService.ReplaceProfile(ctx, ReplaceFundraiserProfileInput{
				WalletAddress: " 0xWalletChecksum ",
				Profile: domain.FundraiserProfileReplacement{
					Name:         " Animal Rescue ",
					Email:        " RESCUE@EXAMPLE.COM ",
					ContactName:  " Jane Doe ",
					ContactPhone: " +62 812 3456 ",
					SocialURL:    " https://example.com/rescue ",
					Country:      " Indonesia ",
					ZipCode:      " 10110 ",
					ImageObjectKey: domain.ImageObjectKeyUpdate{
						Set:   true,
						Value: serviceStringPointer(" profiles/new.png "),
					},
				},
			})

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ReplaceProfile() error = %v, want %v", err, test.wantError)
				}
			} else if err != nil {
				t.Fatalf("ReplaceProfile() unexpected error: %v", err)
			}
			if (repo.replaceCalled > 0) != test.wantRepositoryCall {
				t.Errorf("repository calls = %d, want called = %v", repo.replaceCalled, test.wantRepositoryCall)
			}
			if (deleter.calls > 0) != test.wantDeleteCall {
				t.Errorf("delete calls = %d, want called = %v", deleter.calls, test.wantDeleteCall)
			}
			if test.wantDeleteCall && deleter.objectKey != oldImageObjectKey {
				t.Errorf("deleted object = %q, want %q", deleter.objectKey, oldImageObjectKey)
			}
		})
	}
}

func TestFundraiserServiceDeleteProfile(t *testing.T) {
	imageObjectKey := "profiles/old.png"
	repositoryFailure := errors.New("repository failure")
	deleteFailure := errors.New("storage failure")

	tests := []struct {
		name               string
		found              bool
		repositoryError    error
		deleteError        error
		deleteResult       repository.DeleteFundraiserProfileResult
		wantError          error
		wantDeleteCall     bool
		wantRepositoryCall bool
	}{
		{
			name:  "deletes profile and removes unreferenced image",
			found: true,
			deleteResult: repository.DeleteFundraiserProfileResult{
				ImageObjectKey:        &imageObjectKey,
				DeleteImageObjectFile: true,
			},
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{
			name:  "does not fail after an image delete failure",
			found: true,
			deleteResult: repository.DeleteFundraiserProfileResult{
				ImageObjectKey:        &imageObjectKey,
				DeleteImageObjectFile: true,
			},
			deleteError:        deleteFailure,
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{name: "returns profile not found", wantError: ErrProfileNotFound, wantRepositoryCall: true},
		{name: "rejects fundraiser with active campaigns", found: true, repositoryError: repository.ErrFundraiserHasActiveCampaigns, wantError: ErrActiveCampaignsExist, wantRepositoryCall: true},
		{name: "wraps unexpected repository failure", found: true, repositoryError: repositoryFailure, wantError: repositoryFailure, wantRepositoryCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			deleter := &stubFundraiserObjectDeleter{err: test.deleteError}
			repo := &stubFundraiserRepository{
				delete: func(gotCtx context.Context, walletAddress string) (repository.DeleteFundraiserProfileResult, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					return test.deleteResult, test.found, test.repositoryError
				},
			}
			fundraiserService := NewFundraiserService(repo, nil, deleter)
			err := fundraiserService.DeleteProfile(ctx, " 0xWalletChecksum ")

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("DeleteProfile() error = %v, want %v", err, test.wantError)
				}
			} else if err != nil {
				t.Fatalf("DeleteProfile() unexpected error: %v", err)
			}
			if (repo.deleteCalled > 0) != test.wantRepositoryCall {
				t.Errorf("repository calls = %d, want called = %v", repo.deleteCalled, test.wantRepositoryCall)
			}
			if (deleter.calls > 0) != test.wantDeleteCall {
				t.Errorf("delete calls = %d, want called = %v", deleter.calls, test.wantDeleteCall)
			}
			if test.wantDeleteCall && deleter.objectKey != imageObjectKey {
				t.Errorf("deleted object = %q, want %q", deleter.objectKey, imageObjectKey)
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
