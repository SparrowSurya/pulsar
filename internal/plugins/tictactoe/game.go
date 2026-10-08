package tictactoe

import (
	"encoding/json"
	"fmt"
	"pulsar/internal/config"
	game_plugin "pulsar/internal/plugin/game"
	"pulsar/internal/server/protocol"
)

// Action represents in-game actions for Tic-Tac-Toe.
type Action string

const (
	ActionMarkCell Action = "mark_cell"
)

// TicTacToe implements game_plugin.Game.
type TicTacToe struct{}

// NewGame creates a new TicTacToe factory.
func NewGame() *TicTacToe {
	return &TicTacToe{}
}

// Id returns the unique id of the game.
func (t *TicTacToe) Id() string {
	return "tic-tac-toe"
}

// Name returns the name of the game.
func (t *TicTacToe) Name() string {
	return "Tic-Tac-Toe"
}

// Description return short description of the game.
func (t *TicTacToe) Description() string {
	return "Get three of your marks in a row."
}

// Mode returns the game mode.
func (t *TicTacToe) Mode() game_plugin.GameMode {
	return game_plugin.ModeDuel
}

// Capacity returns the maximum player limit.
func (t *TicTacToe) Capacity() int {
	return 2
}

// GetSupportedPlugins returns the supported shared plugins.
func (t *TicTacToe) GetSupportedPlugins() []string {
	return []string{"chat"}
}

// StartGame instantiates and starts a new TicTacToe instance with the given players.
func (t *TicTacToe) StartGame(ctx game_plugin.RoomContext, players []game_plugin.PlayerInfo) (game_plugin.GameInstance, error) {
	if len(players) != t.Capacity() {
		return nil, fmt.Errorf("%s requires exactly %d players", t.Name(), t.Capacity())
	}

	return &Instance{
		ctx:   ctx,
		state: NewState(players),
	}, nil
}

// Instance implements game_plugin.GameInstance for a single Tic-Tac-Toe game.
type Instance struct {
	ctx   game_plugin.RoomContext
	state *TicTacToeState
}

// HasPlayer checks if the player ID is participating in this game.
func (inst *Instance) HasPlayer(playerID string) bool {
	return inst.state.Players[0].ID == playerID || inst.state.Players[1].ID == playerID
}

// OnPlayerReconnect marks the player as active upon rejoining.
func (inst *Instance) OnPlayerReconnect(playerID string) {
	newDisconnected := make([]string, 0, len(inst.state.Disconnected))
	for _, id := range inst.state.Disconnected {
		if id != playerID {
			newDisconnected = append(newDisconnected, id)
		}
	}
	inst.state.Disconnected = newDisconnected
}

// OnPlayerDisconnect marks the player as temporarily disconnected.
func (inst *Instance) OnPlayerDisconnect(playerID string) {
	for _, id := range inst.state.Disconnected {
		if id == playerID {
			return
		}
	}
	inst.state.Disconnected = append(inst.state.Disconnected, playerID)
}

// OnTimeout handles forfeiture when a disconnected player's grace timer expires.
func (inst *Instance) OnTimeout(playerID string) {
	if otherPlayerID, ok := inst.state.LeavePlayer(playerID); ok {
		if inst.ctx != nil {
			if otherPlayerID != "" {
				inst.ctx.SendToPlayer(otherPlayerID, string(protocol.RoomChannel), protocol.ActionError, protocol.ErrorEvent{
					Error: &protocol.ErrorContent{
						Code:    protocol.ErrPlayerLeft,
						Message: "Opponent left the match",
					},
				})
			}
			inst.ctx.ReturnToLobby()
		}
	}
}

// MoveMarkCellPayload holds the mark coordinates.
type MoveMarkCellPayload struct {
	Cell int `json:"cell"` // cell index
}

// OnAction processes player in-game actions.
func (inst *Instance) OnAction(playerID string, action string, payload json.RawMessage) error {
	if Action(action) != ActionMarkCell {
		return fmt.Errorf("Unknown action: %s", action)
	}

	var data MoveMarkCellPayload
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("Invalid action payload: %w", err)
	}

	if err := inst.state.MakeMove(playerID, data.Cell); err != nil {
		return err
	}

	if inst.ctx != nil {
		inst.ctx.BroadcastGameState()
	}

	if inst.state.Status == TicTacToeGameoverStatus {
		if inst.ctx != nil {
			detail := game_plugin.GameOverDetail{
				IsDraw: inst.state.IsDraw,
			}
			if inst.state.Winner != "" {
				for _, p := range inst.state.Players {
					if p.ID == inst.state.Winner {
						detail.Players = append([]game_plugin.PlayerInfo{p.PlayerInfo}, detail.Players...)
					} else {
						detail.Players = append(detail.Players, p.PlayerInfo)
					}
				}
			} else {
				for _, p := range inst.state.Players {
					detail.Players = append(detail.Players, p.PlayerInfo)
				}
			}
			inst.ctx.EndGame(detail)
		}
	}

	return nil
}

// GetState returns the serializable game state view for a given player ID.
func (inst *Instance) GetState(playerID string) any {
	return *inst.state
}

// InactivityDuration returns the duration of inactivity (in seconds) before room shutdown.
func (inst *Instance) InactivityDuration() uint {
	inactivity := config.LoadEnv().RoomInactivitySecs
	if inactivity <= 0 {
		return 1800
	}
	return uint(inactivity)
}

func init() {
	game_plugin.Register(NewGame())
}
