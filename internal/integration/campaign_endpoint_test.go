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
