//go:build integration

package integration_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/app"
	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/config"
	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/repository"
	"github.com/Blankon-Developer/be-pawfund/internal/routes"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spruceid/siwe-go"
)

const integrationCacheKeyPrefix = "pawfund-integration"

func TestAuthEndpoints(t *testing.T) {
	imageKey := "profiles/cat photo.png"

	tests := []struct {
		name           string
		prepare        func(t *testing.T, walletAddress string)
		messageTTL     time.Duration
		wrongSigner    bool
		removeMessage  bool
		preverify      bool
		wantHTTP       int
		wantCode       string
		wantRegistered bool
		wantName       *string
		wantRole       *domain.UserRole
		wantImageURL   *string
	}{
		{
			name:           "verifies unregistered wallet",
			wantHTTP:       http.StatusOK,
			wantCode:       "AUTH_VERIFIED",
			wantRegistered: false,
		},
		{
			name: "returns supporter profile",
			prepare: func(t *testing.T, walletAddress string) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				supporter := newSupporter("cat@example.com", walletAddress, &imageKey)
				supporter.Name = "Cat Lover"
				mustCreateSupporter(t, repo, supporter)
			},
			wantHTTP:       http.StatusOK,
			wantCode:       "AUTH_VERIFIED",
			wantRegistered: true,
			wantName:       integrationStringPointer("Cat Lover"),
			wantRole:       integrationRolePointer(domain.UserRoleSupporter),
			wantImageURL:   integrationStringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
		},
		{
			name: "returns fundraiser profile without image",
			prepare: func(t *testing.T, walletAddress string) {
				mustCreateFundraiserProfile(t, walletAddress, nil)
			},
			wantHTTP:       http.StatusOK,
			wantCode:       "AUTH_VERIFIED",
			wantRegistered: true,
			wantName:       integrationStringPointer("Paw Rescue"),
			wantRole:       integrationRolePointer(domain.UserRoleFundraiser),
		},
		{
			name:        "rejects wrong signer",
			wrongSigner: true,
			wantHTTP:    http.StatusUnauthorized,
			wantCode:    "SIWE_VERIFICATION_FAILED",
		},
		{
			name:          "rejects missing message",
			removeMessage: true,
			wantHTTP:      http.StatusUnauthorized,
			wantCode:      "SIWE_VERIFICATION_FAILED",
		},
		{
			name:      "rejects replay",
			preverify: true,
			wantHTTP:  http.StatusUnauthorized,
			wantCode:  "SIWE_VERIFICATION_FAILED",
		},
		{
			name:       "rejects expired message",
			messageTTL: time.Nanosecond,
			wantHTTP:   http.StatusUnauthorized,
			wantCode:   "SIWE_VERIFICATION_FAILED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanTestState(t)
			t.Cleanup(func() { cleanTestState(t) })

			privateKey := integrationPrivateKey(t)
			walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
			if test.prepare != nil {
				test.prepare(t, walletAddress)
			}

			messageTTL := test.messageTTL
			if messageTTL == 0 {
				messageTTL = 5 * time.Minute
			}
			router, jwtManager := newAuthIntegrationRouter(t, messageTTL)
			messageStr := requestAuthMessage(t, router, strings.ToLower(walletAddress))

			message, err := siwe.ParseMessage(messageStr)
			if err != nil {
				t.Fatalf("parse endpoint message: %v", err)
			}
			if messageTTL > time.Millisecond {
				prefixedKey := integrationCacheKeyPrefix + ":siwe:message:" + message.GetNonce()
				if exists := testCache.Exists(t.Context(), prefixedKey).Val(); exists != 1 {
					t.Errorf("prefixed cache key exists = %d", exists)
				}
				if exists := testCache.Exists(t.Context(), "siwe:message:"+message.GetNonce()).Val(); exists != 0 {
					t.Errorf("unprefixed cache key exists = %d", exists)
				}
			}

			if test.removeMessage {
				cleanCache(t)
			}
			signingKey := privateKey
			if test.wrongSigner {
				signingKey = integrationPrivateKey(t)
			}
			signature := signIntegrationMessage(t, messageStr, signingKey)
			if test.preverify {
				first := requestAuthVerify(t, router, messageStr, signature)
				if first.HTTPStatus != http.StatusOK || first.Code != "AUTH_VERIFIED" {
					t.Fatalf("first verify = %d/%q; body: %s", first.HTTPStatus, first.Code, first.Body)
				}
			}

			verified := requestAuthVerify(t, router, messageStr, signature)
			if verified.HTTPStatus != test.wantHTTP || verified.Code != test.wantCode {
				t.Fatalf("verify = %d/%q, want %d/%q; body: %s", verified.HTTPStatus, verified.Code, test.wantHTTP, test.wantCode, verified.Body)
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			var data struct {
				AccessToken     string           `json:"accessToken"`
				IsNotRegistered bool             `json:"isNotRegistered"`
				Address         string           `json:"address"`
				Name            *string          `json:"name"`
				Role            *domain.UserRole `json:"role"`
				ImageURL        *string          `json:"imageUrl"`
			}
			if err := json.Unmarshal(verified.Data, &data); err != nil {
				t.Fatalf("decode verify data: %v", err)
			}
			if data.IsNotRegistered == test.wantRegistered || data.Address != walletAddress {
				t.Errorf("auth identity = registered:%v address:%q", !data.IsNotRegistered, data.Address)
			}
			if !equalStringPointers(data.Name, test.wantName) || !equalRoles(data.Role, test.wantRole) || !equalStringPointers(data.ImageURL, test.wantImageURL) {
				t.Errorf("profile = name:%v role:%v image:%v", data.Name, data.Role, data.ImageURL)
			}
			principal, err := jwtManager.Verify(data.AccessToken)
			if err != nil {
				t.Fatalf("verify returned access token: %v", err)
			}
			if principal.WalletAddress != walletAddress {
				t.Errorf("token wallet = %q, want %q", principal.WalletAddress, walletAddress)
			}
			var wantTokenRole domain.UserRole
			if test.wantRole != nil {
				wantTokenRole = *test.wantRole
			}
			if principal.Role != wantTokenRole {
				t.Errorf("token role = %q, want %q", principal.Role, wantTokenRole)
			}
		})
	}
}

func TestGetMeEndpoint(t *testing.T) {
	imageKey := "profiles/cat photo.png"
	tests := []struct {
		name          string
		prepare       func(t *testing.T, walletAddress string)
		authorization string
		wantHTTP      int
		wantCode      string
		wantName      string
		wantRole      domain.UserRole
		wantImageURL  *string
	}{
		{
			name: "returns supporter profile",
			prepare: func(t *testing.T, walletAddress string) {
				repo := repository.NewPostgresSupporterRepository(testDatabase)
				supporter := newSupporter("cat@example.com", walletAddress, &imageKey)
				supporter.Name = "Cat Lover"
				mustCreateSupporter(t, repo, supporter)
			},
			authorization: "valid",
			wantHTTP:      http.StatusOK,
			wantCode:      "PROFILE_RETRIEVED",
			wantName:      "Cat Lover",
			wantRole:      domain.UserRoleSupporter,
			wantImageURL:  integrationStringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo.png"),
		},
		{
			name: "returns fundraiser profile without image",
			prepare: func(t *testing.T, walletAddress string) {
				mustCreateFundraiserProfile(t, walletAddress, nil)
			},
			authorization: "valid",
			wantHTTP:      http.StatusOK,
			wantCode:      "PROFILE_RETRIEVED",
			wantName:      "Paw Rescue",
			wantRole:      domain.UserRoleFundraiser,
		},
		{
			name:          "returns not found for unregistered wallet",
			authorization: "valid",
			wantHTTP:      http.StatusNotFound,
			wantCode:      "PROFILE_NOT_FOUND",
		},
		{
			name:     "requires access token",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "ACCESS_TOKEN_REQUIRED",
		},
		{
			name:          "rejects invalid access token",
			authorization: "invalid",
			wantHTTP:      http.StatusUnauthorized,
			wantCode:      "INVALID_ACCESS_TOKEN",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanTestState(t)
			t.Cleanup(func() { cleanTestState(t) })

			privateKey := integrationPrivateKey(t)
			walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
			if test.prepare != nil {
				test.prepare(t, walletAddress)
			}

			router, jwtManager := newAuthIntegrationRouter(t, 5*time.Minute)
			request := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
			switch test.authorization {
			case "valid":
				token, err := jwtManager.Generate(walletAddress, "", time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				request.Header.Set("Authorization", "Bearer "+token)
			case "invalid":
				request.Header.Set("Authorization", "Bearer invalid-token")
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			result := decodeAuthEndpointResult(t, response)
			if result.HTTPStatus != test.wantHTTP || result.Code != test.wantCode {
				t.Fatalf(
					"get me = %d/%q, want %d/%q; body: %s",
					result.HTTPStatus,
					result.Code,
					test.wantHTTP,
					test.wantCode,
					result.Body,
				)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP != http.StatusOK {
				return
			}

			var data struct {
				Address  string          `json:"address"`
				Name     string          `json:"name"`
				Role     domain.UserRole `json:"role"`
				ImageURL *string         `json:"imageUrl"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode get me data: %v", err)
			}
			if data.Address != walletAddress || data.Name != test.wantName || data.Role != test.wantRole {
				t.Errorf("profile identity = %#v", data)
			}
			if !equalStringPointers(data.ImageURL, test.wantImageURL) {
				t.Errorf("image URL = %v, want %v", data.ImageURL, test.wantImageURL)
			}
		})
	}
}

type authEndpointResult struct {
	HTTPStatus int
	Code       string
	Data       json.RawMessage
	Body       string
}

func newAuthIntegrationRouter(t *testing.T, messageTTL time.Duration) (http.Handler, *auth.JWTManager) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	secret := []byte(strings.Repeat("a", 32))
	application, err := app.New(t.Context(), config.Config{
		HTTPAddr:             ":0",
		DatabaseURL:          testDatabaseURL,
		JWTSecret:            secret,
		StoragePublicBaseURL: "https://cdn.example.com/pawfund",
		StorageEndpoint:      testStorageEndpoint,
		StorageAccessKey:     testStorageAccessKey,
		StorageSecretKey:     testStorageSecretKey,
		StorageBucket:        testStorageBucket,
		StorageRegion:        testStorageRegion,
		StoragePresignTTL:    15 * time.Minute,
		CacheURL:             testCacheURL,
		CacheKeyPrefix:       integrationCacheKeyPrefix,
		SIWEDomain:           "app.example.com",
		SIWEURI:              "https://app.example.com/login",
		SIWEChainID:          84532,
		SIWEMessageTTL:       messageTTL,
		JWTTTL:               time.Hour,
	}, logger)
	if err != nil {
		t.Fatalf("create integration application: %v", err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil {
			t.Errorf("close integration application: %v", err)
		}
	})

	jwtManager, err := auth.NewJWTManager(secret)
	if err != nil {
		t.Fatalf("create JWT verifier: %v", err)
	}
	return routes.Setup(application, logger), jwtManager
}

func requestAuthMessage(t *testing.T, router http.Handler, walletAddress string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"address": walletAddress})
	if err != nil {
		t.Fatalf("encode message request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/message", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	result := decodeAuthEndpointResult(t, response)
	if result.HTTPStatus != http.StatusOK || result.Code != "AUTH_MESSAGE_CREATED" {
		t.Fatalf("message = %d/%q; body: %s", result.HTTPStatus, result.Code, result.Body)
	}
	var data struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode message data: %v", err)
	}
	return data.Message
}

func requestAuthVerify(t *testing.T, router http.Handler, message, signature string) authEndpointResult {
	t.Helper()
	body, err := json.Marshal(map[string]string{"message": message, "signature": signature})
	if err != nil {
		t.Fatalf("encode verify request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return decodeAuthEndpointResult(t, response)
}

func decodeAuthEndpointResult(t *testing.T, response *httptest.ResponseRecorder) authEndpointResult {
	t.Helper()
	var decoded struct {
		Code   string            `json:"code"`
		Data   json.RawMessage   `json:"data"`
		Errors httpx.FieldErrors `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode auth endpoint response: %v; body: %s", err, response.Body.String())
	}
	return authEndpointResult{
		HTTPStatus: response.Code,
		Code:       decoded.Code,
		Data:       decoded.Data,
		Body:       response.Body.String(),
	}
}

func integrationPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return privateKey
}

func signIntegrationMessage(t *testing.T, message string, privateKey *ecdsa.PrivateKey) string {
	t.Helper()
	payload := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256Hash([]byte(payload))
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	signature[64] += 27
	return "0x" + hex.EncodeToString(signature)
}

func integrationStringPointer(value string) *string {
	return &value
}

func integrationRolePointer(value domain.UserRole) *domain.UserRole {
	return &value
}

func equalRoles(left, right *domain.UserRole) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
