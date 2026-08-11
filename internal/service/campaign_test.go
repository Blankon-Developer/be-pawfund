package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/google/uuid"
)

type campaignRepositoryStub struct {
	created           domain.Campaign
	err               error
	calls             int
	wallet            string
	campaign          domain.Campaign
	minimumEndAt      time.Time
	listed            []domain.Campaign
	listErr           error
	listCalls         int
	listWallet        string
	listOptions       domain.CampaignListOptions
	publicListed      []domain.PublicCampaignListItem
	publicListErr     error
	publicListCalls   int
	publicListOptions domain.CampaignListOptions
	publicRetrieved   domain.PublicCampaignDetail
	publicFindErr     error
	publicFindCalls   int
	publicAddress     string
	retrieved         domain.Campaign
	findErr           error
	findCalls         int
	findWallet        string
	findID            uuid.UUID
}

func (s *campaignRepositoryStub) ListPublic(
	_ context.Context,
	options domain.CampaignListOptions,
) ([]domain.PublicCampaignListItem, error) {
	s.publicListCalls++
	s.publicListOptions = options
	if s.publicListErr != nil {
		return nil, s.publicListErr
	}
	return s.publicListed, nil
}

func (s *campaignRepositoryStub) FindPublicByContractAddress(
	_ context.Context,
	contractAddress string,
) (domain.PublicCampaignDetail, error) {
	s.publicFindCalls++
	s.publicAddress = contractAddress
	if s.publicFindErr != nil {
		return domain.PublicCampaignDetail{}, s.publicFindErr
	}
	return s.publicRetrieved, nil
}

func (s *campaignRepositoryStub) ListForFundraiser(
	_ context.Context,
	walletAddress string,
	options domain.CampaignListOptions,
) ([]domain.Campaign, error) {
	s.listCalls++
	s.listWallet = walletAddress
	s.listOptions = options
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listed, nil
}

func (s *campaignRepositoryStub) FindByIDForFundraiser(
	_ context.Context,
	walletAddress string,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	s.findCalls++
	s.findWallet = walletAddress
	s.findID = campaignID
	if s.findErr != nil {
		return domain.Campaign{}, s.findErr
	}
	return s.retrieved, nil
}

func (s *campaignRepositoryStub) CreatePending(
	_ context.Context,
	walletAddress string,
	campaign domain.Campaign,
	minimumEndAt time.Time,
) (domain.Campaign, error) {
	s.calls++
	s.wallet = walletAddress
	s.campaign = campaign
	s.minimumEndAt = minimumEndAt
	if s.err != nil {
		return domain.Campaign{}, s.err
	}
	if s.created.ID != uuid.Nil {
		return s.created, nil
	}
	campaign.CreatedAt = time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	return campaign, nil
}

func TestCampaignServiceCreate(t *testing.T) {
	fixedNow := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	fixedID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	idFailure := errors.New("UUID source failed")
	repositoryFailure := errors.New("database unavailable")

	tests := []struct {
		name               string
		idError            error
		repositoryError    error
		wantError          error
		wantRepositoryCall bool
	}{
		{name: "creates a normalized pending campaign", wantRepositoryCall: true},
		{name: "maps missing fundraiser", repositoryError: repository.ErrCampaignFundraiserNotFound, wantError: ErrProfileNotFound, wantRepositoryCall: true},
		{name: "maps idempotency conflict", repositoryError: repository.ErrCampaignIdempotencyConflict, wantError: ErrCampaignIdempotencyConflict, wantRepositoryCall: true},
		{name: "maps end time rule", repositoryError: repository.ErrCampaignEndAtTooSoon, wantError: ErrCampaignEndAtTooSoon, wantRepositoryCall: true},
		{name: "wraps repository failure", repositoryError: repositoryFailure, wantError: repositoryFailure, wantRepositoryCall: true},
		{name: "wraps UUID failure", idError: idFailure, wantError: idFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &campaignRepositoryStub{err: test.repositoryError}
			campaignService := NewCampaignService(repo, func() (uuid.UUID, error) {
				if test.idError != nil {
					return uuid.Nil, test.idError
				}
				return fixedID, nil
			})
			campaignService.now = func() time.Time { return fixedNow }

			created, err := campaignService.Create(t.Context(), CreateCampaignInput{
				WalletAddress:    " 0xFundraiser ",
				IdempotencyKey:   " create-rescue-1 ",
				Title:            " Emergency Rescue ",
				ShortDescription: " Help rescued animals ",
				Story:            " A long rescue story. ",
				GoalAmount:       10_000_000_000,
				EndAt:            fixedNow.Add(30 * 24 * time.Hour),
				ImageObjectKey:   " campaigns/rescue.png ",
				Country:          " Indonesia ",
				ZipCode:          " 10110 ",
			})

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Create() error = %v, want %v", err, test.wantError)
				}
				if test.repositoryError != nil && !errors.Is(test.repositoryError, repository.ErrCampaignFundraiserNotFound) && !errors.Is(test.repositoryError, repository.ErrCampaignIdempotencyConflict) && !errors.Is(test.repositoryError, repository.ErrCampaignEndAtTooSoon) && !strings.Contains(err.Error(), "create pending campaign") {
					t.Errorf("Create() error lacks operation context: %v", err)
				}
			} else if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
			if (repo.calls > 0) != test.wantRepositoryCall {
				t.Errorf("repository calls = %d, want called = %v", repo.calls, test.wantRepositoryCall)
			}
			if !test.wantRepositoryCall {
				return
			}
			if repo.wallet != "0xFundraiser" || repo.campaign.IdempotencyKey != "create-rescue-1" {
				t.Errorf("repository identity = %q/%q", repo.wallet, repo.campaign.IdempotencyKey)
			}
			if repo.campaign.ID != fixedID || repo.campaign.Title != "Emergency Rescue" || repo.campaign.ImageObjectKey != "campaigns/rescue.png" {
				t.Errorf("repository campaign = %#v", repo.campaign)
			}
			if repo.campaign.Status != domain.CampaignStatusActive || repo.campaign.DeploymentStatus != domain.CampaignDeploymentStatusPending {
				t.Errorf("repository campaign state = %q/%q", repo.campaign.Status, repo.campaign.DeploymentStatus)
			}
			if !repo.minimumEndAt.Equal(fixedNow.Add(CampaignDeploymentLeadTime)) {
				t.Errorf("minimum endAt = %v", repo.minimumEndAt)
			}
			if test.wantError == nil && created.ID != fixedID {
				t.Errorf("created campaign = %#v", created)
			}
		})
	}
}

func TestCampaignServiceListMyCampaigns(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	status := domain.CampaignStatusActive

	tests := []struct {
		name            string
		repositoryError error
		wantError       error
	}{
		{name: "returns owned campaigns"},
		{name: "wraps repository failure", repositoryError: repositoryFailure, wantError: repositoryFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &campaignRepositoryStub{
				listed:  []domain.Campaign{{Title: "Emergency Rescue"}},
				listErr: test.repositoryError,
			}
			campaignService := NewCampaignService(repo, nil)
			options := domain.CampaignListOptions{
				Search:   " rescue ",
				Sort:     domain.CampaignListSortMostDonated,
				Status:   &status,
				Page:     2,
				PageSize: 25,
			}

			campaigns, err := campaignService.ListMyCampaigns(t.Context(), " 0xFundraiser ", options)

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ListMyCampaigns() error = %v, want %v", err, test.wantError)
				}
				if !strings.Contains(err.Error(), "list fundraiser campaigns") {
					t.Errorf("ListMyCampaigns() error lacks operation context: %v", err)
				}
			} else if err != nil {
				t.Fatalf("ListMyCampaigns() unexpected error: %v", err)
			}
			if repo.listCalls != 1 || repo.listWallet != "0xFundraiser" || repo.listOptions.Search != "rescue" || repo.listOptions.Sort != domain.CampaignListSortMostDonated || repo.listOptions.Status == nil || *repo.listOptions.Status != domain.CampaignStatusActive || repo.listOptions.Page != 2 || repo.listOptions.PageSize != 25 {
				t.Errorf("repository input = calls:%d wallet:%q options:%#v", repo.listCalls, repo.listWallet, repo.listOptions)
			}
			if test.wantError == nil && (len(campaigns) != 1 || campaigns[0].Title != "Emergency Rescue") {
				t.Errorf("campaigns = %#v", campaigns)
			}
		})
	}
}

func TestCampaignServiceListPublicCampaigns(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")

	tests := []struct {
		name            string
		repositoryError error
		wantError       error
	}{
		{name: "returns public campaigns"},
		{name: "wraps repository failure", repositoryError: repositoryFailure, wantError: repositoryFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &campaignRepositoryStub{
				publicListed:  []domain.PublicCampaignListItem{{Campaign: domain.Campaign{Title: "Emergency Rescue"}}},
				publicListErr: test.repositoryError,
			}
			campaignService := NewCampaignService(repo, nil)
			options := domain.CampaignListOptions{
				Search:   " rescue ",
				Sort:     domain.CampaignListSortRandom,
				Page:     2,
				PageSize: 25,
			}

			campaigns, err := campaignService.ListPublicCampaigns(t.Context(), options)

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ListPublicCampaigns() error = %v, want %v", err, test.wantError)
				}
				if !strings.Contains(err.Error(), "list public campaigns") {
					t.Errorf("ListPublicCampaigns() error lacks operation context: %v", err)
				}
			} else if err != nil {
				t.Fatalf("ListPublicCampaigns() unexpected error: %v", err)
			}
			if repo.publicListCalls != 1 || repo.publicListOptions.Search != "rescue" || repo.publicListOptions.Sort != domain.CampaignListSortRandom || repo.publicListOptions.Page != 2 || repo.publicListOptions.PageSize != 25 {
				t.Errorf("repository input = calls:%d options:%#v", repo.publicListCalls, repo.publicListOptions)
			}
			if test.wantError == nil && (len(campaigns) != 1 || campaigns[0].Title != "Emergency Rescue") {
				t.Errorf("campaigns = %#v", campaigns)
			}
		})
	}
}

func TestCampaignServiceGetPublicCampaignDetail(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")

	tests := []struct {
		name            string
		repositoryError error
		wantError       error
	}{
		{name: "returns the public campaign"},
		{name: "maps campaign not found", repositoryError: repository.ErrCampaignNotFound, wantError: ErrCampaignNotFound},
		{name: "wraps repository failure", repositoryError: repositoryFailure, wantError: repositoryFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &campaignRepositoryStub{
				publicRetrieved: domain.PublicCampaignDetail{Campaign: domain.Campaign{Title: "Emergency Rescue"}},
				publicFindErr:   test.repositoryError,
			}
			campaignService := NewCampaignService(repo, nil)

			campaign, err := campaignService.GetPublicCampaignDetail(t.Context(), " 0xCaMpAiGn ")

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("GetPublicCampaignDetail() error = %v, want %v", err, test.wantError)
				}
				if test.repositoryError == repositoryFailure && !strings.Contains(err.Error(), "get public campaign detail") {
					t.Errorf("GetPublicCampaignDetail() error lacks operation context: %v", err)
				}
			} else if err != nil {
				t.Fatalf("GetPublicCampaignDetail() unexpected error: %v", err)
			}
			if repo.publicFindCalls != 1 || repo.publicAddress != "0xCaMpAiGn" {
				t.Errorf("repository input = calls:%d address:%q", repo.publicFindCalls, repo.publicAddress)
			}
			if test.wantError == nil && campaign.Title != "Emergency Rescue" {
				t.Errorf("campaign = %#v", campaign)
			}
		})
	}
}

func TestCampaignServiceGetMyCampaignDetail(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	repositoryFailure := errors.New("database unavailable")

	tests := []struct {
		name            string
		repositoryError error
		wantError       error
	}{
		{name: "returns the owned campaign"},
		{name: "maps campaign not found", repositoryError: repository.ErrCampaignNotFound, wantError: ErrCampaignNotFound},
		{name: "wraps repository failure", repositoryError: repositoryFailure, wantError: repositoryFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &campaignRepositoryStub{
				retrieved: domain.Campaign{ID: campaignID, Title: "Emergency Rescue"},
				findErr:   test.repositoryError,
			}
			campaignService := NewCampaignService(repo, nil)

			campaign, err := campaignService.GetMyCampaignDetail(t.Context(), " 0xFundraiser ", campaignID)

			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("GetMyCampaignDetail() error = %v, want %v", err, test.wantError)
				}
				if test.repositoryError == repositoryFailure && !strings.Contains(err.Error(), "get fundraiser campaign detail") {
					t.Errorf("GetMyCampaignDetail() error lacks operation context: %v", err)
				}
			} else if err != nil {
				t.Fatalf("GetMyCampaignDetail() unexpected error: %v", err)
			}
			if repo.findCalls != 1 || repo.findWallet != "0xFundraiser" || repo.findID != campaignID {
				t.Errorf("repository input = calls:%d wallet:%q campaign:%s", repo.findCalls, repo.findWallet, repo.findID)
			}
			if test.wantError == nil && campaign.ID != campaignID {
				t.Errorf("campaign = %#v", campaign)
			}
		})
	}
}

func TestCampaignServiceNormalizesEndAtToPostgresPrecision(t *testing.T) {
	repo := &campaignRepositoryStub{}
	campaignService := NewCampaignService(repo, func() (uuid.UUID, error) { return uuid.New(), nil })
	fixedNow := time.Date(2026, 8, 10, 4, 0, 0, 123456789, time.UTC)
	campaignService.now = func() time.Time { return fixedNow }
	inputEndAt := fixedNow.Add(24 * time.Hour)

	_, err := campaignService.Create(t.Context(), CreateCampaignInput{
		WalletAddress:    "0xFundraiser",
		IdempotencyKey:   "timestamp-precision",
		Title:            "Emergency Rescue",
		ShortDescription: "Help rescued animals",
		Story:            "A long rescue story.",
		GoalAmount:       1,
		EndAt:            inputEndAt,
		ImageObjectKey:   "campaigns/rescue.png",
		Country:          "Indonesia",
		ZipCode:          "10110",
	})
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	want := inputEndAt.Truncate(time.Microsecond)
	if !repo.campaign.EndAt.Equal(want) || repo.campaign.EndAt.Nanosecond()%1000 != 0 {
		t.Errorf("normalized endAt = %v, want %v", repo.campaign.EndAt, want)
	}
}
