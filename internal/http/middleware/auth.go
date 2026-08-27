package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/Blankon-Developer/be-pawfund/internal/auth"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
)

type TokenVerifier interface {
	Verify(token string) (auth.Principal, error)
}

func Authenticate(verifier TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Authorization")
			w.Header().Set("Cache-Control", "no-store")

			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			if authorization == "" {
				writeAuthenticationError(
					w,
					logger,
					"ACCESS_TOKEN_REQUIRED",
					"A Bearer access token is required.",
				)
				return
			}

			parts := strings.Fields(authorization)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAuthenticationError(
					w,
					logger,
					"INVALID_ACCESS_TOKEN",
					"The access token is invalid or expired.",
				)
				return
			}

			principal, err := verifier.Verify(parts[1])
			if err != nil {
				logger.Warn("access token verification failed", "error", err)
				writeAuthenticationError(
					w,
					logger,
					"INVALID_ACCESS_TOKEN",
					"The access token is invalid or expired.",
				)
				return
			}

			ctx := auth.ContextWithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthenticationError(w http.ResponseWriter, logger *slog.Logger, code, message string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	if err := httpx.WriteError(w, http.StatusUnauthorized, code, message, nil); err != nil {
		logger.Error("write authentication response", "error", err)
	}
}
