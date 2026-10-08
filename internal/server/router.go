package server

import (
	"net/http"

	"pulsar/internal/server/handlers"
	"pulsar/internal/server/middleware"
)

// Middleware type wraps a standard http.Handler.
type Middleware func(http.Handler) http.Handler

// Compose chains multiple middlewares sequentially onto an endpoint handler.
func Compose(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// SetupRouter binds all HTTP handlers and registers security middlewares.
func (s *Server) SetupRouter() http.Handler {
	mux := http.NewServeMux()

	public := []Middleware{middleware.RequestLogger, middleware.CORS, middleware.RateLimit}
	health := []Middleware{middleware.RequestLogger, middleware.CORS, middleware.Security}
	protected := []Middleware{middleware.RequestLogger, middleware.CORS, middleware.Security, middleware.RateLimit}

	wsHandler := handlers.NewWSHandler(s.hub, s.upgrader, s.HandshakeTimeout)
	roomHandler := handlers.NewRoomHandler(s.hub)

	mux.Handle("/ws", Compose(http.HandlerFunc(wsHandler.HandleWebsocket), public...))
	mux.Handle("/api/health", Compose(http.HandlerFunc(handlers.HealthHandler), health...))
	mux.Handle("/api/games", Compose(http.HandlerFunc(handlers.HandleListGames), protected...))
	mux.Handle("/api/rooms", Compose(http.HandlerFunc(roomHandler.HandleRoomsRoute), protected...))
	mux.Handle("/api/rooms/{id}", Compose(http.HandlerFunc(roomHandler.HandleGetRoom), protected...))

	return mux
}
