package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

type stubSupporterRepository struct {
	create        func(context.Context, domain.Supporter) (domain.Supporter, error)
	find          func(context.Context, string) (domain.Supporter, bool, error)
	listDonations func(context.Context, string, domain.DonationListOptions) ([]domain.Donation, bool, error)
	replace       func(context.Context, string, domain.SupporterProfileReplacement) (repository.ReplaceSupporterProfileResult, bool, error)
	delete        func(context.Context, string) (repository.DeleteSupporterProfileResult, bool, error)
	called        int
	findCalled    int
	listCalled    int
	replaceCalled int
	deleteCalled  int
}

type stubSupporterObjectDeleter struct {
	err       error
	calls     int
	objectKey string
}

func (s *stubSupporterObjectDeleter) Delete(_ context.Context, objectKey string) error {
	s.calls++
	s.objectKey = objectKey
	return s.err
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

func (s *stubSupporterRepository) ListDonationsByWalletAddress(
	ctx context.Context,
	walletAddress string,
	options domain.DonationListOptions,
) ([]domain.Donation, bool, error) {
	s.listCalled++
	if s.listDonations == nil {
		return nil, false, nil
	}
	return s.listDonations(ctx, walletAddress, options)
}

func (s *stubSupporterRepository) ReplaceProfile(
	ctx context.Context,
	walletAddress string,
	profile domain.SupporterProfileReplacement,
) (repository.ReplaceSupporterProfileResult, bool, error) {
	s.replaceCalled++
	if s.replace == nil {
		return repository.ReplaceSupporterProfileResult{}, false, nil
	}
	return s.replace(ctx, walletAddress, profile)
}

func (s *stubSupporterRepository) DeleteProfile(
	ctx context.Context,
	walletAddress string,
) (repository.DeleteSupporterProfileResult, bool, error) {
	s.deleteCalled++
	if s.delete == nil {
		return repository.DeleteSupporterProfileResult{}, false, nil
	}
	return s.delete(ctx, walletAddress)
}

func TestSupporterServiceRegister(t *testing.T) {
	fixedID := uuid.MustParse("0198f1a8-c0c0-7e1a-a604-d2b6942fc011")
	fixedTime := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	idFailure := errors.New("ID failure")
	repositoryFailure := errors.New("repository failure")
	imageKey := "tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"

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
					if supporter.ImageObjectKey == nil || *supporter.ImageObjectKey != "profiles/0198a123-4567-7abc-8123-456789abcdef.jpg" {
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
			supporterService := NewSupporterService(repo, generator, nil, nil)

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

func TestSupporterServiceRegisterImageObjectKey(t *testing.T) {
	fixedID := uuid.MustParse("0198f1a8-c0c0-7e1a-a604-d2b6942fc011")
	stagingKey := "tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	canonicalKey := "profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	invalidKey := "profiles/cat.png"

	tests := []struct {
		name               string
		imageObjectKey     *string
		promoteError       error
		repositoryError    error
		wantError          error
		wantPromote        bool
		wantDiscard        bool
		wantRepositoryCall bool
		wantPersistedKey   string
	}{
		{
			name:               "promotes staged image before persist then discards staging",
			imageObjectKey:     &stagingKey,
			wantPromote:        true,
			wantDiscard:        true,
			wantRepositoryCall: true,
			wantPersistedKey:   canonicalKey,
		},
		{
			name:           "rejects canonical image key",
			imageObjectKey: &invalidKey,
			wantError:      ErrInvalidImageObjectKey,
		},
		{
			name:           "maps missing staged object",
			imageObjectKey: &stagingKey,
			promoteError:   storage.ErrObjectNotFound,
			wantError:      ErrImageObjectNotFound,
			wantPromote:    true,
		},
		{
			name:               "keeps staging object when persist fails",
			imageObjectKey:     &stagingKey,
			repositoryError:    repository.ErrEmailAlreadyExists,
			wantError:          ErrEmailAlreadyRegistered,
			wantPromote:        true,
			wantRepositoryCall: true,
			wantPersistedKey:   canonicalKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			promoter := &stubObjectPromoter{err: test.promoteError}
			repo := &stubSupporterRepository{
				create: func(_ context.Context, supporter domain.Supporter) (domain.Supporter, error) {
					if promoter.calls != 1 {
						t.Errorf("promoter calls before persist = %d", promoter.calls)
					}
					if promoter.discardCalls != 0 {
						t.Errorf("discard calls before persist = %d", promoter.discardCalls)
					}
					if supporter.ImageObjectKey == nil || *supporter.ImageObjectKey != test.wantPersistedKey {
						t.Errorf("persisted key = %#v, want %q", supporter.ImageObjectKey, test.wantPersistedKey)
					}
					if test.repositoryError != nil {
						return domain.Supporter{}, test.repositoryError
					}
					return supporter, nil
				},
			}
			supporterService := NewSupporterService(repo, func() (uuid.UUID, error) {
				return fixedID, nil
			}, nil, promoter)

			_, err := supporterService.Register(t.Context(), RegisterSupporterInput{
				Name:           "Cat Lover",
				Email:          "cat@example.com",
				WalletAddress:  "0xWallet",
				ImageObjectKey: test.imageObjectKey,
			})
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Register() error = %v, want %v", err, test.wantError)
				}
			} else if err != nil {
				t.Fatalf("Register() unexpected error: %v", err)
			}
			if (promoter.calls > 0) != test.wantPromote {
				t.Errorf("promoter calls = %d, want called = %v", promoter.calls, test.wantPromote)
			}
			if test.wantPromote && (promoter.sourceKey != stagingKey || promoter.destKey != canonicalKey) {
				t.Errorf("promote %q → %q", promoter.sourceKey, promoter.destKey)
			}
			if (promoter.discardCalls > 0) != test.wantDiscard {
				t.Errorf("discard calls = %d, want called = %v", promoter.discardCalls, test.wantDiscard)
			}
			if test.wantDiscard && promoter.discardedKey != stagingKey {
				t.Errorf("discarded key = %q, want %q", promoter.discardedKey, stagingKey)
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
			supporterService := NewSupporterService(repo, nil, nil, nil)

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

func TestSupporterServiceListMyDonations(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	donations := []domain.Donation{{Amount: 2_500_000, TxHash: "0xTransaction"}}
	options := domain.DonationListOptions{Page: 2, PageSize: 25}

	tests := []struct {
		name            string
		found           bool
		repositoryError error
		wantError       error
	}{
		{name: "returns supporter donations", found: true},
		{name: "returns profile not found", wantError: ErrProfileNotFound},
		{name: "wraps repository failure", found: true, repositoryError: repositoryFailure, wantError: repositoryFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			repo := &stubSupporterRepository{
				listDonations: func(gotCtx context.Context, walletAddress string, gotOptions domain.DonationListOptions) ([]domain.Donation, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xSupporter" || gotOptions != options {
						t.Errorf("repository input = %q/%#v", walletAddress, gotOptions)
					}
					return donations, test.found, test.repositoryError
				},
			}
			supporterService := NewSupporterService(repo, nil, nil, nil)

			got, err := supporterService.ListMyDonations(ctx, " 0xSupporter ", options)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ListMyDonations() error = %v, want %v", err, test.wantError)
				}
				if test.repositoryError != nil && !strings.Contains(err.Error(), "list supporter donations") {
					t.Errorf("ListMyDonations() error lacks operation context: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("ListMyDonations() unexpected error: %v", err)
				}
				if len(got) != 1 || got[0].TxHash != "0xTransaction" {
					t.Errorf("donations = %#v", got)
				}
			}
			if repo.listCalled != 1 {
				t.Errorf("repository calls = %d, want 1", repo.listCalled)
			}
		})
	}
}

func TestSupporterServiceReplaceProfile(t *testing.T) {
	oldImageObjectKey := "profiles/old.png"
	repositoryFailure := errors.New("repository failure")
	deleteFailure := errors.New("storage failure")

	tests := []struct {
		name               string
		found              bool
		repositoryError    error
		deleteError        error
		replaceResult      repository.ReplaceSupporterProfileResult
		wantError          error
		wantDeleteCall     bool
		wantRepositoryCall bool
	}{
		{
			name:  "replaces normalized profile and removes old image",
			found: true,
			replaceResult: repository.ReplaceSupporterProfileResult{
				OldImageObjectKey:  &oldImageObjectKey,
				DeleteOldImageFile: true,
			},
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{
			name:  "does not fail after best effort image delete failure",
			found: true,
			replaceResult: repository.ReplaceSupporterProfileResult{
				OldImageObjectKey:  &oldImageObjectKey,
				DeleteOldImageFile: true,
			},
			deleteError:        deleteFailure,
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{name: "returns profile not found", wantError: ErrProfileNotFound, wantRepositoryCall: true},
		{name: "maps duplicate email", found: true, repositoryError: repository.ErrEmailAlreadyExists, wantError: ErrEmailAlreadyRegistered, wantRepositoryCall: true},
		{name: "wraps unexpected repository failure", found: true, repositoryError: repositoryFailure, wantError: repositoryFailure, wantRepositoryCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			deleter := &stubSupporterObjectDeleter{err: test.deleteError}
			repo := &stubSupporterRepository{
				replace: func(gotCtx context.Context, walletAddress string, profile domain.SupporterProfileReplacement) (repository.ReplaceSupporterProfileResult, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					if profile.Name != "Cat Lover" || profile.Email != "cat@example.com" {
						t.Errorf("normalized replacement = %#v", profile)
					}
					if !profile.ImageObjectKey.Set || profile.ImageObjectKey.Value == nil || *profile.ImageObjectKey.Value != "profiles/0198a123-4567-7abc-8123-456789abcdef.png" {
						t.Errorf("image replacement = %#v", profile.ImageObjectKey)
					}
					return test.replaceResult, test.found, test.repositoryError
				},
			}
			supporterService := NewSupporterService(repo, nil, deleter, nil)
			err := supporterService.ReplaceProfile(ctx, ReplaceSupporterProfileInput{
				WalletAddress: " 0xWalletChecksum ",
				Profile: domain.SupporterProfileReplacement{
					Name:  " Cat Lover ",
					Email: " CAT@EXAMPLE.COM ",
					ImageObjectKey: domain.ImageObjectKeyUpdate{
						Set:   true,
						Value: serviceStringPointer(" tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.png "),
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

func TestSupporterServiceDeleteProfile(t *testing.T) {
	imageObjectKey := "profiles/old.png"
	repositoryFailure := errors.New("repository failure")
	deleteFailure := errors.New("storage failure")

	tests := []struct {
		name               string
		found              bool
		repositoryError    error
		deleteError        error
		deleteResult       repository.DeleteSupporterProfileResult
		wantError          error
		wantDeleteCall     bool
		wantRepositoryCall bool
	}{
		{
			name:  "deletes profile and removes unreferenced image",
			found: true,
			deleteResult: repository.DeleteSupporterProfileResult{
				ImageObjectKey:        &imageObjectKey,
				DeleteImageObjectFile: true,
			},
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{
			name:  "does not fail after an image delete failure",
			found: true,
			deleteResult: repository.DeleteSupporterProfileResult{
				ImageObjectKey:        &imageObjectKey,
				DeleteImageObjectFile: true,
			},
			deleteError:        deleteFailure,
			wantDeleteCall:     true,
			wantRepositoryCall: true,
		},
		{name: "returns profile not found", wantError: ErrProfileNotFound, wantRepositoryCall: true},
		{name: "wraps unexpected repository failure", found: true, repositoryError: repositoryFailure, wantError: repositoryFailure, wantRepositoryCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
			deleter := &stubSupporterObjectDeleter{err: test.deleteError}
			repo := &stubSupporterRepository{
				delete: func(gotCtx context.Context, walletAddress string) (repository.DeleteSupporterProfileResult, bool, error) {
					if gotCtx.Value(contextKey{}) != "request-context" {
						t.Error("request context was not propagated")
					}
					if walletAddress != "0xWalletChecksum" {
						t.Errorf("wallet address = %q", walletAddress)
					}
					return test.deleteResult, test.found, test.repositoryError
				},
			}
			supporterService := NewSupporterService(repo, nil, deleter, nil)
			err := supporterService.DeleteProfile(ctx, " 0xWalletChecksum ")

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
