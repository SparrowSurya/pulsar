package domain

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"pulsar/internal/config"
	"pulsar/internal/logger"
	game_plugin "pulsar/internal/plugin/game"
	shared_plugin "pulsar/internal/plugin/shared"
	"pulsar/internal/plugins/chat"
	"pulsar/internal/server/protocol"
	"pulsar/internal/server/transport"
)

// HubController defines the interface that Room uses to communicate with Hub.
type HubController interface {
	DeleteRoom(roomID string)
	SendError(c *transport.Client, channel protocol.Channel, code, errMsg string)
}

// Room represents a logical container for players, spectators, and a running game instance.
type Room struct {
	Mu               sync.RWMutex                               // Mutex isolating room-specific state
	ID               string                                     // Unique room identifier
	Hub              HubController                              // Reference to the central Hub
	Game             game_plugin.Game                           // Game plugin factory definition
	GameID           string                                     // Unique identifier of the hosted game
	GameName         string                                     // Display name of the hosted game
	GameMode         game_plugin.GameMode                       // Mode of the hosted game (single, duel, multiplayer)
	Status           game_plugin.RoomStatus                     // Room lifecycle status (waiting, starting, playing, ending, gameover)
	Clients          map[*transport.Client]game_plugin.UserRole // Mapping of connected clients to their role
	Ready            map[string]bool                            // Mapping of player client IDs to their ready status
	GameInstance     game_plugin.GameInstance                   // Active game state instance (nil while in lobby)
	DisconnectTimers map[string]*time.Timer                     // Mapping of player IDs to active disconnect grace timers
	Plugins          map[string]shared_plugin.RoomPlugin        // Active room plugins (e.g., chat)
	Visibility       protocol.RoomVisibility                    // Room discovery visibility (public / private)
	JoinCode         string                                     // Secret join code for private access
	Spectate         bool                                       // Whether spectators are permitted
	AllowPlugins     []string                                   // Active plugins allowed in this room
	MaxPlayers       int                                        // Player capacity for this room
	OwnerID          string                                     // ID of the room owner/host
}

// NewRoom creates a new Room with an associated game definition from the registry.
func NewRoom(
	id string,
	hub HubController,
	gameName string,
	ownerID string,
	visibility protocol.RoomVisibility,
	spectate bool,
	allowPlugins []string,
	maxPlayers int,
) (*Room, error) {
	g, err := findGame(gameName)
	if err != nil {
		return nil, err
	}

	var joinCode string
	if visibility == protocol.RoomPrivate {
		joinCode = generateJoinCode()
	}

	finalMaxPlayers := g.Capacity()
	switch g.Mode() {
	case game_plugin.ModeSingle:
		finalMaxPlayers = 1
	case game_plugin.ModeDuel:
		finalMaxPlayers = 2
	case game_plugin.ModeMultiplayer:
		if maxPlayers >= 2 && maxPlayers <= g.Capacity() {
			finalMaxPlayers = maxPlayers
		}
	}

	if allowPlugins == nil {
		allowPlugins = g.GetSupportedPlugins()
	}

	room := &Room{
		ID:               id,
		Hub:              hub,
		Game:             g,
		GameID:           g.Id(),
		GameName:         g.Name(),
		GameMode:         g.Mode(),
		Status:           game_plugin.StatusWaiting,
		Clients:          make(map[*transport.Client]game_plugin.UserRole),
		Ready:            make(map[string]bool),
		GameInstance:     nil,
		DisconnectTimers: make(map[string]*time.Timer),
		Plugins:          make(map[string]shared_plugin.RoomPlugin),
		Visibility:       visibility,
		JoinCode:         joinCode,
		Spectate:         spectate,
		AllowPlugins:     make([]string, 0, len(allowPlugins)),
		MaxPlayers:       finalMaxPlayers,
		OwnerID:          ownerID,
	}

	supportedMap := make(map[string]bool)
	for _, p := range g.GetSupportedPlugins() {
		supportedMap[p] = true
	}

	for _, pName := range allowPlugins {
		if supportedMap[pName] {
			factory, err := shared_plugin.GetFactory(shared_plugin.SharedPluginName(pName))
			if err == nil {
				room.Plugins[pName] = factory(room)
				room.AllowPlugins = append(room.AllowPlugins, pName)
			} else {
				logger.Warnf("[Room %s] Warning: failed to load supported plugin %s: %v", id, pName, err)
			}
		}
	}

	logger.Infof("[Room] Created new %s room %s for %s (Game: %s, MaxPlayers: %d, Plugins: %v)", room.Visibility, room.ID, room.OwnerID, room.GameName, room.MaxPlayers, room.AllowPlugins)

	return room, nil
}

// Join adds a client to the room with the specified role or reconnects an existing active player.
func (r *Room) Join(c *transport.Client, role game_plugin.UserRole) {
	r.Mu.Lock()

	for oldClient := range r.Clients {
		if oldClient.Id == c.Id {
			delete(r.Clients, oldClient)
		}
	}

	isReconnect := (r.GameInstance != nil && r.GameInstance.HasPlayer(c.Id))
	if isReconnect {
		if timer, ok := r.DisconnectTimers[c.Id]; ok {
			timer.Stop()
			delete(r.DisconnectTimers, c.Id)
		}
		role = game_plugin.RolePlayer
		r.Clients[c] = role
		r.GameInstance.OnPlayerReconnect(c.Id)
		r.Mu.Unlock()

		c.SetCurrentRoom(r.ID)
		logger.Infof("[Room %s] Client %s (ID: %s) reconnected to active game", r.ID, c.GetName(), c.Id)

		for _, p := range r.Plugins {
			p.OnPlayerJoin(c.Id, c.GetName(), role)
		}

		r.SendSystemMessage(fmt.Sprintf("%s reconnected!", c.GetName()))
		r.sendStateToClient(c)
		r.broadcastRoomInfo()
		r.broadcastGameState()
		return
	}

	r.Clients[c] = role
	if role == game_plugin.RolePlayer {
		r.Ready[c.Id] = (c.Id == r.OwnerID)
	} else {
		delete(r.Ready, c.Id)
	}
	r.Mu.Unlock()
	c.SetCurrentRoom(r.ID)

	logger.Infof("[Room %s] Client %s joined as %s", r.ID, c.GetName(), role)

	for _, p := range r.Plugins {
		p.OnPlayerJoin(c.Id, c.GetName(), role)
	}

	if r.GameInstance != nil {
		r.sendStateToClient(c)
	}

	r.broadcastRoomInfo()
}

// cancelAllDisconnectTimers stops and cleans up any active disconnect timers.
func (r *Room) cancelAllDisconnectTimers() {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for id, timer := range r.DisconnectTimers {
		timer.Stop()
		delete(r.DisconnectTimers, id)
	}
}

// destroyRoom tears down the room, informs remaining clients, and removes it from the hub.
func (r *Room) destroyRoom() {
	r.cancelAllDisconnectTimers()

	r.Mu.Lock()
	for c := range r.Clients {
		c.SetCurrentRoom("")
		r.Hub.SendError(c, protocol.RoomChannel, protocol.ErrRoomNotFound, "Room closed")
	}
	r.Clients = make(map[*transport.Client]game_plugin.UserRole)
	r.Ready = make(map[string]bool)
	r.GameInstance = nil
	r.Mu.Unlock()

	r.Hub.DeleteRoom(r.ID)
}

// reassignOwnerLocked selects a new owner from remaining clients if the current owner left.
// Returns true if an owner is present or newly assigned, or false if no clients remain.
func (r *Room) reassignOwnerLocked() bool {
	if len(r.Clients) == 0 {
		return false
	}

	// Check if current owner is still present in Clients
	for c := range r.Clients {
		if c.Id == r.OwnerID {
			return true // Owner still present
		}
	}

	// Owner left, find a replacement. Prefer players over spectators.
	var newOwner *transport.Client
	for c, role := range r.Clients {
		if role == game_plugin.RolePlayer {
			newOwner = c
			break
		}
	}

	// If no players, pick any client (spectator) and promote to player
	if newOwner == nil {
		for c := range r.Clients {
			newOwner = c
			r.Clients[c] = game_plugin.RolePlayer
			break
		}
	}

	if newOwner != nil {
		r.OwnerID = newOwner.Id
		logger.Infof("[Room %s] Ownership transferred to %s (%s)", r.ID, newOwner.GetName(), newOwner.Id)
		return true
	}

	return false
}

// Leave removes a client from the room or initiates a disconnect grace timer if in active game.
func (r *Room) Leave(c *transport.Client) {
	r.Mu.Lock()
	if _, ok := r.Clients[c]; !ok {
		r.Mu.Unlock()
		return
	}

	delete(r.Clients, c)
	delete(r.Ready, c.Id)
	isEmpty := len(r.Clients) == 0
	isGameActive := (r.GameInstance != nil && r.GameInstance.HasPlayer(c.Id))

	if !isEmpty && !isGameActive {
		r.reassignOwnerLocked()
	}
	r.Mu.Unlock()
	c.SetCurrentRoom("")

	logger.Infof("[Room %s] Client %s left", r.ID, c.GetName())

	// Inform all room plugins
	for _, p := range r.Plugins {
		p.OnPlayerLeave(c.Id)
	}

	if isEmpty {
		logger.Infof("[Room %s] Room is empty, destroying room", r.ID)
		r.destroyRoom()
		return
	}

	if isGameActive {
		r.startPlayerDisconnectTimer(c.Id, c.GetName())
		r.GameInstance.OnPlayerDisconnect(c.Id)
		r.SendSystemMessage(fmt.Sprintf("%s disconnected. Waiting up to 30s to reconnect...", c.GetName()))
		r.broadcastGameState()
		r.broadcastRoomInfo()
		return
	}

	// Broadcast room updates in lobby
	r.broadcastRoomInfo()
}

func (r *Room) startPlayerDisconnectTimer(playerID, playerName string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	if timer, ok := r.DisconnectTimers[playerID]; ok {
		timer.Stop()
	}

	graceSecs := config.LoadEnv().DisconnectGraceSecs
	if graceSecs <= 0 {
		graceSecs = 30
	}

	r.DisconnectTimers[playerID] = time.AfterFunc(time.Duration(graceSecs)*time.Second, func() {
		r.handlePlayerTimeout(playerID, playerName)
	})
}

func (r *Room) handlePlayerTimeout(playerID, playerName string) {
	r.Mu.Lock()
	delete(r.DisconnectTimers, playerID)
	if r.GameInstance == nil || !r.GameInstance.HasPlayer(playerID) {
		r.Mu.Unlock()
		return
	}
	instance := r.GameInstance
	r.Mu.Unlock()

	r.SendSystemMessage(fmt.Sprintf("%s timed out.", playerName))
	instance.OnTimeout(playerID)

	r.Mu.Lock()
	hasOwner := r.reassignOwnerLocked()
	r.Mu.Unlock()

	if !hasOwner {
		r.destroyRoom()
	} else {
		r.broadcastRoomInfo()
	}
}

// HasPlayer returns if player (role), owner, or active game participant is present in the room.
func (r *Room) HasPlayer(id string) bool {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	if r.OwnerID == id {
		return true
	}

	for c, role := range r.Clients {
		if c.Id == id && role == game_plugin.RolePlayer {
			return true
		}
	}

	if r.GameInstance != nil && r.GameInstance.HasPlayer(id) {
		return true
	}

	return false
}

// ChangeRole switches a participant's role between player and spectator while the room is waiting.
func (r *Room) ChangeRole(c *transport.Client, targetRole game_plugin.UserRole) error {
	r.Mu.Lock()
	currentRole, exists := r.Clients[c]
	if !exists {
		r.Mu.Unlock()
		return fmt.Errorf("client not in room")
	}

	if currentRole == targetRole {
		r.Mu.Unlock()
		return nil
	}

	if r.Status != game_plugin.StatusWaiting {
		r.Mu.Unlock()
		return fmt.Errorf("cannot change role while game is not in waiting lobby")
	}

	if targetRole == game_plugin.RoleSpectator {
		if !r.Spectate {
			r.Mu.Unlock()
			return fmt.Errorf("spectating is not allowed in this room")
		}
		r.Clients[c] = game_plugin.RoleSpectator
		delete(r.Ready, c.Id)
		r.Mu.Unlock()

		logger.Infof("[Room %s] Client %s switched from player to spectator", r.ID, c.GetName())
		for _, p := range r.Plugins {
			p.OnPlayerJoin(c.Id, c.GetName(), game_plugin.RoleSpectator)
		}
		r.broadcastRoomInfo()
		return nil
	}

	if targetRole == game_plugin.RolePlayer {
		playerCount := 0
		for _, role := range r.Clients {
			if role == game_plugin.RolePlayer {
				playerCount++
			}
		}

		if playerCount >= r.MaxPlayers {
			r.Mu.Unlock()
			return fmt.Errorf("no player slots available in this room")
		}

		r.Clients[c] = game_plugin.RolePlayer
		r.Ready[c.Id] = (c.Id == r.OwnerID)
		r.Mu.Unlock()

		logger.Infof("[Room %s] Client %s switched from spectator to player", r.ID, c.GetName())
		for _, p := range r.Plugins {
			p.OnPlayerJoin(c.Id, c.GetName(), game_plugin.RolePlayer)
		}
		r.broadcastRoomInfo()
		return nil
	}

	r.Mu.Unlock()
	return fmt.Errorf("invalid role: %s", targetRole)
}

// SetReady updates a player's readiness state while the room is waiting.
func (r *Room) SetReady(c *transport.Client, ready bool) error {
	r.Mu.Lock()
	role, exists := r.Clients[c]
	if !exists {
		r.Mu.Unlock()
		return fmt.Errorf("client not in room")
	}

	if role != game_plugin.RolePlayer {
		r.Mu.Unlock()
		return fmt.Errorf("spectators cannot set ready status")
	}

	if r.Status != game_plugin.StatusWaiting {
		r.Mu.Unlock()
		return fmt.Errorf("cannot change ready status while game is not in waiting lobby")
	}

	if c.Id == r.OwnerID {
		r.Ready[c.Id] = true // Room host is always ready
	} else {
		r.Ready[c.Id] = ready
	}
	r.Mu.Unlock()

	logger.Infof("[Room %s] Player %s set ready = %v", r.ID, c.GetName(), ready)
	r.broadcastRoomInfo()
	return nil
}

// JoinAsPlayer switches a connected spectator's role to player if player slots are available.
func (r *Room) JoinAsPlayer(c *transport.Client) {
	if err := r.ChangeRole(c, game_plugin.RolePlayer); err != nil {
		r.Hub.SendError(c, protocol.RoomChannel, protocol.ErrNotPermitted, err.Error())
	}
}

// StartGame validates prerequisites, instantiates the GameInstance with active players, and starts the match.
func (r *Room) StartGame(callerID string) error {
	r.Mu.Lock()
	if callerID != r.OwnerID {
		r.Mu.Unlock()
		return fmt.Errorf("Only the room host can start the game")
	}

	if r.Status != game_plugin.StatusWaiting {
		r.Mu.Unlock()
		return fmt.Errorf("game is already starting or in progress")
	}

	type playerWithClient struct {
		info   game_plugin.PlayerInfo
		client *transport.Client
	}
	var ownerPlayer *playerWithClient
	var otherPlayers []playerWithClient

	for c, role := range r.Clients {
		if role == game_plugin.RolePlayer {
			p := playerWithClient{
				info: game_plugin.PlayerInfo{
					ID:   c.Id,
					Name: c.GetName(),
				},
				client: c,
			}
			if c.Id == r.OwnerID {
				ownerPlayer = &p
			} else {
				otherPlayers = append(otherPlayers, p)
			}
		}
	}

	activePlayers := make([]playerWithClient, 0, len(r.Clients))
	if ownerPlayer != nil {
		activePlayers = append(activePlayers, *ownerPlayer)
	}
	activePlayers = append(activePlayers, otherPlayers...)

	if r.GameMode != game_plugin.ModeSingle && len(activePlayers) < 2 {
		r.Mu.Unlock()
		return fmt.Errorf("need at least 2 players to start")
	}

	playersInfo := make([]game_plugin.PlayerInfo, len(activePlayers))
	for i, p := range activePlayers {
		playersInfo[i] = p.info
	}

	// Transition to starting status (lobby actions locked)
	r.Status = game_plugin.StatusStarting
	r.Ready = make(map[string]bool)
	r.Mu.Unlock()

	// Broadcast starting room details to notify clients of transition
	r.broadcastRoomInfo()

	// Instantiate game session
	instance, err := r.Game.StartGame(r, playersInfo)
	if err != nil {
		r.Mu.Lock()
		r.Status = game_plugin.StatusWaiting
		r.Mu.Unlock()
		r.broadcastRoomInfo()
		return fmt.Errorf("failed to start game: %w", err)
	}

	r.Mu.Lock()
	r.GameInstance = instance
	r.Status = game_plugin.StatusPlaying
	r.Mu.Unlock()

	logger.Infof("[Room %s] Game started with %d players", r.ID, len(playersInfo))
	r.SendSystemMessage("The game has started!")

	r.broadcastGameState()
	r.broadcastRoomInfo()
	return nil
}

// HandleGameAction passes client actions to the game instance after validating player role.
func (r *Room) HandleGameAction(c *transport.Client, action string, payload json.RawMessage) {
	r.Mu.RLock()
	role, exists := r.Clients[c]
	instance := r.GameInstance
	r.Mu.RUnlock()

	if !exists {
		return
	}

	if role != game_plugin.RolePlayer {
		r.SendToPlayer(c.Id, string(protocol.GameChannel), protocol.ActionError, protocol.ErrorEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrNotPermitted,
				Message: "spectators cannot perform game actions",
			},
		})
		return
	}

	if instance == nil {
		r.SendToPlayer(c.Id, string(protocol.GameChannel), protocol.ActionError, protocol.ErrorEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrNotPermitted,
				Message: "game is not currently active",
			},
		})
		return
	}

	// Validate game instance turn/action with panic recovery
	defer func() {
		if recovery := recover(); recovery != nil {
			logger.Errorf("[Panic Recovery] Room %s crashed during action %s: %v", r.ID, action, recovery)
			r.SendSystemMessage("An internal game error occurred.")
			r.EndGame(game_plugin.GameOverDetail{})
		}
	}()

	err := instance.OnAction(c.Id, action, payload)
	if err != nil {
		logger.Errorf("[Room %s] Action error: %v", r.ID, err)
		r.SendToPlayer(c.Id, string(protocol.GameChannel), protocol.ActionError, protocol.ErrorEvent{
			Error: &protocol.ErrorContent{
				Code:    protocol.ErrInvalidFormat,
				Message: err.Error(),
			},
		})
		return
	}
}

// Broadcast sends a JSON-serializable event to all players and spectators in the room.
func (r *Room) Broadcast(route string, action string, payload any) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorln("Room.Broadcast payload marshal error:", err)
		return
	}

	msg := protocol.ServerMessage{
		Channel:   protocol.Channel(route),
		Type:      action,
		Content:   string(payloadBytes),
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Errorln("Room.Broadcast marshal error:", err)
		return
	}

	r.Mu.Lock()
	defer r.Mu.Unlock()
	for c := range r.Clients {
		if !c.SendMsg(data) {
			delete(r.Clients, c)
		}
	}
}

// SendToPlayer sends a JSON-serializable event to a specific player or spectator in the room.
func (r *Room) SendToPlayer(playerID string, route string, action string, payload any) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorln("Room.SendToPlayer payload marshal error:", err)
		return
	}

	msg := protocol.ServerMessage{
		Channel:   protocol.Channel(route),
		Type:      action,
		Content:   string(payloadBytes),
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Errorln("Room.SendToPlayer marshal error:", err)
		return
	}

	r.Mu.Lock()
	defer r.Mu.Unlock()
	for c := range r.Clients {
		if c.Id == playerID {
			if !c.SendMsg(data) {
				delete(r.Clients, c)
			}
			return
		}
	}
}

func (r *Room) sendStateToClient(c *transport.Client) {
	r.Mu.RLock()
	inst := r.GameInstance
	r.Mu.RUnlock()

	if inst == nil {
		return
	}

	state := inst.GetState(c.Id)
	if state == nil {
		return
	}

	payloadBytes, err := json.Marshal(state)
	if err != nil {
		return
	}

	msg := protocol.ServerMessage{
		Channel:   protocol.GameChannel,
		Type:      protocol.ActionStateUpdate,
		Content:   string(payloadBytes),
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err == nil {
		c.SendMsg(data)
	}
}

// broadcastGameState sends personalized/generic game state to all players and spectators.
func (r *Room) broadcastGameState() {
	r.Mu.RLock()
	inst := r.GameInstance
	if inst == nil {
		r.Mu.RUnlock()
		return
	}

	type clientState struct {
		client *transport.Client
		data   []byte
	}
	messages := make([]clientState, 0, len(r.Clients))
	for c := range r.Clients {
		state := inst.GetState(c.Id)
		if state != nil {
			if payloadBytes, err := json.Marshal(state); err == nil {
				msg := protocol.ServerMessage{
					Channel:   protocol.GameChannel,
					Type:      protocol.ActionStateUpdate,
					Content:   string(payloadBytes),
					Timestamp: time.Now().Unix(),
				}
				if data, err := json.Marshal(msg); err == nil {
					messages = append(messages, clientState{client: c, data: data})
				}
			}
		}
	}
	r.Mu.RUnlock()

	for _, m := range messages {
		m.client.SendMsg(m.data)
	}
}

// BroadcastGameState triggers room to broadcast current game state to all players and spectators.
func (r *Room) BroadcastGameState() {
	r.broadcastGameState()
}

// SendGameState sends current game state to a specific player or spectator.
func (r *Room) SendGameState(playerID string) {
	r.Mu.RLock()
	var client *transport.Client
	for c := range r.Clients {
		if c.Id == playerID {
			client = c
			break
		}
	}
	r.Mu.RUnlock()

	if client != nil {
		r.sendStateToClient(client)
	}
}

// HandlePluginAction routes a plugin action (such as chat) to the corresponding mounted plugin.
func (r *Room) HandlePluginAction(playerID string, actionType string, payload []byte) error {
	if p, ok := r.Plugins[actionType]; ok {
		return p.OnAction(playerID, actionType, payload)
	}
	return fmt.Errorf("plugin %q is not active in this room", actionType)
}

// SendSystemMessage broadcasts a system announcement in the room chat.
func (r *Room) SendSystemMessage(content string) {
	if _, active := r.Plugins[string(chat.PluginChat)]; active {
		r.Broadcast(string(protocol.RoomChannel), protocol.ActionChat, map[string]interface{}{
			"system":  true,
			"content": content,
		})
	} else {
		logger.Debugf("[Room %s SYSTEM LOG] %s", r.ID, content)
	}
}

// EndGame completes the current game instance, transitions through ending to gameover, and returns to waiting lobby.
func (r *Room) EndGame(detail game_plugin.GameOverDetail) {
	r.cancelAllDisconnectTimers()

	r.Mu.Lock()
	r.Status = game_plugin.StatusEnding
	r.Mu.Unlock()

	// Broadcast ending room details
	r.broadcastRoomInfo()

	r.Mu.Lock()
	r.Status = game_plugin.StatusGameOver
	r.Mu.Unlock()

	// Broadcast game over event and final gameover room details
	r.Broadcast(string(protocol.RoomChannel), protocol.ActionGameover, protocol.ServerGameoverDetails{
		GameoverDetails: &detail,
	})
	r.broadcastRoomInfo()

	// Reset room state back to waiting lobby
	r.Mu.Lock()
	r.GameInstance = nil
	r.Status = game_plugin.StatusWaiting
	r.Ready = make(map[string]bool)
	r.Mu.Unlock()

	// Broadcast waiting room details for next match
	r.broadcastRoomInfo()
}

// ReturnToLobby resets the room back to the waiting lobby status and clears the active game instance.
func (r *Room) ReturnToLobby() {
	r.cancelAllDisconnectTimers()

	r.Mu.Lock()
	r.GameInstance = nil
	r.Status = game_plugin.StatusWaiting
	r.Ready = make(map[string]bool)
	r.Mu.Unlock()

	r.broadcastRoomInfo()
}

// GetOwnerID returns the ID of the room owner.
func (r *Room) GetOwnerID() string {
	return r.OwnerID
}

// DestroyRoom terminates the room and kicks out all participants.
func (r *Room) DestroyRoom() {
	r.destroyRoom()
}

// GetRoomDetails constructs and returns the full RoomDetails representation.
func (r *Room) GetRoomDetails(forID string) *protocol.RoomDetails {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	players := make([]protocol.LobbyPlayer, 0, len(r.Clients))
	spectatorsCount := 0
	for c, role := range r.Clients {
		if role == game_plugin.RolePlayer {
			isReady := false
			if c.Id == r.OwnerID {
				isReady = true
			} else {
				isReady = r.Ready[c.Id]
			}
			players = append(players, protocol.LobbyPlayer{
				PlayerInfo: game_plugin.PlayerInfo{
					ID:   c.Id,
					Name: c.GetName(),
				},
				Ready: isReady,
			})
		} else if role == game_plugin.RoleSpectator {
			spectatorsCount++
		}
	}

	joinCode := ""
	if forID == r.OwnerID || r.Visibility == protocol.RoomPrivate {
		joinCode = r.JoinCode
	}

	return &protocol.RoomDetails{
		RoomID:       r.ID,
		GameID:       r.GameID,
		GameName:     r.GameName,
		GameMode:     r.GameMode,
		Status:       r.Status,
		OwnerID:      r.OwnerID,
		Visibility:   r.Visibility,
		Spectate:     r.Spectate,
		AllowPlugins: r.AllowPlugins,
		MaxPlayers:   r.MaxPlayers,
		JoinCode:     joinCode,
		Players:      players,
		Spectators:   spectatorsCount,
	}
}

// GetGameState returns the serialized game state view for a specific player/spectator ID.
func (r *Room) GetGameState(playerID string) interface{} {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	if r.GameInstance != nil {
		return r.GameInstance.GetState(playerID)
	}
	return nil
}

// broadcastRoomInfo broadcasts full room details to all members.
func (r *Room) broadcastRoomInfo() {
	details := r.GetRoomDetails("")
	r.Broadcast(string(protocol.RoomChannel), protocol.ActionRoomDetail, protocol.ServerRoomDetail{
		RoomDetails: details,
	})
}
