package protocol

// ClientMessage represents an incoming message envelope sent by a client over WebSocket.
type ClientMessage struct {
	Channel Channel `json:"channel,omitempty"` // Target channel (e.g., "room", "game")
	Type    string  `json:"type"`              // Action or message type
	Content string  `json:"content,omitempty"` // Message payload or raw content
}

// ClientHelloEvent represents the handshake payload inside the hello message content.
type ClientHelloEvent struct {
	ID   string `json:"id,omitempty"`   // Optional client ID from previous session for rejoining
	Name string `json:"name,omitempty"` // Desired player nickname
}

// ClientRenameEvent represents a client rename request.
type ClientRenameEvent struct {
	Type string `json:"type"` // "rename"
	Name string `json:"name"` // Desired new display name
}
