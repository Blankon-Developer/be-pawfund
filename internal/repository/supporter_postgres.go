package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	usersEmailConstraint  = "users_email_key"
	usersWalletConstraint = "users_wallet_address_key"
)

type PostgresSupporterRepository struct {
	db *sql.DB
}

func NewPostgresSupporterRepository(db *sql.DB) *PostgresSupporterRepository {
	return &PostgresSupporterRepository{db: db}
}

func (r *PostgresSupporterRepository) Create(ctx context.Context, supporter domain.Supporter) (domain.Supporter, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Supporter{}, fmt.Errorf("repository: begin create supporter transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const insertUser = `
		INSERT INTO users (id, role, email, wallet_address)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at
	`
	if err := tx.QueryRowContext(
		ctx,
		insertUser,
		supporter.ID,
		supporter.Role,
		supporter.Email,
		supporter.WalletAddress,
	).Scan(&supporter.CreatedAt); err != nil {
		return domain.Supporter{}, mapPostgresError("insert user", err)
	}

	const insertSupporter = `
		INSERT INTO supporters (id, name, image_object_key)
		VALUES ($1, $2, $3)
	`
	if _, err := tx.ExecContext(
		ctx,
		insertSupporter,
		supporter.ID,
		supporter.Name,
		supporter.ImageObjectKey,
	); err != nil {
		return domain.Supporter{}, mapPostgresError("insert supporter", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Supporter{}, fmt.Errorf("repository: commit create supporter transaction: %w", err)
	}

	return supporter, nil
}

func (r *PostgresSupporterRepository) FindByWalletAddress(
	ctx context.Context,
	walletAddress string,
) (domain.Supporter, bool, error) {
	const query = `
		SELECT
			u.id,
			u.role,
			u.email,
			u.wallet_address,
			u.created_at,
			s.name,
			s.image_object_key
		FROM users u
		JOIN supporters s ON s.id = u.id
		WHERE u.role = 'supporter'
			AND u.deleted_at IS NULL
			AND LOWER(u.wallet_address) = LOWER($1)
	`

	var supporter domain.Supporter
	var imageObjectKey sql.NullString
	err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(walletAddress)).Scan(
		&supporter.ID,
		&supporter.Role,
		&supporter.Email,
		&supporter.WalletAddress,
		&supporter.CreatedAt,
		&supporter.Name,
		&imageObjectKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Supporter{}, false, nil
		}
		return domain.Supporter{}, false, fmt.Errorf("repository: find supporter by wallet address: %w", err)
	}

	if imageObjectKey.Valid {
		supporter.ImageObjectKey = &imageObjectKey.String
	}

	return supporter, true, nil
}

func (r *PostgresSupporterRepository) ReplaceProfile(
	ctx context.Context,
	walletAddress string,
	profile domain.SupporterProfileReplacement,
) (ReplaceSupporterProfileResult, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReplaceSupporterProfileResult{}, false, fmt.Errorf("repository: begin replace supporter profile transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const lockSupporter = `
		SELECT u.id, s.image_object_key
		FROM users u
		JOIN supporters s ON s.id = u.id
		WHERE u.role = 'supporter'
			AND u.deleted_at IS NULL
			AND LOWER(u.wallet_address) = LOWER($1)
		FOR UPDATE OF u, s
	`

	var (
		supporterID        string
		currentImageObject sql.NullString
	)
	if err := tx.QueryRowContext(ctx, lockSupporter, strings.TrimSpace(walletAddress)).Scan(
		&supporterID,
		&currentImageObject,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReplaceSupporterProfileResult{}, false, nil
		}
		return ReplaceSupporterProfileResult{}, false, fmt.Errorf("repository: lock supporter profile for replacement: %w", err)
	}

	const replaceUserEmail = `UPDATE users SET email = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, replaceUserEmail, profile.Email, supporterID); err != nil {
		return ReplaceSupporterProfileResult{}, false, mapPostgresError("replace supporter user email", err)
	}

	const replaceSupporter = `UPDATE supporters SET name = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, replaceSupporter, profile.Name, supporterID); err != nil {
		return ReplaceSupporterProfileResult{}, false, mapPostgresError("replace supporter profile", err)
	}

	if profile.ImageObjectKey.Set {
		const replaceImageObjectKey = `UPDATE supporters SET image_object_key = $1 WHERE id = $2`
		if _, err := tx.ExecContext(ctx, replaceImageObjectKey, profile.ImageObjectKey.Value, supporterID); err != nil {
			return ReplaceSupporterProfileResult{}, false, mapPostgresError("replace supporter image object key", err)
		}
	}

	result := ReplaceSupporterProfileResult{}
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
			return ReplaceSupporterProfileResult{}, false, fmt.Errorf("repository: check old supporter image references: %w", err)
		}
		if !referenced {
			oldImageObjectKey := currentImageObject.String
			result.OldImageObjectKey = &oldImageObjectKey
			result.DeleteOldImageFile = true
		}
	}

	if err := tx.Commit(); err != nil {
		return ReplaceSupporterProfileResult{}, false, fmt.Errorf("repository: commit replace supporter profile transaction: %w", err)
	}

	return result, true, nil
}

func mapPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.ConstraintName {
		case usersEmailConstraint:
			return fmt.Errorf("%s: %w", operation, ErrEmailAlreadyExists)
		case usersWalletConstraint:
			return fmt.Errorf("%s: %w", operation, ErrWalletAlreadyExists)
		}
	}

	return fmt.Errorf("repository: %s: %w", operation, err)
}
