package transport

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// NewUpgrader creates a configured websocket.Upgrader with environment-based origin validation.
func NewUpgrader(allowedOriginsRaw, envName string) websocket.Upgrader {
	origins := strings.Split(allowedOriginsRaw, ",")
	allowedOrigins := make(map[string]bool)
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if len(trimmed) > 0 {
			allowedOrigins[trimmed] = true
		}
	}

	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			if envName == "development" {
				return true
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			if allowedOrigins["*"] || allowedOrigins[origin] {
				return true
			}
			// Fallback to same-origin check
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return strings.EqualFold(u.Host, r.Host)
		},
	}
}
