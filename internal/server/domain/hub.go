package domain

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"sync"
	"time"

	"pulsar/internal/logger"
	"pulsar/internal/server/protocol"
	"pulsar/internal/server/transport"
)

// BroadcastMessage wraps raw payload data with a reference to its sender client.
type BroadcastMessage struct {
	Sender *transport.Client
	Data   []byte
}

// Hub coordinates all connected clients, active game rooms, and message broadcasting.
type Hub struct {
	Mu         sync.RWMutex               // Synchronization mutex protecting clients, rooms, and known IDs
	Clients    map[*transport.Client]bool // Active connected clients in the hub
	Rooms      map[string]*Room           // Active game rooms indexed by room ID
	Broadcast  chan BroadcastMessage      // Channel for receiving raw broadcast messages from clients
	Register   chan *transport.Client     // Channel to register a new client
	Unregister chan *transport.Client     // Channel to unregister a disconnected client
	KnownIDs   map[string]bool            // Set of all known IDs registered on the server (active and disconnected)
}

// NewHub instantiates and returns an initialized Hub.
func NewHub() *Hub {
	return &Hub{
		Clients:    make(map[*transport.Client]bool),
		Rooms:      make(map[string]*Room),
		Broadcast:  make(chan BroadcastMessage, 256),
		Register:   make(chan *transport.Client, 64),
		Unregister: make(chan *transport.Client, 64),
		KnownIDs:   make(map[string]bool),
	}
}

// randomAlphanumeric generates a cryptographically secure random string of uppercase alphanumeric characters.
func randomAlphanumeric(length int, fallback string) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			logger.Warnln("[HUB] Failed to generate random string. Returning fallback value.")
			return fallback
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// GenerateRoomID generates a short 5-character alphanumeric uppercase room identifier.
func GenerateRoomID() string {
	return randomAlphanumeric(5, "ROOMX")
}

func generateRoomID() string {
	return GenerateRoomID()
}

// GenerateJoinCode generates a short 4-character alphanumeric uppercase room join code.
func GenerateJoinCode() string {
	return randomAlphanumeric(4, "CODE")
}

func generateJoinCode() string {
	return GenerateJoinCode()
}

// generateID generates a cryptographically secure random string ID of configured length.
func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const idLength = 8

	b := make([]byte, idLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return ""
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// Run executes the main event select loop of the Hub.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.Register:
			h.Mu.Lock()
			h.Clients[c] = true
			h.KnownIDs[c.Id] = true
			h.Mu.Unlock()
			logger.Debugf("[HUB] Client registered: %s (ID: %s, RemoteAddr: %s)", c.GetName(), c.Id, c.Conn.RemoteAddr())

		case c := <-h.Unregister:
			h.Mu.Lock()
			if _, ok := h.Clients[c]; ok {
				roomID := c.GetCurrentRoom()
				delete(h.Clients, c)
				c.Close()
				h.Mu.Unlock()
				logger.Debugf("[HUB] Client unregistered: %s (ID: %s)", c.GetName(), c.Id)

				if roomID != "" {
					h.Mu.RLock()
					r, ok := h.Rooms[roomID]
					h.Mu.RUnlock()
					if ok {
						r.Leave(c)
					}
				}
			} else {
				h.Mu.Unlock()
			}

		case bm := <-h.Broadcast:
			var incoming protocol.ClientMessage
			if err := json.Unmarshal(bm.Data, &incoming); err != nil {
				logger.Errorln("[HUB] unmarshal broadcast error:", err)
				continue
			}
			if incoming.Content == "" {
				incoming.Content = string(bm.Data)
			}

			switch incoming.Channel {
			case protocol.RoomChannel:
				h.handleRoomAction(bm.Sender, &incoming)
			case protocol.GameChannel:
				h.handleGameAction(bm.Sender, &incoming)
			default:
				h.handleSessionAction(bm.Sender, &incoming)
			}
		}
	}
}

// DeleteRoom removes a room from the hub and implements HubController.
func (h *Hub) DeleteRoom(roomID string) {
	h.Mu.Lock()
	delete(h.Rooms, roomID)
	h.Mu.Unlock()
}

// SendError sends a structured ErrorEvent message to a client on a specific channel and implements HubController.
func (h *Hub) SendError(c *transport.Client, channel protocol.Channel, code, errMsg string) {
	event := protocol.ErrorEvent{
		Error: &protocol.ErrorContent{
			Code:    code,
			Message: errMsg,
		},
	}
	content, err := json.Marshal(event)
	if err != nil {
		return
	}
	msg := protocol.ServerMessage{
		Channel:   channel,
		Type:      protocol.ActionError,
		Content:   string(content),
		Timestamp: time.Now().Unix(),
	}
	data, err := json.Marshal(msg)
	if err == nil {
		c.SendMsg(data)
	}
}

// FindClientByID searches for a registered client using their unique identifier.
func (h *Hub) FindClientByID(id string) (*transport.Client, bool) {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	for client := range h.Clients {
		if client.Id == id {
			return client, true
		}
	}
	return nil, false
}

// FindRoomByPlayerID finds the room for which player ID is a participant.
func (h *Hub) FindRoomByPlayerID(id string) *Room {
	h.Mu.RLock()
	defer h.Mu.RUnlock()

	for _, room := range h.Rooms {
		if room.HasPlayer(id) {
			return room
		}
	}

	return nil
}

func (h *Hub) isIDActiveLocked(id string) bool {
	for client := range h.Clients {
		if client.Id == id {
			return true
		}
	}
	return false
}

// IsIDActive returns true if a client with the given ID is currently connected.
func (h *Hub) IsIDActive(id string) bool {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	return h.isIDActiveLocked(id)
}

// IsIDTakenAndDisconnected returns true if the ID was previously registered and the client is currently disconnected.
func (h *Hub) IsIDTakenAndDisconnected(id string) bool {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	if !h.KnownIDs[id] {
		return false
	}
	return !h.isIDActiveLocked(id)
}

// AssignID determines and returns the appropriate ID for a connecting client.
// If requestedID is taken but disconnected, it is assigned.
// If requestedID is active, unknown, or empty, a new unique ID is generated.
func (h *Hub) AssignID(requestedID string) string {
	h.Mu.RLock()
	defer h.Mu.RUnlock()

	if requestedID != "" && h.KnownIDs[requestedID] && !h.isIDActiveLocked(requestedID) {
		return requestedID
	}

	for {
		newID := generateID()
		if !h.isIDActiveLocked(newID) {
			return newID
		}
	}
}
