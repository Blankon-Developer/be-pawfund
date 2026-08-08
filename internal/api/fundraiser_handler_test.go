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

type stubFundraiserRegistrar struct {
	err    error
	called int
	input  service.RegisterFundraiserInput
}

func (s *stubFundraiserRegistrar) Register(
	_ context.Context,
	input service.RegisterFundraiserInput,
) (domain.Fundraiser, error) {
	s.called++
	s.input = input
	if s.err != nil {
		return domain.Fundraiser{}, s.err
	}
	socialURL := input.SocialURL
	return domain.Fundraiser{
		User: domain.User{
			Role:          domain.UserRoleFundraiser,
			Email:         input.Email,
			WalletAddress: input.WalletAddress,
		},
		Name:           input.Name,
		ImageObjectKey: input.ImageObjectKey,
		ContactName:    input.ContactPerson.Name,
		ContactPhone:   input.ContactPerson.Phone,
		SocialURL:      &socialURL,
		Country:        input.Country,
		ZipCode:        input.ZipCode,
	}, nil
}

func TestHandleRegisterFundraiser(t *testing.T) {
	validBody := `{"name":" Animal Rescue ","email":" RESCUE@EXAMPLE.COM ","contactPerson":{"name":" Jane Doe ","phone":" +62 812 3456 "},"socialUrl":" https://example.com/rescue ","country":" Indonesia ","zipCode":" 10110 "}`
	oversizedBody := `{"name":"` + strings.Repeat("x", maxRegisterFundraiserBodyBytes) + `"}`
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
		wantImageURL  *string
	}{
		{
			name:          "registers fundraiser with authenticated wallet",
			body:          strings.TrimSuffix(validBody, "}") + `,"imageObjectKey":"profiles/rescue photo.png"}`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusCreated,
			wantCode:      "FUNDRAISER_REGISTERED",
			wantCall:      true,
			wantImageURL:  stringPointer("https://cdn.example.com/pawfund/profiles/rescue%20photo.png"),
		},
		{
			name:          "returns null image URL when image is omitted",
			body:          validBody,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusCreated,
			wantCode:      "FUNDRAISER_REGISTERED",
			wantCall:      true,
		},
		{
			name:          "returns all required validation errors",
			body:          `{}`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{
				"name":                {"name is required!"},
				"email":               {"email is required!"},
				"contactPerson.name":  {"contactPerson.name is required!"},
				"contactPerson.phone": {"contactPerson.phone is required!"},
				"socialUrl":           {"socialUrl is required!"},
				"country":             {"country is required!"},
				"zipCode":             {"zipCode is required!"},
			},
		},
		{
			name:          "rejects walletAddress from client body",
			body:          strings.TrimSuffix(validBody, "}") + `,"walletAddress":"0xspoofed"}`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusBadRequest,
			wantCode:      "INVALID_REQUEST",
		},
		{
			name:          "requires JSON content type",
			body:          validBody,
			contentType:   "text/plain",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusUnsupportedMediaType,
			wantCode:      "UNSUPPORTED_MEDIA_TYPE",
		},
		{
			name:          "rejects malformed JSON",
			body:          `{"name":`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusBadRequest,
			wantCode:      "INVALID_REQUEST",
		},
		{
			name:          "rejects body over one MiB",
			body:          oversizedBody,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusRequestEntityTooLarge,
			wantCode:      "REQUEST_TOO_LARGE",
		},
		{
			name:        "requires authenticated principal",
			body:        validBody,
			contentType: "application/json",
			wantHTTP:    http.StatusUnauthorized,
			wantCode:    "INVALID_ACCESS_TOKEN",
		},
		{
			name:          "maps duplicate email",
			body:          validBody,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			serviceError:  service.ErrEmailAlreadyRegistered,
			wantHTTP:      http.StatusConflict,
			wantCode:      "EMAIL_ALREADY_REGISTERED",
			wantErrors:    httpx.FieldErrors{"email": {"email is already registered!"}},
			wantCall:      true,
		},
		{
			name:          "maps duplicate wallet",
			body:          validBody,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			serviceError:  service.ErrWalletAlreadyRegistered,
			wantHTTP:      http.StatusConflict,
			wantCode:      "WALLET_ALREADY_REGISTERED",
			wantErrors:    httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
			wantCall:      true,
		},
		{
			name:          "hides unexpected service error",
			body:          validBody,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			serviceError:  unexpectedFailure,
			wantHTTP:      http.StatusInternalServerError,
			wantCode:      "INTERNAL_SERVER_ERROR",
			wantCall:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := &stubFundraiserRegistrar{err: test.serviceError}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewFundraiserHandler(registrar, urlBuilder, logger)

			request := httptest.NewRequest(http.MethodPost, "/v1/register/fundraiser", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.walletAddress != "" {
				request = request.WithContext(auth.ContextWithPrincipal(
					request.Context(),
					auth.Principal{WalletAddress: test.walletAddress},
				))
			}
			response := httptest.NewRecorder()

			handler.HandleRegisterFundraiser(response, request)

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
				if registrar.input.Name != "Animal Rescue" || registrar.input.Email != "rescue@example.com" {
					t.Errorf("service input was not normalized: %#v", registrar.input)
				}
			}

			if test.wantHTTP == http.StatusCreated {
				var data RegisterFundraiserResponse
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode success data: %v", err)
				}
				if data.WalletAddress != test.walletAddress || data.Role != domain.UserRoleFundraiser {
					t.Errorf("response identity = %q/%q", data.WalletAddress, data.Role)
				}
				if data.ContactPerson.Name != "Jane Doe" || data.SocialURL != "https://example.com/rescue" {
					t.Errorf("response profile = %#v", data)
				}
				if !equalStringPointers(data.ImageURL, test.wantImageURL) {
					t.Errorf("image URL = %v, want %v", pointerValue(data.ImageURL), pointerValue(test.wantImageURL))
				}
				if strings.Contains(string(decoded.Data), "address") || !strings.Contains(string(decoded.Data), "walletAddress") {
					t.Errorf("response wallet field is incorrect: %s", decoded.Data)
				}
			}
		})
	}
}
