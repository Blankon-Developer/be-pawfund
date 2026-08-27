package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

type uploadServiceStub struct {
	result service.PresignProfileImageResult
	err    error
	calls  int
	input  service.PresignProfileImageInput
}

func (s *uploadServiceStub) PresignProfileImage(
	_ context.Context,
	input service.PresignProfileImageInput,
) (service.PresignProfileImageResult, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func (s *uploadServiceStub) PresignCampaignImage(
	_ context.Context,
	input service.PresignCampaignImageInput,
) (service.PresignCampaignImageResult, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func TestHandlePresignProfileImage(t *testing.T) {
	unexpectedFailure := errors.New("storage unavailable")
	largeBody := `{"contentType":"image/jpeg","size":1}` + strings.Repeat(" ", maxPresignProfileImageBodyBytes)

	tests := []struct {
		name           string
		body           string
		contentType    string
		principal      *auth.Principal
		serviceError   error
		wantHTTP       int
		wantCode       string
		wantErrors     httpx.FieldErrors
		wantCalls      int
		wantInput      service.PresignProfileImageInput
		wantAuthHeader bool
	}{
		{
			name:        "presigns upload for unregistered wallet token",
			body:        `{"contentType":" IMAGE/JPEG ","size":123456}`,
			contentType: "application/json",
			principal:   &auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:    http.StatusOK,
			wantCode:    "PROFILE_IMAGE_UPLOAD_PRESIGNED",
			wantCalls:   1,
			wantInput:   service.PresignProfileImageInput{ContentType: "image/jpeg", Size: 123456},
		},
		{
			name:        "presigns upload for registered role",
			body:        `{"contentType":"image/webp","size":1}`,
			contentType: "application/json",
			principal:   &auth.Principal{WalletAddress: "0xwallet", Role: "fundraiser"},
			wantHTTP:    http.StatusOK,
			wantCode:    "PROFILE_IMAGE_UPLOAD_PRESIGNED",
			wantCalls:   1,
			wantInput:   service.PresignProfileImageInput{ContentType: "image/webp", Size: 1},
		},
		{
			name:           "requires authenticated principal",
			body:           `{"contentType":"image/png","size":1}`,
			contentType:    "application/json",
			wantHTTP:       http.StatusUnauthorized,
			wantCode:       "INVALID_ACCESS_TOKEN",
			wantAuthHeader: true,
		},
		{
			name:        "requires JSON content type",
			body:        `{"contentType":"image/png","size":1}`,
			contentType: "text/plain",
			principal:   &auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:    http.StatusUnsupportedMediaType,
			wantCode:    "UNSUPPORTED_MEDIA_TYPE",
		},
		{
			name:        "rejects malformed JSON",
			body:        `{"contentType":`,
			contentType: "application/json",
			principal:   &auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:    http.StatusBadRequest,
			wantCode:    "INVALID_REQUEST",
		},
		{
			name:        "rejects body over 16 KiB",
			body:        largeBody,
			contentType: "application/json",
			principal:   &auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:    http.StatusRequestEntityTooLarge,
			wantCode:    "REQUEST_TOO_LARGE",
		},
		{
			name:        "returns content type and size validation errors",
			body:        `{"contentType":"image/gif","size":5242881}`,
			contentType: "application/json",
			principal:   &auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:    http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{
				"contentType": {"contentType must be image/jpeg, image/png, or image/webp!"},
				"size":        {"size must be between 1 and 5242880 bytes!"},
			},
		},
		{
			name:         "hides presigner failure",
			body:         `{"contentType":"image/png","size":10}`,
			contentType:  "application/json",
			principal:    &auth.Principal{WalletAddress: "0xwallet"},
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
			wantInput:    service.PresignProfileImageInput{ContentType: "image/png", Size: 10},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &uploadServiceStub{
				result: service.PresignProfileImageResult{
					ObjectKey: "profiles/0198a123-4567-7abc-8123-456789abcdef.jpg",
					URL:       "https://storage.example.com/presigned",
				},
				err: test.serviceError,
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewUploadHandler(serviceStub, logger)
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/uploads/profile-image/presign",
				strings.NewReader(test.body),
			)
			request.Header.Set("Content-Type", test.contentType)
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandlePresignProfileImage(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if !reflect.DeepEqual(decoded.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", decoded.Errors, test.wantErrors)
			}
			if serviceStub.calls != test.wantCalls || serviceStub.input != test.wantInput {
				t.Errorf("service call = %d/%#v, want %d/%#v", serviceStub.calls, serviceStub.input, test.wantCalls, test.wantInput)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if test.wantAuthHeader && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
			}
			if test.wantHTTP == http.StatusOK {
				var data presignProfileImageResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode success data: %v", err)
				}
				if data.ObjectKey != serviceStub.result.ObjectKey || data.URL != serviceStub.result.URL {
					t.Errorf("response data = %#v", data)
				}
			}
		})
	}
}

func TestHandlePresignCampaignImage(t *testing.T) {
	tests := []struct {
		name          string
		principal     auth.Principal
		serviceError  error
		wantHTTP      int
		wantCode      string
		wantCalls     int
		wantObjectKey string
	}{
		{
			name:          "presigns a campaign image for fundraiser",
			principal:     auth.Principal{WalletAddress: "0xfundraiser", Role: domain.UserRoleFundraiser},
			wantHTTP:      http.StatusOK,
			wantCode:      "CAMPAIGN_IMAGE_UPLOAD_PRESIGNED",
			wantCalls:     1,
			wantObjectKey: "campaigns/0198a123-4567-7abc-8123-456789abcdef.png",
		},
		{
			name:      "rejects supporter role",
			principal: auth.Principal{WalletAddress: "0xsupporter", Role: domain.UserRoleSupporter},
			wantHTTP:  http.StatusForbidden,
			wantCode:  "FUNDRAISER_ACCESS_REQUIRED",
		},
		{
			name:      "rejects an unregistered role",
			principal: auth.Principal{WalletAddress: "0xwallet"},
			wantHTTP:  http.StatusForbidden,
			wantCode:  "FUNDRAISER_ACCESS_REQUIRED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &uploadServiceStub{
				result: service.PresignCampaignImageResult{
					ObjectKey: "campaigns/0198a123-4567-7abc-8123-456789abcdef.png",
					URL:       "https://storage.example.com/presigned",
				},
				err: test.serviceError,
			}
			handler := NewUploadHandler(serviceStub, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/uploads/campaign-image/presign",
				strings.NewReader(`{"contentType":"image/png","size":123}`),
			)
			request.Header.Set("Content-Type", "application/json")
			request = request.WithContext(auth.ContextWithPrincipal(request.Context(), test.principal))
			response := httptest.NewRecorder()

			handler.HandlePresignCampaignImage(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q", response.Code, decoded.Code, test.wantHTTP, test.wantCode)
			}
			if serviceStub.calls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.calls, test.wantCalls)
			}
			if test.wantHTTP == http.StatusOK {
				var data presignProfileImageResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode success data: %v", err)
				}
				if data.ObjectKey != test.wantObjectKey {
					t.Errorf("object key = %q, want %q", data.ObjectKey, test.wantObjectKey)
				}
			}
		})
	}
}
