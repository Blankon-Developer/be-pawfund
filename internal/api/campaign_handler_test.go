package api

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
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

type campaignServiceStub struct {
	created domain.Campaign
	err     error
	calls   int
	input   service.CreateCampaignInput
}

func (s *campaignServiceStub) Create(
	_ context.Context,
	input service.CreateCampaignInput,
) (domain.Campaign, error) {
	s.calls++
	s.input = input
	return s.created, s.err
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
