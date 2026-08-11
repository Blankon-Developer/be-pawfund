//go:build integration

package integration_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/api"
	"github.com/Blankon-Developer/be-pawfund/internal/app"
	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

func TestCreateCampaignEndpoint(t *testing.T) {
	cleanDatabase(t)
	t.Cleanup(func() { cleanDatabase(t) })

	const walletAddress = "0xFundraiserChecksum"
	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	fundraiser := newFundraiser("rescue@example.com", walletAddress, nil)
	mustCreateFundraiser(t, fundraiserRepo, fundraiser)

	router, jwtManager := newCampaignIntegrationRouter(t)
	fundraiserToken, err := jwtManager.Generate(walletAddress, domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate fundraiser token: %v", err)
	}
	endAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	validBody := `{"title":"Emergency Rescue","shortDescription":"Help rescued animals","story":"A long rescue story.","goalAmount":10000000000,"endAt":"` + endAt.Format(time.RFC3339) + `","imageObjectKey":"campaigns/rescue photo.png","country":"Indonesia","zipCode":"10110"}`

	first := requestCreateCampaign(t, router, fundraiserToken, "create-rescue-1", validBody)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d; body: %s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Code string `json:"code"`
		Data struct {
			ID               string                          `json:"id"`
			GoalAmount       int64                           `json:"goalAmount"`
			RaisedAmount     int64                           `json:"raisedAmount"`
			DonorCount       int64                           `json:"donorCount"`
			Status           domain.CampaignStatus           `json:"status"`
			DeploymentStatus domain.CampaignDeploymentStatus `json:"deploymentStatus"`
			ContractAddress  *string                         `json:"contractAddress"`
			ImageURL         *string                         `json:"imageUrl"`
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
	if firstEnvelope.Data.Status != domain.CampaignStatusActive || firstEnvelope.Data.DeploymentStatus != domain.CampaignDeploymentStatusPending || firstEnvelope.Data.ContractAddress != nil {
		t.Errorf("create state = %#v", firstEnvelope.Data)
	}
	wantImageURL := "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png"
	if firstEnvelope.Data.ImageURL == nil || *firstEnvelope.Data.ImageURL != wantImageURL {
		t.Errorf("image URL = %#v, want %q", firstEnvelope.Data.ImageURL, wantImageURL)
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
}

func TestGetMyCampaignDetailEndpoint(t *testing.T) {
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
			Title            string                          `json:"title"`
			GoalAmount       int64                           `json:"goalAmount"`
			Status           domain.CampaignStatus           `json:"status"`
			DeploymentStatus domain.CampaignDeploymentStatus `json:"deploymentStatus"`
			ContractAddress  *string                         `json:"contractAddress"`
			ImageURL         *string                         `json:"imageUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode campaign detail response: %v", err)
	}
	if envelope.Code != "CAMPAIGN_RETRIEVED" || envelope.Data.Title != campaign.Title || envelope.Data.GoalAmount != campaign.GoalAmount {
		t.Errorf("campaign detail = %#v", envelope)
	}
	if envelope.Data.Status != domain.CampaignStatusActive || envelope.Data.DeploymentStatus != domain.CampaignDeploymentStatusPending || envelope.Data.ContractAddress != nil {
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
	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	owner := newFundraiser("owner@example.com", ownerWallet, nil)
	other := newFundraiser("other@example.com", otherWallet, nil)
	mustCreateFundraiser(t, fundraiserRepo, owner)
	mustCreateFundraiser(t, fundraiserRepo, other)

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
		name    string
		query   string
		wantIDs []uuid.UUID
	}{
		{name: "lists owned campaigns newest first", wantIDs: []uuid.UUID{active, completed, cancelled}},
		{name: "searches titles", query: "?search=emergency", wantIDs: []uuid.UUID{active}},
		{name: "searches short descriptions", query: "?search=mission", wantIDs: []uuid.UUID{completed}},
		{name: "filters active campaigns", query: "?filter=active", wantIDs: []uuid.UUID{active}},
		{name: "filters completed campaigns", query: "?filter=completed", wantIDs: []uuid.UUID{completed}},
		{name: "filters cancelled campaigns", query: "?filter=cancelled", wantIDs: []uuid.UUID{cancelled}},
		{name: "sorts by most donated", query: "?sortBy=most-donated", wantIDs: []uuid.UUID{active, completed, cancelled}},
		{name: "sorts by percentage close to goal", query: "?sortBy=close-to-goal", wantIDs: []uuid.UUID{active, completed, cancelled}},
		{name: "paginates oldest ordering", query: "?sortBy=oldest&page=2&pageSize=1", wantIDs: []uuid.UUID{completed}},
		{name: "returns an empty page", query: "?page=5&pageSize=10", wantIDs: []uuid.UUID{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestMyCampaignList(t, router, ownerToken, test.query)
			if response.Code != http.StatusOK {
				t.Fatalf("list status = %d; body: %s", response.Code, response.Body.String())
			}
			var envelope struct {
				Code string `json:"code"`
				Data []struct {
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
			if len(envelope.Data) != len(test.wantIDs) {
				t.Fatalf("campaign count = %d, want %d; data: %#v", len(envelope.Data), len(test.wantIDs), envelope.Data)
			}
			for index, wantID := range test.wantIDs {
				if envelope.Data[index].ID != wantID {
					t.Errorf("campaign ID at %d = %s, want %s", index, envelope.Data[index].ID, wantID)
				}
			}
			if len(envelope.Data) > 0 && (envelope.Data[0].ImageURL == nil || *envelope.Data[0].ImageURL != "https://cdn.example.com/pawfund/campaigns/listed-campaign.png" || envelope.Data[0].ContractAddress != nil) {
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
	campaignRepo := repository.NewPostgresCampaignRepository(testDatabase)
	campaignService := service.NewCampaignService(campaignRepo, uuid.NewV7)
	application := &app.Application{
		CampaignHandler: api.NewCampaignHandler(campaignService, urlBuilder, logger),
		Authenticate:    appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
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
	campaignID := uuid.New()
	if _, err := testDatabase.ExecContext(
		t.Context(),
		`INSERT INTO campaigns (
			id, fundraiser_id, title, short_description, story,
			goal_amount, raised_amount, donor_count, end_at, image_object_key,
			country, zip_code, status, deployment_status, idempotency_key, created_at
		) VALUES (
			$1, $2, $3, $4, 'Campaign story.',
			$5, $6, 3, $7, 'campaigns/listed-campaign.png',
			'Indonesia', '10110', $8, 'pending', $9, $10
		)`,
		campaignID,
		fundraiserID,
		title,
		shortDescription,
		goalAmount,
		raisedAmount,
		createdAt.Add(30*24*time.Hour),
		status,
		"list-"+campaignID.String(),
		createdAt,
	); err != nil {
		t.Fatalf("prepare listed campaign: %v", err)
	}
	return campaignID
}
