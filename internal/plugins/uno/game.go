package uno

import (
	"encoding/json"
	"fmt"
	"pulsar/internal/config"
	game_plugin "pulsar/internal/plugin/game"
)

// Uno implements game_plugin.Game
type Uno struct{}

const (
	GameId          string = "uno"
	GameName        string = "Uno"
	GameDescription string = "UNO is a fast-paced card game where players match colors and numbers to get rid of their cards first."
	GameMinPlayers  int    = 2
	GameMaxPlayers  int    = 12
)

func NewGame() *Uno {
	return &Uno{}
}

func (u *Uno) Id() string {
	return GameId
}

func (u *Uno) Name() string {
	return GameName
}

func (u *Uno) Description() string {
	return GameDescription
}

func (u *Uno) Mode() game_plugin.GameMode {
	return game_plugin.ModeMultiplayer
}

func (u *Uno) Capacity() int {
	return GameMaxPlayers
}

func (u *Uno) GetSupportedPlugins() []string {
	return []string{"chat"}
}

func (u *Uno) StartGame(ctx game_plugin.RoomContext, players []game_plugin.PlayerInfo) (game_plugin.GameInstance, error) {
	if len(players) < GameMinPlayers || len(players) > GameMaxPlayers {
		return nil, fmt.Errorf("uno requires between %d and %d players", GameMinPlayers, GameMaxPlayers)
	}
	return &Instance{
		ctx:   ctx,
		state: *NewState(players),
	}, nil
}

// Instance implements game_plugin.GameInstance for a single uno game
type Instance struct {
	ctx   game_plugin.RoomContext
	state UnoState
}

// Status represents the game status.
type Status string

const (
	waiting  Status = "waiting"
	playing  Status = "playing"
	finished Status = "finished"
)

// StateView represents the serializable state of the game sent to clients.
type StateView struct {
	DiscardPile []Card        `json:"discardPile"`
	Players     []PlayerState `json:"players"`
	Turn        string        `json:"turn"`
}

func (inst *Instance) HasPlayer(playerID string) bool {
	_, exists := inst.state.players[playerID]
	return exists
}

func (inst *Instance) OnPlayerReconnect(playerID string) {
	delete(inst.state.disconnected, playerID)
}

func (inst *Instance) OnPlayerDisconnect(playerID string) {
	inst.state.disconnected[playerID] = true
}

func (inst *Instance) OnTimeout(playerID string) {
	if inst.state.turn == playerID {
		inst.state.NextTurn()
		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
		}
	}
}

func (inst *Instance) OnAction(playerID string, action string, payload json.RawMessage) error {
	updated, err := inst.state.MakeMove(playerID, Action(action), payload)
	if err != nil {
		return err
	}
	if updated && inst.ctx != nil {
		inst.ctx.BroadcastGameState()
	}
	return nil
}

// InactivityDuration returns the duration of inactivity (in seconds) before room shutdown.
func (inst *Instance) InactivityDuration() uint {
	inactivity := config.LoadEnv().RoomInactivitySecs
	if inactivity <= 0 {
		return 1800
	}
	return uint(inactivity)
}

// GetState returns the serializable game state view for a given player ID.
func (inst *Instance) GetState(playerID string) any {
	return inst.getStateView()
}

func (inst *Instance) getStateView() StateView {
	state := inst.state

	players := make([]PlayerState, 0, len(state.playerIDs))
	for _, playerID := range state.playerIDs {
		playerInfo, exists := state.players[playerID]
		if !exists {
			continue
		}
		player := PlayerState{
			Name:  playerInfo.Name,
			Cards: len(playerInfo.Cards),
		}
		players = append(players, player)
	}

	return StateView{
		DiscardPile: state.discardPile,
		Players:     players,
		Turn:        state.turn,
	}
}

func init() {
	game_plugin.Register(NewGame())
}
