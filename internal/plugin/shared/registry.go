package shared_plugin

import (
	"fmt"
	game "pulsar/internal/plugin/game"
)

var registry = make(map[SharedPluginName]func(ctx game.RoomContext) RoomPlugin)

// Register inserts a RoomPlugin factory into the global registry.
func Register(name SharedPluginName, factory func(ctx game.RoomContext) RoomPlugin) {
	registry[name] = factory
}

// GetFactory retrieves a RoomPlugin factory from the registry by name.
func GetFactory(name SharedPluginName) (func(ctx game.RoomContext) RoomPlugin, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("shared plugin %q not found", name)
	}
	return factory, nil
}

// List returns a slice of all registered plugin names.
func List() []SharedPluginName {
	list := make([]SharedPluginName, 0, len(registry))
	for name := range registry {
		list = append(list, name)
	}
	return list
}
