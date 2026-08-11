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
	"github.com/minio/minio-go/v7"
)

func TestGetSupporterProfileEndpoint(t *testing.T) {
	const (
		supporterWallet    = "0xSupporterChecksum"
		fundraiserWallet   = "0xFundraiserChecksum"
		unregisteredWallet = "0xUnregisteredChecksum"
	)
	imageKey := "profiles/cat photo.png"

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
			name: "returns authenticated supporter profile",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("cat@example.com", supporterWallet, &imageKey))
			},
			authorization: "valid",
			walletAddress: strings.ToLower(supporterWallet),
			wantHTTP:      http.StatusOK,
			wantCode:      "PROFILE_RETRIEVED",
			wantWallet:    supporterWallet,
			wantImageURL:  integrationStringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
			wantNoStore:   true,
		},
		{
			name: "returns not found for fundraiser wallet",
			prepare: func(t *testing.T) {
				repo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, nil))
			},
			authorization: "valid",
			walletAddress: fundraiserWallet,
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

			router, jwtManager := newSupporterProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodGet, "/v1/supporter/profile", nil)
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
					"get supporter profile = %d/%q, want %d/%q; body: %s",
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
				Name          string  `json:"name"`
				Email         string  `json:"email"`
				WalletAddress string  `json:"walletAddress"`
				ImageURL      *string `json:"imageUrl"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode profile data: %v", err)
			}
			if data.Name != "Supporter" || data.Email != "cat@example.com" || data.WalletAddress != test.wantWallet {
				t.Errorf("profile identity = %#v", data)
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

func TestReplaceSupporterProfileEndpoint(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	oldImageObjectKey := "profiles/supporter-update-old.png"
	newImageObjectKey := "profiles/supporter-update-new.png"

	tests := []struct {
		name          string
		shareOldImage bool
		wantOldImage  bool
	}{
		{name: "replaces profile and deletes an unreferenced old image"},
		{name: "keeps old image referenced by fundraiser", shareOldImage: true, wantOldImage: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			removeIntegrationObject(t, oldImageObjectKey)
			removeIntegrationObject(t, newImageObjectKey)
			t.Cleanup(func() { removeIntegrationObject(t, oldImageObjectKey) })
			t.Cleanup(func() { removeIntegrationObject(t, newImageObjectKey) })
			putProfileImageObject(t, oldImageObjectKey)
			putProfileImageObject(t, newImageObjectKey)

			supporterRepo := repository.NewPostgresSupporterRepository(testDatabase)
			mustCreateSupporter(t, supporterRepo, newSupporter("cat@example.com", supporterWallet, &oldImageObjectKey))
			if test.shareOldImage {
				fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &oldImageObjectKey))
			}

			router, jwtManager := newSupporterProfileIntegrationRouter(t)
			token, err := jwtManager.Generate(strings.ToLower(supporterWallet), "", time.Hour)
			if err != nil {
				t.Fatalf("generate access token: %v", err)
			}
			request := httptest.NewRequest(http.MethodPut, "/v1/supporter/profile", strings.NewReader(`{"name":" Updated Cat Lover ","email":" updated@example.com ","imageObjectKey":"profiles/supporter-update-new.png"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("PUT = %d body=%q, want 204 with no body", response.Code, response.Body.String())
			}

			_, err = testStorageClient.StatObject(t.Context(), testStorageBucket, oldImageObjectKey, minio.StatObjectOptions{})
			if test.wantOldImage && err != nil {
				t.Errorf("shared old image was deleted: %v", err)
			}
			if !test.wantOldImage && err == nil {
				t.Error("unreferenced old image still exists")
			}

			getRequest := httptest.NewRequest(http.MethodGet, "/v1/supporter/profile", nil)
			getRequest.Header.Set("Authorization", "Bearer "+token)
			getResponse := httptest.NewRecorder()
			router.ServeHTTP(getResponse, getRequest)
			result := decodeAuthEndpointResult(t, getResponse)
			if result.HTTPStatus != http.StatusOK || result.Code != "PROFILE_RETRIEVED" {
				t.Fatalf("GET after PUT = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
			}
			var data struct {
				Name     string  `json:"name"`
				Email    string  `json:"email"`
				ImageURL *string `json:"imageUrl"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode GET data: %v", err)
			}
			if data.Name != "Updated Cat Lover" || data.Email != "updated@example.com" || !equalStringPointers(data.ImageURL, integrationStringPointer("https://cdn.example.com/pawfund/"+newImageObjectKey)) {
				t.Errorf("replaced fields = %#v", data)
			}
		})
	}
}

func TestDeleteSupporterProfileEndpoint(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"
	imageObjectKey := "profiles/supporter-delete.png"

	tests := []struct {
		name            string
		createProfile   bool
		shareImage      bool
		authorization   string
		wantHTTP        int
		wantCode        string
		wantImageExists *bool
	}{
		{
			name:            "deletes profile and its unreferenced image",
			createProfile:   true,
			wantHTTP:        http.StatusNoContent,
			wantImageExists: integrationBoolPointer(false),
		},
		{
			name:            "keeps a shared image",
			createProfile:   true,
			shareImage:      true,
			wantHTTP:        http.StatusNoContent,
			wantImageExists: integrationBoolPointer(true),
		},
		{name: "returns not found for unknown wallet", wantHTTP: http.StatusNotFound, wantCode: "PROFILE_NOT_FOUND"},
		{name: "requires access token", authorization: "missing", wantHTTP: http.StatusUnauthorized, wantCode: "ACCESS_TOKEN_REQUIRED"},
		{name: "rejects invalid access token", authorization: "invalid", wantHTTP: http.StatusUnauthorized, wantCode: "INVALID_ACCESS_TOKEN"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			removeIntegrationObject(t, imageObjectKey)
			t.Cleanup(func() { removeIntegrationObject(t, imageObjectKey) })

			if test.createProfile {
				putProfileImageObject(t, imageObjectKey)
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				supporter := newSupporter("supporter@example.com", supporterWallet, &imageObjectKey)
				mustCreateSupporter(t, repo, supporter)
				if test.shareImage {
					fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
					mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", "0xFundraiser", &imageObjectKey))
				}
			}

			router, jwtManager := newSupporterProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodDelete, "/v1/supporter/profile", nil)
			switch test.authorization {
			case "missing":
			case "invalid":
				request.Header.Set("Authorization", "Bearer invalid-token")
			default:
				token, err := jwtManager.Generate(strings.ToLower(supporterWallet), "", time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantHTTP {
				t.Fatalf("DELETE = %d, want %d; body: %s", response.Code, test.wantHTTP, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Errorf("204 body = %q, want empty", response.Body.String())
				}
			} else {
				result := decodeAuthEndpointResult(t, response)
				if result.Code != test.wantCode {
					t.Errorf("response code = %q, want %q; body: %s", result.Code, test.wantCode, result.Body)
				}
			}

			if test.wantImageExists != nil {
				_, err := testStorageClient.StatObject(t.Context(), testStorageBucket, imageObjectKey, minio.StatObjectOptions{})
				if *test.wantImageExists && err != nil {
					t.Errorf("profile image was deleted: %v", err)
				}
				if !*test.wantImageExists && err == nil {
					t.Error("unreferenced profile image still exists")
				}
			}

			if test.wantHTTP != http.StatusNoContent {
				return
			}
			profileRequest := httptest.NewRequest(http.MethodGet, "/v1/supporter/profile", nil)
			profileRequest.Header.Set("Authorization", request.Header.Get("Authorization"))
			profileResponse := httptest.NewRecorder()
			router.ServeHTTP(profileResponse, profileRequest)
			profileResult := decodeAuthEndpointResult(t, profileResponse)
			if profileResult.HTTPStatus != http.StatusNotFound || profileResult.Code != "PROFILE_NOT_FOUND" {
				t.Errorf("GET own profile after delete = %d/%q; body: %s", profileResult.HTTPStatus, profileResult.Code, profileResult.Body)
			}
		})
	}
}

func newSupporterProfileIntegrationRouter(t *testing.T) (http.Handler, *auth.JWTManager) {
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
	supporterRepository := repository.NewPostgresSupporterRepository(testDatabase)
	objectDeleter, err := storage.NewObjectDeleter(storage.PresignerConfig{
		Endpoint:  testStorageEndpoint,
		AccessKey: testStorageAccessKey,
		SecretKey: testStorageSecretKey,
		Bucket:    testStorageBucket,
		Region:    testStorageRegion,
	})
	if err != nil {
		t.Fatalf("create object deleter: %v", err)
	}
	supporterService := service.NewSupporterService(supporterRepository, uuid.NewV7, objectDeleter)
	application := &app.Application{
		DB:                testDatabase,
		AuthHandler:       api.NewAuthHandler(nil, urlBuilder, logger),
		SupporterHandler:  api.NewSupporterHandler(supporterService, urlBuilder, logger),
		FundraiserHandler: api.NewFundraiserHandler(nil, urlBuilder, logger),
		Authenticate:      appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
}
