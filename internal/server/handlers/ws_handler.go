package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pulsar/internal/config"
	"pulsar/internal/logger"
	"pulsar/internal/server/domain"
	"pulsar/internal/server/protocol"
	"pulsar/internal/server/transport"

	"github.com/gorilla/websocket"
)

// WSHandler manages websocket upgrades, client registration, and the initial handshake.
type WSHandler struct {
	Hub              *domain.Hub
	Upgrader         websocket.Upgrader
	HandshakeTimeout time.Duration
}

// NewWSHandler creates an initialized WSHandler.
func NewWSHandler(hub *domain.Hub, upgrader websocket.Upgrader, handshakeTimeout time.Duration) *WSHandler {
	return &WSHandler{
		Hub:              hub,
		Upgrader:         upgrader,
		HandshakeTimeout: handshakeTimeout,
	}
}

// HandleWebsocket upgrades incoming HTTP connections to WebSocket, manages client registration and the initial handshake.
func (h *WSHandler) HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("client") != "nebula" {
		http.Error(w, "Forbidden: Missing or invalid client parameter", http.StatusForbidden)
		return
	}

	// Read client old id and name from query params
	requestedID := r.URL.Query().Get("id")
	requestedName := r.URL.Query().Get("name")

	id := h.Hub.AssignID(requestedID)
	name := strings.TrimSpace(requestedName)
	if len(name) == 0 {
		name = fmt.Sprintf("Guest_%s", id) // TODO: provide random 2 word name
	}

	conn, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	c := transport.NewClient(conn, id, name)

	c.OnMessage = func(msg []byte) {
		h.Hub.Broadcast <- domain.BroadcastMessage{Sender: c, Data: msg}
	}

	c.OnDisconnect = func() {
		h.Hub.Unregister <- c
	}

	h.Hub.Register <- c

	initialised := h.HandleClientInit(c)
	if !initialised {
		c.Conn.Close()
		h.Hub.Unregister <- c
		return
	}

	logger.Debugf("[WS] Handshake done: %s (%s)", c.Id, c.GetName())

	go c.Write()
	go c.Read()
}

// HandleClientInit handles client initialization.
func (h *WSHandler) HandleClientInit(client *transport.Client) bool {
	timeout := h.HandshakeTimeout
	if timeout <= 0 {
		cfg := config.LoadEnv()
		if cfg.InitWaitSecs > 0 {
			timeout = time.Duration(cfg.InitWaitSecs) * time.Second
		} else {
			timeout = 2 * time.Minute
		}
	}

	client.Conn.SetReadDeadline(time.Now().Add(timeout))
	mType, rawMsg, err := client.Conn.ReadMessage()
	if err != nil {
		logger.Warnf("[WS] Client disconnected due to missing hello message: ID %s (RemoteAddr: %s, error: %v)", client.Id, client.Conn.RemoteAddr(), err)
		return false
	}

	if mType == websocket.TextMessage {
		transport.LogWSMessage("Received", client.Id, client.GetName(), rawMsg)
	}

	if mType != websocket.TextMessage {
		logger.Warnf("[WS] Client disconnected due to missing hello message: invalid message type %d for ID %s", mType, client.Id)
		return false
	}

	var incoming protocol.ClientMessage
	if err := json.Unmarshal(rawMsg, &incoming); err != nil || incoming.Type != protocol.ActionHello {
		logger.Warnf("[WS] Client disconnected due to missing hello message: expected type '%s' for ID %s", protocol.ActionHello, client.Id)
		return false
	}

	var helloPayload protocol.ClientHelloEvent
	if incoming.Content != "" {
		_ = json.Unmarshal([]byte(incoming.Content), &helloPayload)
	}

	if helloPayload.Name != "" {
		client.SetName(protocol.ValidateHelloName(helloPayload.Name))
	}

	var roomId string
	if room := h.Hub.FindRoomByPlayerID(client.Id); room != nil {
		roomId = room.ID
	}

	content := protocol.ServerHiEvent{
		ID:           client.Id,
		Name:         client.GetName(),
		ExistingRoom: roomId,
	}
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return false
	}

	resp := protocol.ServerMessage{
		Type:    protocol.ActionHi,
		Content: string(contentBytes),
	}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		return false
	}

	client.Conn.SetReadDeadline(time.Time{})

	transport.LogWSMessage("Sent", client.Id, client.GetName(), respBytes)
	if err := client.Conn.WriteMessage(websocket.TextMessage, respBytes); err != nil {
		return false
	}

	return true
}
