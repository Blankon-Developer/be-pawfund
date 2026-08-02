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
