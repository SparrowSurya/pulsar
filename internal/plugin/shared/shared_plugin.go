package shared_plugin

import (
	game "pulsar/internal/plugin/game"
)

// SharedPluginName represents the unique identifier of a shared room plugin.
type SharedPluginName string

// RoomPlugin defines a reusable, room-scoped shared feature (e.g., chat, voice).
type RoomPlugin interface {
	Name() SharedPluginName
	OnPlayerJoin(playerID string, playerName string, role game.UserRole)
	OnPlayerLeave(playerID string)
	OnAction(playerID string, actionType string, payload []byte) error
}
