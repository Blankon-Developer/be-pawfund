package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	const allowedOrigin = "http://localhost:3000"
	const otherOrigin = "https://evil.example"

	tests := []struct {
		name            string
		method          string
		origin          string
		wantHTTP        int
		wantAllowOrigin string
		wantNextCalled  bool
	}{
		{
			name:            "allows a configured origin on GET",
			method:          http.MethodGet,
			origin:          allowedOrigin,
			wantHTTP:        http.StatusNoContent,
			wantAllowOrigin: allowedOrigin,
			wantNextCalled:  true,
		},
		{
			name:            "handles preflight for a configured origin",
			method:          http.MethodOptions,
			origin:          allowedOrigin,
			wantHTTP:        http.StatusNoContent,
			wantAllowOrigin: allowedOrigin,
		},
		{
			name:           "omits allow-origin for an unknown origin",
			method:         http.MethodGet,
			origin:         otherOrigin,
			wantHTTP:       http.StatusNoContent,
			wantNextCalled: true,
		},
		{
			name:     "omits allow-origin on preflight from an unknown origin",
			method:   http.MethodOptions,
			origin:   otherOrigin,
			wantHTTP: http.StatusNoContent,
		},
		{
			name:           "passes through requests without an Origin header",
			method:         http.MethodGet,
			wantHTTP:       http.StatusNoContent,
			wantNextCalled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusNoContent)
			})
			handler := CORS([]string{allowedOrigin, "https://app.example.com"})(next)

			request := httptest.NewRequest(test.method, "/v1/auth/message", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.method == http.MethodOptions {
				request.Header.Set("Access-Control-Request-Method", http.MethodPost)
				request.Header.Set("Access-Control-Request-Headers", "authorization,content-type,x-request-id")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantHTTP {
				t.Errorf("HTTP status = %d, want %d", response.Code, test.wantHTTP)
			}
			if nextCalled != test.wantNextCalled {
				t.Errorf("next called = %v, want %v", nextCalled, test.wantNextCalled)
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != test.wantAllowOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, test.wantAllowOrigin)
			}
			if got := response.Header().Get("Vary"); got != "Origin" {
				t.Errorf("Vary = %q, want %q", got, "Origin")
			}
			if test.wantAllowOrigin == "" {
				return
			}
			if got := response.Header().Get("Access-Control-Allow-Methods"); got != corsAllowMethods {
				t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, corsAllowMethods)
			}
			if got := response.Header().Get("Access-Control-Allow-Headers"); got != corsAllowHeaders {
				t.Errorf("Access-Control-Allow-Headers = %q, want %q", got, corsAllowHeaders)
			}
			if got := response.Header().Get("Access-Control-Allow-Credentials"); got != corsAllowCredentials {
				t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, corsAllowCredentials)
			}
		})
	}
}
