package middleware

import (
	"net/http"
	"strings"
)

const (
	corsAllowMethods     = "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
	corsAllowHeaders     = "Accept, Authorization, Content-Type, X-Request-Id"
	corsAllowCredentials = "true"
	corsMaxAge           = "300"
)

// CORS returns a middleware that handles Cross-Origin Resource Sharing (CORS)
// for configured origins. For preflight OPTIONS requests from allowed origins,
// it responds with appropriate headers and HTTP 204 No Content.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Allow-Credentials", corsAllowCredentials)
				w.Header().Set("Access-Control-Max-Age", corsMaxAge)
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
