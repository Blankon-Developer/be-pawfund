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

func TestPostgresFundraiserRepositoryCreate(t *testing.T) {
	repo := repository.NewPostgresFundraiserRepository(testDatabase)

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		fundraiser    domain.Fundraiser
		context       func() context.Context
		wantError     error
		wantAnyError  bool
		wantPersisted bool
	}{
		{
			name:          "persists fundraiser with image",
			fundraiser:    newFundraiser("rescue@example.com", "0xrescue", stringPointer("profiles/rescue.png")),
			wantPersisted: true,
		},
		{
			name:          "persists SQL null for omitted image",
			fundraiser:    newFundraiser("shelter@example.com", "0xshelter", nil),
			wantPersisted: true,
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				mustCreateFundraiser(t, repo, newFundraiser("duplicate@example.com", "0xfirst", nil))
			},
			fundraiser: newFundraiser("duplicate@example.com", "0xsecond", nil),
			wantError:  repository.ErrEmailAlreadyExists,
		},
		{
			name: "maps duplicate wallet constraint",
			prepare: func(t *testing.T) {
				mustCreateFundraiser(t, repo, newFundraiser("first@example.com", "0xduplicate", nil))
			},
			fundraiser: newFundraiser("second@example.com", "0xduplicate", nil),
			wantError:  repository.ErrWalletAlreadyExists,
		},
		{
			name: "rolls back user when fundraiser insert fails",
			fundraiser: func() domain.Fundraiser {
				fundraiser := newFundraiser("rollback@example.com", "0xrollback", nil)
				fundraiser.Name = strings.Repeat("n", 256)
				return fundraiser
			}(),
			wantAnyError: true,
		},
		{
			name:       "honors canceled context",
			fundraiser: newFundraiser("canceled@example.com", "0xcanceled", nil),
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
			created, err := repo.Create(ctx, test.fundraiser)

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
					t.Error("created fundraiser has zero CreatedAt")
				}
			}

			assertFundraiserPersistence(t, test.fundraiser, test.wantPersisted)
		})
	}
}

func TestPostgresFundraiserRepositoryFindByWalletAddress(t *testing.T) {
	imageKey := "profiles/rescue.png"
	const fundraiserWallet = "0xFundraiserChecksum"

	tests := []struct {
		name       string
		prepare    func(t *testing.T)
		address    string
		wantFound  bool
		wantImage  *string
		wantSocial *string
	}{
		{
			name: "finds fundraiser case-insensitively",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &imageKey))
			},
			address:    strings.ToLower(fundraiserWallet),
			wantFound:  true,
			wantImage:  &imageKey,
			wantSocial: stringPointer("https://example.com/rescue"),
		},
		{
			name: "returns nullable profile fields",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				fundraiser := newFundraiser("shelter@example.com", fundraiserWallet, nil)
				fundraiser.SocialURL = nil
				mustCreateFundraiser(t, repo, fundraiser)
			},
			address:   fundraiserWallet,
			wantFound: true,
		},
		{
			name: "does not return supporter profile",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("supporter@example.com", fundraiserWallet, nil))
			},
			address: fundraiserWallet,
		},
		{
			name:    "returns not found for unregistered wallet",
			address: fundraiserWallet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := repository.NewPostgresFundraiserRepository(testDatabase)
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
			if profile.Role != domain.UserRoleFundraiser || profile.WalletAddress != fundraiserWallet {
				t.Errorf("profile identity = %#v", profile.User)
			}
			if profile.Name != "Animal Rescue" || profile.Email == "" || profile.CreatedAt.IsZero() {
				t.Errorf("profile user fields = %#v", profile)
			}
			if profile.ContactName != "Jane Doe" || profile.ContactPhone != "+62 812 3456" || profile.Country != "Indonesia" || profile.ZipCode != "10110" {
				t.Errorf("profile details = %#v", profile)
			}
			if !equalStringPointers(profile.ImageObjectKey, test.wantImage) {
				t.Errorf("image key = %v, want %v", profile.ImageObjectKey, test.wantImage)
			}
			if !equalStringPointers(profile.SocialURL, test.wantSocial) {
				t.Errorf("social URL = %v, want %v", profile.SocialURL, test.wantSocial)
			}
		})
	}
}

func TestPostgresFundraiserRepositoryReplaceProfile(t *testing.T) {
	const fundraiserWallet = "0xFundraiserChecksum"
	oldImageObjectKey := "profiles/old.png"
	newImageObjectKey := "profiles/new.png"

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		walletAddress string
		profile       domain.FundraiserProfileReplacement
		wantFound     bool
		wantError     error
		wantDeleteOld bool
		assertProfile func(t *testing.T, profile domain.Fundraiser)
	}{
		{
			name: "replaces every profile field and clears image",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &oldImageObjectKey))
			},
			walletAddress: strings.ToLower(fundraiserWallet),
			profile: domain.FundraiserProfileReplacement{
				Name: "Updated Rescue", Email: "updated@example.com", ContactName: "Updated Contact", ContactPhone: "+62 811 9999", SocialURL: "https://example.com/updated", Country: "Malaysia", ZipCode: "50450",
				ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true},
			},
			wantFound:     true,
			wantDeleteOld: true,
			assertProfile: func(t *testing.T, profile domain.Fundraiser) {
				t.Helper()
				if profile.Email != "updated@example.com" || profile.Name != "Updated Rescue" || profile.ContactName != "Updated Contact" || profile.ContactPhone != "+62 811 9999" || profile.SocialURL == nil || *profile.SocialURL != "https://example.com/updated" || profile.Country != "Malaysia" || profile.ZipCode != "50450" {
					t.Errorf("replaced profile fields = %#v", profile)
				}
				if profile.ImageObjectKey != nil {
					t.Errorf("cleared image = %#v", profile.ImageObjectKey)
				}
			},
		},
		{
			name: "does not mark shared old image for deletion",
			prepare: func(t *testing.T) {
				fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
				supporterRepo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", fundraiserWallet, &oldImageObjectKey))
				mustCreateSupporter(t, supporterRepo, newSupporter("supporter@example.com", "0xSupporter", &oldImageObjectKey))
			},
			walletAddress: fundraiserWallet,
			profile: domain.FundraiserProfileReplacement{
				Name: "Updated Rescue", Email: "updated@example.com", ContactName: "Updated Contact", ContactPhone: "+62 811 9999", SocialURL: "https://example.com/updated", Country: "Indonesia", ZipCode: "10110",
				ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true, Value: &newImageObjectKey},
			},
			wantFound: true,
			assertProfile: func(t *testing.T, profile domain.Fundraiser) {
				t.Helper()
				if !equalStringPointers(profile.ImageObjectKey, &newImageObjectKey) || profile.Email != "updated@example.com" || profile.Name != "Updated Rescue" || profile.ContactName != "Updated Contact" {
					t.Errorf("replaced profile = %#v", profile)
				}
			},
		},
		{
			name: "preserves image when replacement omits its key",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &oldImageObjectKey))
			},
			walletAddress: fundraiserWallet,
			profile: domain.FundraiserProfileReplacement{
				Name: "Updated Rescue", Email: "updated@example.com", ContactName: "Updated Contact", ContactPhone: "+62 811 9999", SocialURL: "https://example.com/updated", Country: "Indonesia", ZipCode: "10110",
			},
			wantFound: true,
			assertProfile: func(t *testing.T, profile domain.Fundraiser) {
				t.Helper()
				if !equalStringPointers(profile.ImageObjectKey, &oldImageObjectKey) || profile.Name != "Updated Rescue" || profile.Email != "updated@example.com" {
					t.Errorf("replacement with preserved image = %#v", profile)
				}
			},
		},
		{
			name:          "returns not found for unknown wallet",
			walletAddress: "0xUnknown",
			profile:       domain.FundraiserProfileReplacement{Name: "New Name", Email: "new@example.com", ContactName: "Jane Doe", ContactPhone: "+62 812 3456", SocialURL: "https://example.com/rescue", Country: "Indonesia", ZipCode: "10110"},
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &oldImageObjectKey))
				mustCreateFundraiser(t, repo, newFundraiser("taken@example.com", "0xTaken", nil))
			},
			walletAddress: fundraiserWallet,
			profile:       domain.FundraiserProfileReplacement{Name: "Updated Rescue", Email: "taken@example.com", ContactName: "Updated Contact", ContactPhone: "+62 811 9999", SocialURL: "https://example.com/updated", Country: "Indonesia", ZipCode: "10110"},
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

			repo := repository.NewPostgresFundraiserRepository(testDatabase)
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

			profile, found, err := repo.FindByWalletAddress(t.Context(), fundraiserWallet)
			if err != nil || !found {
				t.Fatalf("FindByWalletAddress() = %#v, %v, %v", profile, found, err)
			}
			test.assertProfile(t, profile)
		})
	}
}

func newFundraiser(email, wallet string, imageObjectKey *string) domain.Fundraiser {
	socialURL := "https://example.com/rescue"
	return domain.Fundraiser{
		User: domain.User{
			ID:            uuid.New(),
			Role:          domain.UserRoleFundraiser,
			Email:         email,
			WalletAddress: wallet,
		},
		Name:           "Animal Rescue",
		ImageObjectKey: imageObjectKey,
		ContactName:    "Jane Doe",
		ContactPhone:   "+62 812 3456",
		SocialURL:      &socialURL,
		Country:        "Indonesia",
		ZipCode:        "10110",
	}
}

func mustCreateFundraiser(t *testing.T, repo *repository.PostgresFundraiserRepository, fundraiser domain.Fundraiser) {
	t.Helper()
	if _, err := repo.Create(t.Context(), fundraiser); err != nil {
		t.Fatalf("prepare fundraiser: %v", err)
	}
}

func assertFundraiserPersistence(t *testing.T, fundraiser domain.Fundraiser, wantPersisted bool) {
	t.Helper()

	var (
		email        string
		wallet       string
		name         string
		image        sql.NullString
		contactName  string
		contactPhone string
		socialURL    sql.NullString
		country      string
		zipCode      string
	)
	err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT u.email, u.wallet_address, f.name, f.image_object_key,
		        f.contact_name, f.contact_phone, f.social_url, f.country, f.zip_code
		 FROM users u
		 JOIN fundraisers f ON f.id = u.id
		 WHERE u.id = $1`,
		fundraiser.ID,
	).Scan(&email, &wallet, &name, &image, &contactName, &contactPhone, &socialURL, &country, &zipCode)

	if !wantPersisted {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("persisted row error = %v, want sql.ErrNoRows", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("query persisted fundraiser: %v", err)
	}
	if email != fundraiser.Email || wallet != fundraiser.WalletAddress || name != fundraiser.Name {
		t.Errorf("persisted fundraiser identity = %q/%q/%q", email, wallet, name)
	}
	if contactName != fundraiser.ContactName || contactPhone != fundraiser.ContactPhone || country != fundraiser.Country || zipCode != fundraiser.ZipCode {
		t.Errorf("persisted fundraiser profile = %q/%q/%q/%q", contactName, contactPhone, country, zipCode)
	}
	if !nullableStringMatches(image, fundraiser.ImageObjectKey) {
		t.Errorf("image object key = %#v, want %#v", image, fundraiser.ImageObjectKey)
	}
	if !nullableStringMatches(socialURL, fundraiser.SocialURL) {
		t.Errorf("social URL = %#v, want %#v", socialURL, fundraiser.SocialURL)
	}
}

func nullableStringMatches(value sql.NullString, want *string) bool {
	if want == nil {
		return !value.Valid
	}
	return value.Valid && value.String == *want
}
