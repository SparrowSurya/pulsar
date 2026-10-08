package protocol

// Channel represents a websocket communication channel.
type Channel string

const (
	// GameChannel represents the game channel for active game updates and actions.
	GameChannel Channel = "game"
	// RoomChannel represents the room channel for room-specific events and plugin notifications.
	RoomChannel Channel = "room"
)
