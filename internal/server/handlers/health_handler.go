package handlers

import (
	"net/http"
)

// HealthHandler handles GET /api/health requests.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"Pulsar Game Server is operational"}`))
}
