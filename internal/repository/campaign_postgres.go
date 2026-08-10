package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/google/uuid"
)

type PostgresCampaignRepository struct {
	db *sql.DB
}

func NewPostgresCampaignRepository(db *sql.DB) *PostgresCampaignRepository {
	return &PostgresCampaignRepository{db: db}
}

func (r *PostgresCampaignRepository) CreatePending(
	ctx context.Context,
	walletAddress string,
	campaign domain.Campaign,
	minimumEndAt time.Time,
) (domain.Campaign, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("repository: begin create pending campaign transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const lockFundraiser = `
		SELECT u.id
		FROM users u
		JOIN fundraisers f ON f.id = u.id
		WHERE u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND LOWER(u.wallet_address) = LOWER($1)
		FOR UPDATE OF u, f
	`
	if err := tx.QueryRowContext(ctx, lockFundraiser, strings.TrimSpace(walletAddress)).Scan(&campaign.FundraiserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Campaign{}, ErrCampaignFundraiserNotFound
		}
		return domain.Campaign{}, fmt.Errorf("repository: lock campaign fundraiser: %w", err)
	}

	existing, found, err := findCampaignByIdempotencyKey(ctx, tx, campaign.FundraiserID, campaign.IdempotencyKey)
	if err != nil {
		return domain.Campaign{}, err
	}
	if found {
		if !sameCampaignCreationPayload(existing, campaign) {
			return domain.Campaign{}, ErrCampaignIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return domain.Campaign{}, fmt.Errorf("repository: commit campaign idempotency replay: %w", err)
		}
		return existing, nil
	}

	if campaign.EndAt.Before(minimumEndAt) {
		return domain.Campaign{}, ErrCampaignEndAtTooSoon
	}

	const insertCampaign = `
		INSERT INTO campaigns (
			id,
			fundraiser_id,
			title,
			short_description,
			story,
			goal_amount,
			end_at,
			image_object_key,
			country,
			zip_code,
			status,
			deployment_status,
			idempotency_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (fundraiser_id, idempotency_key) DO NOTHING
		RETURNING raised_amount, donor_count, created_at
	`
	err = tx.QueryRowContext(
		ctx,
		insertCampaign,
		campaign.ID,
		campaign.FundraiserID,
		campaign.Title,
		campaign.ShortDescription,
		campaign.Story,
		campaign.GoalAmount,
		campaign.EndAt,
		campaign.ImageObjectKey,
		campaign.Country,
		campaign.ZipCode,
		campaign.Status,
		campaign.DeploymentStatus,
		campaign.IdempotencyKey,
	).Scan(&campaign.RaisedAmount, &campaign.DonorCount, &campaign.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, findErr := findCampaignByIdempotencyKey(ctx, tx, campaign.FundraiserID, campaign.IdempotencyKey)
		if findErr != nil {
			return domain.Campaign{}, findErr
		}
		if !found {
			return domain.Campaign{}, fmt.Errorf("repository: resolve campaign idempotency conflict: %w", sql.ErrNoRows)
		}
		if !sameCampaignCreationPayload(existing, campaign) {
			return domain.Campaign{}, ErrCampaignIdempotencyConflict
		}
		campaign = existing
	} else if err != nil {
		return domain.Campaign{}, mapPostgresError("insert pending campaign", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Campaign{}, fmt.Errorf("repository: commit create pending campaign transaction: %w", err)
	}
	return campaign, nil
}

type campaignScanner interface {
	Scan(dest ...any) error
}

func findCampaignByIdempotencyKey(
	ctx context.Context,
	tx *sql.Tx,
	fundraiserID uuid.UUID,
	idempotencyKey string,
) (domain.Campaign, bool, error) {
	const query = `
		SELECT
			id,
			fundraiser_id,
			event_id,
			title,
			short_description,
			story,
			goal_amount,
			raised_amount,
			donor_count,
			contract_address,
			created_at,
			end_at,
			image_object_key,
			country,
			zip_code,
			status,
			deployment_status,
			idempotency_key
		FROM campaigns
		WHERE fundraiser_id = $1 AND idempotency_key = $2
	`
	campaign, err := scanCampaign(tx.QueryRowContext(ctx, query, fundraiserID, idempotencyKey))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Campaign{}, false, nil
	}
	if err != nil {
		return domain.Campaign{}, false, fmt.Errorf("repository: find campaign by idempotency key: %w", err)
	}
	return campaign, true, nil
}

func scanCampaign(scanner campaignScanner) (domain.Campaign, error) {
	var (
		campaign        domain.Campaign
		eventID         uuid.NullUUID
		contractAddress sql.NullString
	)
	err := scanner.Scan(
		&campaign.ID,
		&campaign.FundraiserID,
		&eventID,
		&campaign.Title,
		&campaign.ShortDescription,
		&campaign.Story,
		&campaign.GoalAmount,
		&campaign.RaisedAmount,
		&campaign.DonorCount,
		&contractAddress,
		&campaign.CreatedAt,
		&campaign.EndAt,
		&campaign.ImageObjectKey,
		&campaign.Country,
		&campaign.ZipCode,
		&campaign.Status,
		&campaign.DeploymentStatus,
		&campaign.IdempotencyKey,
	)
	if err != nil {
		return domain.Campaign{}, err
	}
	if eventID.Valid {
		campaign.EventID = &eventID.UUID
	}
	if contractAddress.Valid {
		campaign.ContractAddress = &contractAddress.String
	}
	return campaign, nil
}

func sameCampaignCreationPayload(existing, requested domain.Campaign) bool {
	return existing.Title == requested.Title &&
		existing.ShortDescription == requested.ShortDescription &&
		existing.Story == requested.Story &&
		existing.GoalAmount == requested.GoalAmount &&
		existing.EndAt.Equal(requested.EndAt) &&
		existing.ImageObjectKey == requested.ImageObjectKey &&
		existing.Country == requested.Country &&
		existing.ZipCode == requested.ZipCode
}
