package bingo

import (
	"encoding/json"
	"fmt"
	"pulsar/internal/config"
	game_plugin "pulsar/internal/plugin/game"
)

// BingoAction represents the action types supported by the Bingo game plugin.
type BingoAction string

const (
	ActionReady            BingoAction = "ready"
	MarkNumber             BingoAction = "mark"
	ActionPlayAgain        BingoAction = "request_play_again"
	ActionDeclinePlayAgain BingoAction = "decline_play_again"
	ActionBackToLobby      BingoAction = "back_to_lobby"
)

const (
	GameID          = "bingo"
	GameName        = "Bingo"
	GameDescription = "Classic 5x5 Bingo game with shared number calling and real-time line tracking."
	MinPlayers      = 2
	MaxPlayers      = 5
)

// Bingo implements game_plugin.Game for Bingo.
type Bingo struct{}

// NewGame creates a new Bingo game factory.
func NewGame() *Bingo {
	return &Bingo{}
}

// Id returns the unique identifier for Bingo.
func (b *Bingo) Id() string {
	return GameID
}

// Name returns the display name for Bingo.
func (b *Bingo) Name() string {
	return GameName
}

// Description returns a short description of Bingo.
func (b *Bingo) Description() string {
	return GameDescription
}

// Mode returns the game mode (ModeMultiplayer).
func (b *Bingo) Mode() game_plugin.GameMode {
	return game_plugin.ModeMultiplayer
}

// Capacity returns the maximum player limit (5).
func (b *Bingo) Capacity() int {
	return MaxPlayers
}

// GetSupportedPlugins returns the supported shared room plugins.
func (b *Bingo) GetSupportedPlugins() []string {
	return []string{"chat"}
}

// StartGame initializes a new Bingo game instance with the provided players.
func (b *Bingo) StartGame(ctx game_plugin.RoomContext, players []game_plugin.PlayerInfo) (game_plugin.GameInstance, error) {
	if len(players) < MinPlayers || len(players) > MaxPlayers {
		return nil, fmt.Errorf("bingo requires between %d and %d players (got %d)", MinPlayers, MaxPlayers, len(players))
	}
	return &Instance{
		ctx:   ctx,
		state: NewBingoState(players),
	}, nil
}

// Instance implements game_plugin.GameInstance for Bingo.
type Instance struct {
	ctx   game_plugin.RoomContext
	state *BingoState
}

// PlayerStateView holds the public or private view of an individual player.
type PlayerStateView struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Ready          bool          `json:"ready"`
	Disconnected   bool          `json:"disconnected"`
	Forfeited      bool          `json:"forfeited"`
	CompletedLines int           `json:"completedLines"`
	BingoLetters   string        `json:"bingoLetters"`
	Board          *[TotalCells]int  `json:"board,omitempty"`
	Marked         *[TotalCells]bool `json:"marked,omitempty"`
}

// StateView represents the serializable game state payload sent to clients.
type StateView struct {
	Status            string            `json:"status"`
	CurrentTurn       string            `json:"currentTurn"`
	MarkedNumbers     []int             `json:"markedNumbers"`
	Winners           []string          `json:"winners"`
	Players           []PlayerStateView `json:"players"`
	MyBoard           *[TotalCells]int  `json:"myBoard,omitempty"`
	MyMarked          *[TotalCells]bool `json:"myMarked,omitempty"`
	MyLines           int               `json:"myLines"`
	MyLetters         string            `json:"myLetters"`
	PlayAgainRequests map[string]bool   `json:"playAgainRequests"`
}

// ReadyPayload contains the setup parameters for a player readying up.
type ReadyPayload struct {
	Board  [TotalCells]int `json:"board"`
	Random bool            `json:"random"`
}

// MarkPayload contains the number selected by the active player.
type MarkPayload struct {
	Number int `json:"number"`
}

// HasPlayer checks whether the player ID is part of the game session.
func (inst *Instance) HasPlayer(playerID string) bool {
	_, ok := inst.state.Names[playerID]
	return ok
}

// OnPlayerReconnect restores a returning player's connected status.
func (inst *Instance) OnPlayerReconnect(playerID string) {
	delete(inst.state.Disconnected, playerID)
}

// OnPlayerDisconnect marks a player as temporarily disconnected.
func (inst *Instance) OnPlayerDisconnect(playerID string) {
	inst.state.Disconnected[playerID] = true
}

// OnTimeout handles forfeiture when a player's disconnect grace period expires.
func (inst *Instance) OnTimeout(playerID string) {
	gameOver, _ := inst.state.LeavePlayer(playerID)
	if gameOver {
		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
			inst.ctx.EndGame(inst.buildGameOverDetail())
		}
	} else {
		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
		}
	}
}

func (inst *Instance) buildGameOverDetail() game_plugin.GameOverDetail {
	detail := game_plugin.GameOverDetail{
		Players: make([]game_plugin.PlayerInfo, 0, len(inst.state.PlayerIDs)),
		IsDraw:  false,
	}
	for _, wid := range inst.state.Winners {
		detail.Players = append(detail.Players, game_plugin.PlayerInfo{
			ID:   wid,
			Name: inst.state.Names[wid],
		})
	}
	for _, pid := range inst.state.PlayerIDs {
		isWinner := false
		for _, wid := range inst.state.Winners {
			if wid == pid {
				isWinner = true
				break
			}
		}
		if !isWinner {
			detail.Players = append(detail.Players, game_plugin.PlayerInfo{
				ID:   pid,
				Name: inst.state.Names[pid],
			})
		}
	}
	return detail
}

// OnAction handles client actions for the Bingo game.
func (inst *Instance) OnAction(playerID string, action string, payload json.RawMessage) error {
	switch BingoAction(action) {
	case ActionReady:
		var data ReadyPayload
		if len(payload) > 0 && string(payload) != "null" {
			if err := json.Unmarshal(payload, &data); err != nil {
				return fmt.Errorf("invalid ready payload: %w", err)
			}
		} else {
			data.Random = true
		}

		if err := inst.state.SetReady(playerID, data.Board, data.Random); err != nil {
			return err
		}
		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
		}
		return nil

	case MarkNumber:
		var data MarkPayload
		if err := json.Unmarshal(payload, &data); err != nil {
			return fmt.Errorf("invalid mark payload: %w", err)
		}

		gameOver, _, err := inst.state.MakeMove(playerID, data.Number)
		if err != nil {
			return err
		}

		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
		}

		if gameOver {
			if inst.ctx != nil {
				inst.ctx.EndGame(inst.buildGameOverDetail())
			}
		}
		return nil

	case ActionPlayAgain:
		if inst.state.Status != "finished" {
			return fmt.Errorf("game is not finished")
		}
		inst.state.PlayAgainRequests[playerID] = true

		allAccepted := true
		for _, pid := range inst.state.PlayerIDs {
			if !inst.state.Forfeited[pid] && !inst.state.PlayAgainRequests[pid] {
				allAccepted = false
				break
			}
		}

		if allAccepted && len(inst.state.activePlayers()) >= MinPlayers {
			inst.state.Restart()
		}
		if inst.ctx != nil {
			inst.ctx.BroadcastGameState()
		}
		return nil

	case ActionDeclinePlayAgain, ActionBackToLobby:
		return nil

	default:
		return fmt.Errorf("unknown bingo action: %s", action)
	}
}

// GetState returns the customized state view for the requesting player or spectator.
func (inst *Instance) GetState(playerID string) any {
	state := inst.state
	isFinished := state.Status == "finished"

	playersView := make([]PlayerStateView, 0, len(state.PlayerIDs))
	for _, pid := range state.PlayerIDs {
		lines := state.CompletedLines[pid]
		letters := GetBingoLetters(lines)

		pv := PlayerStateView{
			ID:             pid,
			Name:           state.Names[pid],
			Ready:          state.Ready[pid],
			Disconnected:   state.Disconnected[pid],
			Forfeited:      state.Forfeited[pid],
			CompletedLines: lines,
			BingoLetters:   letters,
		}

		// Reveal board/marked if game is finished OR if this is the player's own view
		if isFinished || pid == playerID {
			b := state.Boards[pid]
			m := state.Marked[pid]
			pv.Board = &b
			pv.Marked = &m
		}

		playersView = append(playersView, pv)
	}

	var myBoard *[TotalCells]int
	var myMarked *[TotalCells]bool
	myLines := 0
	myLetters := ""

	if _, exists := state.Names[playerID]; exists {
		b := state.Boards[playerID]
		m := state.Marked[playerID]
		myBoard = &b
		myMarked = &m
		myLines = state.CompletedLines[playerID]
		myLetters = GetBingoLetters(myLines)
	}

	return StateView{
		Status:            state.Status,
		CurrentTurn:       state.CurrentTurn,
		MarkedNumbers:     state.MarkedNumbers,
		Winners:           state.Winners,
		Players:           playersView,
		MyBoard:           myBoard,
		MyMarked:          myMarked,
		MyLines:           myLines,
		MyLetters:         myLetters,
		PlayAgainRequests: state.PlayAgainRequests,
	}
}

// InactivityDuration returns the configured room inactivity duration.
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
