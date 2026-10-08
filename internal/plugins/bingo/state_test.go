package bingo

import (
	game_plugin "pulsar/internal/plugin/game"
	"testing"
)

func TestBingoState_SetupAndReadyValidation(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	}
	state := NewBingoState(players)

	if state.Status != "setup" {
		t.Fatalf("expected status 'setup', got '%s'", state.Status)
	}

	// 1. Invalid board with duplicate numbers
	var dupBoard [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		dupBoard[i] = 1 // all 1s
	}
	err := state.SetReady("p1", dupBoard, false)
	if err == nil {
		t.Fatal("expected error for board with duplicates, got nil")
	}

	// 2. Invalid board with out-of-range number
	var oobBoard [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		oobBoard[i] = i + 1
	}
	oobBoard[0] = 99
	err = state.SetReady("p1", oobBoard, false)
	if err == nil {
		t.Fatal("expected error for board with out-of-bounds number, got nil")
	}

	// 3. Valid board for p1
	var validBoard [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		validBoard[i] = i + 1
	}
	err = state.SetReady("p1", validBoard, false)
	if err != nil {
		t.Fatalf("unexpected error setting valid board: %v", err)
	}
	if !state.Ready["p1"] {
		t.Fatal("expected p1 to be ready")
	}
	// Status should still be setup because p2 is not ready
	if state.Status != "setup" {
		t.Fatalf("expected status to still be 'setup', got '%s'", state.Status)
	}

	// 4. Auto-generated board for p2
	err = state.SetReady("p2", [TotalCells]int{}, true)
	if err != nil {
		t.Fatalf("unexpected error setting random board: %v", err)
	}
	if !state.Ready["p2"] {
		t.Fatal("expected p2 to be ready")
	}

	// Verify auto-generated board is valid
	if err := ValidateBoard(state.Boards["p2"]); err != nil {
		t.Fatalf("auto-generated board is invalid: %v", err)
	}

	// Now all players are ready -> status must be "playing"
	if state.Status != "playing" {
		t.Fatalf("expected status 'playing', got '%s'", state.Status)
	}
	if state.CurrentTurn != "p1" {
		t.Fatalf("expected current turn to be 'p1', got '%s'", state.CurrentTurn)
	}
}

func TestBingoState_GameplayAndLineCalculations(t *testing.T) {
	// Let's create predictable boards
	// Board 1: 1..25 in order
	// 1  2  3  4  5
	// 6  7  8  9  10
	// 11 12 13 14 15
	// 16 17 18 19 20
	// 21 22 23 24 25
	var b1 [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		b1[i] = i + 1
	}

	// Board 2: Arranged so that p2 has only 2 lines when 21 is marked
	var b2 [TotalCells]int
	b2 = [TotalCells]int{
		1, 2, 3, 4, 5,
		6, 22, 8, 9, 10,
		11, 12, 23, 14, 15,
		16, 17, 18, 24, 20,
		21, 7, 13, 19, 25,
	}

	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", b1, false)
	_ = state.SetReady("p2", b2, false)

	if state.Status != "playing" {
		t.Fatalf("expected status 'playing', got '%s'", state.Status)
	}

	// Complete Row 0 on p1 (numbers: 1, 2, 3, 4, 5)
	// p1 marks 1
	ended, _, err := state.MakeMove("p1", 1)
	if err != nil || ended {
		t.Fatalf("move 1 failed: %v", err)
	}
	if state.CurrentTurn != "p2" {
		t.Fatalf("expected turn 'p2', got '%s'", state.CurrentTurn)
	}

	// p2 marks 2
	ended, _, err = state.MakeMove("p2", 2)
	if err != nil || ended {
		t.Fatalf("move 2 failed: %v", err)
	}

	// p1 marks 3
	ended, _, err = state.MakeMove("p1", 3)
	if err != nil || ended {
		t.Fatalf("move 3 failed: %v", err)
	}

	// p2 marks 4
	ended, _, err = state.MakeMove("p2", 4)
	if err != nil || ended {
		t.Fatalf("move 4 failed: %v", err)
	}

	// p1 marks 5 -> Row 0 (1,2,3,4,5) is now complete for p1!
	ended, _, err = state.MakeMove("p1", 5)
	if err != nil || ended {
		t.Fatalf("move 5 failed: %v", err)
	}

	if state.CompletedLines["p1"] != 1 {
		t.Fatalf("expected p1 to have 1 line, got %d", state.CompletedLines["p1"])
	}
	if GetBingoLetters(state.CompletedLines["p1"]) != "B" {
		t.Fatalf("expected letter 'B', got '%s'", GetBingoLetters(state.CompletedLines["p1"]))
	}

	// Row 1 (6, 7, 8, 9, 10)
	for _, n := range []int{6, 7, 8, 9, 10} {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, n)
		if err != nil {
			t.Fatalf("failed marking %d: %v", n, err)
		}
	}
	if state.CompletedLines["p1"] != 2 {
		t.Fatalf("expected p1 to have 2 lines, got %d", state.CompletedLines["p1"])
	}
	if GetBingoLetters(state.CompletedLines["p1"]) != "BI" {
		t.Fatalf("expected 'BI', got '%s'", GetBingoLetters(state.CompletedLines["p1"]))
	}

	// Row 2 (11, 12, 13, 14, 15)
	for _, n := range []int{11, 12, 13, 14, 15} {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, n)
		if err != nil {
			t.Fatalf("failed marking %d: %v", n, err)
		}
	}
	if state.CompletedLines["p1"] != 3 {
		t.Fatalf("expected p1 to have 3 lines, got %d", state.CompletedLines["p1"])
	}
	if GetBingoLetters(state.CompletedLines["p1"]) != "BIN" {
		t.Fatalf("expected 'BIN', got '%s'", GetBingoLetters(state.CompletedLines["p1"]))
	}

	// Row 3 (16, 17, 18, 19, 20)
	for _, n := range []int{16, 17, 18, 19, 20} {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, n)
		if err != nil {
			t.Fatalf("failed marking %d: %v", n, err)
		}
	}
	if state.CompletedLines["p1"] != 4 {
		t.Fatalf("expected p1 to have 4 lines, got %d", state.CompletedLines["p1"])
	}
	if GetBingoLetters(state.CompletedLines["p1"]) != "BING" {
		t.Fatalf("expected 'BING', got '%s'", GetBingoLetters(state.CompletedLines["p1"]))
	}

	// Marking 21 completes Column 0 (1, 6, 11, 16, 21), which is the 5th completed line -> triggers BINGO win for p1!
	curr := state.CurrentTurn
	ended, winners, err := state.MakeMove(curr, 21)
	if err != nil {
		t.Fatalf("failed marking 21: %v", err)
	}
	if !ended {
		t.Fatal("expected game to end after 5 lines achieved")
	}
	if state.Status != "finished" {
		t.Fatalf("expected status 'finished', got '%s'", state.Status)
	}
	if len(winners) != 1 || winners[0] != "p1" {
		t.Fatalf("expected p1 to win, got %v", winners)
	}
	if state.CompletedLines["p1"] < 5 {
		t.Fatalf("expected p1 to have at least 5 lines, got %d", state.CompletedLines["p1"])
	}
	if GetBingoLetters(state.CompletedLines["p1"]) != "BINGO" {
		t.Fatalf("expected 'BINGO', got '%s'", GetBingoLetters(state.CompletedLines["p1"]))
	}
}

func TestBingoState_SimultaneousWinners(t *testing.T) {
	// Identical boards for both players
	var b [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		b[i] = i + 1
	}

	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", b, false)
	_ = state.SetReady("p2", b, false)

	// Mark 1..20
	for i := 1; i <= 20; i++ {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, i)
		if err != nil {
			t.Fatalf("failed move for %d: %v", i, err)
		}
	}

	// Mark 21 -> both complete line 5 simultaneously
	curr := state.CurrentTurn
	ended, winners, err := state.MakeMove(curr, 21)
	if err != nil || !ended {
		t.Fatalf("expected game over on 21: %v", err)
	}
	if len(winners) != 2 {
		t.Fatalf("expected 2 simultaneous winners, got %d (%v)", len(winners), winners)
	}
}

func TestBingoState_InvalidMoves(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", state.GenerateRandomBoard(), false)
	_ = state.SetReady("p2", state.GenerateRandomBoard(), false)

	// Move out of turn
	_, _, err := state.MakeMove("p2", 10)
	if err == nil {
		t.Fatal("expected error when moving out of turn")
	}

	// Number out of range
	_, _, err = state.MakeMove("p1", 0)
	if err == nil {
		t.Fatal("expected error for number 0")
	}
	_, _, err = state.MakeMove("p1", 26)
	if err == nil {
		t.Fatal("expected error for number 26")
	}

	// Valid move
	_, _, err = state.MakeMove("p1", 10)
	if err != nil {
		t.Fatalf("unexpected error on valid move: %v", err)
	}

	// Duplicate number call
	_, _, err = state.MakeMove("p2", 10)
	if err == nil {
		t.Fatal("expected error when calling already marked number")
	}
}

func TestBingoState_LeavePlayer(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
		{ID: "p3", Name: "Charlie"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", state.GenerateRandomBoard(), false)
	_ = state.SetReady("p2", state.GenerateRandomBoard(), false)
	_ = state.SetReady("p3", state.GenerateRandomBoard(), false)

	// p1 leaves during playing
	ended, winners := state.LeavePlayer("p1")
	if ended {
		t.Fatal("game should not end when 2 players remain")
	}
	if state.CurrentTurn == "p1" {
		t.Fatal("current turn should have moved to active player")
	}

	// p2 leaves -> only p3 left -> p3 wins
	ended, winners = state.LeavePlayer("p2")
	if !ended {
		t.Fatal("game should end when only 1 player remains")
	}
	if len(winners) != 1 || winners[0] != "p3" {
		t.Fatalf("expected p3 to win by default, got %v", winners)
	}
	if state.Status != "finished" {
		t.Fatalf("expected status 'finished', got '%s'", state.Status)
	}
}

func TestBingoState_Restart(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", state.GenerateRandomBoard(), false)
	_ = state.SetReady("p2", state.GenerateRandomBoard(), false)

	state.Status = "finished"
	state.Winners = []string{"p1"}

	state.Restart()

	if state.Status != "setup" {
		t.Fatalf("expected status 'setup' after restart, got '%s'", state.Status)
	}
	if state.Ready["p1"] || state.Ready["p2"] {
		t.Fatal("players should not be ready after restart")
	}
	if len(state.MarkedNumbers) != 0 {
		t.Fatal("marked numbers should be reset")
	}
	if len(state.Winners) != 0 {
		t.Fatal("winners should be reset")
	}
}

func TestBingoState_Diagonals(t *testing.T) {
	var board [TotalCells]int
	for i := 0; i < TotalCells; i++ {
		board[i] = i + 1
	}

	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	state := NewBingoState(players)
	_ = state.SetReady("p1", board, false)
	_ = state.SetReady("p2", board, false)

	// Main diagonal: 0(1), 6(7), 12(13), 18(19), 24(25)
	mainDiagNums := []int{1, 7, 13, 19, 25}
	for _, n := range mainDiagNums {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, n)
		if err != nil {
			t.Fatalf("failed move for %d: %v", n, err)
		}
	}

	if state.CompletedLines["p1"] != 1 {
		t.Fatalf("expected 1 completed diagonal line, got %d", state.CompletedLines["p1"])
	}

	// Anti diagonal: 4(5), 8(9), 12(13-already marked), 16(17), 20(21)
	antiDiagNums := []int{5, 9, 17, 21}
	for _, n := range antiDiagNums {
		curr := state.CurrentTurn
		_, _, err := state.MakeMove(curr, n)
		if err != nil {
			t.Fatalf("failed move for %d: %v", n, err)
		}
	}

	if state.CompletedLines["p1"] != 2 {
		t.Fatalf("expected 2 completed diagonal lines, got %d", state.CompletedLines["p1"])
	}
}

func TestBingo_InstanceActionsAndStateView(t *testing.T) {
	game := NewGame()
	if game.Id() != "bingo" || game.Name() != "Bingo" {
		t.Fatalf("unexpected game metadata: id=%s name=%s", game.Id(), game.Name())
	}
	if game.Capacity() != 5 || game.Mode() != game_plugin.ModeMultiplayer {
		t.Fatalf("unexpected capacity or mode")
	}

	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Alice"},
		{ID: "p2", Name: "Bob"},
	}
	inst, err := game.StartGame(nil, players)
	if err != nil {
		t.Fatalf("failed to start game: %v", err)
	}

	// Ready action with random board
	err = inst.OnAction("p1", string(ActionReady), []byte(`{"random": true}`))
	if err != nil {
		t.Fatalf("p1 ready failed: %v", err)
	}

	// Check state masking: p1 sees their own board, p2's board is nil
	sv1 := inst.GetState("p1").(StateView)
	if sv1.MyBoard == nil {
		t.Fatal("p1 should see their own board")
	}
	for _, p := range sv1.Players {
		if p.ID == "p2" && p.Board != nil {
			t.Fatal("p1 should NOT see p2's board during setup")
		}
	}

	// p2 ready with random board -> transitions to playing
	err = inst.OnAction("p2", string(ActionReady), []byte(`{"random": true}`))
	if err != nil {
		t.Fatalf("p2 ready failed: %v", err)
	}

	svPlaying := inst.GetState("p1").(StateView)
	if svPlaying.Status != "playing" {
		t.Fatalf("expected status 'playing', got '%s'", svPlaying.Status)
	}

	// Reconnect / disconnect
	inst.OnPlayerDisconnect("p2")
	svDisc := inst.GetState("p1").(StateView)
	for _, p := range svDisc.Players {
		if p.ID == "p2" && !p.Disconnected {
			t.Fatal("expected p2 to be marked disconnected")
		}
	}

	inst.OnPlayerReconnect("p2")
	svRec := inst.GetState("p1").(StateView)
	for _, p := range svRec.Players {
		if p.ID == "p2" && p.Disconnected {
			t.Fatal("expected p2 disconnected flag to be cleared")
		}
	}
}
