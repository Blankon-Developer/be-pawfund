//go:build integration

package integration_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

func TestPostgresCampaignRepositoryCreatePending(t *testing.T) {
	const walletAddress = "0xFundraiserChecksum"
	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := repository.NewPostgresCampaignRepository(testDatabase)

	t.Run("creates and replays a pending campaign", func(t *testing.T) {
		cleanDatabase(t)
		t.Cleanup(func() { cleanDatabase(t) })
		fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
		mustCreateFundraiser(t, fundraiserRepo, fundraiser)

		endAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
		requested := newPendingCampaign(endAt, "create-rescue-1")
		created, err := campaignRepo.CreatePending(t.Context(), strings.ToLower(walletAddress), requested, time.Now().UTC().Add(5*time.Minute))
		if err != nil {
			t.Fatalf("CreatePending() unexpected error: %v", err)
		}
		if created.FundraiserID != fundraiser.ID || created.ID != requested.ID {
			t.Errorf("created identity = %s/%s, want %s/%s", created.FundraiserID, created.ID, fundraiser.ID, requested.ID)
		}
		if created.GoalAmount != 10_000_000_000 || created.RaisedAmount != 0 || created.DonorCount != 0 {
			t.Errorf("created amounts = goal:%d raised:%d donors:%d", created.GoalAmount, created.RaisedAmount, created.DonorCount)
		}
		if created.EventID != nil || created.ContractAddress != nil || created.DeploymentStatus != domain.CampaignDeploymentStatusPending || created.Status != domain.CampaignStatusActive {
			t.Errorf("created state = %#v", created)
		}
		if created.CreatedAt.IsZero() {
			t.Error("created campaign has zero CreatedAt")
		}

		replayRequest := requested
		replayRequest.ID = uuid.New()
		replayed, err := campaignRepo.CreatePending(t.Context(), walletAddress, replayRequest, endAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("CreatePending() replay unexpected error: %v", err)
		}
		if replayed.ID != created.ID || !replayed.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("replayed campaign = %s/%v, want %s/%v", replayed.ID, replayed.CreatedAt, created.ID, created.CreatedAt)
		}

		conflictRequest := requested
		conflictRequest.ID = uuid.New()
		conflictRequest.Title = "A different campaign"
		_, err = campaignRepo.CreatePending(t.Context(), walletAddress, conflictRequest, time.Now())
		if !errors.Is(err, repository.ErrCampaignIdempotencyConflict) {
			t.Fatalf("CreatePending() conflict error = %v", err)
		}

		var count int
		if err := testDatabase.QueryRowContext(
			t.Context(),
			`SELECT count(*) FROM campaigns WHERE fundraiser_id = $1 AND idempotency_key = $2`,
			fundraiser.ID,
			requested.IdempotencyKey,
		).Scan(&count); err != nil {
			t.Fatalf("count idempotent campaigns: %v", err)
		}
		if count != 1 {
			t.Errorf("campaign count = %d, want 1", count)
		}
	})

	t.Run("rejects an unknown fundraiser and an end time below the lead time", func(t *testing.T) {
		cleanDatabase(t)
		t.Cleanup(func() { cleanDatabase(t) })
		requested := newPendingCampaign(time.Now().UTC().Add(time.Hour), "unknown")
		_, err := campaignRepo.CreatePending(t.Context(), "0xUnknown", requested, time.Now())
		if !errors.Is(err, repository.ErrCampaignFundraiserNotFound) {
			t.Fatalf("unknown fundraiser error = %v", err)
		}

		fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
		mustCreateFundraiser(t, fundraiserRepo, fundraiser)
		minimumEndAt := time.Now().UTC().Add(5 * time.Minute)
		requested = newPendingCampaign(minimumEndAt.Add(-time.Second), "too-soon")
		_, err = campaignRepo.CreatePending(t.Context(), walletAddress, requested, minimumEndAt)
		if !errors.Is(err, repository.ErrCampaignEndAtTooSoon) {
			t.Fatalf("too-soon error = %v", err)
		}
	})

	t.Run("serializes concurrent retries", func(t *testing.T) {
		cleanDatabase(t)
		t.Cleanup(func() { cleanDatabase(t) })
		fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
		mustCreateFundraiser(t, fundraiserRepo, fundraiser)

		const workers = 5
		endAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
		minimumEndAt := time.Now().UTC().Add(5 * time.Minute)
		start := make(chan struct{})
		results := make(chan domain.Campaign, workers)
		errorsChannel := make(chan error, workers)
		var waitGroup sync.WaitGroup
		for range workers {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				requested := newPendingCampaign(endAt, "concurrent-retry")
				created, err := campaignRepo.CreatePending(t.Context(), walletAddress, requested, minimumEndAt)
				if err != nil {
					errorsChannel <- err
					return
				}
				results <- created
			}()
		}
		close(start)
		waitGroup.Wait()
		close(results)
		close(errorsChannel)
		for err := range errorsChannel {
			t.Errorf("concurrent CreatePending() error: %v", err)
		}
		var createdID uuid.UUID
		for created := range results {
			if createdID == uuid.Nil {
				createdID = created.ID
			} else if created.ID != createdID {
				t.Errorf("concurrent campaign ID = %s, want %s", created.ID, createdID)
			}
		}
		var count int
		if err := testDatabase.QueryRowContext(t.Context(), `SELECT count(*) FROM campaigns WHERE idempotency_key = 'concurrent-retry'`).Scan(&count); err != nil {
			t.Fatalf("count concurrent campaigns: %v", err)
		}
		if count != 1 {
			t.Errorf("concurrent campaign count = %d, want 1", count)
		}
	})
}

func TestPostgresCampaignRepositoryFindByIDForFundraiser(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	const (
		ownerWallet = "0xFundraiserChecksum"
		otherWallet = "0xOtherFundraiser"
	)
	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := repository.NewPostgresCampaignRepository(testDatabase)
	mustCreateFundraiser(t, fundraiserRepo, newFundraiser("owner@example.com", ownerWallet, nil))
	mustCreateFundraiser(t, fundraiserRepo, newFundraiser("other@example.com", otherWallet, nil))

	requested := newPendingCampaign(time.Now().UTC().Add(30*time.Hour), "find-owned-campaign")
	created, err := campaignRepo.CreatePending(t.Context(), ownerWallet, requested, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		t.Fatalf("CreatePending() unexpected error: %v", err)
	}
	const contractAddress = "0xCampaign"
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`UPDATE campaigns SET raised_amount = $1, donor_count = $2, contract_address = $3 WHERE id = $4`,
		100_000_000,
		3,
		contractAddress,
		created.ID,
	); err != nil {
		t.Fatalf("update campaign fixture: %v", err)
	}

	found, err := campaignRepo.FindByIDForFundraiser(t.Context(), strings.ToLower(ownerWallet), created.ID)
	if err != nil {
		t.Fatalf("FindByIDForFundraiser() unexpected error: %v", err)
	}
	if found.ID != created.ID || found.FundraiserID != created.FundraiserID || found.Title != requested.Title {
		t.Errorf("found identity = %#v", found)
	}
	if found.RaisedAmount != 100_000_000 || found.DonorCount != 3 || found.ContractAddress == nil || *found.ContractAddress != contractAddress {
		t.Errorf("found campaign state = %#v", found)
	}

	_, err = campaignRepo.FindByIDForFundraiser(t.Context(), otherWallet, created.ID)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("other fundraiser error = %v, want ErrCampaignNotFound", err)
	}

	_, err = campaignRepo.FindByIDForFundraiser(t.Context(), ownerWallet, uuid.New())
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("unknown campaign error = %v, want ErrCampaignNotFound", err)
	}

	if _, err := testDatabase.ExecContext(t.Context(), `UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, created.FundraiserID); err != nil {
		t.Fatalf("soft-delete fundraiser fixture: %v", err)
	}
	_, err = campaignRepo.FindByIDForFundraiser(t.Context(), ownerWallet, created.ID)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("deleted fundraiser error = %v, want ErrCampaignNotFound", err)
	}
}

func newPendingCampaign(endAt time.Time, idempotencyKey string) domain.Campaign {
	return domain.Campaign{
		ID:               uuid.New(),
		Title:            "Emergency Rescue",
		ShortDescription: "Help rescued animals",
		Story:            "A long rescue story.",
		GoalAmount:       10_000_000_000,
		EndAt:            endAt,
		ImageObjectKey:   "campaigns/rescue.png",
		Country:          "Indonesia",
		ZipCode:          "10110",
		Status:           domain.CampaignStatusActive,
		DeploymentStatus: domain.CampaignDeploymentStatusPending,
		IdempotencyKey:   idempotencyKey,
	}
}
