package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	game_plugin "pulsar/internal/plugin/game"
)

// GameDetailResponse holds serializable metadata for a registered game.
type GameDetailResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Mode        string   `json:"mode"`
	Capacity    int      `json:"capacity"`
	Plugins     []string `json:"plugins,omitempty"`
}

// HandleListGames returns the list of all registered games.
func HandleListGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	games := game_plugin.GetGames()
	features := make([]GameDetailResponse, 0, len(games))

	for _, g := range games {
		features = append(features, GameDetailResponse{
			ID:          strings.ToLower(g.Id()),
			Name:        g.Name(),
			Description: g.Description(),
			Mode:        string(g.Mode()),
			Capacity:    g.Capacity(),
			Plugins:     g.GetSupportedPlugins(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(features)
}
