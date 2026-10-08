package tictactoe

import (
	"errors"
	game_plugin "pulsar/internal/plugin/game"
)

// Mark represents the player mark symbol in Tic-Tac-Toe.
type Mark string

const (
	MarkX Mark = "X"
	MarkO Mark = "O"
)

// TicTacToeGameStatus represents the internal state status of a Tic-Tac-Toe match.
type TicTacToeGameStatus string

const (
	TicTacToePlayingStatus  TicTacToeGameStatus = "playing"
	TicTacToeGameoverStatus TicTacToeGameStatus = "gameover"
)

// TicTacToePlayer represents a player in Tic-Tac-Toe with their mark and base info.
type TicTacToePlayer struct {
	game_plugin.PlayerInfo
	Mark Mark `json:"mark"`
}

// TicTacToeState manages the state transitions and rules of a Tic-Tac-Toe game.
type TicTacToeState struct {
	Board        [9]string           `json:"board"`
	Players      [2]TicTacToePlayer  `json:"players"`
	Disconnected []string            `json:"disconnected"`
	Status       TicTacToeGameStatus `json:"status"`
	Turn         string              `json:"turn"`
	Winner       string              `json:"winner,omitempty"`
	IsDraw       bool                `json:"isDraw"`
	WinningLine  []int               `json:"winningLine,omitempty"`
}

// NewState instantiates a new Tic-Tac-Toe state with initialized player participants.
func NewState(players []game_plugin.PlayerInfo) *TicTacToeState {
	s := &TicTacToeState{
		Board:        [9]string{},
		Players:      [2]TicTacToePlayer{},
		Disconnected: []string{},
		Status:       TicTacToePlayingStatus,
	}

	marks := []Mark{MarkX, MarkO}
	for i := 0; i < len(players) && i < 2; i++ {
		s.Players[i] = TicTacToePlayer{
			PlayerInfo: players[i],
			Mark:       marks[i],
		}
	}
	if len(players) > 0 {
		s.Turn = s.Players[0].ID // First player starts
	}

	return s
}

// LeavePlayer handles a player leaving/forfeiting. Returns the other player's ID and whether the leave occurred.
func (b *TicTacToeState) LeavePlayer(playerID string) (string, bool) {
	isPlayer := (b.Players[0].ID == playerID || b.Players[1].ID == playerID)
	if !isPlayer {
		return "", false
	}

	if b.Status == TicTacToePlayingStatus {
		b.Status = TicTacToeGameoverStatus
		var remainingPlayerID string
		if b.Players[0].ID != playerID {
			remainingPlayerID = b.Players[0].ID
		} else {
			remainingPlayerID = b.Players[1].ID
		}

		b.Winner = ""
		return remainingPlayerID, true
	}

	return "", false
}

// MakeMove executes a turn and returns an error if invalid.
func (b *TicTacToeState) MakeMove(playerID string, cell int) error {
	if b.Status != TicTacToePlayingStatus {
		return errors.New("game is not currently active")
	}

	if b.Turn != playerID {
		return errors.New("it is not your turn")
	}

	if cell < 0 || cell > 8 {
		return errors.New("invalid cell index (must be 0-8)")
	}

	if b.Board[cell] != "" {
		return errors.New("cell is already occupied")
	}

	var mark Mark
	if b.Players[0].ID == playerID {
		mark = b.Players[0].Mark
	} else {
		mark = b.Players[1].Mark
	}
	b.Board[cell] = string(mark)

	if won, line := b.checkWin(string(mark)); won {
		b.Status = TicTacToeGameoverStatus
		b.Winner = playerID
		b.WinningLine = line
		return nil
	}

	if b.checkDraw() {
		b.Status = TicTacToeGameoverStatus
		b.Winner = ""
		b.IsDraw = true
		return nil
	}

	// Switch turn
	if b.Turn == b.Players[0].ID {
		b.Turn = b.Players[1].ID
	} else {
		b.Turn = b.Players[0].ID
	}

	return nil
}

func (b *TicTacToeState) checkWin(symbol string) (bool, []int) {
	lines := [8][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // cols
		{0, 4, 8}, {2, 4, 6}, // diagonals
	}

	for _, line := range lines {
		if b.Board[line[0]] == symbol && b.Board[line[1]] == symbol && b.Board[line[2]] == symbol {
			return true, []int{line[0], line[1], line[2]}
		}
	}
	return false, nil
}

func (b *TicTacToeState) checkDraw() bool {
	for _, cell := range b.Board {
		if cell == "" {
			return false
		}
	}
	return true
}
