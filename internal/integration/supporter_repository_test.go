//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

func TestPostgresSupporterRepositoryCreate(t *testing.T) {
	repo := repository.NewPostgresSupporterRepository(testDatabase)

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		supporter     domain.Supporter
		context       func() context.Context
		wantError     error
		wantAnyError  bool
		wantPersisted bool
	}{
		{
			name:          "persists supporter with image",
			supporter:     newSupporter("cat@example.com", "0xcat", stringPointer("profiles/cat.png")),
			wantPersisted: true,
		},
		{
			name:          "persists SQL null for omitted image",
			supporter:     newSupporter("dog@example.com", "0xdog", nil),
			wantPersisted: true,
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("duplicate@example.com", "0xfirst", nil))
			},
			supporter: newSupporter("duplicate@example.com", "0xsecond", nil),
			wantError: repository.ErrEmailAlreadyExists,
		},
		{
			name: "maps duplicate wallet constraint",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("first@example.com", "0xduplicate", nil))
			},
			supporter: newSupporter("second@example.com", "0xduplicate", nil),
			wantError: repository.ErrWalletAlreadyExists,
		},
		{
			name: "rolls back user when supporter insert fails",
			supporter: func() domain.Supporter {
				supporter := newSupporter("rollback@example.com", "0xrollback", nil)
				supporter.Name = strings.Repeat("n", 256)
				return supporter
			}(),
			wantAnyError: true,
		},
		{
			name:      "honors canceled context",
			supporter: newSupporter("canceled@example.com", "0xcanceled", nil),
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantAnyError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			created, err := repo.Create(ctx, test.supporter)

			switch {
			case test.wantError != nil:
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Create() error = %v, want %v", err, test.wantError)
				}
			case test.wantAnyError:
				if err == nil {
					t.Fatal("Create() expected an error")
				}
			default:
				if err != nil {
					t.Fatalf("Create() unexpected error: %v", err)
				}
				if created.CreatedAt.IsZero() {
					t.Error("created supporter has zero CreatedAt")
				}
			}

			assertSupporterPersistence(t, test.supporter, test.wantPersisted)
		})
	}
}

func TestPostgresSupporterRepositoryFindByWalletAddress(t *testing.T) {
	imageKey := "profiles/cat.png"
	const supporterWallet = "0xSupporterChecksum"

	tests := []struct {
		name      string
		prepare   func(t *testing.T)
		address   string
		wantFound bool
		wantImage *string
	}{
		{
			name: "finds supporter case-insensitively",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &imageKey))
			},
			address:   strings.ToLower(supporterWallet),
			wantFound: true,
			wantImage: &imageKey,
		},
		{
			name: "returns nullable image field",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("dog@example.com", supporterWallet, nil))
			},
			address:   supporterWallet,
			wantFound: true,
		},
		{
			name: "does not return fundraiser profile",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", supporterWallet, nil))
			},
			address: supporterWallet,
		},
		{
			name:    "returns not found for unregistered wallet",
			address: supporterWallet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			profile, found, err := repo.FindByWalletAddress(t.Context(), test.address)
			if err != nil {
				t.Fatalf("FindByWalletAddress() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if profile.Role != domain.UserRoleSupporter || profile.WalletAddress != supporterWallet {
				t.Errorf("profile identity = %#v", profile.User)
			}
			if profile.Name != "Supporter" || profile.Email == "" || profile.CreatedAt.IsZero() {
				t.Errorf("profile user fields = %#v", profile)
			}
			if !equalStringPointers(profile.ImageObjectKey, test.wantImage) {
				t.Errorf("image key = %v, want %v", profile.ImageObjectKey, test.wantImage)
			}
		})
	}
}

func TestPostgresSupporterRepositoryReplaceProfile(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	oldImageObjectKey := "profiles/supporter-old.png"
	newImageObjectKey := "profiles/supporter-new.png"

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		walletAddress string
		profile       domain.SupporterProfileReplacement
		wantFound     bool
		wantError     error
		wantDeleteOld bool
		assertProfile func(t *testing.T, profile domain.Supporter)
	}{
		{
			name: "replaces every profile field and clears image",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
			},
			walletAddress: strings.ToLower(supporterWallet),
			profile: domain.SupporterProfileReplacement{
				Name: "Updated Cat Lover", Email: "updated@example.com", ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true},
			},
			wantFound:     true,
			wantDeleteOld: true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || profile.ImageObjectKey != nil {
					t.Errorf("replaced profile = %#v", profile)
				}
			},
		},
		{
			name: "does not mark image shared by fundraiser for deletion",
			prepare: func(t *testing.T) {
				supporterRepo := repository.NewPostgresSupporterRepository(testDatabase)
				fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateSupporter(t, supporterRepo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
				mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &oldImageObjectKey))
			},
			walletAddress: supporterWallet,
			profile: domain.SupporterProfileReplacement{
				Name: "Updated Cat Lover", Email: "updated@example.com", ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true, Value: &newImageObjectKey},
			},
			wantFound: true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || !equalStringPointers(profile.ImageObjectKey, &newImageObjectKey) {
					t.Errorf("replaced profile = %#v", profile)
				}
			},
		},
		{
			name: "preserves image when replacement omits its key",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
			},
			walletAddress: supporterWallet,
			profile:       domain.SupporterProfileReplacement{Name: "Updated Cat Lover", Email: "updated@example.com"},
			wantFound:     true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || !equalStringPointers(profile.ImageObjectKey, &oldImageObjectKey) {
					t.Errorf("replacement with preserved image = %#v", profile)
				}
			},
		},
		{
			name:          "returns not found for unknown wallet",
			walletAddress: "0xUnknown",
			profile:       domain.SupporterProfileReplacement{Name: "New Name", Email: "new@example.com"},
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
				mustCreateFundraiser(t, repository.NewPostgresFundraiserRepository(testDatabase), newFundraiser("taken@example.com", "0xFundraiser", nil))
			},
			walletAddress: supporterWallet,
			profile:       domain.SupporterProfileReplacement{Name: "Updated Cat Lover", Email: "taken@example.com"},
			wantError:     repository.ErrEmailAlreadyExists,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			result, found, err := repo.ReplaceProfile(t.Context(), test.walletAddress, test.profile)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ReplaceProfile() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReplaceProfile() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if result.DeleteOldImageFile != test.wantDeleteOld {
				t.Errorf("DeleteOldImageFile = %v, want %v", result.DeleteOldImageFile, test.wantDeleteOld)
			}
			if test.wantDeleteOld && !equalStringPointers(result.OldImageObjectKey, &oldImageObjectKey) {
				t.Errorf("OldImageObjectKey = %v, want %v", result.OldImageObjectKey, &oldImageObjectKey)
			}

			profile, found, err := repo.FindByWalletAddress(t.Context(), supporterWallet)
			if err != nil || !found {
				t.Fatalf("FindByWalletAddress() = %#v, %v, %v", profile, found, err)
			}
			test.assertProfile(t, profile)
		})
	}
}

func TestPostgresSupporterRepositoryDeleteProfile(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	imageObjectKey := "profiles/delete-supporter.png"

	tests := []struct {
		name                string
		createProfile       bool
		shareImage          bool
		wantFound           bool
		wantDeleteImage     bool
		wantDeleted         bool
		allowReregistration bool
	}{
		{
			name:                "soft deletes profile and allows fresh registration",
			createProfile:       true,
			wantFound:           true,
			wantDeleteImage:     true,
			wantDeleted:         true,
			allowReregistration: true,
		},
		{
			name:          "keeps image shared by an active fundraiser",
			createProfile: true,
			shareImage:    true,
			wantFound:     true,
			wantDeleted:   true,
		},
		{name: "returns not found for unknown wallet"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			supporter := newSupporter("supporter@example.com", supporterWallet, &imageObjectKey)
			if test.createProfile {
				mustCreateSupporter(t, repo, supporter)
				if test.shareImage {
					fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
					mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &imageObjectKey))
				}
			}

			result, found, err := repo.DeleteProfile(t.Context(), strings.ToLower(supporterWallet))
			if err != nil {
				t.Fatalf("DeleteProfile() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if result.DeleteImageObjectFile != test.wantDeleteImage {
				t.Errorf("DeleteImageObjectFile = %v, want %v", result.DeleteImageObjectFile, test.wantDeleteImage)
			}
			if test.wantDeleteImage && !equalStringPointers(result.ImageObjectKey, &imageObjectKey) {
				t.Errorf("ImageObjectKey = %v, want %v", result.ImageObjectKey, imageObjectKey)
			}
			if !test.createProfile {
				return
			}

			var deletedAt sql.NullTime
			var storedImage sql.NullString
			if err := testDatabase.QueryRowContext(
				t.Context(),
				`SELECT u.deleted_at, s.image_object_key
				 FROM users u
				 JOIN supporters s ON s.id = u.id
				 WHERE u.id = $1`,
				supporter.ID,
			).Scan(&deletedAt, &storedImage); err != nil {
				t.Fatalf("query deleted supporter: %v", err)
			}
			if deletedAt.Valid != test.wantDeleted {
				t.Errorf("deleted_at valid = %v, want %v", deletedAt.Valid, test.wantDeleted)
			}
			if test.wantDeleted && storedImage.Valid {
				t.Errorf("deleted profile image = %q, want NULL", storedImage.String)
			}
			if !test.wantDeleted {
				return
			}

			if profile, active, err := repo.FindByWalletAddress(t.Context(), supporterWallet); err != nil || active {
				t.Errorf("FindByWalletAddress() after delete = %#v, %v, %v", profile, active, err)
			}
			authRepo := repository.NewPostgresAuthRepository(testDatabase)
			if profile, active, err := authRepo.FindProfileByWalletAddress(t.Context(), supporterWallet); err != nil || active {
				t.Errorf("FindProfileByWalletAddress() after delete = %#v, %v, %v", profile, active, err)
			}

			if test.allowReregistration {
				created, err := repo.Create(t.Context(), newSupporter("supporter@example.com", supporterWallet, nil))
				if err != nil {
					t.Fatalf("re-register supporter: %v", err)
				}
				if created.ID == supporter.ID {
					t.Error("re-registered supporter reused deleted ID")
				}
			}
		})
	}
}

func newSupporter(email, wallet string, imageObjectKey *string) domain.Supporter {
	return domain.Supporter{
		User: domain.User{
			ID:            uuid.New(),
			Role:          domain.UserRoleSupporter,
			Email:         email,
			WalletAddress: wallet,
		},
		Name:           "Supporter",
		ImageObjectKey: imageObjectKey,
	}
}

func mustCreateSupporter(t *testing.T, repo *repository.PostgresSupporterRepository, supporter domain.Supporter) {
	t.Helper()
	if _, err := repo.Create(t.Context(), supporter); err != nil {
		t.Fatalf("prepare supporter: %v", err)
	}
}

func assertSupporterPersistence(t *testing.T, supporter domain.Supporter, wantPersisted bool) {
	t.Helper()

	var (
		email  string
		wallet string
		name   string
		image  sql.NullString
	)
	err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT u.email, u.wallet_address, s.name, s.image_object_key
		 FROM users u
		 JOIN supporters s ON s.id = u.id
		 WHERE u.id = $1`,
		supporter.ID,
	).Scan(&email, &wallet, &name, &image)

	if !wantPersisted {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("persisted row error = %v, want sql.ErrNoRows", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("query persisted supporter: %v", err)
	}
	if email != supporter.Email || wallet != supporter.WalletAddress || name != supporter.Name {
		t.Errorf("persisted supporter = %q/%q/%q", email, wallet, name)
	}
	if supporter.ImageObjectKey == nil {
		if image.Valid {
			t.Errorf("image object key = %q, want SQL NULL", image.String)
		}
	} else if !image.Valid || image.String != *supporter.ImageObjectKey {
		t.Errorf("image object key = %#v, want %q", image, *supporter.ImageObjectKey)
	}
}

func stringPointer(value string) *string {
	return &value
}
