//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/infra/database"
)

func TestGetMyDonationsEndpoint(t *testing.T) {
	const supporterWallet = "0xSupporterChecksum"

	tests := []struct {
		name          string
		prepare       func(t *testing.T)
		walletAddress string
		role          domain.UserRole
		query         string
		authorization string
		wantHTTP      int
		wantCode      string
		wantEmpty     bool
		wantTxHash    string
	}{
		{
			name: "returns the requested donation page newest first",
			prepare: func(t *testing.T) {
				supporterRepo := database.NewPostgresSupporterRepository(testDatabase)
				supporter := newSupporter("supporter@example.com", supporterWallet, nil)
				mustCreateSupporter(t, supporterRepo, supporter)
				fundraiserRepo := database.NewPostgresFundraiserRepository(testDatabase)
				fundraiser := newFundraiser("fundraiser@example.com", "0xFundraiser", nil)
				mustCreateFundraiser(t, fundraiserRepo, fundraiser)
				campaignID, _ := mustCreateDonationCampaign(t, fundraiser.ID, "Emergency Rescue")
				createdAt := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)
				mustCreateDonation(t, campaignID, supporter.ID, 1_000_000, "0xOlder", createdAt, 10, 0)
				mustCreateDonation(t, campaignID, supporter.ID, 2_000_000, "0xNewer", createdAt.Add(time.Hour), 11, 0)
			},
			walletAddress: strings.ToLower(supporterWallet),
			role:          domain.UserRoleSupporter,
			query:         "?page=2&pageSize=1",
			wantHTTP:      http.StatusOK,
			wantCode:      "DONATIONS_RETRIEVED",
			wantTxHash:    "0xOlder",
		},
		{
			name: "returns an empty array for an active supporter without donations",
			prepare: func(t *testing.T) {
				repo := database.NewPostgresSupporterRepository(testDatabase)
				mustCreateSupporter(t, repo, newSupporter("supporter@example.com", supporterWallet, nil))
			},
			walletAddress: supporterWallet,
			role:          domain.UserRoleSupporter,
			wantHTTP:      http.StatusOK,
			wantCode:      "DONATIONS_RETRIEVED",
			wantEmpty:     true,
		},
		{
			name:          "requires an access token",
			authorization: "missing",
			wantHTTP:      http.StatusUnauthorized,
			wantCode:      "ACCESS_TOKEN_REQUIRED",
		},
		{
			name:          "rejects a non-supporter role",
			walletAddress: "0xFundraiser",
			role:          domain.UserRoleFundraiser,
			wantHTTP:      http.StatusForbidden,
			wantCode:      "SUPPORTER_ACCESS_REQUIRED",
		},
		{
			name:          "returns not found without an active supporter profile",
			walletAddress: "0xUnknownSupporter",
			role:          domain.UserRoleSupporter,
			wantHTTP:      http.StatusNotFound,
			wantCode:      "PROFILE_NOT_FOUND",
		},
		{
			name:          "validates pagination",
			walletAddress: supporterWallet,
			role:          domain.UserRoleSupporter,
			query:         "?page=0&pageSize=101",
			wantHTTP:      http.StatusUnprocessableEntity,
			wantCode:      "VALIDATION_ERROR",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanDatabase(t)
			t.Cleanup(func() { cleanDatabase(t) })
			if test.prepare != nil {
				test.prepare(t)
			}

			router, jwtManager := newSupporterProfileIntegrationRouter(t)
			request := httptest.NewRequest(http.MethodGet, "/v1/supporter/donations"+test.query, nil)
			if test.authorization != "missing" {
				token, err := jwtManager.Generate(test.walletAddress, test.role, time.Hour)
				if err != nil {
					t.Fatalf("generate access token: %v", err)
				}
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			result := decodeAuthEndpointResult(t, response)
			if result.HTTPStatus != test.wantHTTP || result.Code != test.wantCode {
				t.Fatalf("get donations = %d/%q, want %d/%q; body: %s", result.HTTPStatus, result.Code, test.wantHTTP, test.wantCode, result.Body)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if test.wantHTTP != http.StatusOK {
				return
			}
			if test.wantEmpty {
				if string(result.Data) != "[]" {
					t.Errorf("empty data = %s, want []", result.Data)
				}
				return
			}

			var data []struct {
				Amount   int64 `json:"amount"`
				Campaign struct {
					Title           string `json:"title"`
					ContractAddress string `json:"contractAddress"`
				} `json:"campaign"`
				DonatedOn string `json:"donatedOn"`
				TxHash    string `json:"txHash"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatalf("decode donation response: %v", err)
			}
			if len(data) != 1 || data[0].TxHash != test.wantTxHash || data[0].Amount != 1_000_000 || data[0].Campaign.Title != "Emergency Rescue" || data[0].Campaign.ContractAddress == "" || data[0].DonatedOn != "2026-08-11T08:00:00Z" {
				t.Errorf("donation data = %#v", data)
			}
		})
	}
}
