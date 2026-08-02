package api

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
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
)

type stubSupporterRegistrar struct {
	err    error
	called int
	input  service.RegisterSupporterInput
}

func (s *stubSupporterRegistrar) Register(
	_ context.Context,
	input service.RegisterSupporterInput,
) (domain.Supporter, error) {
	s.called++
	s.input = input
	if s.err != nil {
		return domain.Supporter{}, s.err
	}
	return domain.Supporter{
		User: domain.User{
			Role:          domain.UserRoleSupporter,
			Email:         input.Email,
			WalletAddress: input.WalletAddress,
		},
		Name:           input.Name,
		ImageObjectKey: input.ImageObjectKey,
	}, nil
}

type decodedResponse struct {
	Status string            `json:"status"`
	Code   string            `json:"code"`
	Data   json.RawMessage   `json:"data"`
	Errors httpx.FieldErrors `json:"errors"`
}

func TestHandleRegisterSupporter(t *testing.T) {
	unexpectedFailure := errors.New("unexpected failure")

	tests := []struct {
		name          string
		body          string
		contentType   string
		walletAddress string
		serviceError  error
		wantHTTP      int
		wantCode      string
		wantErrors    httpx.FieldErrors
		wantCall      bool
		wantName      string
		wantImageURL  *string
	}{
		{
			name:          "registers supporter with authenticated wallet",
			body:          `{"name":" Cat Lover ","email":" CAT@EXAMPLE.COM ","imageObjectKey":"profiles/cat photo.png"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusCreated,
			wantCode:      "SUPPORTER_REGISTERED",
			wantCall:      true,
			wantName:      "Cat Lover",
			wantImageURL:  stringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
		},
		{
			name:          "returns null image URL when image is omitted",
			body:          `{"name":"Cat Lover","email":"cat@example.com"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusCreated,
			wantCode:      "SUPPORTER_REGISTERED",
			wantCall:      true,
			wantName:      "Cat Lover",
		},
		{
			name:          "returns all validation errors",
			body:          `{"name":"","email":"bad"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{
				"name":  {"name is required!"},
				"email": {"email format is not valid!"},
			},
		},
		{
			name:          "rejects walletAddress from client body",
			body:          `{"name":"Cat","email":"cat@example.com","walletAddress":"0xspoofed"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusBadRequest,
			wantCode:      "INVALID_REQUEST",
		},
		{
			name:          "requires JSON content type",
			body:          `{"name":"Cat","email":"cat@example.com"}`,
			contentType:   "text/plain",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusUnsupportedMediaType,
			wantCode:      "UNSUPPORTED_MEDIA_TYPE",
		},
		{
			name:        "requires authenticated principal",
			body:        `{"name":"Cat","email":"cat@example.com"}`,
			contentType: "application/json",
			wantHTTP:    http.StatusUnauthorized,
			wantCode:    "INVALID_ACCESS_TOKEN",
		},
		{
			name:          "maps duplicate email",
			body:          `{"name":"Cat","email":"cat@example.com"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			serviceError:  service.ErrEmailAlreadyRegistered,
			wantHTTP:      http.StatusConflict,
			wantCode:      "EMAIL_ALREADY_REGISTERED",
			wantErrors:    httpx.FieldErrors{"email": {"email is already registered!"}},
			wantCall:      true,
			wantName:      "Cat",
		},
		{
			name:          "maps duplicate wallet",
			body:          `{"name":"Cat","email":"cat@example.com"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			serviceError:  service.ErrWalletAlreadyRegistered,
			wantHTTP:      http.StatusConflict,
			wantCode:      "WALLET_ALREADY_REGISTERED",
			wantErrors:    httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
			wantCall:      true,
			wantName:      "Cat",
		},
		{
			name:          "hides unexpected service error",
			body:          `{"name":"Cat","email":"cat@example.com"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			serviceError:  unexpectedFailure,
			wantHTTP:      http.StatusInternalServerError,
			wantCode:      "INTERNAL_SERVER_ERROR",
			wantCall:      true,
			wantName:      "Cat",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := &stubSupporterRegistrar{err: test.serviceError}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewSupporterHandler(registrar, urlBuilder, logger)

			request := httptest.NewRequest(http.MethodPost, "/v1/register/supporter", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.walletAddress != "" {
				request = request.WithContext(auth.ContextWithPrincipal(
					request.Context(),
					auth.Principal{WalletAddress: test.walletAddress},
				))
			}
			response := httptest.NewRecorder()

			handler.HandleRegisterSupporter(response, request)

			if response.Code != test.wantHTTP {
				t.Errorf("HTTP status = %d, want %d; body: %s", response.Code, test.wantHTTP, response.Body.String())
			}
			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if decoded.Code != test.wantCode {
				t.Errorf("code = %q, want %q", decoded.Code, test.wantCode)
			}
			if !reflect.DeepEqual(decoded.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", decoded.Errors, test.wantErrors)
			}
			if (registrar.called > 0) != test.wantCall {
				t.Errorf("service calls = %d, want called = %v", registrar.called, test.wantCall)
			}

			if test.wantCall {
				if registrar.input.WalletAddress != test.walletAddress {
					t.Errorf("service wallet = %q, want %q", registrar.input.WalletAddress, test.walletAddress)
				}
				if registrar.input.Name != test.wantName || registrar.input.Email != "cat@example.com" {
					t.Errorf("service input was not normalized: %#v", registrar.input)
				}
			}

			if test.wantHTTP == http.StatusCreated {
				var data registerSupporterResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode success data: %v", err)
				}
				if !equalStringPointers(data.ImageURL, test.wantImageURL) {
					t.Errorf("image URL = %v, want %v", pointerValue(data.ImageURL), pointerValue(test.wantImageURL))
				}
				if strings.Contains(string(decoded.Data), "wallet_address") || !strings.Contains(string(decoded.Data), "walletAddress") {
					t.Errorf("response does not use camelCase: %s", decoded.Data)
				}
			}
		})
	}
}
