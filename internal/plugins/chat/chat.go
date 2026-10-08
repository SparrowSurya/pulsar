package chat

import (
	game_plugin "pulsar/internal/plugin/game"
	shared_plugin "pulsar/internal/plugin/shared"
	"sync"
)

const PluginChat shared_plugin.SharedPluginName = "chat"

// ChatPlugin implements shared_plugin.RoomPlugin for room text chat functionality.
type ChatPlugin struct {
	ctx   game_plugin.RoomContext
	mu    sync.RWMutex
	names map[string]string // playerID -> playerName
}

func NewChatPlugin(ctx game_plugin.RoomContext) shared_plugin.RoomPlugin {
	return &ChatPlugin{
		ctx:   ctx,
		names: make(map[string]string),
	}
}

// Name returns the unique identifier of the plugin.
func (c *ChatPlugin) Name() shared_plugin.SharedPluginName {
	return PluginChat
}

// OnPlayerJoin saves the player's ID and name mapping.
func (c *ChatPlugin) OnPlayerJoin(playerID string, playerName string, role game_plugin.UserRole) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names[playerID] = playerName
}

// OnPlayerLeave removes the player's mapping.
func (c *ChatPlugin) OnPlayerLeave(playerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.names, playerID)
}

// OnAction processes the chat message and broadcasts it.
func (c *ChatPlugin) OnAction(playerID string, actionType string, payload []byte) error {
	if actionType != "chat" {
		return nil
	}

	c.mu.RLock()
	senderName, ok := c.names[playerID]
	c.mu.RUnlock()
	if !ok {
		senderName = "Unknown"
	}

	// Payload is the raw string of the chat message.
	c.ctx.Broadcast("room", "chat", map[string]any{
		"senderId":   playerID,
		"senderName": senderName,
		"content":    string(payload),
	})
	return nil
}

func init() {
	shared_plugin.Register(PluginChat, func(ctx game_plugin.RoomContext) shared_plugin.RoomPlugin {
		return NewChatPlugin(ctx)
	})
}
