package middleware

import (
	"net/http"

	"pulsar/internal/config"
)

// Security applies request size limits and safety headers.
func Security(next http.Handler) http.Handler {
	cfg := config.LoadEnv()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Limit request body size
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxRequestSizeBytes)
		}

		// Inject standard security protection headers
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none';")

		next.ServeHTTP(w, r)
	})
}
