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
