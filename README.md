# Pulsar Backend Server

The backend server for the Pulsar real-time multiplayer platform, written in Go.

## Features
- **WebSocket Hub**: Manages real-time connection state, client registration, and message routing channels (`lobby`, `room`, `game`).
- **REST APIs**: Exposes endpoints for active rooms, dynamic game plugin feature detection, and system health checks.
- **Pluggable Game Architecture**: Supports modular game plugins (like Tic-Tac-Toe) that hook into the room lifecycle and manage interactive gameplay states.
- **Security Middlewares**: Includes CORS verification, rate-limiting, and headers hardening.

## Getting Started
To run the server locally:
```bash
go run cmd/main.go
```
Or run the server integration tests:
```bash
go test ./...
```
