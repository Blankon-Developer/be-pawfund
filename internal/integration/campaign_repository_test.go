//go:build integration

package integration_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

func TestPostgresCampaignRepositoryCreatePending(t *testing.T) {
	const walletAddress = "0xFundraiserChecksum"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)

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
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)
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

func TestPostgresCampaignRepositoryFindPublicByContractAddress(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	fundraiserImageObjectKey := "profiles/public-fundraiser.png"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)
	fundraiser := newFundraiser("public@example.com", "0xPublicFundraiser", &fundraiserImageObjectKey)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	visibleCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Emergency Rescue",
		"Help animals urgently",
		domain.CampaignStatusCompleted,
		domain.CampaignDeploymentStatusDeployed,
		100,
		90,
		time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	)
	var visibleAddress string
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT contract_address FROM campaigns WHERE id = $1`,
		visibleCampaignID,
	).Scan(&visibleAddress); err != nil {
		t.Fatalf("get public campaign address: %v", err)
	}

	found, err := campaignRepo.FindPublicByContractAddress(t.Context(), strings.ToLower(visibleAddress))
	if err != nil {
		t.Fatalf("FindPublicByContractAddress() unexpected error: %v", err)
	}
	if found.ID != visibleCampaignID || found.FundraiserID != fundraiser.ID || found.FundraiserName != fundraiser.Name || found.FundraiserWalletAddress != fundraiser.WalletAddress {
		t.Errorf("found public campaign identity = %#v", found)
	}
	if found.Status != domain.CampaignStatusCompleted || found.ContractAddress == nil || *found.ContractAddress != visibleAddress || found.FundraiserImageObjectKey == nil || *found.FundraiserImageObjectKey != fundraiserImageObjectKey {
		t.Errorf("found public campaign state = %#v", found)
	}

	hiddenCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Pending Deployment",
		"This campaign must not be visible",
		domain.CampaignStatusActive,
		domain.CampaignDeploymentStatusPending,
		100,
		0,
		time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	)
	const hiddenAddress = "0xPendingCampaign"
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`UPDATE campaigns SET contract_address = $1 WHERE id = $2`,
		hiddenAddress,
		hiddenCampaignID,
	); err != nil {
		t.Fatalf("add hidden campaign address: %v", err)
	}
	_, err = campaignRepo.FindPublicByContractAddress(t.Context(), hiddenAddress)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("pending campaign error = %v, want ErrCampaignNotFound", err)
	}

	if _, err := testDatabase.ExecContext(t.Context(), `UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, fundraiser.ID); err != nil {
		t.Fatalf("soft-delete fundraiser fixture: %v", err)
	}
	_, err = campaignRepo.FindPublicByContractAddress(t.Context(), visibleAddress)
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("deleted fundraiser error = %v, want ErrCampaignNotFound", err)
	}

	_, err = campaignRepo.FindPublicByContractAddress(t.Context(), "0xUnknownCampaign")
	if !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("unknown campaign error = %v, want ErrCampaignNotFound", err)
	}
}

func TestPostgresCampaignRepositoryListPublicDonorsByContractAddress(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)
	supporterRepo := database.NewPostgresSupporterRepository(testDatabase)
	fundraiser := newFundraiser("donor-list@example.com", "0xDonorListFundraiser", nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	createdAt := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	campaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Donor List Campaign",
		"Campaign with donor aggregation",
		domain.CampaignStatusActive,
		domain.CampaignDeploymentStatusDeployed,
		10_000,
		2_100,
		createdAt,
	)
	var contractAddress string
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT contract_address FROM campaigns WHERE id = $1`,
		campaignID,
	).Scan(&contractAddress); err != nil {
		t.Fatalf("get campaign contract address: %v", err)
	}

	registeredImage := "profiles/registered-donor.png"
	registered := newSupporter("registered-donor@example.com", "0xRegisteredDonor", &registeredImage)
	deleted := newSupporter("deleted-donor@example.com", "0xDeletedDonor", nil)
	mustCreateSupporter(t, supporterRepo, registered)
	mustCreateSupporter(t, supporterRepo, deleted)

	mustCreateDonationForAddress(t, campaignID, &registered.ID, registered.WalletAddress, 200, "0xRegisteredFirst", createdAt.Add(time.Hour), 10, 0)
	mustCreateDonationForAddress(t, campaignID, &registered.ID, registered.WalletAddress, 300, "0xRegisteredLatest", createdAt.Add(3*time.Hour), 12, 0)
	mustCreateDonationForAddress(t, campaignID, nil, "0xGuestDonor", 400, "0xGuestFirst", createdAt.Add(2*time.Hour), 11, 0)
	mustCreateDonationForAddress(t, campaignID, nil, "0xguestdonor", 300, "0xGuestLatest", createdAt.Add(4*time.Hour), 13, 0)
	mustCreateDonationForAddress(t, campaignID, &deleted.ID, deleted.WalletAddress, 900, "0xDeleted", createdAt.Add(30*time.Minute), 9, 0)
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`,
		deleted.ID,
	); err != nil {
		t.Fatalf("soft-delete donor profile: %v", err)
	}

	recent, recentTotal, err := campaignRepo.ListPublicDonorsByContractAddress(
		t.Context(),
		strings.ToLower(contractAddress),
		domain.CampaignDonorListOptions{Sort: domain.CampaignDonorListSortRecent, Page: 1, PageSize: 2},
	)
	if err != nil {
		t.Fatalf("ListPublicDonorsByContractAddress(recent) unexpected error: %v", err)
	}
	if recentTotal != 3 {
		t.Errorf("recent donor total = %d, want 3", recentTotal)
	}
	if len(recent) != 2 || recent[0].WalletAddress != "0xguestdonor" || recent[0].Amount != 700 || !recent[0].DonatedAt.Equal(createdAt.Add(4*time.Hour)) {
		t.Fatalf("recent donors = %#v", recent)
	}
	if recent[0].Name != nil || recent[0].ImageObjectKey != nil {
		t.Errorf("unregistered donor profile = %#v", recent[0])
	}
	if recent[1].WalletAddress != registered.WalletAddress || recent[1].Amount != 500 || recent[1].Name == nil || *recent[1].Name != registered.Name || recent[1].ImageObjectKey == nil || *recent[1].ImageObjectKey != registeredImage {
		t.Errorf("registered donor = %#v", recent[1])
	}

	top, topTotal, err := campaignRepo.ListPublicDonorsByContractAddress(
		t.Context(),
		contractAddress,
		domain.CampaignDonorListOptions{Sort: domain.CampaignDonorListSortTop, Page: 1, PageSize: 2},
	)
	if err != nil {
		t.Fatalf("ListPublicDonorsByContractAddress(top) unexpected error: %v", err)
	}
	if topTotal != 3 {
		t.Errorf("top donor total = %d, want 3", topTotal)
	}
	if len(top) != 2 || top[0].WalletAddress != deleted.WalletAddress || top[0].Amount != 900 || top[0].Name != nil || top[1].Amount != 700 {
		t.Errorf("top donors = %#v", top)
	}

	secondPage, secondPageTotal, err := campaignRepo.ListPublicDonorsByContractAddress(
		t.Context(),
		contractAddress,
		domain.CampaignDonorListOptions{Sort: domain.CampaignDonorListSortTop, Page: 2, PageSize: 2},
	)
	if err != nil || len(secondPage) != 1 || secondPage[0].WalletAddress != registered.WalletAddress || secondPageTotal != 3 {
		t.Errorf("top second page = %#v total=%d, err=%v", secondPage, secondPageTotal, err)
	}

	emptyCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Empty Donor Campaign",
		"Campaign without donors",
		domain.CampaignStatusActive,
		domain.CampaignDeploymentStatusDeployed,
		10_000,
		0,
		createdAt.Add(time.Minute),
	)
	var emptyAddress string
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT contract_address FROM campaigns WHERE id = $1`,
		emptyCampaignID,
	).Scan(&emptyAddress); err != nil {
		t.Fatalf("get empty campaign address: %v", err)
	}
	empty, emptyTotal, err := campaignRepo.ListPublicDonorsByContractAddress(
		t.Context(),
		emptyAddress,
		domain.CampaignDonorListOptions{Sort: domain.CampaignDonorListSortRecent, Page: 1, PageSize: 10},
	)
	if err != nil || empty == nil || len(empty) != 0 || emptyTotal != 0 {
		t.Errorf("empty campaign donors = %#v total=%d, err=%v", empty, emptyTotal, err)
	}

	hiddenCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Hidden Campaign",
		"Pending campaign",
		domain.CampaignStatusActive,
		domain.CampaignDeploymentStatusPending,
		10_000,
		0,
		createdAt.Add(2*time.Minute),
	)
	const hiddenAddress = "0xHiddenDonorCampaign"
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`UPDATE campaigns SET contract_address = $1 WHERE id = $2`,
		hiddenAddress,
		hiddenCampaignID,
	); err != nil {
		t.Fatalf("set hidden campaign address: %v", err)
	}
	options := domain.CampaignDonorListOptions{Sort: domain.CampaignDonorListSortRecent, Page: 1, PageSize: 10}
	if _, _, err := campaignRepo.ListPublicDonorsByContractAddress(t.Context(), hiddenAddress, options); !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("hidden campaign error = %v, want ErrCampaignNotFound", err)
	}
	if _, _, err := campaignRepo.ListPublicDonorsByContractAddress(t.Context(), "0xUnknown", options); !errors.Is(err, repository.ErrCampaignNotFound) {
		t.Errorf("unknown campaign error = %v, want ErrCampaignNotFound", err)
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
