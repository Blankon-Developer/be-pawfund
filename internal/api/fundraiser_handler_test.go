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
	err               error
	called            int
	input             service.RegisterFundraiserInput
	profile           domain.Fundraiser
	getProfileErr     error
	getProfileCalls   int
	getProfileAddress string
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

func (s *stubFundraiserRegistrar) GetProfile(
	_ context.Context,
	walletAddress string,
) (domain.Fundraiser, error) {
	s.getProfileCalls++
	s.getProfileAddress = walletAddress
	if s.getProfileErr != nil {
		return domain.Fundraiser{}, s.getProfileErr
	}
	return s.profile, nil
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

func TestFundraiserHandlerHandleGetProfile(t *testing.T) {
	imageKey := "profiles/rescue photo.png"
	socialURL := "https://example.com/rescue"
	profile := domain.Fundraiser{
		User: domain.User{
			Role:          domain.UserRoleFundraiser,
			Email:         "rescue@example.com",
			WalletAddress: "0xWalletChecksum",
		},
		Name:           "Animal Rescue",
		ImageObjectKey: &imageKey,
		ContactName:    "Jane Doe",
		ContactPhone:   "+62 812 3456",
		SocialURL:      &socialURL,
		Country:        "Indonesia",
		ZipCode:        "10110",
	}
	profileWithoutOptionalFields := profile
	profileWithoutOptionalFields.ImageObjectKey = nil
	profileWithoutOptionalFields.SocialURL = nil
	unexpectedFailure := errors.New("unexpected failure")

	tests := []struct {
		name             string
		principal        *auth.Principal
		profile          domain.Fundraiser
		serviceError     error
		wantHTTP         int
		wantCode         string
		wantServiceCalls int
		wantAddress      string
		wantImageURL     *string
		wantSocialURL    string
	}{
		{
			name:             "returns authenticated fundraiser profile",
			principal:        &auth.Principal{WalletAddress: " 0xWalletChecksum "},
			profile:          profile,
			wantHTTP:         http.StatusOK,
			wantCode:         "PROFILE_RETRIEVED",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
			wantImageURL:     stringPointer("https://cdn.example.com/pawfund/profiles/rescue%20photo.png"),
			wantSocialURL:    socialURL,
		},
		{
			name:             "returns null image and empty social URL when omitted",
			principal:        &auth.Principal{WalletAddress: "0xWalletChecksum"},
			profile:          profileWithoutOptionalFields,
			wantHTTP:         http.StatusOK,
			wantCode:         "PROFILE_RETRIEVED",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
		},
		{
			name:     "requires authenticated principal",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "INVALID_ACCESS_TOKEN",
		},
		{
			name:      "rejects blank wallet claim",
			principal: &auth.Principal{WalletAddress: " "},
			wantHTTP:  http.StatusUnauthorized,
			wantCode:  "INVALID_ACCESS_TOKEN",
		},
		{
			name:             "maps missing fundraiser profile",
			principal:        &auth.Principal{WalletAddress: "0xWalletChecksum"},
			serviceError:     service.ErrProfileNotFound,
			wantHTTP:         http.StatusNotFound,
			wantCode:         "PROFILE_NOT_FOUND",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
		},
		{
			name:             "hides unexpected service error",
			principal:        &auth.Principal{WalletAddress: "0xWalletChecksum"},
			serviceError:     unexpectedFailure,
			wantHTTP:         http.StatusInternalServerError,
			wantCode:         "INTERNAL_SERVER_ERROR",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &stubFundraiserRegistrar{
				profile:       test.profile,
				getProfileErr: test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewFundraiserHandler(serviceStub, urlBuilder, logger)
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/profile", nil)
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandleGetProfile(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf(
					"status/code = %d/%q, want %d/%q; body: %s",
					response.Code,
					decoded.Code,
					test.wantHTTP,
					test.wantCode,
					response.Body.String(),
				)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP == http.StatusUnauthorized && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
			}
			if serviceStub.getProfileCalls != test.wantServiceCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.getProfileCalls, test.wantServiceCalls)
			}
			if serviceStub.getProfileAddress != test.wantAddress {
				t.Errorf("service address = %q, want %q", serviceStub.getProfileAddress, test.wantAddress)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			var data GetProfileResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode profile data: %v", err)
			}
			if data.Name != profile.Name || data.Email != profile.Email || data.WalletAddress != profile.WalletAddress {
				t.Errorf("profile identity = %#v", data)
			}
			if data.ContactPerson.Name != profile.ContactName || data.ContactPerson.Phone != profile.ContactPhone {
				t.Errorf("contact person = %#v", data.ContactPerson)
			}
			if data.SocialURL != test.wantSocialURL || data.Country != profile.Country || data.ZipCode != profile.ZipCode {
				t.Errorf("profile details = %#v", data)
			}
			if !equalStringPointers(data.ImageURL, test.wantImageURL) {
				t.Errorf("image URL = %v, want %v", pointerValue(data.ImageURL), pointerValue(test.wantImageURL))
			}
			if strings.Contains(string(decoded.Data), "imageObjectKey") || strings.Contains(string(decoded.Data), `"role"`) {
				t.Errorf("response leaks internal fields: %s", decoded.Data)
			}
		})
	}
}
