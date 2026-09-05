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
)

type stubSupporterRegistrar struct {
	err               error
	called            int
	input             service.RegisterSupporterInput
	replaceErr        error
	replaceCalls      int
	replaceInput      service.ReplaceSupporterProfileInput
	deleteErr         error
	deleteCalls       int
	deleteAddress     string
	profile           domain.Supporter
	getProfileErr     error
	getProfileCalls   int
	getProfileAddress string
	donations         []domain.Donation
	listDonationsErr  error
	listDonationCalls int
	listWallet        string
	listOptions       domain.DonationListOptions
}

func (s *stubSupporterRegistrar) ReplaceProfile(
	_ context.Context,
	input service.ReplaceSupporterProfileInput,
) error {
	s.replaceCalls++
	s.replaceInput = input
	return s.replaceErr
}

func (s *stubSupporterRegistrar) DeleteProfile(_ context.Context, walletAddress string) error {
	s.deleteCalls++
	s.deleteAddress = walletAddress
	return s.deleteErr
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

func (s *stubSupporterRegistrar) GetProfile(
	_ context.Context,
	walletAddress string,
) (domain.Supporter, error) {
	s.getProfileCalls++
	s.getProfileAddress = walletAddress
	if s.getProfileErr != nil {
		return domain.Supporter{}, s.getProfileErr
	}
	return s.profile, nil
}

func (s *stubSupporterRegistrar) ListMyDonations(
	_ context.Context,
	walletAddress string,
	options domain.DonationListOptions,
) ([]domain.Donation, error) {
	s.listDonationCalls++
	s.listWallet = walletAddress
	s.listOptions = options
	return s.donations, s.listDonationsErr
}

type decodedResponse struct {
	Status     string            `json:"status"`
	Code       string            `json:"code"`
	Data       json.RawMessage   `json:"data"`
	Pagination *httpx.Pagination `json:"pagination"`
	Errors     httpx.FieldErrors `json:"errors"`
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
			body:          `{"name":" Cat Lover ","email":" CAT@EXAMPLE.COM ","imageObjectKey":"tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusCreated,
			wantCode:      "SUPPORTER_REGISTERED",
			wantCall:      true,
			wantName:      "Cat Lover",
			wantImageURL:  stringPointer("https://cdn.example.com/pawfund/tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"),
		},
		{
			name:          "rejects a canonical profile image key",
			body:          `{"name":"Cat Lover","email":"cat@example.com","imageObjectKey":"profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors:    httpx.FieldErrors{"imageObjectKey": {"imageObjectKey must be a staged profile image key!"}},
		},
		{
			name:          "maps a missing staged image",
			body:          `{"name":"Cat Lover","email":"cat@example.com","imageObjectKey":"tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"}`,
			contentType:   "application/json",
			walletAddress: "0xauthenticated",
			serviceError:  service.ErrImageObjectNotFound,
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
			wantErrors:    httpx.FieldErrors{"imageObjectKey": {"imageObjectKey does not reference an uploaded image!"}},
			wantCall:      true,
			wantName:      "Cat Lover",
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

func TestHandleGetSupporterProfile(t *testing.T) {
	imageKey := "profiles/cat photo.png"
	profile := domain.Supporter{
		User: domain.User{
			Role:          domain.UserRoleSupporter,
			Email:         "cat@example.com",
			WalletAddress: "0xWalletChecksum",
		},
		Name:           "Cat Lover",
		ImageObjectKey: &imageKey,
	}
	profileWithoutImage := profile
	profileWithoutImage.ImageObjectKey = nil
	unexpectedFailure := errors.New("unexpected failure")

	tests := []struct {
		name             string
		principal        *auth.Principal
		profile          domain.Supporter
		serviceError     error
		wantHTTP         int
		wantCode         string
		wantServiceCalls int
		wantAddress      string
		wantImageURL     *string
	}{
		{
			name:             "returns authenticated supporter profile",
			principal:        &auth.Principal{WalletAddress: " 0xWalletChecksum "},
			profile:          profile,
			wantHTTP:         http.StatusOK,
			wantCode:         "PROFILE_RETRIEVED",
			wantServiceCalls: 1,
			wantAddress:      "0xWalletChecksum",
			wantImageURL:     stringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
		},
		{
			name:             "returns null image when omitted",
			principal:        &auth.Principal{WalletAddress: "0xWalletChecksum"},
			profile:          profileWithoutImage,
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
			name:             "maps missing supporter profile",
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
			serviceStub := &stubSupporterRegistrar{
				profile:       test.profile,
				getProfileErr: test.serviceError,
			}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewSupporterHandler(serviceStub, urlBuilder, logger)
			request := httptest.NewRequest(http.MethodGet, "/v1/supporter/profile", nil)
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

			var data mySupporterProfileResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode profile data: %v", err)
			}
			if data.Name != test.profile.Name || data.Email != test.profile.Email || data.WalletAddress != test.profile.WalletAddress {
				t.Errorf("profile identity = %#v", data)
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

func TestSupporterHandlerHandleReplaceProfile(t *testing.T) {
	fullBody := `{"name":" Cat Lover ","email":" CAT@EXAMPLE.COM "}`
	unexpectedFailure := errors.New("unexpected failure")
	tests := []struct {
		name         string
		body         string
		wallet       string
		serviceError error
		wantHTTP     int
		wantCode     string
		wantErrors   httpx.FieldErrors
		wantCall     bool
		assertInput  func(t *testing.T, input service.ReplaceSupporterProfileInput)
	}{
		{
			name:     "replaces profile and clears explicit image",
			body:     strings.TrimSuffix(fullBody, "}") + `,"imageObjectKey":null}`,
			wallet:   "0xWalletChecksum",
			wantHTTP: http.StatusNoContent,
			wantCall: true,
			assertInput: func(t *testing.T, input service.ReplaceSupporterProfileInput) {
				t.Helper()
				if input.WalletAddress != "0xWalletChecksum" || input.Profile.Name != "Cat Lover" || input.Profile.Email != "cat@example.com" {
					t.Errorf("replacement input = %#v", input)
				}
				if !input.Profile.ImageObjectKey.Set || input.Profile.ImageObjectKey.Value != nil {
					t.Errorf("image update = %#v", input.Profile.ImageObjectKey)
				}
			},
		},
		{
			name:     "preserves image when omitted",
			body:     fullBody,
			wallet:   "0xWalletChecksum",
			wantHTTP: http.StatusNoContent,
			wantCall: true,
			assertInput: func(t *testing.T, input service.ReplaceSupporterProfileInput) {
				t.Helper()
				if input.Profile.ImageObjectKey.Set {
					t.Errorf("image update = %#v, want omitted", input.Profile.ImageObjectKey)
				}
			},
		},
		{
			name:       "requires profile fields",
			body:       `{}`,
			wallet:     "0xWalletChecksum",
			wantHTTP:   http.StatusUnprocessableEntity,
			wantCode:   "VALIDATION_ERROR",
			wantErrors: httpx.FieldErrors{"name": {"name is required!"}, "email": {"email is required!"}},
		},
		{name: "rejects client supplied wallet address", body: strings.TrimSuffix(fullBody, "}") + `,"walletAddress":"0xSpoofed"}`, wallet: "0xWalletChecksum", wantHTTP: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "requires authenticated principal", body: fullBody, wantHTTP: http.StatusUnauthorized, wantCode: "INVALID_ACCESS_TOKEN"},
		{name: "maps missing profile", body: fullBody, wallet: "0xWalletChecksum", serviceError: service.ErrProfileNotFound, wantHTTP: http.StatusNotFound, wantCode: "PROFILE_NOT_FOUND", wantCall: true},
		{name: "maps duplicate email", body: fullBody, wallet: "0xWalletChecksum", serviceError: service.ErrEmailAlreadyRegistered, wantHTTP: http.StatusConflict, wantCode: "EMAIL_ALREADY_REGISTERED", wantErrors: httpx.FieldErrors{"email": {"email is already registered!"}}, wantCall: true},
		{name: "hides unexpected service error", body: fullBody, wallet: "0xWalletChecksum", serviceError: unexpectedFailure, wantHTTP: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR", wantCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceStub := &stubSupporterRegistrar{replaceErr: test.serviceError}
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			handler := NewSupporterHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPut, "/v1/supporter/profile", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.wallet != "" {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{WalletAddress: test.wallet}))
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

func TestSupporterHandlerHandleDeleteProfile(t *testing.T) {
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
		{name: "hides unexpected service error", walletAddress: "0xWalletChecksum", serviceError: unexpectedFailure, wantHTTP: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR", wantCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			serviceStub := &stubSupporterRegistrar{deleteErr: test.serviceError}
			handler := NewSupporterHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodDelete, "/v1/supporter/profile", nil)
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

func TestSupporterHandlerHandleGetMyDonations(t *testing.T) {
	donatedAt := time.Date(2026, time.August, 11, 8, 30, 45, 0, time.UTC)
	donation := domain.Donation{
		Amount: 2_500_000,
		Campaign: domain.DonationCampaign{
			Title:           "Emergency Rescue",
			ContractAddress: "0xCampaign",
		},
		DonatedAt: donatedAt,
		TxHash:    "0xTransaction",
	}
	unexpectedFailure := errors.New("database unavailable")

	tests := []struct {
		name         string
		query        string
		principal    *auth.Principal
		donations    []domain.Donation
		serviceError error
		wantHTTP     int
		wantCode     string
		wantCalls    int
		wantEmpty    bool
	}{
		{
			name:      "returns paginated supporter donations",
			query:     "?page=2&pageSize=25",
			principal: &auth.Principal{WalletAddress: " 0xSupporter ", Role: domain.UserRoleSupporter},
			donations: []domain.Donation{donation},
			wantHTTP:  http.StatusOK,
			wantCode:  "DONATIONS_RETRIEVED",
			wantCalls: 1,
		},
		{
			name:      "returns an empty JSON array",
			principal: &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			wantHTTP:  http.StatusOK,
			wantCode:  "DONATIONS_RETRIEVED",
			wantCalls: 1,
			wantEmpty: true,
		},
		{
			name:     "requires authenticated principal",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "INVALID_ACCESS_TOKEN",
		},
		{
			name:      "requires supporter role",
			principal: &auth.Principal{WalletAddress: "0xFundraiser", Role: domain.UserRoleFundraiser},
			wantHTTP:  http.StatusForbidden,
			wantCode:  "SUPPORTER_ACCESS_REQUIRED",
		},
		{
			name:      "rejects invalid pagination",
			query:     "?page=0&pageSize=101",
			principal: &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			wantHTTP:  http.StatusUnprocessableEntity,
			wantCode:  "VALIDATION_ERROR",
		},
		{
			name:         "maps missing supporter profile",
			principal:    &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			serviceError: service.ErrProfileNotFound,
			wantHTTP:     http.StatusNotFound,
			wantCode:     "PROFILE_NOT_FOUND",
			wantCalls:    1,
		},
		{
			name:         "hides unexpected service error",
			principal:    &auth.Principal{WalletAddress: "0xSupporter", Role: domain.UserRoleSupporter},
			serviceError: unexpectedFailure,
			wantHTTP:     http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
			if err != nil {
				t.Fatalf("create public URL builder: %v", err)
			}
			serviceStub := &stubSupporterRegistrar{
				donations:        test.donations,
				listDonationsErr: test.serviceError,
			}
			handler := NewSupporterHandler(serviceStub, urlBuilder, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/v1/supporter/donations"+test.query, nil)
			if test.principal != nil {
				request = request.WithContext(auth.ContextWithPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()

			handler.HandleGetMyDonations(response, request)

			var decoded decodedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v; body: %s", err, response.Body.String())
			}
			if response.Code != test.wantHTTP || decoded.Code != test.wantCode {
				t.Errorf("status/code = %d/%q, want %d/%q; body: %s", response.Code, decoded.Code, test.wantHTTP, test.wantCode, response.Body.String())
			}
			if serviceStub.listDonationCalls != test.wantCalls {
				t.Errorf("service calls = %d, want %d", serviceStub.listDonationCalls, test.wantCalls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if serviceStub.listWallet != "0xSupporter" {
				t.Errorf("wallet = %q, want normalized supporter wallet", serviceStub.listWallet)
			}
			wantOptions := domain.DonationListOptions{Page: 1, PageSize: 10}
			if test.query != "" {
				wantOptions = domain.DonationListOptions{Page: 2, PageSize: 25}
			}
			if serviceStub.listOptions != wantOptions {
				t.Errorf("options = %#v, want %#v", serviceStub.listOptions, wantOptions)
			}
			if test.wantEmpty {
				if string(decoded.Data) != "[]" {
					t.Errorf("empty data = %s, want []", decoded.Data)
				}
				return
			}

			var data []myDonationItemListResponse
			if err := json.Unmarshal(decoded.Data, &data); err != nil {
				t.Fatalf("decode donation data: %v", err)
			}
			if len(data) != 1 || data[0].Amount != donation.Amount || data[0].Campaign.Title != donation.Campaign.Title || data[0].Campaign.ContractAddress != donation.Campaign.ContractAddress || data[0].DonatedOn != "2026-08-11T08:30:45Z" || data[0].TxHash != donation.TxHash {
				t.Errorf("donation response = %#v", data)
			}
		})
	}
}
