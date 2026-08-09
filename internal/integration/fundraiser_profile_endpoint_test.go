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
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

func TestGetFundraiserProfileEndpoint(t *testing.T) {
	const (
		fundraiserWallet   = "0xFundraiserChecksum"
		supporterWallet    = "0xSupporterChecksum"
		unregisteredWallet = "0xUnregisteredChecksum"
	)
	imageKey := "profiles/rescue photo.png"

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		authorization string
		walletAddress string
		wantHTTP      int
		wantCode      string
		wantWallet    string
		wantImageURL  *string
		wantNoStore   bool
	}{
		{
			name: "returns authenticated fundraiser profile",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &imageKey))
			},
			authorization: "valid",
			walletAddress: strings.ToLower(fundraiserWallet),
			wantHTTP:      http.StatusOK,
			wantCode:      "PROFILE_RETRIEVED",
			wantWallet:    fundraiserWallet,
			wantImageURL:  integrationStringPointer("https://cdn.example.com/pawfund/profiles/rescue%20photo.png"),
			wantNoStore:   true,
		},
		{
			name: "returns not found for supporter wallet",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("supporter@example.com", supporterWallet, nil))
			},
			authorization: "valid",
			walletAddress: supporterWallet,
			wantHTTP:      http.StatusNotFound,
			wantCode:      "PROFILE_NOT_FOUND",
			wantNoStore:   true,
		},
		{
			name:          "returns not found for unregistered wallet",
			authorization: "valid",
			walletAddress: unregisteredWallet,
			wantHTTP:      http.StatusNotFound,
			wantCode:      "PROFILE_NOT_FOUND",
			wantNoStore:   true,
		},
		{
			name:     "requires access token",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "ACCESS_TOKEN_REQUIRED",
		},
		{
			name:          "rejects invalid access token",
			authorization: "invalid",
			wantHTTP:      http.StatusUnauthorized,
			wantCode:      "INVALID_ACCESS_TOKEN",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			router, jwtManager := newFundraiserProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/profile", nil)
			switch test.authorization {
			case "valid":
				token, err := jwtManager.Generate(test.walletAddress, "", time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				request.Header.Set("Authorization", "Bearer "+token)
			case "invalid":
				request.Header.Set("Authorization", "Bearer invalid-token")
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			result := decodeAuthEndpointResult(t, response)
			if result.HTTPStatus != test.wantHTTP || result.Code != test.wantCode {
				t.Fatalf(
					"get fundraiser profile = %d/%q, want %d/%q; body: %s",
					result.HTTPStatus,
					result.Code,
					test.wantHTTP,
					test.wantCode,
					result.Body,
				)
			}
			if test.wantNoStore && response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			var data struct {
				Name          string `json:"name"`
				Email         string `json:"email"`
				ContactPerson struct {
					Name  string `json:"name"`
					Phone string `json:"phone"`
				} `json:"contactPerson"`
				SocialURL     string  `json:"socialUrl"`
				Country       string  `json:"country"`
				ZipCode       string  `json:"zipCode"`
				ImageURL      *string `json:"imageUrl"`
				WalletAddress string  `json:"walletAddress"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode profile data: %v", err)
			}
			if data.Name != "Animal Rescue" || data.Email != "rescue@example.com" || data.WalletAddress != test.wantWallet {
				t.Errorf("profile identity = %#v", data)
			}
			if data.ContactPerson.Name != "Jane Doe" || data.ContactPerson.Phone != "+62 812 3456" {
				t.Errorf("contact person = %#v", data.ContactPerson)
			}
			if data.SocialURL != "https://example.com/rescue" || data.Country != "Indonesia" || data.ZipCode != "10110" {
				t.Errorf("profile details = %#v", data)
			}
			if !equalStringPointers(data.ImageURL, test.wantImageURL) {
				t.Errorf("image URL = %v, want %v", data.ImageURL, test.wantImageURL)
			}
			if strings.Contains(string(result.Data), "imageObjectKey") || strings.Contains(string(result.Data), `"role"`) {
				t.Errorf("response leaks internal fields: %s", result.Data)
			}
		})
	}
}

func newFundraiserProfileIntegrationRouter(t *testing.T) (http.Handler, *auth.JWTManager) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtManager, err := auth.NewJWTManager([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
	if err != nil {
		t.Fatalf("create URL builder: %v", err)
	}
	fundraiserRepository := repository.NewPostgresFundraiserRepository(testDatabase)
	fundraiserService := service.NewFundraiserService(fundraiserRepository, uuid.NewV7)
	application := &app.Application{
		DB:                testDatabase,
		AuthHandler:       api.NewAuthHandler(nil, urlBuilder, logger),
		SupporterHandler:  api.NewSupporterHandler(nil, urlBuilder, logger),
		FundraiserHandler: api.NewFundraiserHandler(fundraiserService, urlBuilder, logger),
		Authenticate:      appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
}
