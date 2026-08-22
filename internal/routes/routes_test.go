package routes

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/app"
)

func TestCORSPreflight(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authenticateCalled := false
	router := Setup(&app.Application{
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		Authenticate: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authenticateCalled = true
				next.ServeHTTP(w, r)
			})
		},
	}, logger)

	request := httptest.NewRequest(http.MethodOptions, "/v1/auth/me", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if authenticateCalled {
		t.Error("preflight request invoked authentication middleware")
	}
	if response.Code != http.StatusNoContent {
		t.Errorf("HTTP status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:3000")
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want Authorization", got)
	}
}

func TestCORSAllowsConfiguredOriginOnHealth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := Setup(&app.Application{
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		Authenticate: func(next http.Handler) http.Handler {
			return next
		},
	}, logger)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:3000")
	}
}
