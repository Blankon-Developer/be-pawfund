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
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

func TestRegisterSupporterEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtManager, err := auth.NewJWTManager([]byte(strings.Repeat("i", 32)))
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
	if err != nil {
		t.Fatalf("create URL builder: %v", err)
	}
	repo := repository.NewPostgresSupporterRepository(testDatabase)
	supporterService := service.NewSupporterService(repo, uuid.NewV7)
	handler := api.NewSupporterHandler(supporterService, urlBuilder, logger)
	application := &app.Application{
		DB:               testDatabase,
		SupporterHandler: handler,
		Authenticate:     appmiddleware.Authenticate(jwtManager, logger),
	}
	router := routes.Setup(application, logger)

	tests := []struct {
		name           string
		prepare        func(t *testing.T)
		body           string
		walletAddress  string
		authorization  string
		wantHTTP       int
		wantCode       string
		wantErrors     httpx.FieldErrors
		candidateEmail string
		wantPersisted  bool
		wantImageURL   *string
	}{
		{
			name:           "registers supporter with image",
			body:           `{"name":" Cat Lover ","email":" CAT@EXAMPLE.COM ","imageObjectKey":"profiles/cat photo.png"}`,
			walletAddress:  "0xendpoint-cat",
			wantHTTP:       http.StatusCreated,
			wantCode:       "SUPPORTER_REGISTERED",
			candidateEmail: "cat@example.com",
			wantPersisted:  true,
			wantImageURL:   stringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
		},
		{
			name:           "registers supporter without image",
			body:           `{"name":"Dog Lover","email":"dog@example.com"}`,
			walletAddress:  "0xendpoint-dog",
			wantHTTP:       http.StatusCreated,
			wantCode:       "SUPPORTER_REGISTERED",
			candidateEmail: "dog@example.com",
			wantPersisted:  true,
		},
		{
			name: "returns conflict for duplicate email",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("duplicate@example.com", "0xexisting-email", nil))
			},
			body:           `{"name":"Duplicate","email":"duplicate@example.com"}`,
			walletAddress:  "0xnew-email-wallet",
			wantHTTP:       http.StatusConflict,
			wantCode:       "EMAIL_ALREADY_REGISTERED",
			wantErrors:     httpx.FieldErrors{"email": {"email is already registered!"}},
			candidateEmail: "duplicate@example.com",
		},
		{
			name: "returns conflict for duplicate wallet",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, repo, newSupporter("existing@example.com", "0xduplicate-wallet", nil))
			},
			body:           `{"name":"Duplicate","email":"new@example.com"}`,
			walletAddress:  "0xduplicate-wallet",
			wantHTTP:       http.StatusConflict,
			wantCode:       "WALLET_ALREADY_REGISTERED",
			wantErrors:     httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
			candidateEmail: "new@example.com",
		},
		{
			name:           "returns all validation errors without inserting",
			body:           `{"name":"","email":"bad"}`,
			walletAddress:  "0xvalidation",
			wantHTTP:       http.StatusUnprocessableEntity,
			wantCode:       "VALIDATION_ERROR",
			wantErrors:     httpx.FieldErrors{"name": {"name is required!"}, "email": {"email format is not valid!"}},
			candidateEmail: "bad",
		},
		{
			name:           "rejects spoofed wallet field",
			body:           `{"name":"Spoof","email":"spoof@example.com","walletAddress":"0xspoofed"}`,
			walletAddress:  "0xauthenticated",
			wantHTTP:       http.StatusBadRequest,
			wantCode:       "INVALID_REQUEST",
			candidateEmail: "spoof@example.com",
		},
		{
			name:           "rejects invalid token",
			body:           `{"name":"Invalid","email":"invalid@example.com"}`,
			authorization:  "Bearer invalid-token",
			wantHTTP:       http.StatusUnauthorized,
			wantCode:       "INVALID_ACCESS_TOKEN",
			candidateEmail: "invalid@example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			authorization := test.authorization
			if authorization == "" {
				token, err := jwtManager.Generate(test.walletAddress, time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				authorization = "Bearer " + token
			}

			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/register/supporter",
				strings.NewReader(test.body),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", authorization)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantHTTP {
				t.Errorf("HTTP status = %d, want %d; body: %s", response.Code, test.wantHTTP, response.Body.String())
			}
			var decoded struct {
				Code   string            `json:"code"`
				Data   json.RawMessage   `json:"data"`
				Errors httpx.FieldErrors `json:"errors"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if decoded.Code != test.wantCode {
				t.Errorf("code = %q, want %q", decoded.Code, test.wantCode)
			}
			if !equalFieldErrors(decoded.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", decoded.Errors, test.wantErrors)
			}

			assertEndpointPersistence(
				t,
				test.candidateEmail,
				test.walletAddress,
				test.wantPersisted,
			)
			if test.wantPersisted {
				var data struct {
					WalletAddress string  `json:"walletAddress"`
					ImageURL      *string `json:"imageUrl"`
				}
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode response data: %v", err)
				}
				if data.WalletAddress != test.walletAddress {
					t.Errorf("response wallet = %q, want %q", data.WalletAddress, test.walletAddress)
				}
				if !equalStringPointers(data.ImageURL, test.wantImageURL) {
					t.Errorf("response image URL = %v, want %v", data.ImageURL, test.wantImageURL)
				}
				if strings.Contains(string(decoded.Data), "wallet_address") {
					t.Errorf("response does not use camelCase: %s", decoded.Data)
				}
			}
		})
	}
}

func assertEndpointPersistence(t *testing.T, email, wallet string, wantPersisted bool) {
	t.Helper()
	if email == "" || wallet == "" {
		return
	}

	var count int
	if err := testDatabase.QueryRowContext(
		t.Context(),
		"SELECT count(*) FROM users WHERE email = $1 AND wallet_address = $2",
		email,
		wallet,
	).Scan(&count); err != nil {
		t.Fatalf("query endpoint persistence: %v", err)
	}
	if got := count == 1; got != wantPersisted {
		t.Errorf("supporter persisted = %v, want %v", got, wantPersisted)
	}
}

func equalFieldErrors(left, right httpx.FieldErrors) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
