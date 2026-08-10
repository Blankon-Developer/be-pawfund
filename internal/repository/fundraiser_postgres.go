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

func (r *PostgresFundraiserRepository) ReplaceProfile(
	ctx context.Context,
	walletAddress string,
	profile domain.FundraiserProfileReplacement,
) (ReplaceFundraiserProfileResult, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReplaceFundraiserProfileResult{}, false, fmt.Errorf("repository: begin replace fundraiser profile transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const lockFundraiser = `
		SELECT u.id, f.image_object_key
		FROM users u
		JOIN fundraisers f ON f.id = u.id
		WHERE u.role = 'fundraiser'
			AND LOWER(u.wallet_address) = LOWER($1)
		FOR UPDATE OF u, f
	`

	var (
		fundraiser         domain.Fundraiser
		currentImageObject sql.NullString
	)
	if err := tx.QueryRowContext(ctx, lockFundraiser, strings.TrimSpace(walletAddress)).Scan(
		&fundraiser.ID,
		&currentImageObject,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReplaceFundraiserProfileResult{}, false, nil
		}
		return ReplaceFundraiserProfileResult{}, false, fmt.Errorf("repository: lock fundraiser profile for replacement: %w", err)
	}

	const replaceUserEmail = `UPDATE users SET email = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, replaceUserEmail, profile.Email, fundraiser.ID); err != nil {
		return ReplaceFundraiserProfileResult{}, false, mapPostgresError("replace fundraiser user email", err)
	}

	const replaceFundraiser = `
		UPDATE fundraisers
		SET name = $1,
			contact_name = $2,
			contact_phone = $3,
			social_url = $4,
			country = $5,
			zip_code = $6
		WHERE id = $7
	`
	if _, err := tx.ExecContext(
		ctx,
		replaceFundraiser,
		profile.Name,
		profile.ContactName,
		profile.ContactPhone,
		profile.SocialURL,
		profile.Country,
		profile.ZipCode,
		fundraiser.ID,
	); err != nil {
		return ReplaceFundraiserProfileResult{}, false, mapPostgresError("replace fundraiser profile", err)
	}

	if profile.ImageObjectKey.Set {
		const replaceImageObjectKey = `UPDATE fundraisers SET image_object_key = $1 WHERE id = $2`
		if _, err := tx.ExecContext(ctx, replaceImageObjectKey, profile.ImageObjectKey.Value, fundraiser.ID); err != nil {
			return ReplaceFundraiserProfileResult{}, false, mapPostgresError("replace fundraiser image object key", err)
		}
	}

	result := ReplaceFundraiserProfileResult{}
	if profile.ImageObjectKey.Set && currentImageObject.Valid && imageObjectKeyChanged(currentImageObject.String, profile.ImageObjectKey.Value) {
		const imageStillReferenced = `
			SELECT EXISTS (
				SELECT 1 FROM fundraisers WHERE image_object_key = $1
				UNION ALL
				SELECT 1 FROM supporters WHERE image_object_key = $1
			)
		`
		var referenced bool
		if err := tx.QueryRowContext(ctx, imageStillReferenced, currentImageObject.String).Scan(&referenced); err != nil {
			return ReplaceFundraiserProfileResult{}, false, fmt.Errorf("repository: check old fundraiser image references: %w", err)
		}
		if !referenced {
			oldImageObjectKey := currentImageObject.String
			result.OldImageObjectKey = &oldImageObjectKey
			result.DeleteOldImageFile = true
		}
	}

	if err := tx.Commit(); err != nil {
		return ReplaceFundraiserProfileResult{}, false, fmt.Errorf("repository: commit replace fundraiser profile transaction: %w", err)
	}

	return result, true, nil
}

func imageObjectKeyChanged(current string, updated *string) bool {
	return updated == nil || current != *updated
}
