package middleware

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
)

type stubTokenVerifier struct {
	principal auth.Principal
	err       error
	called    int
	token     string
}

func (s *stubTokenVerifier) Verify(token string) (auth.Principal, error) {
	s.called++
	s.token = token
	return s.principal, s.err
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name             string
		authorization    string
		principal        auth.Principal
		verifyError      error
		wantHTTP         int
		wantCode         string
		wantVerifierCall bool
	}{
		{
			name:     "requires authorization header",
			wantHTTP: http.StatusUnauthorized,
			wantCode: "ACCESS_TOKEN_REQUIRED",
		},
		{
			name:          "rejects malformed scheme",
			authorization: "Basic credential",
			wantHTTP:      http.StatusUnauthorized,
			wantCode:      "INVALID_ACCESS_TOKEN",
		},
		{
			name:             "rejects verifier error",
			authorization:    "Bearer rejected",
			verifyError:      errors.New("rejected"),
			wantHTTP:         http.StatusUnauthorized,
			wantCode:         "INVALID_ACCESS_TOKEN",
			wantVerifierCall: true,
		},
		{
			name:             "adds principal to request context",
			authorization:    "bearer valid-token",
			principal:        auth.Principal{WalletAddress: "0xmiddleware"},
			wantHTTP:         http.StatusNoContent,
			wantVerifierCall: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &stubTokenVerifier{principal: test.principal, err: test.verifyError}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				principal, ok := auth.PrincipalFromContext(r.Context())
				if !ok || principal != test.principal {
					t.Errorf("request principal = %#v, %v; want %#v, true", principal, ok, test.principal)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			handler := Authenticate(verifier, logger)(next)

			request := httptest.NewRequest(http.MethodPost, "/", nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantHTTP {
				t.Errorf("HTTP status = %d, want %d", response.Code, test.wantHTTP)
			}
			if (verifier.called > 0) != test.wantVerifierCall {
				t.Errorf("verifier called = %d, want call = %v", verifier.called, test.wantVerifierCall)
			}
			if test.wantCode != "" && !responseContainsCode(response.Body.String(), test.wantCode) {
				t.Errorf("response body = %s, want code %q", response.Body.String(), test.wantCode)
			}
			if test.wantVerifierCall && test.verifyError == nil && verifier.token != "valid-token" {
				t.Errorf("verified token = %q", verifier.token)
			}
		})
	}
}

func responseContainsCode(body, code string) bool {
	return body != "" && stringContains(body, `"code":"`+code+`"`)
}

func stringContains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
