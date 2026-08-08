package repository

import (
	"context"
	"database/sql"
	"fmt"

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
