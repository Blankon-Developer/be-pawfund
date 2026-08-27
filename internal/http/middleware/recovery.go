package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error(
						"panic while serving request",
						"panic", recovered,
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					if err := httpx.WriteError(
						w,
						http.StatusInternalServerError,
						"INTERNAL_SERVER_ERROR",
						"An internal server error occurred.",
						nil,
					); err != nil {
						logger.Error("write panic response", "error", err)
					}
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
