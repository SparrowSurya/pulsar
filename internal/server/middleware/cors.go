package middleware

import (
	"net/http"
	"strings"

	"pulsar/internal/config"
)

// CORS handles Cross-Origin Resource Sharing based on configuration.
func CORS(next http.Handler) http.Handler {
	cfg := config.LoadEnv()
	// Parse allowed origins
	origins := strings.Split(cfg.AllowedOrigins, ",")
	allowedOrigins := make(map[string]bool)
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if len(trimmed) > 0 {
			allowedOrigins[trimmed] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Detect local loopback and private LAN network ranges
		isLocal := strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") ||
			strings.HasPrefix(origin, "http://192.168.") ||
			strings.HasPrefix(origin, "http://10.") ||
			strings.HasPrefix(origin, "http://172.16.") ||
			strings.HasPrefix(origin, "http://172.17.") ||
			strings.HasPrefix(origin, "http://172.18.") ||
			strings.HasPrefix(origin, "http://172.19.") ||
			strings.HasPrefix(origin, "http://172.20.") ||
			strings.HasPrefix(origin, "http://172.21.") ||
			strings.HasPrefix(origin, "http://172.22.") ||
			strings.HasPrefix(origin, "http://172.23.") ||
			strings.HasPrefix(origin, "http://172.24.") ||
			strings.HasPrefix(origin, "http://172.25.") ||
			strings.HasPrefix(origin, "http://172.26.") ||
			strings.HasPrefix(origin, "http://172.27.") ||
			strings.HasPrefix(origin, "http://172.28.") ||
			strings.HasPrefix(origin, "http://172.29.") ||
			strings.HasPrefix(origin, "http://172.30.") ||
			strings.HasPrefix(origin, "http://172.31.") ||
			strings.HasPrefix(origin, "https://localhost") ||
			strings.HasPrefix(origin, "https://127.0.0.1") ||
			strings.HasPrefix(origin, "https://192.168.") ||
			strings.HasPrefix(origin, "https://10.") ||
			strings.HasPrefix(origin, "https://172.16.") ||
			strings.HasPrefix(origin, "https://172.17.") ||
			strings.HasPrefix(origin, "https://172.18.") ||
			strings.HasPrefix(origin, "https://172.19.") ||
			strings.HasPrefix(origin, "https://172.20.") ||
			strings.HasPrefix(origin, "https://172.21.") ||
			strings.HasPrefix(origin, "https://172.22.") ||
			strings.HasPrefix(origin, "https://172.23.") ||
			strings.HasPrefix(origin, "https://172.24.") ||
			strings.HasPrefix(origin, "https://172.25.") ||
			strings.HasPrefix(origin, "https://172.26.") ||
			strings.HasPrefix(origin, "https://172.27.") ||
			strings.HasPrefix(origin, "https://172.28.") ||
			strings.HasPrefix(origin, "https://172.29.") ||
			strings.HasPrefix(origin, "https://172.30.") ||
			strings.HasPrefix(origin, "https://172.31.")

		// If origin is allowed, write CORS headers
		if allowedOrigins["*"] || allowedOrigins[origin] || config.IsDevEnv() || isLocal {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
