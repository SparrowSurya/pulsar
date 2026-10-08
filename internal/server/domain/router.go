package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"pulsar/internal/logger"
	game_plugin "pulsar/internal/plugin/game"
	"pulsar/internal/server/protocol"
	"pulsar/internal/server/transport"
)

// handleSessionAction handles session related actions which require no specific channels.
func (h *Hub) handleSessionAction(c *transport.Client, msg *protocol.ClientMessage) {
	switch msg.Type {
	case protocol.ActionRename:
		h.handleRenameAction(c, msg)
	case protocol.ActionCreateRoom:
		h.handleCreateRoom(c, msg)
	}
}

// handleRoomAction manages room operations by dispatching to dedicated action handlers.
func (h *Hub) handleRoomAction(c *transport.Client, msg *protocol.ClientMessage) {
	switch msg.Type {
	case protocol.ActionCreateRoom:
		h.handleCreateRoom(c, msg)
	case protocol.ActionJoinRoom:
		h.handleJoinRoom(c, msg)
	case protocol.ActionChat:
		h.handleRoomChat(c, msg)
	case protocol.ActionLeaveRoom:
		h.handleRoomLeave(c, msg)
	case protocol.ActionChangeRole:
		h.handleRoomChangeRole(c, msg)
	case protocol.ActionReady:
		h.handleRoomReady(c, msg)
	case protocol.ActionRoomDetail:
		h.handleRoomDetail(c, msg)
	case protocol.ActionStartGame:
		h.handleRoomStartGame(c, msg)
	}
}

// sendRoomResponse is a common haelper function to send room response.
func sendRoomResponse(c *transport.Client, pType string, data any) {
	content, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := protocol.ServerMessage{
		Channel: protocol.RoomChannel,
		Type:    pType,
		Content: string(content),
	}
	if data, err := json.Marshal(msg); err == nil {
		c.SendMsg(data)
	}
}

// findGame resolves a game plugin by name or ID (case-insensitive).
func findGame(idOrName string) (game_plugin.Game, error) {
	if g, err := game_plugin.Get(idOrName); err == nil {
		return g, nil
	}
	for _, g := range game_plugin.GetGames() {
		if strings.EqualFold(g.Name(), idOrName) ||
			strings.EqualFold(g.Id(), idOrName) ||
			strings.EqualFold(strings.ReplaceAll(g.Id(), "-", ""), strings.ReplaceAll(idOrName, "-", "")) ||
			strings.EqualFold(strings.ReplaceAll(g.Name(), "-", ""), strings.ReplaceAll(idOrName, "-", "")) {
			return g, nil
		}
	}
	return nil, fmt.Errorf("game plugin %q not found", idOrName)
}

// getActiveRoom retrieves the client's current room or sends an appropriate error.
func (h *Hub) getActiveRoom(c *transport.Client, channel protocol.Channel) (*Room, bool) {
	currentRoom := c.GetCurrentRoom()
	if currentRoom == "" {
		h.SendError(c, channel, protocol.ErrNotPermitted, "Not currently in a room")
		return nil, false
	}

	h.Mu.RLock()
	room, ok := h.Rooms[currentRoom]
	h.Mu.RUnlock()

	if !ok {
		c.SetCurrentRoom("")
		h.SendError(c, channel, protocol.ErrRoomNotFound, "Room no longer exists")
		return nil, false
	}
	return room, true
}

func (h *Hub) handleRoomChat(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	if err := room.HandlePluginAction(c.Id, msg.Type, []byte(msg.Content)); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrInvalidFormat, err.Error())
	}
}

func (h *Hub) handleRoomLeave(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	room.Leave(c)
}

func (h *Hub) handleRoomChangeRole(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	var req protocol.ClientChangeRoleEvent
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrInvalidFormat, "Invalid change_role payload format")
		return
	}
	if err := room.ChangeRole(c, req.Role); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrNotPermitted, err.Error())
	}
}

func (h *Hub) handleRoomReady(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	var req protocol.ClientReadyEvent
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrInvalidFormat, "Invalid ready payload format")
		return
	}
	if err := room.SetReady(c, req.Ready); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrNotPermitted, err.Error())
	}
}

func (h *Hub) handleRoomDetail(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	sendRoomResponse(c, protocol.ActionRoomDetail, protocol.ServerRoomDetail{
		RoomDetails: room.GetRoomDetails(c.Id),
	})
}

func (h *Hub) handleRoomStartGame(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.RoomChannel)
	if !ok {
		return
	}
	if err := room.StartGame(c.Id); err != nil {
		h.SendError(c, protocol.RoomChannel, protocol.ErrNotPermitted, err.Error())
	}
}

// handleGameAction forwards inputs to the player's active room game instance.
func (h *Hub) handleGameAction(c *transport.Client, msg *protocol.ClientMessage) {
	room, ok := h.getActiveRoom(c, protocol.GameChannel)
	if !ok {
		return
	}

	room.HandleGameAction(c, msg.Type, json.RawMessage(msg.Content))
}

// handleRenameAction processes client rename requests.
func (h *Hub) handleRenameAction(c *transport.Client, msg *protocol.ClientMessage) {
	rawData := []byte(msg.Content)

	var req protocol.ClientRenameEvent
	if len(rawData) > 0 {
		_ = json.Unmarshal(rawData, &req)
	}
	requestedName := req.Name
	if requestedName == "" {
		requestedName = msg.Content
	}
	oldName := c.GetName()

	// Disallow renaming if the client is currently in a room
	if c.GetCurrentRoom() != "" {
		errMsg := "Cannot rename while in a room"
		logger.Debugf("[HUB] Rename rejected for ID %s: %s (room: %s)", c.Id, errMsg, c.GetCurrentRoom())
		resp := protocol.ServerRenameEvent{
			Name: oldName,
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrPlayerAlreadyPlaying,
				Message: errMsg,
			},
		}
		if data, err := json.Marshal(resp); err == nil {
			c.SendMsg(data)
		}
		return
	}

	finalName, errMsg, ok := protocol.ValidateRename(requestedName, oldName)
	if ok {
		c.SetName(finalName)
		logger.Debugf("[HUB] Legacy Client renamed: %s -> %s (ID: %s)", oldName, finalName, c.Id)
		resp := protocol.ServerRenameEvent{
			Name:  finalName,
			Error: nil,
		}
		if data, err := json.Marshal(resp); err == nil {
			c.SendMsg(data)
		}
	} else {
		logger.Debugf("[HUB] Legacy Client rename rejected for ID %s (requested: %q, error: %s), retained: %s", c.Id, requestedName, errMsg, oldName)
		resp := protocol.ServerRenameEvent{
			Name: oldName,
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidName,
				Message: errMsg,
			},
		}
		if data, err := json.Marshal(resp); err == nil {
			c.SendMsg(data)
		}
	}
}

func (h *Hub) handleCreateRoom(c *transport.Client, msg *protocol.ClientMessage) {
	rawData := []byte(msg.Content)

	var req protocol.ClientCreateRoomEvent
	if err := json.Unmarshal(rawData, &req); err != nil {
		sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidFormat,
				Message: "Invalid content format",
			},
		})
		return
	}

	req.GameID = strings.TrimSpace(req.GameID)
	if req.GameID == "" {
		sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidGameID,
				Message: "Game ID is required",
			},
		})
		return
	}

	game, err := findGame(req.GameID)
	if err != nil {
		sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidGameID,
				Message: "Unsupported game ID: " + req.GameID,
			},
		})
		return
	}

	// Check if player already joined some room
	if room := h.FindRoomByPlayerID(c.Id); room != nil {
		sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
			RoomID: room.ID,
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrPlayerAlreadyPlaying,
				Message: "Player already in a room",
			},
		})
		return
	}

	visibility := protocol.RoomVisibility(req.Visibility)
	if visibility != protocol.RoomPrivate {
		visibility = protocol.RoomPublic
	}

	spectate := true
	if req.Spectate != nil {
		spectate = *req.Spectate
	}

	roomID := generateRoomID()
	room, err := NewRoom(roomID, h, game.Name(), c.Id, visibility, spectate, req.AllowPlugins, req.MaxPlayers)
	if err != nil {
		sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidFormat,
				Message: "Failed to initialize game room: " + err.Error(),
			},
		})
		return
	}

	h.Mu.Lock()
	h.Rooms[roomID] = room
	h.Mu.Unlock()

	logger.Infof("[HUB] Room %s created by owner %s (Name: %s) for game %s", roomID, c.Id, c.GetName(), game.Name())

	sendRoomResponse(c, protocol.ActionCreateRoom, protocol.ServerRoomCreateEvent{
		RoomID:   roomID,
		JoinCode: room.JoinCode,
	})
}

func (h *Hub) handleJoinRoom(c *transport.Client, msg *protocol.ClientMessage) {
	var payload protocol.ClientJoinRoomEvent
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidFormat,
				Message: "Invalid request format",
			},
		})
		return
	}

	roomID := payload.RoomID
	joinCode := payload.JoinCode
	spectate := payload.Spectate

	h.Mu.RLock()
	room, ok := h.Rooms[roomID]
	h.Mu.RUnlock()

	if !ok {
		sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrRoomNotFound,
				Message: "Room not found: " + roomID,
			},
		})
		return
	}

	// Enforce join code verification for private rooms except owner.
	if room.Visibility == protocol.RoomPrivate && c.Id != room.OwnerID {
		if joinCode == "" {
			sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
				Error: &protocol.ErrorContent{
					Code:    protocol.ErrMissingJoinCode,
					Message: "Join code required",
				},
			})
			return
		}
		if joinCode != room.JoinCode {
			sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
				Error: &protocol.ErrorContent{
					Code:    protocol.ErrInvalidJoinCode,
					Message: "Invalid join code",
				},
			})
			return
		}
	}

	// Determine role and validate capacity / spectate permissions
	var role game_plugin.UserRole
	room.Mu.RLock()
	isReconnectingPlayer := (room.GameInstance != nil && room.GameInstance.HasPlayer(c.Id))
	isGamePlaying := (room.Status != game_plugin.StatusWaiting)
	playerCount := 0
	for _, r := range room.Clients {
		if r == game_plugin.RolePlayer {
			playerCount++
		}
	}
	room.Mu.RUnlock()

	if isReconnectingPlayer {
		role = game_plugin.RolePlayer
	} else if spectate {
		if !room.Spectate {
			sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
				Error: &protocol.ErrorContent{
					Code:    protocol.ErrSpectatedNotAllowed,
					Message: "Spectating is not allowed in this room",
				},
			})
			return
		}
		role = game_plugin.RoleSpectator
	} else {
		if isGamePlaying {
			if room.Spectate {
				role = game_plugin.RoleSpectator
			} else {
				sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
					Error: &protocol.ErrorContent{
						Code:    protocol.ErrPlayerSlotUnavailable,
						Message: "Game already in progress and spectating is disabled",
					},
				})
				return
			}
		} else if playerCount >= room.MaxPlayers {
			if room.Spectate {
				role = game_plugin.RoleSpectator
			} else {
				sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
					Error: &protocol.ErrorContent{
						Code:    protocol.ErrPlayerSlotUnavailable,
						Message: "Room is full",
					},
				})
				return
			}
		} else {
			role = game_plugin.RolePlayer
		}
	}

	room.Join(c, role)

	sendRoomResponse(c, protocol.ActionJoinRoom, protocol.ServerJoinRoom{
		RoomDetails: room.GetRoomDetails(c.Id),
	})
}
