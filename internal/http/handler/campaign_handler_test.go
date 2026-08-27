package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type campaignServiceStub struct {
	created           domain.Campaign
	err               error
	calls             int
	input             service.CreateCampaignInput
	listed            []domain.Campaign
	listTotal         int64
	listErr           error
	listCalls         int
	listWallet        string
	listOptions       domain.CampaignListOptions
	publicListed      []domain.PublicCampaignListItem
	publicListTotal   int64
	publicListErr     error
	publicListCalls   int
	publicListOptions domain.CampaignListOptions
	publicRetrieved   domain.PublicCampaignDetail
	publicGetErr      error
	publicGetCalls    int
	publicAddress     string
	publicDonors      []domain.PublicCampaignDonor
	publicDonorsTotal int64
	publicDonorsErr   error
	publicDonorCalls  int
	publicDonorOpts   domain.CampaignDonorListOptions
	retrieved         domain.Campaign
	getErr            error
	getCalls          int
	getWallet         string
	getCampaignID     uuid.UUID
}

func (s *campaignServiceStub) ListPublicCampaigns(
	_ context.Context,
	options domain.CampaignListOptions,
) ([]domain.PublicCampaignListItem, int64, error) {
	s.publicListCalls++
	s.publicListOptions = options
	if s.publicListErr != nil {
		return nil, 0, s.publicListErr
	}
	return s.publicListed, s.publicListTotal, nil
}

func (s *campaignServiceStub) GetPublicCampaignDetail(
	_ context.Context,
	contractAddress string,
) (domain.PublicCampaignDetail, error) {
	s.publicGetCalls++
	s.publicAddress = contractAddress
	if s.publicGetErr != nil {
		return domain.PublicCampaignDetail{}, s.publicGetErr
	}
	return s.publicRetrieved, nil
}

func (s *campaignServiceStub) ListPublicCampaignDonors(
	_ context.Context,
	contractAddress string,
	options domain.CampaignDonorListOptions,
) ([]domain.PublicCampaignDonor, int64, error) {
	s.publicDonorCalls++
	s.publicAddress = contractAddress
	s.publicDonorOpts = options
	if s.publicDonorsErr != nil {
		return nil, 0, s.publicDonorsErr
	}
	return s.publicDonors, s.publicDonorsTotal, nil
}

func (s *campaignServiceStub) Create(
	_ context.Context,
	input service.CreateCampaignInput,
) (domain.Campaign, error) {
	s.calls++
	s.input = input
	return s.created, s.err
}

func (s *campaignServiceStub) ListMyCampaigns(
	_ context.Context,
	walletAddress string,
	options domain.CampaignListOptions,
) ([]domain.Campaign, int64, error) {
	s.listCalls++
	s.listWallet = walletAddress
	s.listOptions = options
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.listed, s.listTotal, nil
}

func (s *campaignServiceStub) GetMyCampaignDetail(
	_ context.Context,
	walletAddress string,
	campaignID uuid.UUID,
) (domain.Campaign, error) {
	s.getCalls++
	s.getWallet = walletAddress
	s.getCampaignID = campaignID
	return s.retrieved, s.getErr
}

func TestCampaignHandlerHandleCreateCampaign(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	endAt := createdAt.Add(30 * 24 * time.Hour)
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	validBody := `{"title":" Emergency Rescue ","shortDescription":" Help rescued animals ","story":" A long rescue story. ","goalAmount":10000000000,"endAt":"` + endAt.Format(time.RFC3339) + `","imageObjectKey":" campaigns/rescue photo.png ","country":" Indonesia ","zipCode":" 10110 "}`
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name           string
		body           string
		principal      *auth.Principal
		idempotencyKey string
		serviceError   error
		wantHTTP       int
		wantCode       string
		wantCalls      int
	}{
		{
			name:           "creates a pending campaign",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: " 0xFundraiser ", Role: domain.UserRoleFundraiser},
			idempotencyKey: " create-rescue-1 ",
			wantHTTP:       http.StatusCreated,
			wantCode:       "CAMPAIGN_CREATED",
			wantCalls:      1,
		},
		{
			name:      "requires idempotency key",
			body:      validBody,
			principal: &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			wantHTTP:  http.StatusBadRequest,
			wantCode:  "IDEMPOTENCY_KEY_REQUIRED",
		},
		{
			name:           "rejects an oversized idempotency key",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: strings.Repeat("k", 256),
			wantHTTP:       http.StatusBadRequest,
			wantCode:       "INVALID_IDEMPOTENCY_KEY",
		},
		{
			name:           "rejects supporter role",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			idempotencyKey: "key",
			wantHTTP:       http.StatusForbidden,
			wantCode:       "FUNDRAISER_ACCESS_REQUIRED",
		},
		{
			name:           "requires authenticated principal",
			body:           validBody,
			idempotencyKey: "key",
			wantHTTP:       http.StatusUnauthorized,
			wantCode:       "INVALID_ACCESS_TOKEN",
		},
		{
			name:           "rejects invalid fields",
			body:           `{}`,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: "key",
			wantHTTP:       http.StatusUnprocessableEntity,
			wantCode:       "VALIDATION_ERROR",
		},
		{
			name:           "maps end time rule",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: "key",
			serviceError:   service.ErrCampaignEndAtTooSoon,
			wantHTTP:       http.StatusUnprocessableEntity,
			wantCode:       "VALIDATION_ERROR",
			wantCalls:      1,
		},
		{
			name:           "maps idempotency conflict",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: "key",
			serviceError:   service.ErrCampaignIdempotencyConflict,
			wantHTTP:       http.StatusConflict,
			wantCode:       "IDEMPOTENCY_KEY_CONFLICT",
			wantCalls:      1,
		},
		{
			name:           "maps deleted fundraiser profile",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: "key",
			serviceError:   service.ErrProfileNotFound,
			wantHTTP:       http.StatusNotFound,
			wantCode:       "PROFILE_NOT_FOUND",
			wantCalls:      1,
		},
		{
			name:           "hides unexpected service failure",
			body:           validBody,
			principal:      &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			idempotencyKey: "key",
			serviceError:   unexpectedFailure,
			wantHTTP:       http.StatusInternalServerError,
			wantCode:       "INTERNAL_SERVER_ERROR",
			wantCalls:      1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				created: domain.Campaign{
					ID:               campaignID,
					Title:            "Emergency Rescue",
					ShortDescription: "Help rescued animals",
					Story:            "A long rescue story.",
					GoalAmount:       10_000_000_000,
					EndAt:            endAt,
					ImageObjectKey:   "campaigns/rescue photo.png",
					Country:          "Indonesia",
					ZipCode:          "10110",
					Status:           domain.CampaignStatusActive,
					DeploymentStatus: domain.CampaignDeploymentStatusPending,
					CreatedAt:        createdAt,
				},
				err: test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPost, "/v1/fundraiser/campaigns", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", test.idempotencyKey)
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandleCreateCampaign(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if serviceStub.calls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.calls, test.wantCalls)
			}
			if test.wantHTTP != http.StatusCreated {
				return
			}
			if serviceStub.input.WalletAddress != "0xFundraiser" || serviceStub.input.IdempotencyKey != "create-rescue-1" {
				t.Errorf("service identity input = %#v", serviceStub.input)
			}
			if serviceStub.input.Title != "Emergency Rescue" || serviceStub.input.GoalAmount != 10_000_000_000 || !serviceStub.input.EndAt.Equal(endAt) {
				t.Errorf("service campaign input = %#v", serviceStub.input)
			}
			var data createCampaignResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode success data: %v", err)
			}
			if data.ID != campaignID.String() || data.Status != domain.CampaignStatusActive || data.DeploymentStatus != domain.CampaignDeploymentStatusPending || data.ContractAddress != nil {
				t.Errorf("response state = %#v", data)
			}
			wantImageURL := "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png"
			if data.ImageURL == nil || *data.ImageURL != wantImageURL {
				t.Errorf("image URL = %#v, want %q", data.ImageURL, wantImageURL)
			}
		})
	}
}

func TestCampaignHandlerHandleGetMyCampaignList(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	createdAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	endAt := createdAt.Add(30 * 24 * time.Hour)
	contractAddress := "0xCampaign"
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		query        string
		principal    *auth.Principal
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
	}{
		{
			name:      "returns authenticated fundraiser campaigns",
			query:     "?search=+rescue+&sortBy=close-to-goal&filter=completed&page=2&pageSize=25",
			principal: &auth.Principal{WalletAddress: " 0xFundraiser ", Role: domain.UserRoleFundraiser},
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGNS_RETRIEVED",
			wantCalls: 1,
		},
		{
			name:     "requires an authenticated principal",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "INVALID_ACCESS_TOKEN",
		},
		{
			name:      "requires fundraiser access",
			principal: &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			wantHTTP:  http.StatusForbidden,
			wantCode:  "FUNDRAISER_ACCESS_REQUIRED",
		},
		{
			name:      "rejects invalid query parameters",
			query:     "?sortBy=popular&page=0",
			principal: &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			wantHTTP:  http.StatusUnprocessableEntity,
			wantCode:  "VALIDATION_ERROR",
		},
		{
			name:         "hides unexpected service failure",
			principal:    &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				listed: []domain.Campaign{
					{
						ID:               campaignID,
						Title:            "Emergency Rescue",
						ShortDescription: "Help rescued animals",
						GoalAmount:       10_000_000_000,
						RaisedAmount:     1_000_000_000,
						DonorCount:       3,
						EndAt:            endAt,
						ImageObjectKey:   "campaigns/rescue photo.png",
						ContractAddress:  &contractAddress,
						Status:           domain.CampaignStatusCompleted,
						CreatedAt:        createdAt,
					},
				},
				listTotal: 47,
				listErr:   test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/campaigns"+test.query, nil)
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandleGetMyCampaignList(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if serviceStub.listCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.listCalls, test.wantCalls)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			if serviceStub.listWallet != "0xFundraiser" {
				t.Errorf("service wallet = %q", serviceStub.listWallet)
			}
			if serviceStub.listOptions.Search != "rescue" || serviceStub.listOptions.Sort != domain.CampaignListSortCloseToGoal || serviceStub.listOptions.Status == nil || *serviceStub.listOptions.Status != domain.CampaignStatusCompleted || serviceStub.listOptions.Page != 2 || serviceStub.listOptions.PageSize != 25 {
				t.Errorf("service options = %#v", serviceStub.listOptions)
			}
			var data []myCampaignListItemResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode success data: %v", err)
			}
			if len(data) != 1 || data[0].ID != campaignID || data[0].DonorCount != 3 || data[0].ContractAddress == nil || *data[0].ContractAddress != contractAddress {
				t.Errorf("response data = %#v", data)
			}
			wantImageURL := "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png"
			if data[0].ImageURL == nil || *data[0].ImageURL != wantImageURL {
				t.Errorf("image URL = %#v, want %q", data[0].ImageURL, wantImageURL)
			}
			wantPagination := httpx.Pagination{Current: 2, PageSize: 25, TotalPages: 2, TotalItems: 47}
			if decoded.Pagination == nil || *decoded.Pagination != wantPagination {
				t.Errorf("pagination = %#v, want %#v", decoded.Pagination, wantPagination)
			}
		})
	}
}

func TestCampaignHandlerHandleGetPublicCampaignList(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	createdAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	endAt := createdAt.Add(30 * 24 * time.Hour)
	contractAddress := "0xCampaign"
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		query        string
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
		wantSort     domain.CampaignListSort
	}{
		{
			name:      "returns public campaigns with randomized default sorting",
			query:     "?search=+rescue+&filter=completed&page=2&pageSize=25",
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGNS_RETRIEVED",
			wantCalls: 1,
			wantSort:  domain.CampaignListSortRandom,
		},
		{
			name:      "treats a blank sort as randomized default sorting",
			query:     "?sortBy=",
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGNS_RETRIEVED",
			wantCalls: 1,
			wantSort:  domain.CampaignListSortRandom,
		},
		{
			name:      "uses explicitly requested sorting",
			query:     "?sortBy=most-donated",
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGNS_RETRIEVED",
			wantCalls: 1,
			wantSort:  domain.CampaignListSortMostDonated,
		},
		{
			name:     "rejects invalid query parameters",
			query:    "?sortBy=popular&page=0",
			wantHTTP: http.StatusUnprocessableEntity,
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:         "hides unexpected service failure",
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
			wantSort:     domain.CampaignListSortRandom,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				publicListed: []domain.PublicCampaignListItem{
					{
						Campaign: domain.Campaign{
							ID:               campaignID,
							Title:            "Emergency Rescue",
							ShortDescription: "Help rescued animals",
							GoalAmount:       10_000_000_000,
							RaisedAmount:     1_000_000_000,
							DonorCount:       3,
							EndAt:            endAt,
							ImageObjectKey:   "campaigns/rescue photo.png",
							ContractAddress:  &contractAddress,
							Status:           domain.CampaignStatusCompleted,
							CreatedAt:        createdAt,
						},
					},
				},
				publicListTotal: 47,
				publicListErr:   test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/campaigns"+test.query, nil)
			response := httptest.NewRecorder()

			handler.HandleGetPublicCampaignList(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if serviceStub.publicListCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.publicListCalls, test.wantCalls)
			}
			if test.wantCalls == 0 {
				return
			}
			if serviceStub.publicListOptions.Sort != test.wantSort {
				t.Errorf("service sort = %q, want %q", serviceStub.publicListOptions.Sort, test.wantSort)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if test.name == "returns public campaigns with randomized default sorting" && (serviceStub.publicListOptions.Search != "rescue" || serviceStub.publicListOptions.Status == nil || *serviceStub.publicListOptions.Status != domain.CampaignStatusCompleted || serviceStub.publicListOptions.Page != 2 || serviceStub.publicListOptions.PageSize != 25) {
				t.Errorf("service options = %#v", serviceStub.publicListOptions)
			}

			var data []publicCampaignListItemResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode success data: %v", err)
			}
			if len(data) != 1 || data[0].ID != campaignID || data[0].GoalAmount != 10_000_000_000 || data[0].DonorCount != 3 || data[0].ContractAddress == nil || *data[0].ContractAddress != contractAddress {
				t.Errorf("response data = %#v", data)
			}
			wantCampaignImageURL := "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png"
			if data[0].CampaignImageURL == nil || *data[0].CampaignImageURL != wantCampaignImageURL || data[0].FundraiserImageURL != nil {
				t.Errorf("image URLs = %#v", data[0])
			}
			wantPagination := httpx.NewPagination(serviceStub.publicListOptions.Page, serviceStub.publicListOptions.PageSize, 47)
			if decoded.Pagination == nil || *decoded.Pagination != wantPagination {
				t.Errorf("pagination = %#v, want %#v", decoded.Pagination, wantPagination)
			}
		})
	}
}

func TestCampaignHandlerHandleGetPublicCampaignDetail(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	fundraiserID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdee")
	createdAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	endAt := createdAt.Add(30 * 24 * time.Hour)
	contractAddress := "0xCampaign"
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		address      string
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
	}{
		{
			name:      "returns a public deployed campaign",
			address:   " 0xCaMpAiGn ",
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGN_RETRIEVED",
			wantCalls: 1,
		},
		{
			name:         "maps a missing campaign",
			address:      contractAddress,
			serviceError: service.ErrCampaignNotFound,
			wantHTTP:     http.StatusNotFound,
			wantCode:     "CAMPAIGN_NOT_FOUND",
			wantCalls:    1,
		},
		{
			name:         "hides unexpected service failure",
			address:      contractAddress,
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				publicRetrieved: domain.PublicCampaignDetail{
					Campaign: domain.Campaign{
						ID:               campaignID,
						FundraiserID:     fundraiserID,
						Title:            "Emergency Rescue",
						ShortDescription: "Help rescued animals",
						Story:            "A long rescue story.",
						GoalAmount:       10_000_000_000,
						RaisedAmount:     100_000_000,
						DonorCount:       3,
						ContractAddress:  &contractAddress,
						EndAt:            endAt,
						CreatedAt:        createdAt,
						ImageObjectKey:   "campaigns/rescue photo.png",
						Country:          "Indonesia",
						ZipCode:          "10110",
						Status:           domain.CampaignStatusActive,
					},
					FundraiserName:          "Paw Rescue",
					FundraiserWalletAddress: "0xFundraiser",
				},
				publicGetErr: test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/campaigns/0xCaMpAiGn", nil)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("address", test.address)
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
			response := httptest.NewRecorder()

			handler.HandleGetPublicCampaignDetail(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if serviceStub.publicGetCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.publicGetCalls, test.wantCalls)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if serviceStub.publicAddress != "0xCaMpAiGn" {
				t.Errorf("service address = %q, want trimmed address", serviceStub.publicAddress)
			}

			var data publicCampaignDetailResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode success data: %v", err)
			}
			if data.ID != campaignID || data.Fundraiser.ID != fundraiserID || data.Fundraiser.Name != "Paw Rescue" || data.Fundraiser.Address != "0xFundraiser" || data.ContractAddress != contractAddress || data.Status != domain.CampaignStatusActive {
				t.Errorf("response data = %#v", data)
			}
			if data.ImageURL != "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png" || data.Fundraiser.ImageURL != nil {
				t.Errorf("response image URLs = %#v", data)
			}
		})
	}
}

func TestCampaignHandlerHandleGetPublicCampaignDonors(t *testing.T) {
	donatedAt := time.Date(2026, 8, 10, 11, 30, 0, 0, time.FixedZone("WIB", 7*60*60))
	donorName := "Animal Friend"
	imageObjectKey := "profiles/friend photo.png"
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		query        string
		donors       []domain.PublicCampaignDonor
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
	}{
		{
			name:  "returns aggregated donors with optional profiles",
			query: "?sortBy=top&page=2&pageSize=25",
			donors: []domain.PublicCampaignDonor{
				{
					Name:           &donorName,
					WalletAddress:  "0xRegistered",
					ImageObjectKey: &imageObjectKey,
					Amount:         3_000_000,
					DonatedAt:      donatedAt,
				},
				{WalletAddress: "0xUnregistered", Amount: 1_000_000, DonatedAt: donatedAt.Add(-time.Hour)},
			},
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGN_DONORS_RETRIEVED",
			wantCalls: 1,
		},
		{
			name:      "returns a non-null empty array",
			wantHTTP:  http.StatusOK,
			wantCode:  "CAMPAIGN_DONORS_RETRIEVED",
			wantCalls: 1,
		},
		{
			name:      "rejects invalid query options",
			query:     "?sortBy=largest&page=0&pageSize=101",
			wantHTTP:  http.StatusUnprocessableEntity,
			wantCode:  "VALIDATION_ERROR",
			wantCalls: 0,
		},
		{
			name:         "maps a missing campaign",
			serviceError: service.ErrCampaignNotFound,
			wantHTTP:     http.StatusNotFound,
			wantCode:     "CAMPAIGN_NOT_FOUND",
			wantCalls:    1,
		},
		{
			name:         "hides an unexpected failure",
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				publicDonors:      test.donors,
				publicDonorsTotal: 47,
				publicDonorsErr:   test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/campaigns/0xCampaign/donors"+test.query, nil)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("address", " 0xCampaign ")
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
			response := httptest.NewRecorder()

			handler.HandleGetPublicCampaignDonors(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if serviceStub.publicDonorCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.publicDonorCalls, test.wantCalls)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if serviceStub.publicAddress != "0xCampaign" {
				t.Errorf("service address = %q, want trimmed address", serviceStub.publicAddress)
			}
			if test.query != "" && (serviceStub.publicDonorOpts.Sort != domain.CampaignDonorListSortTop || serviceStub.publicDonorOpts.Page != 2 || serviceStub.publicDonorOpts.PageSize != 25) {
				t.Errorf("service options = %#v", serviceStub.publicDonorOpts)
			}
			wantPagination := httpx.NewPagination(serviceStub.publicDonorOpts.Page, serviceStub.publicDonorOpts.PageSize, 47)
			if decoded.Pagination == nil || *decoded.Pagination != wantPagination {
				t.Errorf("pagination = %#v, want %#v", decoded.Pagination, wantPagination)
			}
			if test.donors == nil {
				if string(decoded.Data) != "[]" {
					t.Errorf("empty response data = %s, want []", decoded.Data)
				}
				return
			}

			var data []publicCampaignDonorsItemResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode donor data: %v", err)
			}
			if len(data) != 2 || data[0].Name == nil || *data[0].Name != donorName || data[0].Address != "0xRegistered" || data[0].Amount != 3_000_000 || data[0].DonatedOn != "2026-08-10T04:30:00Z" {
				t.Errorf("registered donor response = %#v", data)
			}
			wantImageURL := "https://cdn.example.com/pawfund/profiles/friend%20photo.png"
			if data[0].ImageURL == nil || *data[0].ImageURL != wantImageURL || data[1].Name != nil || data[1].ImageURL != nil {
				t.Errorf("optional donor fields = %#v", data)
			}
		})
	}
}

func TestCampaignHandlerHandleGetMyCampaignDetail(t *testing.T) {
	campaignID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	createdAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	endAt := createdAt.Add(30 * 24 * time.Hour)
	contractAddress := "0xCampaign"
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		campaignID   string
		principal    *auth.Principal
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
	}{
		{
			name:       "returns the authenticated fundraiser campaign",
			campaignID: campaignID.String(),
			principal:  &auth.Principal{WalletAddress: " 0xFundraiser ", Role: domain.UserRoleFundraiser},
			wantHTTP:   http.StatusOK,
			wantCode:   "CAMPAIGN_RETRIEVED",
			wantCalls:  1,
		},
		{
			name:       "rejects an invalid campaign ID",
			campaignID: "not-a-uuid",
			principal:  &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			wantHTTP:   http.StatusUnprocessableEntity,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "requires an authenticated principal",
			campaignID: campaignID.String(),
			wantHTTP:   http.StatusUnauthorized,
			wantCode:   "INVALID_ACCESS_TOKEN",
		},
		{
			name:       "requires fundraiser access",
			campaignID: campaignID.String(),
			principal:  &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			wantHTTP:   http.StatusForbidden,
			wantCode:   "FUNDRAISER_ACCESS_REQUIRED",
		},
		{
			name:         "hides a campaign owned by another fundraiser",
			campaignID:   campaignID.String(),
			principal:    &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			serviceError: service.ErrCampaignNotFound,
			wantHTTP:     http.StatusNotFound,
			wantCode:     "CAMPAIGN_NOT_FOUND",
			wantCalls:    1,
		},
		{
			name:         "hides unexpected service failure",
			campaignID:   campaignID.String(),
			principal:    &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &campaignServiceStub{
				retrieved: domain.Campaign{
					ID:               campaignID,
					Title:            "Emergency Rescue",
					ShortDescription: "Help rescued animals",
					Story:            "A long rescue story.",
					GoalAmount:       10_000_000_000,
					RaisedAmount:     100_000_000,
					DonorCount:       3,
					EndAt:            endAt,
					ImageObjectKey:   "campaigns/rescue photo.png",
					Country:          "Indonesia",
					ZipCode:          "10110",
					Status:           domain.CampaignStatusActive,
					DeploymentStatus: domain.CampaignDeploymentStatusDeployed,
					ContractAddress:  &contractAddress,
					CreatedAt:        createdAt,
				},
				getErr: test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewCampaignHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/campaigns/"+test.campaignID, nil)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("id", test.campaignID)
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandleGetMyCampaignDetail(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if serviceStub.getCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.getCalls, test.wantCalls)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if serviceStub.getWallet != "0xFundraiser" || serviceStub.getCampaignID != campaignID {
				t.Errorf("service input = %q/%s", serviceStub.getWallet, serviceStub.getCampaignID)
			}
			var data myCampaignResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode success data: %v", err)
			}
			if data.Title != "Emergency Rescue" || data.RaisedAmount != 100_000_000 || data.Status != domain.CampaignStatusActive || data.DeploymentStatus != domain.CampaignDeploymentStatusDeployed {
				t.Errorf("response data = %#v", data)
			}
			wantImageURL := "https://cdn.example.com/pawfund/campaigns/rescue%20photo.png"
			if data.ImageURL == nil || *data.ImageURL != wantImageURL || data.ContractAddress == nil || *data.ContractAddress != contractAddress {
				t.Errorf("response URLs = %#v", data)
			}
		})
	}
}
