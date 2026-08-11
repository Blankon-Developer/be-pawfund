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

func (r *PostgresCampaignRepository) ListPublic(
	ctx context.Context,
	options domain.CampaignListOptions,
) ([]domain.PublicCampaignListItem, error) {
	const query = `
		SELECT
			c.id,
			c.fundraiser_id,
			c.event_id,
			c.title,
			c.short_description,
			c.story,
			c.goal_amount,
			c.raised_amount,
			c.donor_count,
			c.contract_address,
			c.created_at,
			c.end_at,
			c.image_object_key,
			c.country,
			c.zip_code,
			c.status,
			c.deployment_status,
			c.idempotency_key,
			f.image_object_key
		FROM campaigns c
		JOIN fundraisers f ON f.id = c.fundraiser_id
		JOIN users u ON u.id = f.id
		WHERE u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND c.deployment_status = 'deployed'
			AND c.contract_address IS NOT NULL
			AND (
				$1 = ''
				OR c.title ILIKE '%' || $1 || '%'
				OR c.short_description ILIKE '%' || $1 || '%'
			)
			AND ($2::campaign_status IS NULL OR c.status = $2::campaign_status)
	`

	offset := (options.Page - 1) * options.PageSize
	rows, err := r.db.QueryContext(
		ctx,
		query+campaignListOrderBy(options.Sort)+" LIMIT $3 OFFSET $4",
		options.Search,
		campaignListStatusArgument(options.Status),
		options.PageSize,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list public campaigns: %w", err)
	}
	defer rows.Close()

	campaigns := make([]domain.PublicCampaignListItem, 0)
	for rows.Next() {
		campaign, err := scanPublicCampaignListItem(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan public campaign list: %w", err)
		}
		campaigns = append(campaigns, campaign)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate public campaign list: %w", err)
	}
	return campaigns, nil
}

func (r *PostgresCampaignRepository) FindPublicByContractAddress(
	ctx context.Context,
	contractAddress string,
) (domain.PublicCampaignDetail, error) {
	const query = `
		SELECT
			c.id,
			c.fundraiser_id,
			c.event_id,
			c.title,
			c.short_description,
			c.story,
			c.goal_amount,
			c.raised_amount,
			c.donor_count,
			c.contract_address,
			c.created_at,
			c.end_at,
			c.image_object_key,
			c.country,
			c.zip_code,
			c.status,
			c.deployment_status,
			c.idempotency_key,
			f.name,
			u.wallet_address,
			f.image_object_key
		FROM campaigns c
		JOIN fundraisers f ON f.id = c.fundraiser_id
		JOIN users u ON u.id = f.id
		WHERE LOWER(c.contract_address) = LOWER($1)
			AND u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND c.deployment_status = 'deployed'
			AND c.contract_address IS NOT NULL
	`

	campaign, err := scanPublicCampaignDetail(r.db.QueryRowContext(ctx, query, strings.TrimSpace(contractAddress)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PublicCampaignDetail{}, ErrCampaignNotFound
	}
	if err != nil {
		return domain.PublicCampaignDetail{}, fmt.Errorf("repository: find public campaign by contract address: %w", err)
	}
	return campaign, nil
}

func (r *PostgresCampaignRepository) ListPublicDonorsByContractAddress(
	ctx context.Context,
	contractAddress string,
	options domain.CampaignDonorListOptions,
) ([]domain.PublicCampaignDonor, error) {
	const findCampaign = `
		SELECT c.id
		FROM campaigns c
		JOIN fundraisers f ON f.id = c.fundraiser_id
		JOIN users u ON u.id = f.id
		WHERE LOWER(c.contract_address) = LOWER($1)
			AND u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND c.deployment_status = 'deployed'
			AND c.contract_address IS NOT NULL
	`

	var campaignID uuid.UUID
	if err := r.db.QueryRowContext(ctx, findCampaign, strings.TrimSpace(contractAddress)).Scan(&campaignID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCampaignNotFound
		}
		return nil, fmt.Errorf("repository: find public campaign for donor list: %w", err)
	}

	const query = `
		WITH donor_totals AS (
			SELECT
				LOWER(d.donor_address) AS normalized_address,
				(ARRAY_AGG(
					d.donor_address
					ORDER BY be.created_at DESC, be.block_number DESC, be.log_index DESC, d.id DESC
				))[1] AS donor_address,
				SUM(d.amount)::BIGINT AS amount,
				MAX(be.created_at) AS donated_at
			FROM donations d
			JOIN blockchain_events be ON be.id = d.event_id
			WHERE d.campaign_id = $1
			GROUP BY LOWER(d.donor_address)
		)
		SELECT
			s.name,
			COALESCE(u.wallet_address, dt.donor_address),
			s.image_object_key,
			dt.amount,
			dt.donated_at
		FROM donor_totals dt
		LEFT JOIN users u ON LOWER(u.wallet_address) = dt.normalized_address
			AND u.role = 'supporter'
			AND u.deleted_at IS NULL
		LEFT JOIN supporters s ON s.id = u.id
	`

	offset := (options.Page - 1) * options.PageSize
	rows, err := r.db.QueryContext(
		ctx,
		query+campaignDonorListOrderBy(options.Sort)+" LIMIT $2 OFFSET $3",
		campaignID,
		options.PageSize,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list public campaign donors: %w", err)
	}
	defer rows.Close()

	donors := make([]domain.PublicCampaignDonor, 0)
	for rows.Next() {
		var (
			donor          domain.PublicCampaignDonor
			name           sql.NullString
			imageObjectKey sql.NullString
		)
		if err := rows.Scan(
			&name,
			&donor.WalletAddress,
			&imageObjectKey,
			&donor.Amount,
			&donor.DonatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan public campaign donor: %w", err)
		}
		if name.Valid {
			donor.Name = &name.String
		}
		if imageObjectKey.Valid {
			donor.ImageObjectKey = &imageObjectKey.String
		}
		donors = append(donors, donor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate public campaign donors: %w", err)
	}
	return donors, nil
}

func (r *PostgresCampaignRepository) ListForFundraiser(
	ctx context.Context,
	walletAddress string,
	options domain.CampaignListOptions,
) ([]domain.Campaign, error) {
	const query = `
		SELECT
			c.id,
			c.fundraiser_id,
			c.event_id,
			c.title,
			c.short_description,
			c.story,
			c.goal_amount,
			c.raised_amount,
			c.donor_count,
			c.contract_address,
			c.created_at,
			c.end_at,
			c.image_object_key,
			c.country,
			c.zip_code,
			c.status,
			c.deployment_status,
			c.idempotency_key
		FROM campaigns c
		JOIN fundraisers f ON f.id = c.fundraiser_id
		JOIN users u ON u.id = f.id
		WHERE u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND LOWER(u.wallet_address) = LOWER($1)
			AND (
				$2 = ''
				OR c.title ILIKE '%' || $2 || '%'
				OR c.short_description ILIKE '%' || $2 || '%'
			)
			AND ($3::campaign_status IS NULL OR c.status = $3::campaign_status)
	`

	offset := (options.Page - 1) * options.PageSize
	rows, err := r.db.QueryContext(
		ctx,
		query+campaignListOrderBy(options.Sort)+" LIMIT $4 OFFSET $5",
		strings.TrimSpace(walletAddress),
		options.Search,
		campaignListStatusArgument(options.Status),
		options.PageSize,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list campaigns for fundraiser: %w", err)
	}
	defer rows.Close()

	campaigns := make([]domain.Campaign, 0)
	for rows.Next() {
		campaign, err := scanCampaign(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan campaign list for fundraiser: %w", err)
		}
		campaigns = append(campaigns, campaign)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate campaign list for fundraiser: %w", err)
	}
	return campaigns, nil
}

func campaignListStatusArgument(status *domain.CampaignStatus) any {
	if status == nil {
		return nil
	}
	return string(*status)
}

func campaignListOrderBy(sort domain.CampaignListSort) string {
	switch sort {
	case domain.CampaignListSortRandom:
		return " ORDER BY RANDOM()"
	case domain.CampaignListSortOldest:
		return " ORDER BY c.created_at ASC, c.id ASC"
	case domain.CampaignListSortCloseToGoal:
		return " ORDER BY (c.raised_amount::numeric / c.goal_amount) DESC, c.created_at DESC, c.id DESC"
	case domain.CampaignListSortMostDonated:
		return " ORDER BY c.raised_amount DESC, c.created_at DESC, c.id DESC"
	default:
		return " ORDER BY c.created_at DESC, c.id DESC"
	}
}

func campaignDonorListOrderBy(sort domain.CampaignDonorListSort) string {
	switch sort {
	case domain.CampaignDonorListSortTop:
		return " ORDER BY dt.amount DESC, dt.donated_at DESC, dt.normalized_address ASC"
	default:
		return " ORDER BY dt.donated_at DESC, dt.amount DESC, dt.normalized_address ASC"
	}
}

func (r *PostgresCampaignRepository) FindByIDForFundraiser(
	ctx context.Context,
	walletAddress string,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	const query = `
		SELECT
			c.id,
			c.fundraiser_id,
			c.event_id,
			c.title,
			c.short_description,
			c.story,
			c.goal_amount,
			c.raised_amount,
			c.donor_count,
			c.contract_address,
			c.created_at,
			c.end_at,
			c.image_object_key,
			c.country,
			c.zip_code,
			c.status,
			c.deployment_status,
			c.idempotency_key
		FROM campaigns c
		JOIN fundraisers f ON f.id = c.fundraiser_id
		JOIN users u ON u.id = f.id
		WHERE c.id = $1
			AND u.role = 'fundraiser'
			AND u.deleted_at IS NULL
			AND LOWER(u.wallet_address) = LOWER($2)
	`

	campaign, err := scanCampaign(r.db.QueryRowContext(ctx, query, campaignID, strings.TrimSpace(walletAddress)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Campaign{}, ErrCampaignNotFound
	}
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("repository: find campaign by ID for fundraiser: %w", err)
	}
	return campaign, nil
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

func scanPublicCampaignListItem(scanner campaignScanner) (domain.PublicCampaignListItem, error) {
	var (
		item               domain.PublicCampaignListItem
		eventID            uuid.NullUUID
		contractAddress    sql.NullString
		fundraiserImageKey sql.NullString
	)
	err := scanner.Scan(
		&item.ID,
		&item.FundraiserID,
		&eventID,
		&item.Title,
		&item.ShortDescription,
		&item.Story,
		&item.GoalAmount,
		&item.RaisedAmount,
		&item.DonorCount,
		&contractAddress,
		&item.CreatedAt,
		&item.EndAt,
		&item.ImageObjectKey,
		&item.Country,
		&item.ZipCode,
		&item.Status,
		&item.DeploymentStatus,
		&item.IdempotencyKey,
		&fundraiserImageKey,
	)
	if err != nil {
		return domain.PublicCampaignListItem{}, err
	}
	if eventID.Valid {
		item.EventID = &eventID.UUID
	}
	if contractAddress.Valid {
		item.ContractAddress = &contractAddress.String
	}
	if fundraiserImageKey.Valid {
		item.FundraiserImageObjectKey = &fundraiserImageKey.String
	}
	return item, nil
}

func scanPublicCampaignDetail(scanner campaignScanner) (domain.PublicCampaignDetail, error) {
	var (
		detail             domain.PublicCampaignDetail
		eventID            uuid.NullUUID
		contractAddress    sql.NullString
		fundraiserImageKey sql.NullString
	)
	err := scanner.Scan(
		&detail.ID,
		&detail.FundraiserID,
		&eventID,
		&detail.Title,
		&detail.ShortDescription,
		&detail.Story,
		&detail.GoalAmount,
		&detail.RaisedAmount,
		&detail.DonorCount,
		&contractAddress,
		&detail.CreatedAt,
		&detail.EndAt,
		&detail.ImageObjectKey,
		&detail.Country,
		&detail.ZipCode,
		&detail.Status,
		&detail.DeploymentStatus,
		&detail.IdempotencyKey,
		&detail.FundraiserName,
		&detail.FundraiserWalletAddress,
		&fundraiserImageKey,
	)
	if err != nil {
		return domain.PublicCampaignDetail{}, err
	}
	if eventID.Valid {
		detail.EventID = &eventID.UUID
	}
	if contractAddress.Valid {
		detail.ContractAddress = &contractAddress.String
	}
	if fundraiserImageKey.Valid {
		detail.FundraiserImageObjectKey = &fundraiserImageKey.String
	}
	return detail, nil
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
