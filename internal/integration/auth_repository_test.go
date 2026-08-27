//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/google/uuid"
)

func TestPostgresAuthRepositoryFindProfileByWalletAddress(t *testing.T) {
	imageKey := "profiles/cat.png"
	supporterWallet := "0x123456789012345678901234567890123456789A"
	fundraiserWallet := "0x223456789012345678901234567890123456789B"

	tests := []struct {
		name      string
		prepare   func(t *testing.T)
		address   string
		wantFound bool
		wantName  string
		wantRole  domain.UserRole
		wantImage *string
		wantError bool
	}{
		{
			name: "finds supporter case-insensitively",
			prepare: func(t *testing.T) {
				repo := database.NewPostgresSupporterRepository(testDatabase)
				supporter := newSupporter("supporter@example.com", supporterWallet, &imageKey)
				supporter.Name = "Cat Lover"
				mustCreateSupporter(t, repo, supporter)
			},
			address:   strings.ToLower(supporterWallet),
			wantFound: true,
			wantName:  "Cat Lover",
			wantRole:  domain.UserRoleSupporter,
			wantImage: &imageKey,
		},
		{
			name: "finds fundraiser without image",
			prepare: func(t *testing.T) {
				mustCreateFundraiserProfile(t, fundraiserWallet, nil)
			},
			address:   fundraiserWallet,
			wantFound: true,
			wantName:  "Paw Rescue",
			wantRole:  domain.UserRoleFundraiser,
		},
		{
			name:      "returns not found for unregistered wallet",
			address:   "0x323456789012345678901234567890123456789C",
			wantFound: false,
		},
		{
			name: "rejects user without role profile",
			prepare: func(t *testing.T) {
				if _, err := testDatabase.ExecContext(
					t.Context(),
					`INSERT INTO users (id, role, email, wallet_address) VALUES ($1, $2, $3, $4)`,
					uuid.New(),
					domain.UserRoleSupporter,
					"broken@example.com",
					"0x423456789012345678901234567890123456789D",
				); err != nil {
					t.Fatalf("prepare inconsistent user: %v", err)
				}
			},
			address:   "0x423456789012345678901234567890123456789D",
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := database.NewPostgresAuthRepository(testDatabase)
			profile, found, err := repo.FindProfileByWalletAddress(t.Context(), test.address)
			if test.wantError {
				if err == nil {
					t.Fatal("FindProfileByWalletAddress() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("FindProfileByWalletAddress() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if profile.Name != test.wantName || profile.Role != test.wantRole {
				t.Errorf("profile = %#v", profile)
			}
			if !equalStringPointers(profile.ImageObjectKey, test.wantImage) {
				t.Errorf("image key = %v, want %v", profile.ImageObjectKey, test.wantImage)
			}
		})
	}
}

func mustCreateFundraiserProfile(t *testing.T, walletAddress string, imageObjectKey *string) {
	t.Helper()
	id := uuid.New()
	tx, err := testDatabase.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin fundraiser fixture: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(
		t.Context(),
		`INSERT INTO users (id, role, email, wallet_address) VALUES ($1, $2, $3, $4)`,
		id,
		domain.UserRoleFundraiser,
		"fundraiser@example.com",
		walletAddress,
	); err != nil {
		t.Fatalf("insert fundraiser user: %v", err)
	}
	if _, err := tx.ExecContext(
		t.Context(),
		`INSERT INTO fundraisers (
			id, name, image_object_key, contact_name, contact_phone, country, zip_code
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id,
		"Paw Rescue",
		imageObjectKey,
		"Contact",
		"+620000000",
		"Indonesia",
		"12345",
	); err != nil {
		t.Fatalf("insert fundraiser profile: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit fundraiser fixture: %v", err)
	}
}
