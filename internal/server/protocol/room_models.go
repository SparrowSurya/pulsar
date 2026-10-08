package protocol

import game_plugin "pulsar/internal/plugin/game"

// RoomVisibility defines whether a room is publicly listed or private.
type RoomVisibility string

const (
	// RoomPublic indicates the room is open and discoverable without a join code.
	RoomPublic RoomVisibility = "public"
	// RoomPrivate indicates the room is hidden and protected by a join code.
	RoomPrivate RoomVisibility = "private"
)

// ClientCreateRoomEvent represents the room creation payload request from client.
type ClientCreateRoomEvent struct {
	GameID       string         `json:"gameId"`
	Visibility   RoomVisibility `json:"visibility"`
	Spectate     *bool          `json:"spectate,omitempty"`
	AllowPlugins []string       `json:"allowPlugins,omitempty"`
	MaxPlayers   int            `json:"maxPlayers,omitempty"`
}

// ServerRoomCreateEvent represents the room details returned upon room creation.
type ServerRoomCreateEvent struct {
	RoomID   string        `json:"roomId"`
	JoinCode string        `json:"joinCode,omitempty"`
	Error    *ErrorContent `json:"error,omitempty"`
}

// ClientJoinRoomEvent represents the payload sent by a client to join a room.
type ClientJoinRoomEvent struct {
	RoomID   string `json:"roomId"`
	Spectate bool   `json:"spectate,omitempty"`
	JoinCode string `json:"joinCode,omitempty"`
}

// ServerJoinRoom represents the server's response to a join_room request.
type ServerJoinRoom struct {
	RoomDetails *RoomDetails  `json:"roomDetails,omitempty"`
	Error       *ErrorContent `json:"error"`
}

// ServerRoomDetail represents the server's response or broadcast for room_detail.
type ServerRoomDetail struct {
	RoomDetails *RoomDetails  `json:"roomDetails,omitempty"`
	Error       *ErrorContent `json:"error,omitempty"`
}

// ServerGameoverDetails represents the payload broadcast on the room channel when a game finishes.
type ServerGameoverDetails struct {
	GameoverDetails *game_plugin.GameOverDetail `json:"gameoverDetails,omitempty"`
	Error           *ErrorContent               `json:"error,omitempty"`
}

// ClientChangeRoleEvent represents the payload sent by a client to switch role.
type ClientChangeRoleEvent struct {
	Role game_plugin.UserRole `json:"role"`
}

// ClientReadyEvent represents the payload sent by a client to toggle or set ready status.
type ClientReadyEvent struct {
	Ready bool `json:"ready"`
}

// LobbyPlayer represents a participating player in a room info broadcast.
type LobbyPlayer struct {
	game_plugin.PlayerInfo
	Ready bool `json:"ready"`
}

// RoomDetails represents the complete settings and current metadata of a room.
type RoomDetails struct {
	RoomID       string                 `json:"roomId"`
	GameID       string                 `json:"gameId"`
	GameName     string                 `json:"gameName"`
	GameMode     game_plugin.GameMode   `json:"gameMode"`
	Status       game_plugin.RoomStatus `json:"status"`
	OwnerID      string                 `json:"ownerId"`
	Visibility   RoomVisibility         `json:"visibility"`
	Spectate     bool                   `json:"spectate"`
	AllowPlugins []string               `json:"allowPlugins"`
	MaxPlayers   int                    `json:"maxPlayers"`
	JoinCode     string                 `json:"joinCode,omitempty"`
	Players      []LobbyPlayer          `json:"players"`
	Spectators   int                    `json:"spectators"`
}
