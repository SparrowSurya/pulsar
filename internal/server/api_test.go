package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"pulsar/internal/config"
	_ "pulsar/internal/plugins/tictactoe"
	"pulsar/internal/server/handlers"
	"pulsar/internal/server/middleware"
)

func init() {
	os.Setenv("ENV", "development")
	os.Setenv("HOSTNAMEKEY", "HOSTNAME")
	os.Setenv("HOSTNAME", "127.0.0.1")
}

func TestAPIHealthAndSecurity(t *testing.T) {
	s := &Server{}
	s.Setup()
	router := s.SetupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify Status Code
	if w.Code != http.StatusOK {
		t.Errorf("Expected health status 200, got %d", w.Code)
	}

	// Verify JSON body
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %q", body["status"])
	}

	// Verify Security Headers
	headers := w.Result().Header
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("Expected X-Frame-Options: DENY, got %q", headers.Get("X-Frame-Options"))
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options: nosniff, got %q", headers.Get("X-Content-Type-Options"))
	}
	if !strings.Contains(headers.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("Expected CSP to contain frame-ancestors 'none', got %q", headers.Get("Content-Security-Policy"))
	}
}

func TestAPIFeaturesListing(t *testing.T) {
	s := &Server{}
	s.Setup()
	router := s.SetupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/games", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var features []handlers.GameDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&features); err != nil {
		t.Fatalf("Failed to decode features: %v", err)
	}
}

func TestAPICORSHeaders(t *testing.T) {
	// Configure test environment allowed origins
	os.Setenv("ALLOWED_ORIGINS", "http://test-frontend.example.com")
	os.Setenv("ENV", "production")
	config.ResetEnv()
	defer func() {
		os.Unsetenv("ALLOWED_ORIGINS")
		os.Unsetenv("ENV")
		config.ResetEnv()
	}()

	s := &Server{}
	s.Setup()
	router := s.SetupRouter()

	// Allowed Origin Request
	reqAllowed := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	reqAllowed.Header.Set("Origin", "http://test-frontend.example.com")
	wAllowed := httptest.NewRecorder()
	router.ServeHTTP(wAllowed, reqAllowed)

	if wAllowed.Result().Header.Get("Access-Control-Allow-Origin") != "http://test-frontend.example.com" {
		t.Errorf("Expected CORS Access-Control-Allow-Origin header, got %q", wAllowed.Result().Header.Get("Access-Control-Allow-Origin"))
	}

	// Disallowed Origin Request
	reqDisallowed := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	reqDisallowed.Header.Set("Origin", "http://malicious-origin.com")
	wDisallowed := httptest.NewRecorder()
	router.ServeHTTP(wDisallowed, reqDisallowed)

	if wDisallowed.Result().Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Expected no CORS Access-Control-Allow-Origin header for unauthorized origins, got %q", wDisallowed.Result().Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestAPIRateLimiter(t *testing.T) {
	// Force rate limiting env settings to make limiter very sensitive
	os.Setenv("RATE_LIMIT_RPS", "1.0")
	os.Setenv("RATE_LIMIT_BURST", "1")
	config.ResetEnv()
	defer func() {
		os.Unsetenv("RATE_LIMIT_RPS")
		os.Unsetenv("RATE_LIMIT_BURST")
		config.ResetEnv()
		middleware.ResetRateLimiter()
	}()

	s := &Server{}
	s.Setup()
	router := s.SetupRouter()

	// Client IP address configuration
	clientIP := "203.0.113.195:45903"

	// First request - should be allowed (consuming the single token burst allowance)
	req1 := httptest.NewRequest(http.MethodGet, "/api/games", nil)
	req1.RemoteAddr = clientIP
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Expected first request to pass, got status %d", w1.Code)
	}

	// Second immediate request - should be rate limited (returning 429)
	req2 := httptest.NewRequest(http.MethodGet, "/api/games", nil)
	req2.RemoteAddr = clientIP
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Expected second immediate request to trigger rate limiter 429, got %d", w2.Code)
	}
}
