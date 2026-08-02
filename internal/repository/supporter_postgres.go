package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
