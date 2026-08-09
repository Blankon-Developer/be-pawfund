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
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	appmiddleware "github.com/Blankon-Developer/be-pawfund/internal/middleware"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/routes"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/Blankon-Developer/be-pawfund/internal/storage"
	"github.com/google/uuid"
)

func TestRegisterFundraiserEndpoint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jwtManager, err := auth.NewJWTManager([]byte(strings.Repeat("f", 32)))
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	urlBuilder, err := storage.NewPublicURLBuilder("https://cdn.example.com/pawfund")
	if err != nil {
		t.Fatalf("create URL builder: %v", err)
	}
	fundraiserRepo := repository.NewPostgresFundraiserRepository(testDatabase)
	fundraiserService := service.NewFundraiserService(fundraiserRepo, uuid.NewV7)
	fundraiserHandler := api.NewFundraiserHandler(fundraiserService, urlBuilder, logger)
	application := &app.Application{
		DB:                testDatabase,
		AuthHandler:       api.NewAuthHandler(nil, urlBuilder, logger),
		SupporterHandler:  api.NewSupporterHandler(nil, urlBuilder, logger),
		FundraiserHandler: fundraiserHandler,
		Authenticate:      appmiddleware.Authenticate(jwtManager, logger),
	}
	router := routes.Setup(application, logger)

	validBody := `{"name":" Animal Rescue ","email":" RESCUE@EXAMPLE.COM ","contactPerson":{"name":" Jane Doe ","phone":" +62 812 3456 "},"socialUrl":"https://example.com/rescue","country":" Indonesia ","zipCode":" 10110 ","imageObjectKey":"profiles/rescue photo.png"}`
	supporterRepo := repository.NewPostgresSupporterRepository(testDatabase)

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
	}{
		{
			name:           "registers fundraiser",
			body:           validBody,
			walletAddress:  "0xFundraiserChecksum",
			wantHTTP:       http.StatusCreated,
			wantCode:       "FUNDRAISER_REGISTERED",
			candidateEmail: "rescue@example.com",
			wantPersisted:  true,
		},
		{
			name: "returns conflict for email registered by another role",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, supporterRepo, newSupporter("rescue@example.com", "0xexisting-email", nil))
			},
			body:           validBody,
			walletAddress:  "0xnew-wallet",
			wantHTTP:       http.StatusConflict,
			wantCode:       "EMAIL_ALREADY_REGISTERED",
			wantErrors:     httpx.FieldErrors{"email": {"email is already registered!"}},
			candidateEmail: "rescue@example.com",
		},
		{
			name: "returns conflict for wallet registered by another role",
			prepare: func(t *testing.T) {
				mustCreateSupporter(t, supporterRepo, newSupporter("existing@example.com", "0xduplicate-wallet", nil))
			},
			body:           strings.Replace(validBody, "RESCUE@EXAMPLE.COM", "NEW@EXAMPLE.COM", 1),
			walletAddress:  "0xduplicate-wallet",
			wantHTTP:       http.StatusConflict,
			wantCode:       "WALLET_ALREADY_REGISTERED",
			wantErrors:     httpx.FieldErrors{"walletAddress": {"wallet address is already registered!"}},
			candidateEmail: "new@example.com",
		},
		{
			name:          "returns validation errors without inserting",
			body:          `{}`,
			walletAddress: "0xvalidation",
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
			candidateEmail: "validation@example.com",
		},
		{
			name:           "rejects spoofed wallet field",
			body:           strings.TrimSuffix(validBody, "}") + `,"walletAddress":"0xspoofed"}`,
			walletAddress:  "0xauthenticated",
			wantHTTP:       http.StatusBadRequest,
			wantCode:       "INVALID_REQUEST",
			candidateEmail: "rescue@example.com",
		},
		{
			name:           "rejects invalid token",
			body:           validBody,
			authorization:  "Bearer invalid-token",
			wantHTTP:       http.StatusUnauthorized,
			wantCode:       "INVALID_ACCESS_TOKEN",
			candidateEmail: "rescue@example.com",
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
				token, err := jwtManager.Generate(test.walletAddress, "", time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				authorization = "Bearer " + token
			}

			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/register/fundraiser",
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

			assertFundraiserEndpointPersistence(t, test.candidateEmail, test.walletAddress, test.wantPersisted)
			if test.wantPersisted {
				var data struct {
					WalletAddress string          `json:"walletAddress"`
					ImageURL      *string         `json:"imageUrl"`
					Role          domain.UserRole `json:"role"`
				}
				if err := json.Unmarshal(decoded.Data, &data); err != nil {
					t.Fatalf("decode response data: %v", err)
				}
				if data.WalletAddress != test.walletAddress || data.Role != domain.UserRoleFundraiser {
					t.Errorf("response identity = %q/%q", data.WalletAddress, data.Role)
				}
				wantImageURL := "https://cdn.example.com/pawfund/profiles/rescue%20photo.png"
				if data.ImageURL == nil || *data.ImageURL != wantImageURL {
					t.Errorf("response image URL = %#v, want %q", data.ImageURL, wantImageURL)
				}
			}
		})
	}
}

func assertFundraiserEndpointPersistence(t *testing.T, email, wallet string, wantPersisted bool) {
	t.Helper()
	if email == "" || wallet == "" {
		return
	}

	var count int
	if err := testDatabase.QueryRowContext(
		t.Context(),
		`SELECT count(*)
		 FROM users u
		 JOIN fundraisers f ON f.id = u.id
		 WHERE u.email = $1 AND u.wallet_address = $2`,
		email,
		wallet,
	).Scan(&count); err != nil {
		t.Fatalf("query endpoint persistence: %v", err)
	}
	if got := count == 1; got != wantPersisted {
		t.Errorf("fundraiser persisted = %v, want %v", got, wantPersisted)
	}
}
