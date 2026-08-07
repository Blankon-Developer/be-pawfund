package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

type PostgresAuthRepository struct {
	db *sql.DB
}

func NewPostgresAuthRepository(db *sql.DB) *PostgresAuthRepository {
	return &PostgresAuthRepository{db: db}
}

func (r *PostgresAuthRepository) FindProfileByWalletAddress(
	ctx context.Context,
	walletAddress string,
) (domain.AuthProfile, bool, error) {
	const query = `
		SELECT
			u.role,
			COALESCE(s.name, f.name),
			COALESCE(s.image_object_key, f.image_object_key)
		FROM users u
		LEFT JOIN supporters s
			ON s.id = u.id AND u.role = 'supporter'
		LEFT JOIN fundraisers f
			ON f.id = u.id AND u.role = 'fundraiser'
		WHERE LOWER(u.wallet_address) = LOWER($1)
	`

	var profile domain.AuthProfile
	var name sql.NullString
	var imageObjectKey sql.NullString
	if err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(walletAddress)).Scan(
		&profile.Role,
		&name,
		&imageObjectKey,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthProfile{}, false, nil
		}
		return domain.AuthProfile{}, false, fmt.Errorf("repository: find auth profile: %w", err)
	}

	if profile.Role != domain.UserRoleSupporter && profile.Role != domain.UserRoleFundraiser {
		return domain.AuthProfile{}, false, fmt.Errorf(
			"repository: find auth profile: unsupported role %q",
			profile.Role,
		)
	}
	if !name.Valid || strings.TrimSpace(name.String) == "" {
		return domain.AuthProfile{}, false, fmt.Errorf(
			"repository: find auth profile: %s profile is missing",
			profile.Role,
		)
	}

	profile.Name = name.String
	if imageObjectKey.Valid {
		profile.ImageObjectKey = &imageObjectKey.String
	}

	return profile, true, nil
}
