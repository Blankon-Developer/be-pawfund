//go:build integration

package integration_test

import (
	"bytes"
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
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/http/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/http/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestProfileImagePresignEndpoint(t *testing.T) {
	router, jwtManager := newUploadIntegrationRouter(t)
	token, err := jwtManager.Generate("0xUnregisteredWallet", "", time.Hour)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}
	campaignToken, err := jwtManager.Generate("0xFundraiserWallet", domain.UserRoleFundraiser, time.Hour)
	if err != nil {
		t.Fatalf("generate fundraiser access token: %v", err)
	}

	t.Run("raw PUT with signed type and size succeeds", func(t *testing.T) {
		payload := []byte("raw-jpeg-fixture")
		presigned := requestProfileImagePresign(t, router, token, "image/jpeg", int64(len(payload)))
		t.Cleanup(func() { removeIntegrationObject(t, presigned.ObjectKey) })

		response := putPresignedObject(t, presigned.URL, "image/jpeg", payload)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("raw PUT status = %d; body: %s", response.StatusCode, readResponseBody(t, response))
		}
		_ = response.Body.Close()

		object, err := testStorageClient.StatObject(t.Context(), testStorageBucket, presigned.ObjectKey, minio.StatObjectOptions{})
		if err != nil {
			t.Fatalf("stat uploaded object: %v", err)
		}
		if object.Size != int64(len(payload)) || object.ContentType != "image/jpeg" {
			t.Errorf("stored metadata = size:%d type:%q", object.Size, object.ContentType)
		}
	})

	t.Run("different Content-Type is rejected by MinIO", func(t *testing.T) {
		payload := []byte("same-length-data")
		presigned := requestProfileImagePresign(t, router, token, "image/jpeg", int64(len(payload)))
		t.Cleanup(func() { removeIntegrationObject(t, presigned.ObjectKey) })

		response := putPresignedObject(t, presigned.URL, "image/png", payload)
		defer closeIntegrationResponseBody(t, response)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("mismatched Content-Type status = %d; body: %s", response.StatusCode, readResponseBody(t, response))
		}
	})

	t.Run("different Content-Length is rejected by MinIO", func(t *testing.T) {
		payload := []byte("signed-size")
		presigned := requestProfileImagePresign(t, router, token, "image/webp", int64(len(payload)))
		t.Cleanup(func() { removeIntegrationObject(t, presigned.ObjectKey) })

		response := putPresignedObject(t, presigned.URL, "image/webp", append(payload, '!'))
		defer closeIntegrationResponseBody(t, response)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("mismatched Content-Length status = %d; body: %s", response.StatusCode, readResponseBody(t, response))
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/uploads/profile-image/presign",
			strings.NewReader(`{"contentType":"image/png","size":1}`),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		result := decodeAuthEndpointResult(t, response)
		if result.HTTPStatus != http.StatusUnauthorized || result.Code != "ACCESS_TOKEN_REQUIRED" {
			t.Fatalf("unauthenticated presign = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
		}
	})

	t.Run("rejects an image over 5 MiB", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/uploads/profile-image/presign",
			strings.NewReader(`{"contentType":"image/png","size":5242881}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		result := decodeAuthEndpointResult(t, response)
		if result.HTTPStatus != http.StatusUnprocessableEntity || result.Code != "VALIDATION_ERROR" {
			t.Fatalf("oversized presign = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
		}
	})

	t.Run("campaign image uses the campaign namespace", func(t *testing.T) {
		payload := []byte("campaign-image-fixture")
		presigned := requestCampaignImagePresign(t, router, campaignToken, "image/webp", int64(len(payload)))
		t.Cleanup(func() { removeIntegrationObject(t, presigned.ObjectKey) })

		response := putPresignedObject(t, presigned.URL, "image/webp", payload)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("raw PUT status = %d; body: %s", response.StatusCode, readResponseBody(t, response))
		}
		_ = response.Body.Close()
		if !strings.HasPrefix(presigned.ObjectKey, "campaigns/") {
			t.Errorf("campaign object key = %q", presigned.ObjectKey)
		}
	})

	t.Run("campaign image requires fundraiser role", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/uploads/campaign-image/presign",
			strings.NewReader(`{"contentType":"image/png","size":1}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		result := decodeAuthEndpointResult(t, response)
		if result.HTTPStatus != http.StatusForbidden || result.Code != "FUNDRAISER_ACCESS_REQUIRED" {
			t.Fatalf("unregistered campaign presign = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
		}
	})
}

type profileImagePresignData struct {
	ObjectKey string `json:"objectKey"`
	URL       string `json:"url"`
}

func newUploadIntegrationRouter(t *testing.T) (http.Handler, *auth.JWTManager) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtManager, err := auth.NewJWTManager([]byte(strings.Repeat("u", 32)))
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	presigner, err := storage.NewPutPresigner(storage.PresignerConfig{
		Endpoint:  testStorageEndpoint,
		AccessKey: testStorageAccessKey,
		SecretKey: testStorageSecretKey,
		Bucket:    testStorageBucket,
		Region:    testStorageRegion,
		TTL:       15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create upload presigner: %v", err)
	}
	uploadService := service.NewUploadService(presigner, uuid.NewV7)
	application := &app.Application{
		UploadHandler: handler.NewUploadHandler(uploadService, logger),
		Authenticate:  appmiddleware.Authenticate(jwtManager, logger),
	}
	return routes.Setup(application, logger), jwtManager
}

func requestProfileImagePresign(
	t *testing.T,
	router http.Handler,
	token string,
	contentType string,
	size int64,
) profileImagePresignData {
	t.Helper()
	body, err := json.Marshal(map[string]any{"contentType": contentType, "size": size})
	if err != nil {
		t.Fatalf("encode presign request: %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/uploads/profile-image/presign",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	result := decodeAuthEndpointResult(t, response)
	if result.HTTPStatus != http.StatusOK || result.Code != "PROFILE_IMAGE_UPLOAD_PRESIGNED" {
		t.Fatalf("presign = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}
	var data profileImagePresignData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode presign response: %v", err)
	}
	if !strings.HasPrefix(data.ObjectKey, "profiles/") || data.URL == "" {
		t.Fatalf("presign data = %#v", data)
	}
	return data
}

func requestCampaignImagePresign(
	t *testing.T,
	router http.Handler,
	token string,
	contentType string,
	size int64,
) profileImagePresignData {
	t.Helper()
	body, err := json.Marshal(map[string]any{"contentType": contentType, "size": size})
	if err != nil {
		t.Fatalf("encode campaign presign request: %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/uploads/campaign-image/presign",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	result := decodeAuthEndpointResult(t, response)
	if result.HTTPStatus != http.StatusOK || result.Code != "CAMPAIGN_IMAGE_UPLOAD_PRESIGNED" {
		t.Fatalf("campaign presign = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}
	var data profileImagePresignData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode campaign presign response: %v", err)
	}
	return data
}

func putPresignedObject(t *testing.T, rawURL, contentType string, payload []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, rawURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create raw PUT: %v", err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform raw PUT: %v", err)
	}
	return response
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(body)
}

func closeIntegrationResponseBody(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("close storage response body: %v", err)
	}
}

func removeIntegrationObject(t *testing.T, objectKey string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := testStorageClient.RemoveObject(
		ctx,
		testStorageBucket,
		objectKey,
		minio.RemoveObjectOptions{},
	); err != nil {
		t.Errorf("remove test object %q: %v", objectKey, err)
	}
}
