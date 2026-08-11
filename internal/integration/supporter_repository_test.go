//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

func TestPostgresSupporterRepositoryCreate(t *testing.T) {
	repo := repository.NewPostgresSupporterRepository(testDatabase)

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		supporter     domain.Supporter
		context       func() context.Context
		wantError     error
		wantAnyError  bool
		wantPersisted bool
	}{
		{
			name:          "persists supporter with image",
			supporter:     newSupporter("cat@example.com", "0xcat", stringPointer("profiles/cat.png")),
			wantPersisted: true,
		},
		{
			name:          "persists SQL null for omitted image",
			supporter:     newSupporter("dog@example.com", "0xdog", nil),
			wantPersisted: true,
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("duplicate@example.com", "0xfirst", nil))
			},
			supporter: newSupporter("duplicate@example.com", "0xsecond", nil),
			wantError: repository.ErrEmailAlreadyExists,
		},
		{
			name: "maps duplicate wallet constraint",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("first@example.com", "0xduplicate", nil))
			},
			supporter: newSupporter("second@example.com", "0xduplicate", nil),
			wantError: repository.ErrWalletAlreadyExists,
		},
		{
			name: "rolls back user when supporter insert fails",
			supporter: func() domain.Supporter {
				supporter := newSupporter("rollback@example.com", "0xrollback", nil)
				supporter.Name = strings.Repeat("n", 256)
				return supporter
			}(),
			wantAnyError: true,
		},
		{
			name:      "honors canceled context",
			supporter: newSupporter("canceled@example.com", "0xcanceled", nil),
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantAnyError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			created, err := repo.Create(ctx, test.supporter)

			switch {
			case test.wantError != nil:
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Create() error = %v, want %v", err, test.wantError)
				}
			case test.wantAnyError:
				if err == nil {
					t.Fatal("Create() expected an error")
				}
			default:
				if err != nil {
					t.Fatalf("Create() unexpected error: %v", err)
				}
				if created.CreatedAt.IsZero() {
					t.Error("created supporter has zero CreatedAt")
				}
			}

			assertSupporterPersistence(t, test.supporter, test.wantPersisted)
		})
	}
}

func TestPostgresSupporterRepositoryFindByWalletAddress(t *testing.T) {
	imageKey := "profiles/cat.png"
	const supporterWallet = "0xSupporterChecksum"

	tests := []struct {
		name      string
		prepare   func(t *testing.T)
		address   string
		wantFound bool
		wantImage *string
	}{
		{
			name: "finds supporter case-insensitively",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &imageKey))
			},
			address:   strings.ToLower(supporterWallet),
			wantFound: true,
			wantImage: &imageKey,
		},
		{
			name: "returns nullable image field",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("dog@example.com", supporterWallet, nil))
			},
			address:   supporterWallet,
			wantFound: true,
		},
		{
			name: "does not return fundraiser profile",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", supporterWallet, nil))
			},
			address: supporterWallet,
		},
		{
			name:    "returns not found for unregistered wallet",
			address: supporterWallet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			profile, found, err := repo.FindByWalletAddress(t.Context(), test.address)
			if err != nil {
				t.Fatalf("FindByWalletAddress() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if profile.Role != domain.UserRoleSupporter || profile.WalletAddress != supporterWallet {
				t.Errorf("profile identity = %#v", profile.User)
			}
			if profile.Name != "Supporter" || profile.Email == "" || profile.CreatedAt.IsZero() {
				t.Errorf("profile user fields = %#v", profile)
			}
			if !equalStringPointers(profile.ImageObjectKey, test.wantImage) {
				t.Errorf("image key = %v, want %v", profile.ImageObjectKey, test.wantImage)
			}
		})
	}
}

func TestPostgresSupporterRepositoryReplaceProfile(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	oldImageObjectKey := "profiles/supporter-old.png"
	newImageObjectKey := "profiles/supporter-new.png"

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		walletAddress string
		profile       domain.SupporterProfileReplacement
		wantFound     bool
		wantError     error
		wantDeleteOld bool
		assertProfile func(t *testing.T, profile domain.Supporter)
	}{
		{
			name: "replaces every profile field and clears image",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
			},
			walletAddress: strings.ToLower(supporterWallet),
			profile: domain.SupporterProfileReplacement{
				Name: "Updated Cat Lover", Email: "updated@example.com", ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true},
			},
			wantFound:     true,
			wantDeleteOld: true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || profile.ImageObjectKey != nil {
					t.Errorf("replaced profile = %#v", profile)
				}
			},
		},
		{
			name: "does not mark image shared by fundraiser for deletion",
			prepare: func(t *testing.T) {
				supporterRepo := repository.NewPostgresSupporterRepository(testDatabase)
				fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateSupporter(t, supporterRepo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
				mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &oldImageObjectKey))
			},
			walletAddress: supporterWallet,
			profile: domain.SupporterProfileReplacement{
				Name: "Updated Cat Lover", Email: "updated@example.com", ImageObjectKey: domain.ImageObjectKeyUpdate{Set: true, Value: &newImageObjectKey},
			},
			wantFound: true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || !equalStringPointers(profile.ImageObjectKey, &newImageObjectKey) {
					t.Errorf("replaced profile = %#v", profile)
				}
			},
		},
		{
			name: "preserves image when replacement omits its key",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
			},
			walletAddress: supporterWallet,
			profile:       domain.SupporterProfileReplacement{Name: "Updated Cat Lover", Email: "updated@example.com"},
			wantFound:     true,
			assertProfile: func(t *testing.T, profile domain.Supporter) {
				t.Helper()
				if profile.Name != "Updated Cat Lover" || profile.Email != "updated@example.com" || !equalStringPointers(profile.ImageObjectKey, &oldImageObjectKey) {
					t.Errorf("replacement with preserved image = %#v", profile)
				}
			},
		},
		{
			name:          "returns not found for unknown wallet",
			walletAddress: "0xUnknown",
			profile:       domain.SupporterProfileReplacement{Name: "New Name", Email: "new@example.com"},
		},
		{
			name: "maps duplicate email constraint",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
				mustCreateFundraiser(t, repository.NewPostgresFundraiserRepository(testDatabase), newFundraiser("taken@example.com", "0xFundraiser", nil))
			},
			walletAddress: supporterWallet,
			profile:       domain.SupporterProfileReplacement{Name: "Updated Cat Lover", Email: "taken@example.com"},
			wantError:     repository.ErrEmailAlreadyExists,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			result, found, err := repo.ReplaceProfile(t.Context(), test.walletAddress, test.profile)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ReplaceProfile() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReplaceProfile() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if result.DeleteOldImageFile != test.wantDeleteOld {
				t.Errorf("DeleteOldImageFile = %v, want %v", result.DeleteOldImageFile, test.wantDeleteOld)
			}
			if test.wantDeleteOld && !equalStringPointers(result.OldImageObjectKey, &oldImageObjectKey) {
				t.Errorf("OldImageObjectKey = %v, want %v", result.OldImageObjectKey, &oldImageObjectKey)
			}

			profile, found, err := repo.FindByWalletAddress(t.Context(), supporterWallet)
			if err != nil || !found {
				t.Fatalf("FindByWalletAddress() = %#v, %v, %v", profile, found, err)
			}
			test.assertProfile(t, profile)
		})
	}
}

func TestPostgresSupporterRepositoryDeleteProfile(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	imageObjectKey := "profiles/delete-supporter.png"

	tests := []struct {
		name                string
		createProfile       bool
		shareImage          bool
		wantFound           bool
		wantDeleteImage     bool
		wantDeleted         bool
		allowReregistration bool
	}{
		{
			name:                "soft deletes profile and allows fresh registration",
			createProfile:       true,
			wantFound:           true,
			wantDeleteImage:     true,
			wantDeleted:         true,
			allowReregistration: true,
		},
		{
			name:          "keeps image shared by an active fundraiser",
			createProfile: true,
			shareImage:    true,
			wantFound:     true,
			wantDeleted:   true,
		},
		{name: "returns not found for unknown wallet"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })

			repo := repository.NewPostgresSupporterRepository(testDatabase)
			supporter := newSupporter("supporter@example.com", supporterWallet, &imageObjectKey)
			if test.createProfile {
				mustCreateSupporter(t, repo, supporter)
				if test.shareImage {
					fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
					mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &imageObjectKey))
				}
			}

			result, found, err := repo.DeleteProfile(t.Context(), strings.ToLower(supporterWallet))
			if err != nil {
				t.Fatalf("DeleteProfile() unexpected error: %v", err)
			}
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if result.DeleteImageObjectFile != test.wantDeleteImage {
				t.Errorf("DeleteImageObjectFile = %v, want %v", result.DeleteImageObjectFile, test.wantDeleteImage)
			}
			if test.wantDeleteImage && !equalStringPointers(result.ImageObjectKey, &imageObjectKey) {
				t.Errorf("ImageObjectKey = %v, want %v", result.ImageObjectKey, imageObjectKey)
			}
			if !test.createProfile {
				return
			}

			var deletedAt sql.NullTime
			var storedImage sql.NullString
			if err := testDatabase.QueryRowContext(
				t.Context(),
				`SELECT u.deleted_at, s.image_object_key
				 FROM users u
				 JOIN supporters s ON s.id = u.id
				 WHERE u.id = $1`,
				supporter.ID,
			).Scan(&deletedAt, &storedImage); err != nil {
				t.Fatalf("query deleted supporter: %v", err)
			}
			if deletedAt.Valid != test.wantDeleted {
				t.Errorf("deleted_at valid = %v, want %v", deletedAt.Valid, test.wantDeleted)
			}
			if test.wantDeleted && storedImage.Valid {
				t.Errorf("deleted profile image = %q, want NULL", storedImage.String)
			}
			if !test.wantDeleted {
				return
			}

			if profile, active, err := repo.FindByWalletAddress(t.Context(), supporterWallet); err != nil || active {
				t.Errorf("FindByWalletAddress() after delete = %#v, %v, %v", profile, active, err)
			}
			authRepo := repository.NewPostgresAuthRepository(testDatabase)
			if profile, active, err := authRepo.FindProfileByWalletAddress(t.Context(), supporterWallet); err != nil || active {
				t.Errorf("FindProfileByWalletAddress() after delete = %#v, %v, %v", profile, active, err)
			}

			if test.allowReregistration {
				created, err := repo.Create(t.Context(), newSupporter("supporter@example.com", supporterWallet, nil))
				if err != nil {
					t.Fatalf("re-register supporter: %v", err)
				}
				if created.ID == supporter.ID {
					t.Error("re-registered supporter reused deleted ID")
				}
			}
		})
	}
}

func TestPostgresSupporterRepositoryListDonationsByWalletAddress(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	repo := repository.NewPostgresSupporterRepository(testDatabase)
	owner := newSupporter("owner@example.com", "0xSupporterChecksum", nil)
	other := newSupporter("other@example.com", "0xOtherSupporter", nil)
	empty := newSupporter("empty@example.com", "0xEmptySupporter", nil)
	mustCreateSupporter(t, repo, owner)
	mustCreateSupporter(t, repo, other)
	mustCreateSupporter(t, repo, empty)

	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("fundraiser@example.com", "0xFundraiser", nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)
	campaignID, contractAddress := mustCreateDonationCampaign(t, fundraiser.ID, "Emergency Rescue")

	oldest := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	middle := oldest.Add(time.Hour)
	newest := middle.Add(time.Hour)
	mustCreateDonation(t, campaignID, owner.ID, 1_000_000, "0xOldest", oldest, 10, 0)
	mustCreateDonation(t, campaignID, owner.ID, 2_000_000, "0xMiddle", middle, 11, 0)
	mustCreateDonation(t, campaignID, owner.ID, 3_000_000, "0xNewest", newest, 12, 0)
	mustCreateDonation(t, campaignID, other.ID, 9_000_000, "0xOther", newest.Add(time.Hour), 13, 0)
	const lateWallet = "0xLateSupporter"
	mustCreateDonationForAddress(t, campaignID, nil, lateWallet, 4_000_000, "0xBeforeRegistration", oldest.Add(-time.Hour), 9, 0)
	late := newSupporter("late@example.com", lateWallet, nil)
	mustCreateSupporter(t, repo, late)

	firstPage, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		strings.ToLower(owner.WalletAddress),
		domain.DonationListOptions{Page: 1, PageSize: 2},
	)
	if err != nil {
		t.Fatalf("ListDonationsByWalletAddress() unexpected error: %v", err)
	}
	if !found {
		t.Fatal("active supporter was not found")
	}
	if len(firstPage) != 2 || firstPage[0].TxHash != "0xNewest" || firstPage[1].TxHash != "0xMiddle" {
		t.Fatalf("first page = %#v", firstPage)
	}
	if firstPage[0].Amount != 3_000_000 || firstPage[0].Campaign.Title != "Emergency Rescue" || firstPage[0].Campaign.ContractAddress != contractAddress || !firstPage[0].DonatedAt.Equal(newest) {
		t.Errorf("first donation = %#v", firstPage[0])
	}

	secondPage, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		owner.WalletAddress,
		domain.DonationListOptions{Page: 2, PageSize: 2},
	)
	if err != nil || !found {
		t.Fatalf("second page found/error = %v/%v", found, err)
	}
	if len(secondPage) != 1 || secondPage[0].TxHash != "0xOldest" {
		t.Errorf("second page = %#v", secondPage)
	}

	lateDonations, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		strings.ToLower(lateWallet),
		domain.DonationListOptions{Page: 1, PageSize: 10},
	)
	if err != nil || !found || len(lateDonations) != 1 || lateDonations[0].TxHash != "0xBeforeRegistration" {
		t.Errorf("pre-registration donations = %#v, found=%v, err=%v", lateDonations, found, err)
	}

	emptyPage, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		empty.WalletAddress,
		domain.DonationListOptions{Page: 1, PageSize: 10},
	)
	if err != nil || !found || len(emptyPage) != 0 || emptyPage == nil {
		t.Errorf("empty supporter result = %#v, found=%v, err=%v", emptyPage, found, err)
	}

	unknownPage, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		"0xUnknown",
		domain.DonationListOptions{Page: 1, PageSize: 10},
	)
	if err != nil || found || unknownPage != nil {
		t.Errorf("unknown supporter result = %#v, found=%v, err=%v", unknownPage, found, err)
	}

	if _, _, err := repo.DeleteProfile(t.Context(), owner.WalletAddress); err != nil {
		t.Fatalf("delete supporter profile: %v", err)
	}
	deletedPage, found, err := repo.ListDonationsByWalletAddress(
		t.Context(),
		owner.WalletAddress,
		domain.DonationListOptions{Page: 1, PageSize: 10},
	)
	if err != nil || found || deletedPage != nil {
		t.Errorf("deleted supporter result = %#v, found=%v, err=%v", deletedPage, found, err)
	}
}

func mustCreateDonationCampaign(t *testing.T, fundraiserID uuid.UUID, title string) (uuid.UUID, string) {
	t.Helper()
	eventID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO blockchain_events (id, tx_hash, log_index, type, block_number, created_at)
		 VALUES ($1, $2, 0, 'campaign_created', 1, CURRENT_TIMESTAMP)`,
		eventID,
		"0xCampaignCreated"+eventID.String(),
	); err != nil {
		t.Fatalf("prepare campaign blockchain event: %v", err)
	}

	campaignID := uuid.New()
	contractAddress := "0xCampaign" + campaignID.String()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO campaigns (
			id, fundraiser_id, event_id, title, short_description, story,
			goal_amount, contract_address, end_at, image_object_key, country,
			zip_code, deployment_status, idempotency_key
		) VALUES (
			$1, $2, $3, $4, 'Help animals urgently', 'Campaign story.',
			100000000, $5, CURRENT_TIMESTAMP + INTERVAL '30 days',
			'campaigns/donation-campaign.png', 'Indonesia', '10110', 'deployed', $6
		)`,
		campaignID,
		fundraiserID,
		eventID,
		title,
		contractAddress,
		"donation-campaign-"+campaignID.String(),
	); err != nil {
		t.Fatalf("prepare donation campaign: %v", err)
	}
	return campaignID, contractAddress
}

func mustCreateDonation(
	t *testing.T,
	campaignID uuid.UUID,
	supporterID uuid.UUID,
	amount int64,
	txHash string,
	createdAt time.Time,
	blockNumber int,
	logIndex int,
) {
	t.Helper()
	var donorAddress string
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT wallet_address FROM users WHERE id = $1`,
		supporterID,
	).Scan(&donorAddress); err != nil {
		t.Fatalf("get donation supporter wallet: %v", err)
	}
	mustCreateDonationForAddress(
		t,
		campaignID,
		&supporterID,
		donorAddress,
		amount,
		txHash,
		createdAt,
		blockNumber,
		logIndex,
	)
}

func mustCreateDonationForAddress(
	t *testing.T,
	campaignID uuid.UUID,
	supporterID *uuid.UUID,
	donorAddress string,
	amount int64,
	txHash string,
	createdAt time.Time,
	blockNumber int,
	logIndex int,
) {
	t.Helper()
	eventID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO blockchain_events (id, tx_hash, log_index, type, block_number, created_at)
		 VALUES ($1, $2, $3, 'donation_created', $4, $5)`,
		eventID,
		txHash,
		logIndex,
		blockNumber,
		createdAt,
	); err != nil {
		t.Fatalf("prepare donation blockchain event: %v", err)
	}
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO donations (id, campaign_id, supporter_id, donor_address, event_id, amount)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(),
		campaignID,
		supporterID,
		donorAddress,
		eventID,
		amount,
	); err != nil {
		t.Fatalf("prepare donation: %v", err)
	}
}

func newSupporter(email, wallet string, imageObjectKey *string) domain.Supporter {
	return domain.Supporter{
		User: domain.User{
			ID:            uuid.New(),
			Role:          domain.UserRoleSupporter,
			Email:         email,
			WalletAddress: wallet,
		},
		Name:           "Supporter",
		ImageObjectKey: imageObjectKey,
	}
}

func mustCreateSupporter(t *testing.T, repo *repository.PostgresSupporterRepository, supporter domain.Supporter) {
	t.Helper()
	if _, err := repo.Create(t.Context(), supporter); err != nil {
		t.Fatalf("prepare supporter: %v", err)
	}
}

func assertSupporterPersistence(t *testing.T, supporter domain.Supporter, wantPersisted bool) {
	t.Helper()

	var (
		email  string
		wallet string
		name   string
		image  sql.NullString
	)
	err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT u.email, u.wallet_address, s.name, s.image_object_key
		 FROM users u
		 JOIN supporters s ON s.id = u.id
		 WHERE u.id = $1`,
		supporter.ID,
	).Scan(&email, &wallet, &name, &image)

	if !wantPersisted {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("persisted row error = %v, want sql.ErrNoRows", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("query persisted supporter: %v", err)
	}
	if email != supporter.Email || wallet != supporter.WalletAddress || name != supporter.Name {
		t.Errorf("persisted supporter = %q/%q/%q", email, wallet, name)
	}
	if supporter.ImageObjectKey == nil {
		if image.Valid {
			t.Errorf("image object key = %q, want SQL NULL", image.String)
		}
	} else if !image.Valid || image.String != *supporter.ImageObjectKey {
		t.Errorf("image object key = %#v, want %q", image, *supporter.ImageObjectKey)
	}
}

func stringPointer(value string) *string {
	return &value
}
