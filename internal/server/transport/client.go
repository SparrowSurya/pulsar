package transport

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"pulsar/internal/config"
	"pulsar/internal/logger"

	"github.com/gorilla/websocket"
)

func getWriteWait() time.Duration {
	secs := config.LoadEnv().WsWriteWaitSecs
	if secs <= 0 {
		secs = 10
	}
	return time.Duration(secs) * time.Second
}

func getPongWait() time.Duration {
	secs := config.LoadEnv().WsPongWaitSecs
	if secs <= 0 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

func getPingPeriod(pWait time.Duration) time.Duration {
	return (pWait * 9) / 10
}

func getMaxMessageSize() int64 {
	sz := config.LoadEnv().WsMaxMessageSizeBytes
	if sz <= 0 {
		sz = 512 * 1024
	}
	return sz
}

// LogWSMessage logs the message type, channel, and recipient/sender for WebSocket messages.
func LogWSMessage(direction, id, name string, msg []byte) {
	var hdr struct {
		Type    string `json:"type"`
		Action  string `json:"action"`
		Channel string `json:"channel"`
	}
	_ = json.Unmarshal(msg, &hdr)
	t := hdr.Type
	if t == "" {
		t = hdr.Action
	}
	if direction == "Sent" {
		logger.Debugf("[WS] Sent to %s (%s): type=%s, channel=%s", id, name, t, hdr.Channel)
	} else {
		logger.Debugf("[WS] Received from %s (%s): type=%s, channel=%s", id, name, t, hdr.Channel)
	}
}

// Client represents a connected websocket client session.
// It manages low-level websocket read/write operations and propagates events via callbacks.
type Client struct {
	Mu sync.RWMutex
	// Conn is the underlying websocket connection.
	Conn *websocket.Conn
	// Send is a channel used to queue outgoing messages to the websocket client.
	Send chan []byte
	// Id is the unique identifier for the client session.
	Id string
	// Name is the display nickname of the client in the chat room.
	Name string
	// CurrentRoom tracks the room ID the client is in (empty string if not in a room).
	CurrentRoom string
	// OnMessage is triggered whenever a valid text message is received from the client.
	OnMessage func([]byte)
	// OnDisconnect is triggered when the connection is closed or lost.
	OnDisconnect func()

	closeOnce sync.Once
	closed    atomic.Bool
}

// NewClient instantiates a new Client wrapper around a websocket connection.
func NewClient(conn *websocket.Conn, id, name string) *Client {
	return &Client{
		Conn: conn,
		Send: make(chan []byte, 256),
		Id:   id,
		Name: name,
	}
}

// GetName returns the client's display nickname in a thread-safe manner.
func (c *Client) GetName() string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return c.Name
}

// SetName updates the client's display nickname in a thread-safe manner.
func (c *Client) SetName(name string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Name = name
}

// GetCurrentRoom returns the client's active room ID in a thread-safe manner.
func (c *Client) GetCurrentRoom() string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return c.CurrentRoom
}

// SetCurrentRoom updates the client's active room ID in a thread-safe manner.
func (c *Client) SetCurrentRoom(roomID string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.CurrentRoom = roomID
}

// SendMsg attempts to queue a message for transmission without blocking.
// It returns true if queued, or false if the client is closed or buffer is full.
func (c *Client) SendMsg(msg []byte) bool {
	if c.closed.Load() {
		return false
	}
	select {
	case c.Send <- msg:
		return true
	default:
		c.Close()
		return false
	}
}

// Close safely shuts down the client connection and closes the send channel once.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.Send)
		c.Conn.Close()
	})
}

// Write pumps messages from the c.Send channel to the client's websocket connection.
// It runs in its own goroutine and terminates when c.Send is closed.
func (c *Client) Write() {
	pWait := getPongWait()
	pPeriod := getPingPeriod(pWait)
	ticker := time.NewTicker(pPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		wWait := getWriteWait()
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(wWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			LogWSMessage("Sent", c.Id, c.GetName(), msg)
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				logger.Errorln("Client.Write error:", err)
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(wWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Read reads messages from the client's websocket connection and triggers callbacks.
// It runs in its own goroutine and blocks until the connection is closed.
func (c *Client) Read() {
	defer func() {
		if c.OnDisconnect != nil {
			c.OnDisconnect()
		}
		c.Close()
	}()

	pWait := getPongWait()
	c.Conn.SetReadLimit(getMaxMessageSize())
	c.Conn.SetReadDeadline(time.Now().Add(pWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(getPongWait()))
		return nil
	})

	for {
		mType, msg, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorln("Client.Read error:", err)
			}
			break
		}

		if mType == websocket.TextMessage {
			LogWSMessage("Received", c.Id, c.GetName(), msg)
			if c.OnMessage != nil {
				c.OnMessage(msg)
			}
		}
	}
}
