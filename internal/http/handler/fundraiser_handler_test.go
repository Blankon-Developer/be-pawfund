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
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/go-chi/chi/v5"
)

type stubFundraiserRegistrar struct {
	err               error
	called            int
	input             service.RegisterFundraiserInput
	replaceErr        error
	replaceCalls      int
	replaceInput      service.ReplaceFundraiserProfileInput
	deleteErr         error
	deleteCalls       int
	deleteAddress     string
	profile           domain.Fundraiser
	getProfileErr     error
	getProfileCalls   int
	getProfileAddress string
}

func (s *stubFundraiserRegistrar) ReplaceProfile(
	_ context.Context,
	input service.ReplaceFundraiserProfileInput,
) error {
	s.replaceCalls++
	s.replaceInput = input
	return s.replaceErr
}

func (s *stubFundraiserRegistrar) DeleteProfile(_ context.Context, walletAddress string) error {
	s.deleteCalls++
	s.deleteAddress = walletAddress
	return s.deleteErr
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
			body:          strings.TrimSuffix(validBody, "}") + `,"imageObjectKey":"tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"}`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusCreated,
			wantCode:      "FUNDRAISER_REGISTERED",
			wantCall:      true,
			wantImageURL:  stringPointer("https://cdn.example.com/pawfund/tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"),
		},
		{
			name:          "maps a missing staged image",
			body:          strings.TrimSuffix(validBody, "}") + `,"imageObjectKey":"tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"}`,
			contentType:   "application/json",
			walletAddress: "0xWalletChecksum",
			serviceError:  service.ErrImageObjectNotFound,
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors:    httpx.FieldErrors{"imageObjectKey": {"imageObjectKey does not reference an uploaded image!"}},
			wantCall:      true,
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
				var data registerFundraiserResponse
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

func TestFundraiserHandlerHandleReplaceProfile(t *testing.T) {
	fullBody := `{"name":" Animal Rescue ","email":" RESCUE@EXAMPLE.COM ","contactPerson":{"name":" Jane Doe ","phone":" +62 812 3456 "},"socialUrl":" https://example.com/rescue ","country":" Indonesia ","zipCode":" 10110 "}`
	unexpectedFailure := errors.New("unexpected failure")
	tests := []struct {
		name          string
		body          string
		walletAddress string
		serviceError  error
		wantHTTP      int
		wantCode      string
		wantErrors    httpx.FieldErrors
		wantCall      bool
		assertInput   func(t *testing.T, input service.ReplaceFundraiserProfileInput)
	}{
		{
			name:          "replaces the full profile and clears an explicit image",
			body:          strings.TrimSuffix(fullBody, "}") + `,"imageObjectKey":null}`,
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusNoContent,
			wantCall:      true,
			assertInput: func(t *testing.T, input service.ReplaceFundraiserProfileInput) {
				t.Helper()
				profile := input.Profile
				if input.WalletAddress != "0xWalletChecksum" || profile.Name != "Animal Rescue" || profile.Email != "rescue@example.com" || profile.ContactName != "Jane Doe" || profile.ContactPhone != "+62 812 3456" || profile.SocialURL != "https://example.com/rescue" || profile.Country != "Indonesia" || profile.ZipCode != "10110" {
					t.Errorf("replacement input = %#v", input)
				}
				if !profile.ImageObjectKey.Set || profile.ImageObjectKey.Value != nil {
					t.Errorf("image update = %#v", profile.ImageObjectKey)
				}
			},
		},
		{
			name:          "preserves image when its key is omitted",
			body:          fullBody,
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusNoContent,
			wantCall:      true,
			assertInput: func(t *testing.T, input service.ReplaceFundraiserProfileInput) {
				t.Helper()
				if input.Profile.ImageObjectKey.Set {
					t.Errorf("image update = %#v, want omitted", input.Profile.ImageObjectKey)
				}
			},
		},
		{
			name:          "requires every full profile field",
			body:          `{}`,
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{
				"name": {"name is required!"}, "email": {"email is required!"}, "contactPerson.name": {"contactPerson.name is required!"}, "contactPerson.phone": {"contactPerson.phone is required!"}, "socialUrl": {"socialUrl is required!"}, "country": {"country is required!"}, "zipCode": {"zipCode is required!"},
			},
		},
		{
			name:          "rejects client supplied wallet address",
			body:          strings.TrimSuffix(fullBody, "}") + `,"walletAddress":"0xSpoofed"}`,
			walletAddress: "0xWalletChecksum",
			wantHTTP:      http.StatusBadRequest,
			wantCode:      "INVALID_REQUEST",
		},
		{name: "requires authenticated principal", body: fullBody, wantHTTP: http.StatusUnauthorized, wantCode: "INVALID_ACCESS_TOKEN"},
		{name: "maps missing profile", body: fullBody, walletAddress: "0xWalletChecksum", serviceError: service.ErrProfileNotFound, wantHTTP: http.StatusNotFound, wantCode: "PROFILE_NOT_FOUND", wantCall: true},
		{name: "maps duplicate email", body: fullBody, walletAddress: "0xWalletChecksum", serviceError: service.ErrEmailAlreadyRegistered, wantHTTP: http.StatusConflict, wantCode: "EMAIL_ALREADY_REGISTERED", wantErrors: httpx.FieldErrors{"email": {"email is already registered!"}}, wantCall: true},
		{name: "hides unexpected service error", body: fullBody, walletAddress: "0xWalletChecksum", serviceError: unexpectedFailure, wantHTTP: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR", wantCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &stubFundraiserRegistrar{replaceErr: test.serviceError}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewFundraiserHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPut, "/v1/fundraiser/profile", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.walletAddress != "" {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{WalletAddress: test.walletAddress}))
			}
			response := httptest.NewRecorder()

			handler.HandleReplaceProfile(response, request)

			if response.Code != test.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d; body: %s", response.Code, test.wantHTTP, response.Body.String())
			}
			if (serviceStub.replaceCalls > 0) != test.wantCall {
				t.Errorf("service calls = %d, want called = %v", serviceStub.replaceCalls, test.wantCall)
			}
			if test.wantHTTP == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Errorf("204 body = %q, want empty", response.Body.String())
				}
				if test.assertInput != nil {
					test.assertInput(t, serviceStub.replaceInput)
				}
				return
			}

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if decoded.Code != test.wantCode || !reflect.DeepEqual(decoded.Errors, test.wantErrors) {
				t.Errorf("response = %q/%#v, want %q/%#v", decoded.Code, decoded.Errors, test.wantCode, test.wantErrors)
			}
		})
	}
}

func TestFundraiserHandlerHandleDeleteProfile(t *testing.T) {
	unexpectedFailure := errors.New("unexpected failure")
	tests := []struct {
		name          string
		walletAddress string
		serviceError  error
		wantHTTP      int
		wantCode      string
		wantCall      bool
	}{
		{name: "deletes the authenticated profile", walletAddress: " 0xWalletChecksum ", wantHTTP: http.StatusNoContent, wantCall: true},
		{name: "requires authenticated principal", wantHTTP: http.StatusUnauthorized, wantCode: "INVALID_ACCESS_TOKEN"},
		{name: "maps missing profile", walletAddress: "0xWalletChecksum", serviceError: service.ErrProfileNotFound, wantHTTP: http.StatusNotFound, wantCode: "PROFILE_NOT_FOUND", wantCall: true},
		{name: "rejects active campaigns", walletAddress: "0xWalletChecksum", serviceError: service.ErrActiveCampaignsExist, wantHTTP: http.StatusConflict, wantCode: "ACTIVE_CAMPAIGNS_EXIST", wantCall: true},
		{name: "hides unexpected service error", walletAddress: "0xWalletChecksum", serviceError: unexpectedFailure, wantHTTP: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR", wantCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			serviceStub := &stubFundraiserRegistrar{deleteErr: test.serviceError}
			handler := NewFundraiserHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodDelete, "/v1/fundraiser/profile", nil)
			if test.walletAddress != "" {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{WalletAddress: test.walletAddress}))
			}
			response := httptest.NewRecorder()

			handler.HandleDeleteProfile(response, request)

			if response.Code != test.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d; body: %s", response.Code, test.wantHTTP, response.Body.String())
			}
			if (serviceStub.deleteCalls > 0) != test.wantCall {
				t.Errorf("service calls = %d, want called = %v", serviceStub.deleteCalls, test.wantCall)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Errorf("204 body = %q, want empty", response.Body.String())
				}
				if serviceStub.deleteAddress != "0xWalletChecksum" {
					t.Errorf("wallet address = %q, want normalized authenticated wallet", serviceStub.deleteAddress)
				}
				return
			}

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if decoded.Code != test.wantCode {
				t.Errorf("response code = %q, want %q", decoded.Code, test.wantCode)
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

			var data myFundraiserProfileResponse
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

func TestFundraiserHandlerHandleGetPublicProfile(t *testing.T) {
	imageKey := "profiles/rescue photo.png"
	socialURL := "https://example.com/rescue"
	createdAt := time.Date(2026, time.August, 9, 10, 30, 0, 0, time.FixedZone("WIB", 7*60*60))
	profile := domain.Fundraiser{
		User: domain.User{
			Role:          domain.UserRoleFundraiser,
			Email:         "rescue@example.com",
			WalletAddress: "0xWalletChecksum",
			CreatedAt:     createdAt,
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
		address          string
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
			name:             "returns public fundraiser profile for path address",
			address:          " 0xwalletchecksum ",
			profile:          profile,
			wantHTTP:         http.StatusOK,
			wantCode:         "PROFILE_RETRIEVED",
			wantServiceCalls: 1,
			wantAddress:      "0xwalletchecksum",
			wantImageURL:     stringPointer("https://cdn.example.com/pawfund/profiles/rescue%20photo.png"),
			wantSocialURL:    socialURL,
		},
		{
			name:             "returns null image and empty social URL when omitted",
			address:          "0xWalletChecksum",
			profile:          profileWithoutOptionalFields,
			wantHTTP:         http.StatusOK,
			wantCode:         "PROFILE_RETRIEVED",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
		},
		{
			name:             "maps missing fundraiser profile",
			address:          "0xMissing",
			serviceError:     service.ErrProfileNotFound,
			wantHTTP:         http.StatusNotFound,
			wantCode:         "PROFILE_NOT_FOUND",
			wantServiceCalls: 1,
			wantAddress:      "0xMissing",
		},
		{
			name:             "hides unexpected service error",
			address:          "0xWalletChecksum",
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
			request := httptest.NewRequest(http.MethodGet, "/v1/fundraiser/test", nil)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("address", test.address)
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
			response := httptest.NewRecorder()

			handler.HandleGetPublicProfile(response, request)

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
			if serviceStub.getProfileCalls != test.wantServiceCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.getProfileCalls, test.wantServiceCalls)
			}
			if serviceStub.getProfileAddress != test.wantAddress {
				t.Errorf("service address = %q, want %q", serviceStub.getProfileAddress, test.wantAddress)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			var data publicFundraiserProfileResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode profile data: %v", err)
			}
			if data.Name != profile.Name || data.Email != profile.Email {
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
			wantCreatedAt := profile.CreatedAt.UTC().Format(time.RFC3339)
			if data.CreatedAt != wantCreatedAt {
				t.Errorf("createdAt = %q, want %q", data.CreatedAt, wantCreatedAt)
			}
			if strings.Contains(string(decoded.Data), "walletAddress") || strings.Contains(string(decoded.Data), "imageObjectKey") || strings.Contains(string(decoded.Data), `"role"`) {
				t.Errorf("response leaks non-public fields: %s", decoded.Data)
			}
		})
	}
}
