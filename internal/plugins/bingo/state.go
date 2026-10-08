package bingo

import (
	"errors"
	"fmt"
	"math/rand"
	game_plugin "pulsar/internal/plugin/game"
	"time"
)

const (
	GridSize      = 5
	TotalCells    = 25
	RequiredLines = 5
)

// BingoState manages the pure state machine logic of a 5x5 Bingo game session.
type BingoState struct {
	Boards            map[string][TotalCells]int  // playerID -> 25 board numbers (1..25)
	Marked            map[string][TotalCells]bool // playerID -> marked cell flags
	Ready             map[string]bool             // playerID -> ready state
	Names             map[string]string           // playerID -> display name
	Disconnected      map[string]bool             // playerID -> disconnected flag
	Forfeited         map[string]bool             // playerID -> forfeited flag
	PlayerIDs         []string                    // ordered list of participating player IDs
	MarkedNumbers     []int                       // chronological history of called numbers
	MarkedSet         map[int]bool                // set of called numbers
	Status            string                      // "setup", "playing", "finished"
	CurrentTurn       string                      // playerID whose turn it is to call a number
	Winners           []string                    // playerIDs who achieved BINGO
	CompletedLines    map[string]int              // playerID -> completed line count
	PlayAgainRequests map[string]bool             // playerID -> wants to play again
	rng               *rand.Rand
}

// NewBingoState initializes a new Bingo game state with the given players.
func NewBingoState(players []game_plugin.PlayerInfo) *BingoState {
	s := &BingoState{
		Boards:            make(map[string][TotalCells]int),
		Marked:            make(map[string][TotalCells]bool),
		Ready:             make(map[string]bool),
		Names:             make(map[string]string),
		Disconnected:      make(map[string]bool),
		Forfeited:         make(map[string]bool),
		PlayerIDs:         make([]string, 0, len(players)),
		MarkedNumbers:     make([]int, 0),
		MarkedSet:         make(map[int]bool),
		Status:            "setup",
		Winners:           make([]string, 0),
		CompletedLines:    make(map[string]int),
		PlayAgainRequests: make(map[string]bool),
		rng:               rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, p := range players {
		s.PlayerIDs = append(s.PlayerIDs, p.ID)
		s.Names[p.ID] = p.Name
		s.Boards[p.ID] = [TotalCells]int{}
		s.Marked[p.ID] = [TotalCells]bool{}
		s.CompletedLines[p.ID] = 0
	}

	if len(s.PlayerIDs) > 0 {
		s.CurrentTurn = s.PlayerIDs[0]
	}

	return s
}

// ValidateBoard checks that a board contains exactly the numbers 1 through 25 with no duplicates.
func ValidateBoard(board [TotalCells]int) error {
	seen := make(map[int]bool)
	for i, val := range board {
		if val < 1 || val > TotalCells {
			return fmt.Errorf("cell %d has invalid number %d (must be between 1 and %d)", i, val, TotalCells)
		}
		if seen[val] {
			return fmt.Errorf("duplicate number %d found at cell %d", val, i)
		}
		seen[val] = true
	}
	if len(seen) != TotalCells {
		return fmt.Errorf("board must contain all numbers from 1 to %d", TotalCells)
	}
	return nil
}

// GenerateRandomBoard generates a random 5x5 permutation of numbers 1 through 25.
func (b *BingoState) GenerateRandomBoard() [TotalCells]int {
	nums := make([]int, TotalCells)
	for i := 0; i < TotalCells; i++ {
		nums[i] = i + 1
	}

	b.rng.Shuffle(TotalCells, func(i, j int) {
		nums[i], nums[j] = nums[j], nums[i]
	})

	var board [TotalCells]int
	copy(board[:], nums)
	return board
}

// SetReady configures the player's board and flags them as ready.
// If all active players are ready, the game automatically transitions to "playing".
func (b *BingoState) SetReady(playerID string, board [TotalCells]int, autoGenerate bool) error {
	if b.Status != "setup" {
		return errors.New("cannot configure board: game is not in setup phase")
	}

	if _, exists := b.Names[playerID]; !exists {
		return errors.New("player not in game session")
	}

	if autoGenerate {
		board = b.GenerateRandomBoard()
	} else {
		if err := ValidateBoard(board); err != nil {
			return err
		}
	}

	b.Boards[playerID] = board
	b.Ready[playerID] = true

	// Check if all players are ready
	allReady := true
	for _, pid := range b.PlayerIDs {
		if !b.Forfeited[pid] && !b.Ready[pid] {
			allReady = false
			break
		}
	}

	if allReady && len(b.activePlayers()) >= 2 {
		b.Status = "playing"
		// Ensure current turn is set to the first active player
		for _, pid := range b.PlayerIDs {
			if !b.Forfeited[pid] && !b.Disconnected[pid] {
				b.CurrentTurn = pid
				break
			}
		}
	}

	return nil
}

// MakeMove marks a called number on all players' boards, checks for winners, and advances turn.
func (b *BingoState) MakeMove(playerID string, number int) (bool, []string, error) {
	if b.Status != "playing" {
		return false, nil, errors.New("game is not currently active")
	}

	if b.CurrentTurn != playerID {
		return false, nil, errors.New("it is not your turn")
	}

	if number < 1 || number > TotalCells {
		return false, nil, fmt.Errorf("invalid number %d (must be between 1 and %d)", number, TotalCells)
	}

	if b.MarkedSet[number] {
		return false, nil, fmt.Errorf("number %d is already marked", number)
	}

	// Record marked number
	b.MarkedNumbers = append(b.MarkedNumbers, number)
	b.MarkedSet[number] = true

	// Mark number on every player's board and update line counts
	var newWinners []string
	for _, pid := range b.PlayerIDs {
		board := b.Boards[pid]
		marked := b.Marked[pid]

		for i := 0; i < TotalCells; i++ {
			if board[i] == number {
				marked[i] = true
				break
			}
		}
		b.Marked[pid] = marked

		lines := b.CalculateLines(marked)
		b.CompletedLines[pid] = lines

		if lines >= RequiredLines && !b.Forfeited[pid] {
			newWinners = append(newWinners, pid)
		}
	}

	// Check for game completion
	if len(newWinners) > 0 {
		b.Status = "finished"
		b.Winners = newWinners
		return true, b.Winners, nil
	}

	// Check if all numbers have been marked
	if len(b.MarkedNumbers) >= TotalCells {
		b.Status = "finished"
		// If no one got 5 lines, find player(s) with highest line count
		maxLines := -1
		for _, pid := range b.PlayerIDs {
			if !b.Forfeited[pid] && b.CompletedLines[pid] > maxLines {
				maxLines = b.CompletedLines[pid]
			}
		}
		for _, pid := range b.PlayerIDs {
			if !b.Forfeited[pid] && b.CompletedLines[pid] == maxLines {
				newWinners = append(newWinners, pid)
			}
		}
		b.Winners = newWinners
		return true, b.Winners, nil
	}

	// Advance turn to next active player
	b.advanceTurn()

	return false, nil, nil
}

// CalculateLines computes the count of completed rows, columns, and diagonals on a 5x5 board.
func (b *BingoState) CalculateLines(marked [TotalCells]bool) int {
	lines := 0

	// 5 Rows
	for r := 0; r < GridSize; r++ {
		rowComplete := true
		for c := 0; c < GridSize; c++ {
			if !marked[r*GridSize+c] {
				rowComplete = false
				break
			}
		}
		if rowComplete {
			lines++
		}
	}

	// 5 Columns
	for c := 0; c < GridSize; c++ {
		colComplete := true
		for r := 0; r < GridSize; r++ {
			if !marked[r*GridSize+c] {
				colComplete = false
				break
			}
		}
		if colComplete {
			lines++
		}
	}

	// Main diagonal (top-left to bottom-right: 0, 6, 12, 18, 24)
	diag1 := true
	for i := 0; i < GridSize; i++ {
		if !marked[i*GridSize+i] {
			diag1 = false
			break
		}
	}
	if diag1 {
		lines++
	}

	// Anti diagonal (top-right to bottom-left: 4, 8, 12, 16, 20)
	diag2 := true
	for i := 0; i < GridSize; i++ {
		if !marked[i*GridSize+(GridSize-1-i)] {
			diag2 = false
			break
		}
	}
	if diag2 {
		lines++
	}

	return lines
}

// GetBingoLetters returns the B-I-N-G-O progression string corresponding to the completed line count.
func GetBingoLetters(lines int) string {
	switch {
	case lines <= 0:
		return ""
	case lines == 1:
		return "B"
	case lines == 2:
		return "BI"
	case lines == 3:
		return "BIN"
	case lines == 4:
		return "BING"
	default:
		return "BINGO"
	}
}

// LeavePlayer handles a player forfeiting or disconnecting permanently.
func (b *BingoState) LeavePlayer(playerID string) (bool, []string) {
	if _, exists := b.Names[playerID]; !exists {
		return false, nil
	}

	b.Forfeited[playerID] = true

	active := b.activePlayers()
	if b.Status == "setup" {
		if len(active) < 2 && len(b.PlayerIDs) >= 2 {
			b.Status = "finished"
			b.Winners = active
			return true, b.Winners
		}
		return false, nil
	}

	if b.Status == "playing" {
		if len(active) == 1 {
			b.Status = "finished"
			b.Winners = active
			return true, b.Winners
		} else if len(active) == 0 {
			b.Status = "finished"
			b.Winners = nil
			return true, nil
		}

		if b.CurrentTurn == playerID {
			b.advanceTurn()
		}
	}

	return false, nil
}

// NextTurn passes turn to the next active player.
func (b *BingoState) NextTurn() {
	b.advanceTurn()
}

// Restart resets the state for a new round with the same participants.
func (b *BingoState) Restart() {
	b.Boards = make(map[string][TotalCells]int)
	b.Marked = make(map[string][TotalCells]bool)
	b.Ready = make(map[string]bool)
	b.MarkedNumbers = make([]int, 0)
	b.MarkedSet = make(map[int]bool)
	b.Status = "setup"
	b.Winners = make([]string, 0)
	b.CompletedLines = make(map[string]int)
	b.PlayAgainRequests = make(map[string]bool)

	for _, pid := range b.PlayerIDs {
		b.Boards[pid] = [TotalCells]int{}
		b.Marked[pid] = [TotalCells]bool{}
		b.CompletedLines[pid] = 0
		b.Forfeited[pid] = false
	}

	if len(b.PlayerIDs) > 0 {
		b.CurrentTurn = b.PlayerIDs[0]
	}
}

func (b *BingoState) advanceTurn() {
	if len(b.PlayerIDs) == 0 {
		return
	}

	currIdx := -1
	for i, pid := range b.PlayerIDs {
		if pid == b.CurrentTurn {
			currIdx = i
			break
		}
	}

	for i := 1; i <= len(b.PlayerIDs); i++ {
		nextIdx := (currIdx + i) % len(b.PlayerIDs)
		pid := b.PlayerIDs[nextIdx]
		if !b.Forfeited[pid] && !b.Disconnected[pid] {
			b.CurrentTurn = pid
			return
		}
	}
}

func (b *BingoState) activePlayers() []string {
	var active []string
	for _, pid := range b.PlayerIDs {
		if !b.Forfeited[pid] {
			active = append(active, pid)
		}
	}
	return active
}
