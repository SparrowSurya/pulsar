package protocol

// Session & Handshake Actions
const (
	ActionHello  = "hello"
	ActionHi     = "hi"
	ActionRename = "rename"
	ActionError  = "error"
)

// Room Actions & Events
const (
	ActionCreateRoom = "create_room"
	ActionJoinRoom   = "join_room"
	ActionLeaveRoom  = "leave_room"
	ActionChangeRole = "change_role"
	ActionReady      = "ready"
	ActionStartGame  = "start_game"
	ActionChat       = "chat"
	ActionRoomDetail = "room_detail"
	ActionGameover   = "gameover"
)

// In-Game Actions & Events
const (
	ActionStateUpdate      = "state_update"
	ActionRequestPlayAgain = "request_play_again"
	ActionDeclinePlayAgain = "decline_play_again"
	ActionBackToLobby      = "back_to_lobby"
)

// Error Codes
const (
	ErrInvalidFormat         = "invalid_format"
	ErrInitTimeout           = "init_timeout"
	ErrInvalidName           = "invalid_name"
	ErrInvalidGameID         = "invalid_game_id"
	ErrPlayerAlreadyPlaying  = "player_already_playing"
	ErrMissingRoomID         = "missing_room_id"
	ErrInvalidRoomID         = "invalid_room_id"
	ErrMissingJoinCode       = "missing_join_code"
	ErrInvalidJoinCode       = "invalid_join_code"
	ErrRoomNotFound          = "room_not_found"
	ErrSpectatedNotAllowed   = "spectate_not_allowed"
	ErrPlayerSlotUnavailable = "player_slot_unavailable"
	ErrNotPermitted          = "not_permitted"
	ErrInvalidRole           = "invalid_role"
	ErrPlayerLeft            = "player_left"
)
