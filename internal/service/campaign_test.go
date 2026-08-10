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
	created      domain.Campaign
	err          error
	calls        int
	wallet       string
	campaign     domain.Campaign
	minimumEndAt time.Time
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
