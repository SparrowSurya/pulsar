package server

import (
	"net/http"
	"time"

	"pulsar/internal/config"
	"pulsar/internal/logger"
	"pulsar/internal/server/domain"
	"pulsar/internal/server/transport"

	"github.com/gorilla/websocket"
)

// Server coordinates the HTTP routes, upgrading connections to websockets, and starting the Hub loop.
type Server struct {
	// upgrader is used to upgrade incoming HTTP connections into websocket connections.
	upgrader websocket.Upgrader

	// hub references the client coordinator.
	hub *domain.Hub

	// HandshakeTimeout defines the maximum duration to wait for the initial 'hello' message.
	HandshakeTimeout time.Duration
}

// Setup registers the HTTP request handlers and initializes the WebSocket upgrader and the hub loop.
func (s *Server) Setup() {
	cfg := config.LoadEnv()
	if s.HandshakeTimeout == 0 {
		if cfg.InitWaitSecs > 0 {
			s.HandshakeTimeout = time.Duration(cfg.InitWaitSecs) * time.Second
		} else {
			s.HandshakeTimeout = 2 * time.Minute
		}
	}
	s.hub = domain.NewHub()
	go s.hub.Run()

	s.upgrader = transport.NewUpgrader(cfg.AllowedOrigins, cfg.EnvName)
}

// Hub returns the server's central domain hub instance.
func (s *Server) Hub() *domain.Hub {
	return s.hub
}

// Run starts the HTTP server listening on the specified address with the custom router.
// It blocks until the server stops or crashes.
func (s *Server) Run(address string) {
	var scheme string
	if config.IsProdEnv() {
		scheme = "https"
	} else {
		scheme = "http"
	}

	router := s.SetupRouter()

	logger.Infof("Starting server on: %s://%s", scheme, address)
	err := http.ListenAndServe(address, router)
	if err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Server stopped due to error: %v", err)
	} else {
		logger.Infoln("Server stopped gracefully")
	}
}
