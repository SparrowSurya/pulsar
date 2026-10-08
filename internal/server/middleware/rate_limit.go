package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"pulsar/internal/config"
)

// clientLimiter holds rate limiting states for a single client IP.
type clientLimiter struct {
	tokens   float64
	lastSeen time.Time
	mu       sync.Mutex
}

var (
	clients sync.Map
	once    sync.Once
)

// ResetRateLimiter clears all active client limiters (useful for testing).
func ResetRateLimiter() {
	clients.Range(func(key, value interface{}) bool {
		clients.Delete(key)
		return true
	})
}

// RateLimit implements an IP-based token-bucket rate limiter.
func RateLimit(next http.Handler) http.Handler {
	// Clean up stale rate limiter entries every 5 minutes
	once.Do(func() {
		go func() {
			for {
				time.Sleep(5 * time.Minute)
				now := time.Now()
				clients.Range(func(key, value interface{}) bool {
					limiter := value.(*clientLimiter)
					limiter.mu.Lock()
					// If a client hasn't been seen in 10 minutes, remove it from the map
					if now.Sub(limiter.lastSeen) > 10*time.Minute {
						clients.Delete(key)
					}
					limiter.mu.Unlock()
					return true
				})
			}
		}()
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := config.LoadEnv()
		rps := cfg.RateLimitRps
		burst := float64(cfg.RateLimitBurst)

		// Extract IP address from RemoteAddr
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		// Load or store rate limiter instance for IP
		val, _ := clients.LoadOrStore(ip, &clientLimiter{
			tokens:   burst,
			lastSeen: time.Now(),
		})
		limiter := val.(*clientLimiter)

		limiter.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(limiter.lastSeen).Seconds()
		limiter.lastSeen = now

		// Recharge tokens
		limiter.tokens += elapsed * rps
		if limiter.tokens > burst {
			limiter.tokens = burst
		}

		// Check if request is allowed
		if limiter.tokens >= 1.0 {
			limiter.tokens -= 1.0
			limiter.mu.Unlock()
			next.ServeHTTP(w, r)
		} else {
			limiter.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Too Many Requests",
				"message": "API rate limit exceeded. Please slow down.",
			})
		}
	})
}

