//go:build integration

package integration_test

import (
	"bytes"
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
	"github.com/Blankon-Developer/be-pawfund/internal/http/handler"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/http/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/http/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
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
				repo := database.NewPostgresFundraiserRepository(testDatabase)
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
				repo := database.NewPostgresSupporterRepository(testDatabase)
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

func TestGetPublicFundraiserProfileEndpoint(t *testing.T) {
	const fundraiserWallet = "0xFundraiserChecksum"
	imageKey := "profiles/rescue photo.png"

	tests := []struct {
		name         string
		prepare      func(t *testing.T)
		address      string
		wantHTTP     int
		wantCode     string
		wantImageURL *string
	}{
		{
			name: "returns fundraiser profile without access token",
			prepare: func(t *testing.T) {
				repo := database.NewPostgresFundraiserRepository(testDatabase)
				mustCreateFundraiser(t, repo, newFundraiser("rescue@example.com", fundraiserWallet, &imageKey))
			},
			address:      strings.ToLower(fundraiserWallet),
			wantHTTP:     http.StatusOK,
			wantCode:     "PROFILE_RETRIEVED",
			wantImageURL: integrationStringPointer("https://cdn.example.com/pawfund/profiles/rescue%20photo.png"),
		},
		{
			name:     "returns not found for unknown wallet",
			address:  "0xUnknown",
			wantHTTP: http.StatusNotFound,
			wantCode: "PROFILE_NOT_FOUND",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			router, _ := newFundraiserProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/"+test.address, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			result := decodeAuthEndpointResult(t, response)
			if result.HTTPStatus != test.wantHTTP || result.Code != test.wantCode {
				t.Fatalf(
					"get public fundraiser profile = %d/%q, want %d/%q; body: %s",
					result.HTTPStatus,
					result.Code,
					test.wantHTTP,
					test.wantCode,
					result.Body,
				)
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
				SocialURL string  `json:"socialUrl"`
				Country   string  `json:"country"`
				ZipCode   string  `json:"zipCode"`
				ImageURL  *string `json:"imageUrl"`
				CreatedAt string  `json:"createdAt"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode public profile data: %v", err)
			}
			if data.Name != "Animal Rescue" || data.Email != "rescue@example.com" {
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
			createdAt, err := time.Parse(time.RFC3339, data.CreatedAt)
			if err != nil {
				t.Errorf("createdAt = %q, want RFC3339 UTC: %v", data.CreatedAt, err)
			} else if data.CreatedAt != createdAt.UTC().Format(time.RFC3339) {
				t.Errorf("createdAt = %q, want UTC RFC3339", data.CreatedAt)
			}
			if strings.Contains(string(result.Data), "walletAddress") || strings.Contains(string(result.Data), "imageObjectKey") || strings.Contains(string(result.Data), `"role"`) {
				t.Errorf("response leaks non-public fields: %s", result.Data)
			}
		})
	}
}

func TestReplaceFundraiserProfileEndpoint(t *testing.T) {
	const fundraiserWallet = "0xFundraiserChecksum"
	oldImageObjectKey := "profiles/fundraiser-update-old.png"
	newImageObjectKey := "profiles/fundraiser-update-new.png"

	tests := []struct {
		name          string
		shareOldImage bool
		wantOldImage  bool
	}{
		{
			name:         "replaces profile and deletes an unreferenced old image",
			wantOldImage: false,
		},
		{
			name:          "keeps an old image referenced by a supporter",
			shareOldImage: true,
			wantOldImage:  true,
		},
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

			fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
			mustCreateFundraiser(t, fundraiserRepo, newFundraiser("rescue@example.com", fundraiserWallet, &oldImageObjectKey))
			if test.shareOldImage {
				supporterRepo := database.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, supporterRepo, newSupporter("supporter@example.com", "0xSupporter", &oldImageObjectKey))
			}

			router, jwtManager := newFundraiserProfileIntegrationRouter(t)
			token, err := jwtManager.Generate(strings.ToLower(fundraiserWallet), "", time.Hour)
			if err != nil {
				t.Fatalf("generate access token: %v", err)
			}
			request := httptest.NewRequest(
				http.MethodPut,
				"/v1/fundraiser/profile",
				strings.NewReader(`{"name":" Updated Rescue ","email":" updated@example.com ","imageObjectKey":"profiles/fundraiser-update-new.png","contactPerson":{"name":" Updated Contact ","phone":" +62 811 9999 "},"socialUrl":" https://example.com/updated ","country":" Malaysia ","zipCode":" 50450 "}`),
			)
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

			getRequest := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/profile", nil)
			getRequest.Header.Set("Authorization", "Bearer "+token)
			getResponse := httptest.NewRecorder()
			router.ServeHTTP(getResponse, getRequest)
			result := decodeAuthEndpointResult(t, getResponse)
			if result.HTTPStatus != http.StatusOK || result.Code != "PROFILE_RETRIEVED" {
				t.Fatalf("GET after PUT = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
			}
			var data struct {
				Name          string `json:"name"`
				Email         string `json:"email"`
				ContactPerson struct {
					Name  string `json:"name"`
					Phone string `json:"phone"`
				} `json:"contactPerson"`
				SocialURL string  `json:"socialUrl"`
				Country   string  `json:"country"`
				ZipCode   string  `json:"zipCode"`
				ImageURL  *string `json:"imageUrl"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode GET data: %v", err)
			}
			if data.Name != "Updated Rescue" || data.Email != "updated@example.com" || data.ContactPerson.Name != "Updated Contact" || data.ContactPerson.Phone != "+62 811 9999" {
				t.Errorf("replaced fields = %#v", data)
			}
			if data.SocialURL != "https://example.com/updated" || data.Country != "Malaysia" || data.ZipCode != "50450" || !equalStringPointers(data.ImageURL, integrationStringPointer("https://cdn.example.com/pawfund/"+newImageObjectKey)) {
				t.Errorf("replaced fields = %#v", data)
			}
		})
	}
}

func TestDeleteFundraiserProfileEndpoint(t *testing.T) {
	const fundraiserWallet = "0xFundraiserChecksum"
	imageObjectKey := "profiles/fundraiser-delete.png"

	tests := []struct {
		name            string
		createProfile   bool
		shareImage      bool
		activeCampaign  bool
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
		{
			name:            "rejects fundraiser with active campaign",
			createProfile:   true,
			activeCampaign:  true,
			wantHTTP:        http.StatusConflict,
			wantCode:        "ACTIVE_CAMPAIGNS_EXIST",
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
				repo := database.NewPostgresFundraiserRepository(testDatabase)
				fundraiser := newFundraiser("rescue@example.com", fundraiserWallet, &imageObjectKey)
				mustCreateFundraiser(t, repo, fundraiser)
				if test.shareImage {
					supporterRepo := database.NewPostgresSupporterRepository(testDatabase)
					mustCreateSupporter(t, supporterRepo, newSupporter("supporter@example.com", "0xSupporter", &imageObjectKey))
				}
				if test.activeCampaign {
					mustCreateActiveCampaign(t, fundraiser.ID)
				}
			}

			router, jwtManager := newFundraiserProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodDelete, "/v1/fundraiser/profile", nil)
			switch test.authorization {
			case "missing":
			case "invalid":
				request.Header.Set("Authorization", "Bearer invalid-token")
			default:
				token, err := jwtManager.Generate(strings.ToLower(fundraiserWallet), "", time.Hour)
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
			profileRequest := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/profile", nil)
			profileRequest.Header.Set("Authorization", request.Header.Get("Authorization"))
			profileResponse := httptest.NewRecorder()
			router.ServeHTTP(profileResponse, profileRequest)
			profileResult := decodeAuthEndpointResult(t, profileResponse)
			if profileResult.HTTPStatus != http.StatusNotFound || profileResult.Code != "PROFILE_NOT_FOUND" {
				t.Errorf("GET own profile after delete = %d/%q; body: %s", profileResult.HTTPStatus, profileResult.Code, profileResult.Body)
			}

			publicRequest := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/"+fundraiserWallet, nil)
			publicResponse := httptest.NewRecorder()
			router.ServeHTTP(publicResponse, publicRequest)
			publicResult := decodeAuthEndpointResult(t, publicResponse)
			if publicResult.HTTPStatus != http.StatusNotFound || publicResult.Code != "PROFILE_NOT_FOUND" {
				t.Errorf("GET public profile after delete = %d/%q; body: %s", publicResult.HTTPStatus, publicResult.Code, publicResult.Body)
			}
		})
	}
}

func TestFundraiserProfileReplaceRouteRejectsPatch(t *testing.T) {
	router, _ := newFundraiserProfileIntegrationRouter(t)
	request := httptest.NewRequest(http.MethodPatch, "/v1/fundraiser/profile", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH status = %d, want %d; body: %s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
	}
}

func putProfileImageObject(t *testing.T, objectKey string) {
	t.Helper()
	if _, err := testStorageClient.PutObject(
		t.Context(),
		testStorageBucket,
		objectKey,
		bytes.NewReader([]byte("profile-image")),
		int64(len("profile-image")),
		minio.PutObjectOptions{ContentType: "image/png"},
	); err != nil {
		t.Fatalf("put profile image %q: %v", objectKey, err)
	}
}

func integrationBoolPointer(value bool) *bool {
	return &value
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
	fundraiserRepository := database.NewPostgresFundraiserRepository(testDatabase)
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
	fundraiserService := service.NewFundraiserService(fundraiserRepository, uuid.NewV7, objectDeleter)
	application := &app.Application{
		DB:                testDatabase,
		AuthHandler:       handler.NewAuthHandler(nil, urlBuilder, logger),
		SupporterHandler:  handler.NewSupporterHandler(nil, urlBuilder, logger),
		FundraiserHandler: handler.NewFundraiserHandler(fundraiserService, urlBuilder, logger),
		Authenticate:      appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
}
