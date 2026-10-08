package game_plugin

import "encoding/json"

// Represents a user role in the room.
type UserRole string

const (
	RolePlayer    UserRole = "player"
	RoleSpectator UserRole = "spectator"
)

// RoomContext provides an interface for GameInstances to communicate with the containing Room.
type RoomContext interface {

	// BroadcastGameState triggers Room to broadcast updated game state to all players and spectators.
	BroadcastGameState()

	// SendGameState sends the current game state to a specific player/spectator.
	SendGameState(playerID string)

	// Broadcast sends a JSON-serializable event to all players and spectators in the room.
	Broadcast(route string, action string, payload any)

	// SendToPlayer sends a JSON-serializable event to a specific player/spectator.
	SendToPlayer(playerID string, route string, action string, payload any)

	// SendSystemMessage broadcasts a system chat notification to all room members.
	SendSystemMessage(content string)

	// EndGame terminates the current game instance and broadcasts gameover details.
	EndGame(detail GameOverDetail)

	// ReturnToLobby resets the room to waiting lobby state and clears the active game instance.
	ReturnToLobby()

	// DestroyRoom terminates the room and kicks out all participants.
	DestroyRoom()

	// GetOwnerID returns the ID of the room owner.
	GetOwnerID() string
}

// Represents the game mode classification.
type GameMode string

const (
	ModeSingle      GameMode = "single"
	ModeDuel        GameMode = "duel"
	ModeMultiplayer GameMode = "multiplayer"
)

// Represents the status of room.
type RoomStatus string

const (
	StatusWaiting  RoomStatus = "waiting"
	StatusStarting RoomStatus = "starting"
	StatusPlaying  RoomStatus = "playing"
	StatusEnding   RoomStatus = "ending"
	StatusGameOver RoomStatus = "gameover"
)

// PlayerInfo represents the player data required to initialize a game session.
type PlayerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GameOverDetail describes the result and player rankings of a completed game session.
type GameOverDetail struct {
	Players []PlayerInfo `json:"players"`
	IsDraw  bool         `json:"isDraw,omitempty"`
}

// Game defines the factory properties and details of a game.
type Game interface {

	// Unique ID of the game
	Id() string

	// Name of the game in UI.
	Name() string

	// Short description of the game.
	Description() string

	// Mode describes the game mode type (single, duel, multiplayer).
	Mode() GameMode

	// Capacity returns the maximum player capacity of the game.
	// For ModeSingle it is 1, for ModeDuel it is 2, and for ModeMultiplayer it is the upper player limit.
	Capacity() int

	// StartGame creates and starts a game instance with the participating players and room context.
	StartGame(ctx RoomContext, players []PlayerInfo) (GameInstance, error)

	// Provides list of supported plugins.
	GetSupportedPlugins() []string
}

// GameInstance manages the state and logic rules of an active game session.
type GameInstance interface {

	// GetState returns the serializable game state view for a given player or spectator ID.
	GetState(playerID string) any

	// HasPlayer checks if the player ID is an active participant in this game session.
	HasPlayer(playerID string) bool

	// OnPlayerReconnect is called when a disconnected player rejoins the room.
	OnPlayerReconnect(playerID string)

	// OnPlayerDisconnect is called when a player temporarily disconnects during an active game.
	OnPlayerDisconnect(playerID string)

	// OnAction processes inputs/moves sent by players. The game instance triggers state broadcasts and EndGame via RoomContext.
	OnAction(playerID string, action string, payload json.RawMessage) error

	// OnTimeout is called when a disconnected player's grace timer expires.
	OnTimeout(playerID string)

	// InactivityDuration describes the seconds after which room will be closed.
	InactivityDuration() uint
}
