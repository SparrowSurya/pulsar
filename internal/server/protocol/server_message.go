package protocol

// ServerMessage represents an outgoing message envelope sent from the server to clients.
type ServerMessage struct {
	Channel   Channel `json:"channel,omitempty"`   // Channel name ("room", "game")
	Type      string  `json:"type"`                // Action or event type (e.g., "chat", "info", "error")
	Timestamp int64   `json:"timestamp,omitempty"` // Unix timestamp in seconds
	Content   string  `json:"content,omitempty"`   // Serialized message payload or content string
}

// ServerHiEvent represents the handshake response sent by the server to the client.
type ServerHiEvent struct {
	ID           string `json:"id"`           // Server-assigned client ID
	Name         string `json:"name"`         // Confirmed client nickname
	ExistingRoom string `json:"existingRoom"` // Existing room joined by the user
}

// ServerRenameEvent represents the server response to a client's rename request.
type ServerRenameEvent struct {
	Name  string        `json:"name"`  // New name if successful, old name if error
	Error *ErrorContent `json:"error"` // Error description if failed, null if successful
}

// ErrorContent represents the error for some event.
type ErrorContent struct {
	Code    string         `json:"code"`    // Error code associated with type
	Message string         `json:"message"` // Error message representation
	Args    map[string]any `json:"args"`    // Arguments to the error
}

// ErrorEvent represents a generic error event payload sent on a channel.
type ErrorEvent struct {
	Error *ErrorContent `json:"error"`
}

