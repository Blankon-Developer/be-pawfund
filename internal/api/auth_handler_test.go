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

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

type authServiceStub struct {
	message           string
	createErr         error
	verifyResult      service.VerifyAuthResult
	verifyErr         error
	requestedAddress  string
	verifiedMessage   string
	verifiedSignature string
}

func (s *authServiceStub) CreateMessage(_ context.Context, walletAddress string) (string, error) {
	s.requestedAddress = walletAddress
	return s.message, s.createErr
}

func (s *authServiceStub) Verify(
	_ context.Context,
	message string,
	signature string,
) (service.VerifyAuthResult, error) {
	s.verifiedMessage = message
	s.verifiedSignature = signature
	return s.verifyResult, s.verifyErr
}

func TestAuthHandlerHandleCreateMessage(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		message     string
		serviceErr  error
		wantHTTP    int
		wantCode    string
		wantErrors  httpx.FieldErrors
		wantAddress string
	}{
		{
			name:        "creates message",
			contentType: "application/json",
			body:        `{"address":" 0x1234567890123456789012345678901234567890 "}`,
			message:     "SIWE message",
			wantHTTP:    http.StatusOK,
			wantCode:    "AUTH_MESSAGE_CREATED",
			wantAddress: "0x1234567890123456789012345678901234567890",
		},
		{
			name:        "requires address",
			contentType: "application/json",
			body:        `{"address":" "}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_ERROR",
			wantErrors:  httpx.FieldErrors{"address": {"address is required!"}},
		},
		{
			name:        "rejects invalid wallet address",
			contentType: "application/json",
			body:        `{"address":"0xinvalid"}`,
			serviceErr:  service.ErrInvalidWalletAddress,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_ERROR",
			wantErrors:  httpx.FieldErrors{"address": {"address must be a valid Ethereum address!"}},
			wantAddress: "0xinvalid",
		},
		{
			name:     "requires JSON content type",
			body:     `{"address":"0x1234"}`,
			wantHTTP: http.StatusUnsupportedMediaType,
			wantCode: "UNSUPPORTED_MEDIA_TYPE",
		},
		{
			name:        "rejects malformed JSON",
			contentType: "application/json",
			body:        `{"address":`,
			wantHTTP:    http.StatusBadRequest,
			wantCode:    "INVALID_REQUEST",
		},
		{
			name:        "rejects unknown fields",
			contentType: "application/json",
			body:        `{"address":"0x1234","nonce":"client"}`,
			wantHTTP:    http.StatusBadRequest,
			wantCode:    "INVALID_REQUEST",
		},
		{
			name:        "rejects oversized body",
			contentType: "application/json",
			body:        `{"address":"` + strings.Repeat("a", maxAuthBodyBytes) + `"}`,
			wantHTTP:    http.StatusRequestEntityTooLarge,
			wantCode:    "REQUEST_TOO_LARGE",
		},
		{
			name:        "maps service failure",
			contentType: "application/json",
			body:        `{"address":"0x1234567890123456789012345678901234567890"}`,
			serviceErr:  errors.New("cache unavailable"),
			wantHTTP:    http.StatusInternalServerError,
			wantCode:    "INTERNAL_SERVER_ERROR",
			wantAddress: "0x1234567890123456789012345678901234567890",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &authServiceStub{message: test.message, createErr: test.serviceErr}
			handler := newTestAuthHandler(t, serviceStub)
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/message", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()

			handler.HandleCreateMessage(response, request)

			decoded := decodeAuthResponse(t, response)
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if !authFieldErrorsEqual(decoded.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", decoded.Errors, test.wantErrors)
			}
			if serviceStub.requestedAddress != test.wantAddress {
				t.Errorf("service address = %q, want %q", serviceStub.requestedAddress, test.wantAddress)
			}
			if test.message != "" && test.serviceErr == nil {
				var data createAuthMessageResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode message data: %v", err)
				}
				if data.Message != test.message {
					t.Errorf("message = %q, want %q", data.Message, test.message)
				}
			}
		})
	}
}

func TestAuthHandlerHandleVerify(t *testing.T) {
	imageKey := "profiles/cat photo.png"
	registeredResult := service.VerifyAuthResult{
		AccessToken: "registered-token",
		Address:     "0x1234567890123456789012345678901234567890",
		Profile: &domain.AuthProfile{
			Name:           "Cat Lover",
			Role:           domain.UserRoleSupporter,
			ImageObjectKey: &imageKey,
		},
	}

	tests := []struct {
		name                string
		contentType         string
		body                string
		result              service.VerifyAuthResult
		serviceErr          error
		wantHTTP            int
		wantCode            string
		wantErrors          httpx.FieldErrors
		wantIsNotRegistered bool
		wantImageURL        *string
		wantServiceCall     bool
	}{
		{
			name:                "verifies unregistered wallet",
			contentType:         "application/json",
			body:                `{"message":"message","signature":"0xsigned"}`,
			result:              service.VerifyAuthResult{AccessToken: "token", Address: "0xabc"},
			wantHTTP:            http.StatusOK,
			wantCode:            "AUTH_VERIFIED",
			wantIsNotRegistered: true,
			wantServiceCall:     true,
		},
		{
			name:                "returns registered profile",
			contentType:         "application/json",
			body:                `{"message":"message","signature":"0xsigned"}`,
			result:              registeredResult,
			wantHTTP:            http.StatusOK,
			wantCode:            "AUTH_VERIFIED",
			wantIsNotRegistered: false,
			wantImageURL:        stringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
			wantServiceCall:     true,
		},
		{
			name:        "requires message and signature",
			contentType: "application/json",
			body:        `{"message":" ","signature":" "}`,
			wantHTTP:    http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{
				"message":   {"message is required!"},
				"signature": {"signature is required!"},
			},
		},
		{
			name:            "maps invalid message",
			contentType:     "application/json",
			body:            `{"message":"invalid","signature":"0xsigned"}`,
			serviceErr:      service.ErrInvalidMessage,
			wantHTTP:        http.StatusUnprocessableEntity,
			wantCode:        "VALIDATION_ERROR",
			wantErrors:      httpx.FieldErrors{"message": {"message must be a valid SIWE message!"}},
			wantServiceCall: true,
		},
		{
			name:            "maps invalid signature format",
			contentType:     "application/json",
			body:            `{"message":"message","signature":"bad"}`,
			serviceErr:      service.ErrInvalidSignature,
			wantHTTP:        http.StatusUnprocessableEntity,
			wantCode:        "VALIDATION_ERROR",
			wantErrors:      httpx.FieldErrors{"signature": {"signature must be a valid 65-byte Ethereum signature!"}},
			wantServiceCall: true,
		},
		{
			name:            "maps verification failure",
			contentType:     "application/json",
			body:            `{"message":"message","signature":"0xsigned"}`,
			serviceErr:      service.ErrSIWEVerification,
			wantHTTP:        http.StatusUnauthorized,
			wantCode:        "SIWE_VERIFICATION_FAILED",
			wantServiceCall: true,
		},
		{
			name:            "maps internal failure",
			contentType:     "application/json",
			body:            `{"message":"message","signature":"0xsigned"}`,
			serviceErr:      errors.New("database unavailable"),
			wantHTTP:        http.StatusInternalServerError,
			wantCode:        "INTERNAL_SERVER_ERROR",
			wantServiceCall: true,
		},
		{
			name:        "rejects unknown address field",
			contentType: "application/json",
			body:        `{"message":"message","signature":"0xsigned","address":"0xspoofed"}`,
			wantHTTP:    http.StatusBadRequest,
			wantCode:    "INVALID_REQUEST",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &authServiceStub{verifyResult: test.result, verifyErr: test.serviceErr}
			handler := newTestAuthHandler(t, serviceStub)
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()

			handler.HandleVerify(response, request)

			decoded := decodeAuthResponse(t, response)
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if !authFieldErrorsEqual(decoded.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", decoded.Errors, test.wantErrors)
			}
			if got := serviceStub.verifiedMessage != ""; got != test.wantServiceCall {
				t.Errorf("service called = %v, want %v", got, test.wantServiceCall)
			}
			if test.wantHTTP == http.StatusOK {
				var data verifyAuthResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode verify data: %v", err)
				}
				if data.IsNotRegistered != test.wantIsNotRegistered {
					t.Errorf("isNotRegistered = %v, want %v", data.IsNotRegistered, test.wantIsNotRegistered)
				}
				if !equalStringPointers(data.ImageURL, test.wantImageURL) {
					t.Errorf("image URL = %v, want %v", data.ImageURL, test.wantImageURL)
				}
				if test.wantIsNotRegistered && (data.Name != nil || data.Role != nil || data.ImageURL != nil) {
					t.Errorf("unregistered profile fields = %#v", data)
				}
			}
		})
	}
}

type decodedAuthResponse struct {
	Code   string            `json:"code"`
	Data   json.RawMessage   `json:"data"`
	Errors httpx.FieldErrors `json:"errors"`
}

func decodeAuthResponse(t *testing.T, response *httptest.ResponseRecorder) decodedAuthResponse {
	t.Helper()
	var decoded decodedAuthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
	}
	return decoded
}

func newTestAuthHandler(t *testing.T, service AuthService) *AuthHandler {
	t.Helper()
	urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
	if err != nil {
		t.Fatalf("create URL builder: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAuthHandler(service, urlBuilder, logger)
}

func authFieldErrorsEqual(left, right httpx.FieldErrors) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
