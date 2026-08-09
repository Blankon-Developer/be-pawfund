package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
)

type PostgresFundraiserRepository struct {
	db *sql.DB
}

func NewPostgresFundraiserRepository(db *sql.DB) *PostgresFundraiserRepository {
	return &PostgresFundraiserRepository{db: db}
}

func (r *PostgresFundraiserRepository) Create(ctx context.Context, fundraiser domain.Fundraiser) (domain.Fundraiser, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Fundraiser{}, fmt.Errorf("repository: begin create fundraiser transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const insertUser = `
		INSERT INTO users (id, role, email, wallet_address)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at
		`

	err = tx.QueryRowContext(ctx, insertUser, fundraiser.ID, fundraiser.Role, fundraiser.Email, fundraiser.WalletAddress).Scan(&fundraiser.CreatedAt)
	if err != nil {
		return domain.Fundraiser{}, mapPostgresError("insert user", err)
	}

	const insertFundraiser = `
		INSERT INTO fundraisers (id, name, image_object_key, contact_name, contact_phone, social_url, country, zip_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`

	_, err = tx.ExecContext(ctx, insertFundraiser, fundraiser.ID, fundraiser.Name, fundraiser.ImageObjectKey, fundraiser.ContactName, fundraiser.ContactPhone, fundraiser.SocialURL, fundraiser.Country, fundraiser.ZipCode)
	if err != nil {
		return domain.Fundraiser{}, mapPostgresError("insert fundraiser", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Fundraiser{}, fmt.Errorf("repository: commit create fundraiser transaction: %w", err)
	}

	return fundraiser, nil
}

func (r *PostgresFundraiserRepository) FindByWalletAddress(
	ctx context.Context,
	walletAddress string,
) (domain.Fundraiser, bool, error) {
	const query = `
		SELECT
			u.id,
			u.role,
			u.email,
			u.wallet_address,
			u.created_at,
			f.name,
			f.image_object_key,
			f.contact_name,
			f.contact_phone,
			f.social_url,
			f.country,
			f.zip_code
		FROM users u
		JOIN fundraisers f ON f.id = u.id
		WHERE u.role = 'fundraiser'
			AND LOWER(u.wallet_address) = LOWER($1)
	`

	var fundraiser domain.Fundraiser
	var imageObjectKey sql.NullString
	var socialURL sql.NullString
	err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(walletAddress)).Scan(
		&fundraiser.ID,
		&fundraiser.Role,
		&fundraiser.Email,
		&fundraiser.WalletAddress,
		&fundraiser.CreatedAt,
		&fundraiser.Name,
		&imageObjectKey,
		&fundraiser.ContactName,
		&fundraiser.ContactPhone,
		&socialURL,
		&fundraiser.Country,
		&fundraiser.ZipCode,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Fundraiser{}, false, nil
		}
		return domain.Fundraiser{}, false, fmt.Errorf("repository: find fundraiser by wallet address: %w", err)
	}

	if imageObjectKey.Valid {
		fundraiser.ImageObjectKey = &imageObjectKey.String
	}
	if socialURL.Valid {
		fundraiser.SocialURL = &socialURL.String
	}

	return fundraiser, true, nil
}
