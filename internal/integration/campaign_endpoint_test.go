//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/app"
	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/handler"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/http/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/http/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/queue"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

const campaignIntegrationChainID = 84532

func TestCreateCampaignEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	const walletAddress = "0xFundraiserChecksum"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	router, jwtManager := newCampaignIntegrationRouter(t)
	fundraiserToken, err := jwtManager.Generate(walletAddress, domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate fundraiser token: %v", err)
	}
	endAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	stagingImageKey := "tmp/campaigns/0198a123-4567-7abc-8123-456789abcdef.webp"
	canonicalImageKey := "campaigns/0198a123-4567-7abc-8123-456789abcdef.webp"
	putProfileImageObject(t, stagingImageKey)
	t.Cleanup(func() {
		removeIntegrationObject(t, stagingImageKey)
		removeIntegrationObject(t, canonicalImageKey)
	})
	validBody := `{"title":"Emergency Rescue","shortDescription":"Help rescued animals","story":"A long rescue story.","goalAmount":10000000000,"endAt":"` + endAt.Format(time.RFC3339) + `","imageObjectKey":"` + stagingImageKey + `","country":"Indonesia","zipCode":"10110"}`

	deploymentTopology := app.CampaignDeploymentTopology()
	deploymentQueue := openTestQueueClient(t, "")
	if err := queue.DeclareOn(deploymentQueue, deploymentTopology); err != nil {
		t.Fatalf("declare campaign deployment topology: %v", err)
	}
	purgeQueue(t, deploymentQueue, deploymentTopology.Queue)

	first := requestCreateCampaign(t, router, fundraiserToken, "create-rescue-1", validBody)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d; body: %s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Code string `json:"code"`
		Data struct {
			ID              string                `json:"id"`
			GoalAmount      int64                 `json:"goalAmount"`
			RaisedAmount    int64                 `json:"raisedAmount"`
			DonorCount      int64                 `json:"donorCount"`
			Status          domain.CampaignStatus `json:"status"`
			ContractAddress *string               `json:"contractAddress"`
			ImageURL        *string               `json:"imageUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if firstEnvelope.Code != "CAMPAIGN_CREATED" || firstEnvelope.Data.ID == "" {
		t.Errorf("create response = %#v", firstEnvelope)
	}
	if firstEnvelope.Data.GoalAmount != 10_000_000_000 || firstEnvelope.Data.RaisedAmount != 0 || firstEnvelope.Data.DonorCount != 0 {
		t.Errorf("create amounts = %#v", firstEnvelope.Data)
	}
	if firstEnvelope.Data.Status != domain.CampaignStatusPending || firstEnvelope.Data.ContractAddress != nil {
		t.Errorf("create state = %#v", firstEnvelope.Data)
	}
	wantImageURL := "https://cdn.example.com/pawfund/" + canonicalImageKey
	if firstEnvelope.Data.ImageURL == nil || *firstEnvelope.Data.ImageURL != wantImageURL {
		t.Errorf("image URL = %#v, want %q", firstEnvelope.Data.ImageURL, wantImageURL)
	}
	if _, err := testStorageClient.StatObject(t.Context(), testStorageBucket, canonicalImageKey, minio.StatObjectOptions{}); err != nil {
		t.Errorf("canonical campaign image missing: %v", err)
	}
	if _, err := testStorageClient.StatObject(t.Context(), testStorageBucket, stagingImageKey, minio.StatObjectOptions{}); err == nil {
		t.Error("staging campaign image still exists")
	}

	replayed := requestCreateCampaign(t, router, fundraiserToken, "create-rescue-1", validBody)
	if replayed.Code != http.StatusCreated {
		t.Fatalf("replay status = %d; body: %s", replayed.Code, replayed.Body.String())
	}
	var replayEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayEnvelope); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayEnvelope.Data.ID != firstEnvelope.Data.ID {
		t.Errorf("replay ID = %q, want %q", replayEnvelope.Data.ID, firstEnvelope.Data.ID)
	}

	conflictingBody := strings.Replace(validBody, "Emergency Rescue", "Different Campaign", 1)
	conflict := requestCreateCampaign(t, router, fundraiserToken, "create-rescue-1", conflictingBody)
	result := decodeAuthEndpointResult(t, conflict)
	if result.HTTPStatus != http.StatusConflict || result.Code != "IDEMPOTENCY_KEY_CONFLICT" {
		t.Errorf("conflict = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	var count int
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT count(*) FROM campaigns WHERE fundraiser_id = $1 AND idempotency_key = 'create-rescue-1'`,
		fundraiser.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count created campaigns: %v", err)
	}
	if count != 1 {
		t.Errorf("campaign count = %d, want 1", count)
	}

	supporterToken, err := jwtManager.Generate("0xSupporter", domain.UserRoleSupporter, time.Hour)
	if err != nil {
		t.Fatalf("generate supporter token: %v", err)
	}
	forbidden := requestCreateCampaign(t, router, supporterToken, "supporter-key", validBody)
	result = decodeAuthEndpointResult(t, forbidden)
	if result.HTTPStatus != http.StatusForbidden || result.Code != "FUNDRAISER_ACCESS_REQUIRED" {
		t.Errorf("supporter create = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	staleToken, err := jwtManager.Generate("0xDeletedFundraiser", domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate stale fundraiser token: %v", err)
	}
	notFound := requestCreateCampaign(t, router, staleToken, "stale-key", validBody)
	result = decodeAuthEndpointResult(t, notFound)
	if result.HTTPStatus != http.StatusNotFound || result.Code != "PROFILE_NOT_FOUND" {
		t.Errorf("stale fundraiser create = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	deliveries := collectQueueDeliveries(t, deploymentQueue, deploymentTopology.Queue, 2)
	if string(deliveries[0].Body) != string(deliveries[1].Body) {
		t.Fatalf("replay body = %s, want %s", deliveries[1].Body, deliveries[0].Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(deliveries[0].Body, &payload); err != nil {
		t.Fatalf("decode deployment task: %v", err)
	}
	if len(payload) != 6 {
		t.Fatalf("deployment fields = %#v", payload)
	}
	var task struct {
		CampaignID       string `json:"campaignId"`
		FundraiserID     string `json:"fundraiserId"`
		FundraiserWallet string `json:"fundraiserWallet"`
		GoalAmount       int64  `json:"goalAmount"`
		EndAt            string `json:"endAt"`
		ChainID          int    `json:"chainId"`
	}
	if err := json.Unmarshal(deliveries[0].Body, &task); err != nil {
		t.Fatalf("decode deployment task: %v", err)
	}
	if task.CampaignID != firstEnvelope.Data.ID || task.FundraiserID != fundraiser.ID.String() || task.FundraiserWallet != walletAddress || task.GoalAmount != 10_000_000_000 || task.EndAt != endAt.Format(time.RFC3339) || task.ChainID != campaignIntegrationChainID {
		t.Fatalf("deployment task = %#v", task)
	}
}

func TestGetMyCampaignDetailEndpoint(t *testing.T) {
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
	campaign, err := campaignRepo.CreatePending(
		t.Context(),
		ownerWallet,
		newPendingCampaign(time.Now().UTC().Add(30*time.Hour), "get-owned-campaign"),
		time.Now().UTC().Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("create campaign fixture: %v", err)
	}

	router, jwtManager := newCampaignIntegrationRouter(t)
	ownerToken, err := jwtManager.Generate(ownerWallet, domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	response := requestMyCampaignDetail(t, router, ownerToken, campaign.ID.String())
	if response.Code != http.StatusOK {
		t.Fatalf("get campaign status = %d; body: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code string `json:"code"`
		Data struct {
			Title           string                `json:"title"`
			GoalAmount      int64                 `json:"goalAmount"`
			Status          domain.CampaignStatus `json:"status"`
			ContractAddress *string               `json:"contractAddress"`
			ImageURL        *string               `json:"imageUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode campaign detail response: %v", err)
	}
	if envelope.Code != "CAMPAIGN_RETRIEVED" || envelope.Data.Title != campaign.Title || envelope.Data.GoalAmount != campaign.GoalAmount {
		t.Errorf("campaign detail = %#v", envelope)
	}
	if envelope.Data.Status != domain.CampaignStatusPending || envelope.Data.ContractAddress != nil {
		t.Errorf("campaign detail state = %#v", envelope.Data)
	}
	wantImageURL := "https://cdn.example.com/pawfund/campaigns/rescue.png"
	if envelope.Data.ImageURL == nil || *envelope.Data.ImageURL != wantImageURL {
		t.Errorf("campaign image URL = %#v, want %q", envelope.Data.ImageURL, wantImageURL)
	}

	otherToken, err := jwtManager.Generate(otherWallet, domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate other token: %v", err)
	}
	result := decodeAuthEndpointResult(t, requestMyCampaignDetail(t, router, otherToken, campaign.ID.String()))
	if result.HTTPStatus != http.StatusNotFound || result.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("other fundraiser detail = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	supporterToken, err := jwtManager.Generate("0xSupporter", domain.UserRoleSupporter, time.Hour)
	if err != nil {
		t.Fatalf("generate supporter token: %v", err)
	}
	result = decodeAuthEndpointResult(t, requestMyCampaignDetail(t, router, supporterToken, campaign.ID.String()))
	if result.HTTPStatus != http.StatusForbidden || result.Code != "FUNDRAISER_ACCESS_REQUIRED" {
		t.Errorf("supporter detail = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	result = decodeAuthEndpointResult(t, requestMyCampaignDetail(t, router, ownerToken, "not-a-uuid"))
	if result.HTTPStatus != http.StatusUnprocessableEntity || result.Code != "VALIDATION_ERROR" {
		t.Errorf("invalid campaign ID = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}
}

func TestGetMyCampaignListEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	const (
		ownerWallet = "0xFundraiserChecksum"
		otherWallet = "0xOtherFundraiser"
	)
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	owner := newFundraiser("owner@example.com", ownerWallet, nil)
	other := newFundraiser("other@example.com", otherWallet, nil)
	mustCreateFundraiser(t, fundraiserRepo, owner)
	mustCreateFundraiser(t, fundraiserRepo, other)

	pending := mustCreateListedCampaign(
		t,
		owner.ID,
		"Queued Intake",
		"Waiting to go live",
		domain.CampaignStatusPending,
		100,
		0,
		time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	)
	active := mustCreateListedCampaign(
		t,
		owner.ID,
		"Emergency Rescue",
		"Help animals urgently",
		domain.CampaignStatusActive,
		100,
		90,
		time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	)
	completed := mustCreateListedCampaign(
		t,
		owner.ID,
		"Shelter Supplies",
		"Mission completed for the shelter",
		domain.CampaignStatusCompleted,
		100,
		80,
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	cancelled := mustCreateListedCampaign(
		t,
		owner.ID,
		"Cancelled Intake",
		"Cancelled campaign",
		domain.CampaignStatusCancelled,
		50,
		10,
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	)
	failed := mustCreateListedCampaign(
		t,
		owner.ID,
		"Failed Deploy",
		"Deployment did not finish",
		domain.CampaignStatusFailed,
		100,
		0,
		time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	)
	mustCreateListedCampaign(
		t,
		other.ID,
		"Other Fundraiser Rescue",
		"This campaign must not be visible",
		domain.CampaignStatusActive,
		100,
		99,
		time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	)

	router, jwtManager := newCampaignIntegrationRouter(t)
	ownerToken, err := jwtManager.Generate(ownerWallet, domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}

	tests := []struct {
		name           string
		query          string
		wantIDs        []uuid.UUID
		wantPagination httpx.Pagination
	}{
		{name: "lists owned campaigns newest first", wantIDs: []uuid.UUID{pending, active, completed, cancelled, failed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 5}},
		{name: "searches titles", query: "?search=emergency", wantIDs: []uuid.UUID{active}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "searches short descriptions", query: "?search=mission", wantIDs: []uuid.UUID{completed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters pending campaigns", query: "?filter=pending", wantIDs: []uuid.UUID{pending}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters active campaigns", query: "?filter=active", wantIDs: []uuid.UUID{active}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters completed campaigns", query: "?filter=completed", wantIDs: []uuid.UUID{completed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters cancelled campaigns", query: "?filter=cancelled", wantIDs: []uuid.UUID{cancelled}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters failed campaigns", query: "?filter=failed", wantIDs: []uuid.UUID{failed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "sorts by most donated", query: "?sortBy=most-donated", wantIDs: []uuid.UUID{active, completed, cancelled, pending, failed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 5}},
		{name: "sorts by percentage close to goal", query: "?sortBy=close-to-goal", wantIDs: []uuid.UUID{active, completed, cancelled, pending, failed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 5}},
		{name: "paginates oldest ordering", query: "?sortBy=oldest&page=2&pageSize=1", wantIDs: []uuid.UUID{cancelled}, wantPagination: httpx.Pagination{Current: 2, PageSize: 1, TotalPages: 5, TotalItems: 5}},
		{name: "returns an empty page", query: "?page=5&pageSize=10", wantIDs: []uuid.UUID{}, wantPagination: httpx.Pagination{Current: 5, PageSize: 10, TotalPages: 1, TotalItems: 5}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestMyCampaignList(t, router, ownerToken, test.query)
			if response.Code != http.StatusOK {
				t.Fatalf("list status = %d; body: %s", response.Code, response.Body.String())
			}
			var envelope struct {
				Code       string           `json:"code"`
				Pagination httpx.Pagination `json:"pagination"`
				Data       []struct {
					ID              uuid.UUID             `json:"id"`
					DonorCount      int64                 `json:"donorCount"`
					ImageURL        *string               `json:"imageUrl"`
					ContractAddress *string               `json:"contractAddress"`
					Status          domain.CampaignStatus `json:"status"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode campaign list response: %v", err)
			}
			if envelope.Code != "CAMPAIGNS_RETRIEVED" {
				t.Errorf("list code = %q", envelope.Code)
			}
			if envelope.Pagination != test.wantPagination {
				t.Errorf("pagination = %#v, want %#v", envelope.Pagination, test.wantPagination)
			}
			if len(envelope.Data) != len(test.wantIDs) {
				t.Fatalf("campaign count = %d, want %d; data: %#v", len(envelope.Data), len(test.wantIDs), envelope.Data)
			}
			for index, wantID := range test.wantIDs {
				if envelope.Data[index].ID != wantID {
					t.Errorf("campaign ID at %d = %s, want %s", index, envelope.Data[index].ID, wantID)
				}
			}
			if len(envelope.Data) > 0 && (envelope.Data[0].ImageURL == nil || *envelope.Data[0].ImageURL != "https://cdn.example.com/pawfund/campaigns/listed-campaign.png") {
				t.Errorf("list item fields = %#v", envelope.Data[0])
			}
		})
	}

	supporterToken, err := jwtManager.Generate("0xSupporter", domain.UserRoleSupporter, time.Hour)
	if err != nil {
		t.Fatalf("generate supporter token: %v", err)
	}
	forbidden := decodeAuthEndpointResult(t, requestMyCampaignList(t, router, supporterToken, ""))
	if forbidden.HTTPStatus != http.StatusForbidden || forbidden.Code != "FUNDRAISER_ACCESS_REQUIRED" {
		t.Errorf("supporter list = %d/%q; body: %s", forbidden.HTTPStatus, forbidden.Code, forbidden.Body)
	}
}

func TestGetPublicCampaignListEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	fundraiserImageObjectKey := "profiles/public-fundraiser.png"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("public@example.com", "0xPublicFundraiser", &fundraiserImageObjectKey)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	active := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Emergency Rescue",
		"Help animals urgently",
		domain.CampaignStatusActive,
		100,
		90,
		time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	)
	completed := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Shelter Supplies",
		"Mission completed for the shelter",
		domain.CampaignStatusCompleted,
		100,
		80,
		time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	)
	cancelled := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Cancelled Intake",
		"Cancelled campaign",
		domain.CampaignStatusCancelled,
		50,
		10,
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	)
	mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Pending Deployment",
		"This campaign must not be visible",
		domain.CampaignStatusPending,
		100,
		0,
		time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	)
	mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Failed Deployment",
		"This failed campaign must not be visible",
		domain.CampaignStatusFailed,
		100,
		0,
		time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	)

	router, _ := newCampaignIntegrationRouter(t)
	tests := []struct {
		name           string
		query          string
		wantIDs        []uuid.UUID
		orderedIDs     bool
		wantPagination httpx.Pagination
	}{
		{
			name:           "lists only live on-chain campaigns in randomized default order",
			wantIDs:        []uuid.UUID{active, completed, cancelled},
			wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 3},
		},
		{name: "searches titles", query: "?search=emergency", wantIDs: []uuid.UUID{active}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "searches short descriptions", query: "?search=mission", wantIDs: []uuid.UUID{completed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters active campaigns", query: "?filter=active", wantIDs: []uuid.UUID{active}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters completed campaigns", query: "?filter=completed", wantIDs: []uuid.UUID{completed}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{name: "filters cancelled campaigns", query: "?filter=cancelled", wantIDs: []uuid.UUID{cancelled}, wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 1}},
		{
			name:           "sorts by most donated when requested",
			query:          "?sortBy=most-donated",
			wantIDs:        []uuid.UUID{active, completed, cancelled},
			orderedIDs:     true,
			wantPagination: httpx.Pagination{Current: 1, PageSize: 10, TotalPages: 1, TotalItems: 3},
		},
		{
			name:           "paginates explicit oldest ordering",
			query:          "?sortBy=oldest&page=2&pageSize=1",
			wantIDs:        []uuid.UUID{completed},
			orderedIDs:     true,
			wantPagination: httpx.Pagination{Current: 2, PageSize: 1, TotalPages: 3, TotalItems: 3},
		},
		{
			name:           "returns an empty page",
			query:          "?page=5&pageSize=10",
			wantIDs:        []uuid.UUID{},
			orderedIDs:     true,
			wantPagination: httpx.Pagination{Current: 5, PageSize: 10, TotalPages: 1, TotalItems: 3},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestPublicCampaignList(t, router, test.query)
			if response.Code != http.StatusOK {
				t.Fatalf("list status = %d; body: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			var envelope struct {
				Code       string           `json:"code"`
				Pagination httpx.Pagination `json:"pagination"`
				Data       []struct {
					ID                 uuid.UUID             `json:"id"`
					CampaignImageURL   *string               `json:"campaignImageUrl"`
					FundraiserImageURL *string               `json:"fundraiserImageUrl"`
					ContractAddress    *string               `json:"contractAddress"`
					Status             domain.CampaignStatus `json:"status"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode campaign list response: %v", err)
			}
			if envelope.Code != "CAMPAIGNS_RETRIEVED" {
				t.Errorf("list code = %q", envelope.Code)
			}
			if envelope.Pagination != test.wantPagination {
				t.Errorf("pagination = %#v, want %#v", envelope.Pagination, test.wantPagination)
			}
			if len(envelope.Data) != len(test.wantIDs) {
				t.Fatalf("campaign count = %d, want %d; data: %#v", len(envelope.Data), len(test.wantIDs), envelope.Data)
			}

			gotIDs := make([]uuid.UUID, 0, len(envelope.Data))
			for _, campaign := range envelope.Data {
				gotIDs = append(gotIDs, campaign.ID)
			}
			if test.orderedIDs {
				for index, wantID := range test.wantIDs {
					if gotIDs[index] != wantID {
						t.Errorf("campaign ID at %d = %s, want %s", index, gotIDs[index], wantID)
					}
				}
			} else {
				gotIDSet := make(map[uuid.UUID]struct{}, len(gotIDs))
				for _, gotID := range gotIDs {
					gotIDSet[gotID] = struct{}{}
				}
				for _, wantID := range test.wantIDs {
					if _, found := gotIDSet[wantID]; !found {
						t.Errorf("campaign IDs = %v, missing %s", gotIDs, wantID)
					}
				}
			}

			if len(envelope.Data) > 0 {
				campaign := envelope.Data[0]
				if campaign.CampaignImageURL == nil || *campaign.CampaignImageURL != "https://cdn.example.com/pawfund/campaigns/public-listed-campaign.png" || campaign.FundraiserImageURL == nil || *campaign.FundraiserImageURL != "https://cdn.example.com/pawfund/profiles/public-fundraiser.png" || campaign.ContractAddress == nil || campaign.Status == "" {
					t.Errorf("public campaign fields = %#v", campaign)
				}
			}
		})
	}
}

func TestGetPublicCampaignDetailEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	fundraiserImageObjectKey := "profiles/public-fundraiser.png"
	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("public@example.com", "0xPublicFundraiser", &fundraiserImageObjectKey)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	visibleCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Emergency Rescue",
		"Help animals urgently",
		domain.CampaignStatusCompleted,
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

	router, _ := newCampaignIntegrationRouter(t)
	response := requestPublicCampaignDetail(t, router, strings.ToLower(visibleAddress))
	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d; body: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Code string `json:"code"`
		Data struct {
			ID              uuid.UUID             `json:"id"`
			Title           string                `json:"title"`
			Status          domain.CampaignStatus `json:"status"`
			ContractAddress string                `json:"contractAddress"`
			ImageURL        string                `json:"imageUrl"`
			Fundraiser      struct {
				ID       uuid.UUID `json:"id"`
				Name     string    `json:"name"`
				Address  string    `json:"address"`
				ImageURL *string   `json:"imageUrl"`
			} `json:"fundraiser"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode campaign detail response: %v", err)
	}
	if envelope.Code != "CAMPAIGN_RETRIEVED" || envelope.Data.ID != visibleCampaignID || envelope.Data.Title != "Emergency Rescue" || envelope.Data.Status != domain.CampaignStatusCompleted || envelope.Data.ContractAddress != visibleAddress {
		t.Errorf("campaign detail = %#v", envelope)
	}
	if envelope.Data.ImageURL != "https://cdn.example.com/pawfund/campaigns/public-listed-campaign.png" || envelope.Data.Fundraiser.ID != fundraiser.ID || envelope.Data.Fundraiser.Name != fundraiser.Name || envelope.Data.Fundraiser.Address != fundraiser.WalletAddress || envelope.Data.Fundraiser.ImageURL == nil || *envelope.Data.Fundraiser.ImageURL != "https://cdn.example.com/pawfund/profiles/public-fundraiser.png" {
		t.Errorf("campaign detail public fields = %#v", envelope.Data)
	}

	hiddenCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Pending Deployment",
		"This campaign must not be visible",
		domain.CampaignStatusPending,
		100,
		0,
		time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	)
	const hiddenAddress = "0xPendingCampaign"
	mustAssignCampaignContractAddress(t, hiddenCampaignID, hiddenAddress)
	result := decodeAuthEndpointResult(t, requestPublicCampaignDetail(t, router, hiddenAddress))
	if result.HTTPStatus != http.StatusNotFound || result.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("pending public detail = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	failedCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Failed Deployment",
		"This failed campaign must not be visible",
		domain.CampaignStatusFailed,
		100,
		0,
		time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	)
	const failedAddress = "0xFailedCampaign"
	mustAssignCampaignContractAddress(t, failedCampaignID, failedAddress)
	result = decodeAuthEndpointResult(t, requestPublicCampaignDetail(t, router, failedAddress))
	if result.HTTPStatus != http.StatusNotFound || result.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("failed public detail = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}

	result = decodeAuthEndpointResult(t, requestPublicCampaignDetail(t, router, "0xUnknownCampaign"))
	if result.HTTPStatus != http.StatusNotFound || result.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("unknown public detail = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}
}

func TestGetPublicCampaignDonorsEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("campaign-donors@example.com", "0xCampaignDonorFundraiser", nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)
	createdAt := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	campaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Public Donor Campaign",
		"Campaign donor endpoint",
		domain.CampaignStatusActive,
		10_000,
		350,
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

	imageObjectKey := "profiles/public-donor.png"
	supporterRepo := database.NewPostgresSupporterRepository(testDatabase)
	donor := newSupporter("public-donor@example.com", "0xPublicDonor", &imageObjectKey)
	mustCreateSupporter(t, supporterRepo, donor)
	mustCreateDonationForAddress(t, campaignID, &donor.ID, donor.WalletAddress, 100, "0xPublicDonorFirst", createdAt.Add(time.Hour), 10, 0)
	mustCreateDonationForAddress(t, campaignID, &donor.ID, donor.WalletAddress, 200, "0xPublicDonorLatest", createdAt.Add(2*time.Hour), 11, 0)
	mustCreateDonationForAddress(t, campaignID, nil, "0xGuest", 50, "0xGuestDonor", createdAt.Add(3*time.Hour), 12, 0)

	router, _ := newCampaignIntegrationRouter(t)
	response := requestPublicCampaignDonors(t, router, strings.ToLower(contractAddress), "?sortBy=top&page=1&pageSize=1")
	if response.Code != http.StatusOK {
		t.Fatalf("donor list status = %d; body: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Status     string           `json:"status"`
		Code       string           `json:"code"`
		Pagination httpx.Pagination `json:"pagination"`
		Data       []struct {
			Name      *string `json:"name"`
			Address   string  `json:"address"`
			ImageURL  *string `json:"imageUrl"`
			Amount    int64   `json:"amount"`
			DonatedOn string  `json:"donatedOn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode donor list response: %v", err)
	}
	wantImageURL := "https://cdn.example.com/pawfund/profiles/public-donor.png"
	wantPagination := httpx.Pagination{Current: 1, PageSize: 1, TotalPages: 2, TotalItems: 2}
	if envelope.Status != "success" || envelope.Code != "CAMPAIGN_DONORS_RETRIEVED" || len(envelope.Data) != 1 {
		t.Fatalf("donor list envelope = %#v", envelope)
	}
	if envelope.Pagination != wantPagination {
		t.Errorf("donor list pagination = %#v, want %#v", envelope.Pagination, wantPagination)
	}
	if envelope.Data[0].Name == nil || *envelope.Data[0].Name != donor.Name || envelope.Data[0].Address != donor.WalletAddress || envelope.Data[0].ImageURL == nil || *envelope.Data[0].ImageURL != wantImageURL || envelope.Data[0].Amount != 300 || envelope.Data[0].DonatedOn != "2026-08-09T10:00:00Z" {
		t.Errorf("donor list item = %#v", envelope.Data[0])
	}

	emptyPage := requestPublicCampaignDonors(t, router, contractAddress, "?page=3&pageSize=1")
	var emptyEnvelope struct {
		Data       json.RawMessage  `json:"data"`
		Pagination httpx.Pagination `json:"pagination"`
	}
	wantEmptyPagination := httpx.Pagination{Current: 3, PageSize: 1, TotalPages: 2, TotalItems: 2}
	if emptyPage.Code != http.StatusOK || json.Unmarshal(emptyPage.Body.Bytes(), &emptyEnvelope) != nil || string(emptyEnvelope.Data) != "[]" {
		t.Errorf("empty donor page status/body = %d/%s", emptyPage.Code, emptyPage.Body.String())
	}
	if emptyEnvelope.Pagination != wantEmptyPagination {
		t.Errorf("empty donor page pagination = %#v, want %#v", emptyEnvelope.Pagination, wantEmptyPagination)
	}

	invalid := decodeAuthEndpointResult(t, requestPublicCampaignDonors(t, router, contractAddress, "?sortBy=largest&page=0&pageSize=101"))
	if invalid.HTTPStatus != http.StatusUnprocessableEntity || invalid.Code != "VALIDATION_ERROR" {
		t.Errorf("invalid donor query = %d/%q; body: %s", invalid.HTTPStatus, invalid.Code, invalid.Body)
	}

	failedCampaignID := mustCreatePublicListedCampaign(
		t,
		fundraiser.ID,
		"Failed Donor Campaign",
		"Failed campaign",
		domain.CampaignStatusFailed,
		10_000,
		0,
		createdAt.Add(time.Minute),
	)
	const failedAddress = "0xFailedDonorCampaign"
	mustAssignCampaignContractAddress(t, failedCampaignID, failedAddress)
	failed := decodeAuthEndpointResult(t, requestPublicCampaignDonors(t, router, failedAddress, ""))
	if failed.HTTPStatus != http.StatusNotFound || failed.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("failed donor campaign = %d/%q; body: %s", failed.HTTPStatus, failed.Code, failed.Body)
	}

	notFound := decodeAuthEndpointResult(t, requestPublicCampaignDonors(t, router, "0xUnknownCampaign", ""))
	if notFound.HTTPStatus != http.StatusNotFound || notFound.Code != "CAMPAIGN_NOT_FOUND" {
		t.Errorf("unknown donor campaign = %d/%q; body: %s", notFound.HTTPStatus, notFound.Code, notFound.Body)
	}
}

func newCampaignIntegrationRouter(t *testing.T) (http.Handler, *auth.JWTManager) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtManager, err := auth.NewJWTManager([]byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
	if err != nil {
		t.Fatalf("create public URL builder: %v", err)
	}
	campaignRepo := database.NewPostgresCampaignRepository(testDatabase)
	queueClient, err := queue.Open(t.Context(), queue.Config{URL: testQueueURL})
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	t.Cleanup(func() {
		if err := queueClient.Close(); err != nil {
			t.Errorf("close queue: %v", err)
		}
	})
	if err := queue.DeclareOn(queueClient, app.CampaignDeploymentTopology()); err != nil {
		t.Fatalf("declare campaign deployment topology: %v", err)
	}
	campaignService := service.NewCampaignService(
		campaignRepo,
		uuid.NewV7,
		newIntegrationObjectPromoter(t),
		app.NewCampaignDeploymentPublisher(queueClient),
		campaignIntegrationChainID,
	)
	application := &app.Application{
		CampaignHandler: handler.NewCampaignHandler(campaignService, urlBuilder, logger),
		Authenticate:    appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
}

func purgeQueue(t *testing.T, client *queue.QueueClient, queueName string) {
	t.Helper()
	conn := client.Conn()
	if conn == nil || conn.IsClosed() {
		t.Fatal("queue connection is not available")
	}
	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer channel.Close()
	if _, err := channel.QueuePurge(queueName, false); err != nil {
		t.Fatalf("purge queue %s: %v", queueName, err)
	}
}

func collectQueueDeliveries(t *testing.T, client *queue.QueueClient, queueName string, want int) []queue.Delivery {
	t.Helper()
	consumer, err := queue.NewConsumer(client, queue.ConsumerConfig{Queue: queueName})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got := make(chan queue.Delivery, want+1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- consumer.Run(ctx, func(_ context.Context, delivery queue.Delivery) error {
			got <- delivery
			return nil
		})
	}()

	deliveries := make([]queue.Delivery, 0, want)
	deadline := time.After(10 * time.Second)
	for len(deliveries) < want {
		select {
		case delivery := <-got:
			deliveries = append(deliveries, delivery)
		case <-deadline:
			t.Fatalf("got %d deliveries, want %d", len(deliveries), want)
		}
	}
	select {
	case extra := <-got:
		t.Fatalf("unexpected delivery: %s", extra.Body)
	case <-time.After(time.Second):
	}
	cancel()
	waitConsumerStop(t, errCh)
	return deliveries
}

func requestCreateCampaign(
	t *testing.T,
	router http.Handler,
	token string,
	idempotencyKey string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/fundraiser/campaigns", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requestMyCampaignDetail(
	t *testing.T,
	router http.Handler,
	token string,
	campaignID string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/campaigns/"+campaignID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requestMyCampaignList(
	t *testing.T,
	router http.Handler,
	token string,
	query string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/campaigns"+query, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requestPublicCampaignList(
	t *testing.T,
	router http.Handler,
	query string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/campaigns"+query, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requestPublicCampaignDetail(
	t *testing.T,
	router http.Handler,
	contractAddress string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/campaigns/"+contractAddress, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requestPublicCampaignDonors(
	t *testing.T,
	router http.Handler,
	contractAddress string,
	query string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/campaigns/"+contractAddress+"/donors"+query, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func mustCreateListedCampaign(
	t *testing.T,
	fundraiserID uuid.UUID,
	title string,
	shortDescription string,
	status domain.CampaignStatus,
	goalAmount int64,
	raisedAmount int64,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	eventID, contractAddress := maybeCreateCampaignChainData(t, status, createdAt)
	campaignID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO campaigns (
			id, fundraiser_id, event_id, title, short_description, story,
			goal_amount, raised_amount, donor_count, contract_address, end_at, image_object_key,
			country, zip_code, status, idempotency_key, created_at
		) VALUES (
			$1, $2, $3, $4, $5, 'Campaign story.',
			$6, $7, 3, $8, $9, 'campaigns/listed-campaign.png',
			'Indonesia', '10110', $10, $11, $12
		)`,
		campaignID,
		fundraiserID,
		eventID,
		title,
		shortDescription,
		goalAmount,
		raisedAmount,
		contractAddress,
		createdAt.Add(30*24*time.Hour),
		status,
		"list-"+campaignID.String(),
		createdAt,
	); err != nil {
		t.Fatalf("prepare listed campaign: %v", err)
	}
	return campaignID
}

func mustCreatePublicListedCampaign(
	t *testing.T,
	fundraiserID uuid.UUID,
	title string,
	shortDescription string,
	status domain.CampaignStatus,
	goalAmount int64,
	raisedAmount int64,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	eventID, contractAddress := maybeCreateCampaignChainData(t, status, createdAt)
	campaignID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO campaigns (
			id, fundraiser_id, event_id, title, short_description, story,
			goal_amount, raised_amount, donor_count, contract_address, end_at,
			image_object_key, country, zip_code, status,
			idempotency_key, created_at
		) VALUES (
			$1, $2, $3, $4, $5, 'Campaign story.',
			$6, $7, 3, $8, $9,
			'campaigns/public-listed-campaign.png', 'Indonesia', '10110', $10,
			$11, $12
		)`,
		campaignID,
		fundraiserID,
		eventID,
		title,
		shortDescription,
		goalAmount,
		raisedAmount,
		contractAddress,
		createdAt.Add(30*24*time.Hour),
		status,
		"public-list-"+campaignID.String(),
		createdAt,
	); err != nil {
		t.Fatalf("prepare public listed campaign: %v", err)
	}
	return campaignID
}

func maybeCreateCampaignChainData(t *testing.T, status domain.CampaignStatus, createdAt time.Time) (eventID any, contractAddress any) {
	t.Helper()
	switch status {
	case domain.CampaignStatusActive, domain.CampaignStatusCompleted, domain.CampaignStatusCancelled:
	default:
		return nil, nil
	}
	createdEventID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO blockchain_events (id, tx_hash, log_index, type, block_number, created_at)
		 VALUES ($1, $2, 0, 'campaign_created', 1, $3)`,
		createdEventID,
		"0xCampaignCreated"+createdEventID.String(),
		createdAt,
	); err != nil {
		t.Fatalf("prepare campaign blockchain event: %v", err)
	}
	return createdEventID, "0xCampaign" + createdEventID.String()
}

func mustAssignCampaignContractAddress(t *testing.T, campaignID uuid.UUID, address string) {
	t.Helper()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`UPDATE campaigns SET contract_address = $1 WHERE id = $2`,
		address,
		campaignID,
	); err != nil {
		t.Fatalf("assign campaign contract address: %v", err)
	}
}
