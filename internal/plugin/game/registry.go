package game_plugin

import "fmt"

var registry = make(map[string]Game)

// Register inserts a Game plugin into the global registry.
func Register(g Game) {
	registry[g.Name()] = g
}

// Get retrieves a Game plugin by its registered name.
func Get(name string) (Game, error) {
	g, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("game plugin %q not found", name)
	}
	return g, nil
}

// List returns the names of all registered game plugins.
func List() []string {
	list := make([]string, 0, len(registry))
	for name := range registry {
		list = append(list, name)
	}
	return list
}

// GetGames returns all registered Game plugins.
func GetGames() []Game {
	list := make([]Game, 0, len(registry))
	for _, g := range registry {
		list = append(list, g)
	}
	return list
}
