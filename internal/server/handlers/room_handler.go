package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"pulsar/internal/logger"
	game_plugin "pulsar/internal/plugin/game"
	"pulsar/internal/server/domain"
	"pulsar/internal/server/protocol"
)

// RoomMemberAPI represents a connected client's details and role in a room.
type RoomMemberAPI struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// CreateRoomRequest defines the request payload for creating a room via REST API.
type CreateRoomRequest struct {
	ClientID     string                  `json:"clientId"`
	GameName     string                  `json:"gameName"`
	Visibility   protocol.RoomVisibility `json:"visibility"`
	Spectate     *bool                   `json:"spectate,omitempty"`
	AllowPlugins []string                `json:"allowPlugins,omitempty"`
	MaxPlayers   int                     `json:"maxPlayers,omitempty"`
}

// CreateRoomResponse defines the response payload returned upon room creation via REST API.
type CreateRoomResponse struct {
	RoomID        string                 `json:"roomId"`
	GameName      string                 `json:"gameName"`
	GameStatus    game_plugin.RoomStatus `json:"gameStatus"`
	PlayersJoined uint                   `json:"playersJoined"`
	Capacity      uint                   `json:"capacity"`
	Spectate      bool                   `json:"spectate"`
	SpectateCount uint                   `json:"spectateCount"`
}

// RoomHandler handles REST endpoints for room inspection and creation.
type RoomHandler struct {
	Hub *domain.Hub
}

// NewRoomHandler creates an initialized RoomHandler.
func NewRoomHandler(hub *domain.Hub) *RoomHandler {
	return &RoomHandler{Hub: hub}
}

// HandleListRooms returns public room information visible to lobby players.
func (h *RoomHandler) HandleListRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	h.Hub.Mu.RLock()
	roomsList := make([]*domain.Room, 0, len(h.Hub.Rooms))
	for _, room := range h.Hub.Rooms {
		roomsList = append(roomsList, room)
	}
	h.Hub.Mu.RUnlock()

	rooms := make([]map[string]any, 0, len(roomsList))
	for _, room := range roomsList {
		room.Mu.RLock()
		if room.Visibility != protocol.RoomPublic {
			room.Mu.RUnlock()
			continue
		}

		if room.GameMode == game_plugin.ModeSingle {
			room.Mu.RUnlock()
			continue
		}

		spectateCount := 0
		playerCount := 0
		for _, role := range room.Clients {
			switch role {
			case game_plugin.RolePlayer:
				playerCount++
			case game_plugin.RoleSpectator:
				spectateCount++
			}
		}

		status := room.Status

		rooms = append(rooms, map[string]any{
			"id":       room.ID,
			"status":   status,
			"gameName": room.GameName,
			"players":  playerCount,
			"capacity": room.MaxPlayers,
			"joinCode": room.JoinCode != "",
			"spectate": room.Spectate,
			"mode":     room.GameMode,
		})
		room.Mu.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rooms)
}

// HandleGetRoom returns detailed information for a specific game room.
func (h *RoomHandler) HandleGetRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	roomID := r.PathValue("id")
	if roomID == "" {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) >= 4 {
			roomID = parts[3]
		}
	}

	if roomID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room ID is required"})
		return
	}

	h.Hub.Mu.RLock()
	room, ok := h.Hub.Rooms[roomID]
	h.Hub.Mu.RUnlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Room not found"})
		return
	}

	room.Mu.RLock()
	playerCount := 0
	spectatorCount := 0
	members := make([]RoomMemberAPI, 0, len(room.Clients))
	for c, role := range room.Clients {
		members = append(members, RoomMemberAPI{
			ID:   c.Id,
			Name: c.GetName(),
			Role: string(role),
		})
		if role == game_plugin.RolePlayer {
			playerCount++
		} else {
			spectatorCount++
		}
	}
	gameName := room.GameName
	gameMode := room.GameMode
	joinCodeReq := room.JoinCode != ""
	spectate := room.Spectate
	allowPlugins := room.AllowPlugins
	ownerID := room.OwnerID
	status := room.Status
	maxPlayers := room.MaxPlayers
	room.Mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"roomId":           room.ID,
		"gameId":           strings.ToLower(gameName),
		"gameName":         gameName,
		"playerCount":      playerCount,
		"spectatorCount":   spectatorCount,
		"maxPlayers":       maxPlayers,
		"joinCodeRequired": joinCodeReq,
		"spectate":         spectate,
		"allowPlugins":     allowPlugins,
		"status":           string(status),
		"ownerId":          ownerID,
		"mode":             string(gameMode),
		"members":          members,
	})
}

// HandleCreateRoom creates a new game room from a POST API request.
func (h *RoomHandler) HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body: " + err.Error()})
		return
	}

	if req.ClientID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "clientId is required"})
		return
	}

	client, exists := h.Hub.FindClientByID(req.ClientID)
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Client not found or not connected to lobby"})
		return
	}

	_, err := game_plugin.Get(req.GameName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported game type: " + req.GameName})
		return
	}

	h.Hub.Mu.RLock()
	var alreadyOwns bool
	for _, room := range h.Hub.Rooms {
		if room.OwnerID == req.ClientID {
			alreadyOwns = true
			break
		}
	}
	h.Hub.Mu.RUnlock()

	if alreadyOwns {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Player already owns an active room"})
		return
	}

	if req.Visibility != protocol.RoomPrivate {
		req.Visibility = protocol.RoomPublic
	}

	spectate := true
	if req.Spectate != nil {
		spectate = *req.Spectate
	}

	roomID := domain.GenerateRoomID()
	room, err := domain.NewRoom(roomID, h.Hub, req.GameName, req.ClientID, req.Visibility, spectate, req.AllowPlugins, req.MaxPlayers)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to initialize game room: " + err.Error()})
		return
	}

	h.Hub.Mu.Lock()
	h.Hub.Rooms[roomID] = room
	h.Hub.Mu.Unlock()

	logger.Infof("[API] Room %s created by owner %s (Name: %s) for game %s", roomID, client.Id, client.GetName(), req.GameName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateRoomResponse{
		RoomID:        roomID,
		GameName:      strings.ToLower(req.GameName),
		GameStatus:    game_plugin.StatusWaiting,
		PlayersJoined: 0,
		Capacity:      uint(room.MaxPlayers),
		Spectate:      spectate,
		SpectateCount: 0,
	})
}

// HandleRoomsRoute routes room API requests based on HTTP method.
func (h *RoomHandler) HandleRoomsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.HandleListRooms(w, r)
	case http.MethodPost:
		h.HandleCreateRoom(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
