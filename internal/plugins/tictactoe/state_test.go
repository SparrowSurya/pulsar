package tictactoe

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	game_plugin "pulsar/internal/plugin/game"
	"pulsar/internal/server/protocol"
)

type mockRoomContext struct {
	broadcastGameStateCount int
	endGameCount            int
	returnToLobbyCount      int
	lastSummary             any
	lastSentPlayerID        string
	lastSentRoute           string
	lastSentAction          string
	lastSentPayload         any
}

func (m *mockRoomContext) BroadcastGameState() {
	m.broadcastGameStateCount++
}
func (m *mockRoomContext) SendGameState(playerID string) {}
func (m *mockRoomContext) Broadcast(route string, action string, payload any) {}
func (m *mockRoomContext) SendToPlayer(playerID string, route string, action string, payload any) {
	m.lastSentPlayerID = playerID
	m.lastSentRoute = route
	m.lastSentAction = action
	m.lastSentPayload = payload
}
func (m *mockRoomContext) SendSystemMessage(content string) {}
func (m *mockRoomContext) EndGame(detail game_plugin.GameOverDetail) {
	m.endGameCount++
	m.lastSummary = detail
}
func (m *mockRoomContext) ReturnToLobby() {
	m.returnToLobbyCount++
}
func (m *mockRoomContext) DestroyRoom() {}
func (m *mockRoomContext) GetOwnerID() string { return "owner" }

func TestTicTacToeState_Gameplay(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "user1", Name: "Alice"},
		{ID: "user2", Name: "Bob"},
	}
	state := NewState(players)

	// 1. Initial State
	if state.Status != TicTacToePlayingStatus {
		t.Errorf("Expected initial status %q, got %q", TicTacToePlayingStatus, state.Status)
	}
	if state.Players[0].Mark != MarkX || state.Players[0].ID != "user1" {
		t.Errorf("Expected user1 mark %q, got %+v", MarkX, state.Players[0])
	}
	if state.Players[1].Mark != MarkO || state.Players[1].ID != "user2" {
		t.Errorf("Expected user2 mark %q, got %+v", MarkO, state.Players[1])
	}
	if state.Turn != "user1" {
		t.Errorf("Expected user1 turn to start, got %q", state.Turn)
	}

	// 2. Make Move (Valid)
	err := state.MakeMove("user1", 0) // Alice plays X at 0
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if state.Status != TicTacToePlayingStatus {
		t.Errorf("Expected game to still be playing")
	}
	if state.Winner != "" {
		t.Errorf("Expected no winner yet, got %q", state.Winner)
	}
	if state.Turn != "user2" {
		t.Errorf("Expected turn to switch to user2, got %q", state.Turn)
	}

	// 3. Make Move (Invalid turn)
	err = state.MakeMove("user1", 1)
	if err == nil {
		t.Errorf("Expected error when user1 plays out of turn")
	}

	// 4. Make Move (Occupied cell)
	err = state.MakeMove("user2", 0)
	if err == nil {
		t.Errorf("Expected error when playing in occupied cell")
	}

	// 5. Bob plays O at 4
	err = state.MakeMove("user2", 4)
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if state.Status != TicTacToePlayingStatus {
		t.Errorf("Expected game to still be playing")
	}

	// 6. Alice plays X at 1
	state.MakeMove("user1", 1)
	// Bob plays O at 5
	state.MakeMove("user2", 5)
	// Alice plays X at 2 (Winning move: row 0 is all X)
	err = state.MakeMove("user1", 2)
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if state.Status != TicTacToeGameoverStatus {
		t.Errorf("Expected status %q, got %q", TicTacToeGameoverStatus, state.Status)
	}
	if state.Winner != "user1" {
		t.Errorf("Expected state winner playerID 'user1', got %q", state.Winner)
	}
	if !reflect.DeepEqual(state.WinningLine, []int{0, 1, 2}) {
		t.Errorf("Expected WinningLine [0, 1, 2], got %v", state.WinningLine)
	}
	if state.IsDraw {
		t.Errorf("Expected IsDraw false on win, got true")
	}
}

func TestTicTacToeState_Draw(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	}
	state := NewState(players)

	// Moves for a draw:
	// X O X
	// X O O
	// O X X
	//
	// 0: X (p1), 1: O (p2), 2: X (p1)
	// 4: O (p2), 3: X (p1), 5: O (p2)
	// 7: X (p1), 6: O (p2), 8: X (p1)
	moves := []struct {
		player string
		cell   int
	}{
		{"p1", 0},
		{"p2", 1},
		{"p1", 2},
		{"p2", 4},
		{"p1", 3},
		{"p2", 5},
		{"p1", 7},
		{"p2", 6},
	}

	for _, m := range moves {
		err := state.MakeMove(m.player, m.cell)
		if err != nil {
			t.Fatalf("Move for %s at %d failed: %v", m.player, m.cell, err)
		}
		if state.Status != TicTacToePlayingStatus {
			t.Fatalf("Game ended prematurely at move %d by %s", m.cell, m.player)
		}
	}

	// Final move (p1 at 8)
	err := state.MakeMove("p1", 8)
	if err != nil {
		t.Fatalf("Final move failed: %v", err)
	}
	if state.Winner != "" {
		t.Errorf("Expected state.Winner '' on draw, got %q", state.Winner)
	}
	if !state.IsDraw {
		t.Errorf("Expected state.IsDraw true on draw, got false")
	}
	if state.Status != TicTacToeGameoverStatus {
		t.Errorf("Expected status %q on draw, got %q", TicTacToeGameoverStatus, state.Status)
	}
}

func TestTicTacToeState_LeavePlayer(t *testing.T) {
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	}
	state := NewState(players)

	otherPlayerID, ok := state.LeavePlayer("p1")
	if !ok {
		t.Errorf("Expected ok true when p1 leaves")
	}
	if otherPlayerID != "p2" {
		t.Errorf("Expected otherPlayerID 'p2', got %q", otherPlayerID)
	}
	if state.Status != TicTacToeGameoverStatus {
		t.Errorf("Expected status %q on leave, got %q", TicTacToeGameoverStatus, state.Status)
	}
}

func TestTicTacToeInstance_StateViewAndActions(t *testing.T) {
	factory := NewGame()
	players := []game_plugin.PlayerInfo{
		{ID: "host_1", Name: "HostUser"},
		{ID: "guest_2", Name: "GuestUser"},
	}

	mockCtx := &mockRoomContext{}
	inst, err := factory.StartGame(mockCtx, players)
	if err != nil {
		t.Fatalf("Failed to start game: %v", err)
	}

	// 1. Initial State
	viewRaw := inst.GetState("host_1")
	view, ok := viewRaw.(TicTacToeState)
	if !ok {
		t.Fatalf("Expected TicTacToeState type, got %T", viewRaw)
	}

	if len(view.Players) != 2 {
		t.Fatalf("Expected 2 players in State, got %d", len(view.Players))
	}
	if view.Players[0].ID != "host_1" || view.Players[0].Mark != MarkX {
		t.Errorf("Unexpected player 0: %+v", view.Players[0])
	}
	if view.Players[1].ID != "guest_2" || view.Players[1].Mark != MarkO {
		t.Errorf("Unexpected player 1: %+v", view.Players[1])
	}
	if view.Turn != "host_1" {
		t.Errorf("Expected turn host_1, got %q", view.Turn)
	}
	if view.Status != TicTacToePlayingStatus {
		t.Errorf("Expected status %q, got %q", TicTacToePlayingStatus, view.Status)
	}

	// 2. Disconnect and Reconnect handling
	inst.OnPlayerDisconnect("guest_2")
	viewAfterDisconnect := inst.GetState("host_1").(TicTacToeState)
	if len(viewAfterDisconnect.Disconnected) != 1 || viewAfterDisconnect.Disconnected[0] != "guest_2" {
		t.Errorf("Expected disconnected [guest_2], got %v", viewAfterDisconnect.Disconnected)
	}

	inst.OnPlayerReconnect("guest_2")
	viewAfterReconnect := inst.GetState("host_1").(TicTacToeState)
	if len(viewAfterReconnect.Disconnected) != 0 {
		t.Errorf("Expected empty disconnected slice, got %v", viewAfterReconnect.Disconnected)
	}

	// 3. Play moves via OnAction
	err = inst.OnAction("host_1", "mark_cell", json.RawMessage(`{"cell": 0}`))
	if err != nil {
		t.Fatalf("Move 0 failed: %v", err)
	}
	if mockCtx.broadcastGameStateCount != 1 {
		t.Errorf("Expected 1 broadcastGameStateCount, got %d", mockCtx.broadcastGameStateCount)
	}

	err = inst.OnAction("guest_2", "mark_cell", json.RawMessage(`{"cell": 3}`))
	if err != nil {
		t.Fatalf("Move 3 failed: %v", err)
	}

	err = inst.OnAction("host_1", "mark_cell", json.RawMessage(`{"cell": 1}`))
	if err != nil {
		t.Fatalf("Move 1 failed: %v", err)
	}

	err = inst.OnAction("guest_2", "mark_cell", json.RawMessage(`{"cell": 4}`))
	if err != nil {
		t.Fatalf("Move 4 failed: %v", err)
	}

	// Winning move
	err = inst.OnAction("host_1", "mark_cell", json.RawMessage(`{"cell": 2}`))
	if err != nil {
		t.Fatalf("Winning move failed: %v", err)
	}

	if mockCtx.endGameCount != 1 {
		t.Errorf("Expected endGameCount 1, got %d", mockCtx.endGameCount)
	}

	// Verify GameOverDetail payload passed to EndGame
	detail, ok := mockCtx.lastSummary.(game_plugin.GameOverDetail)
	if !ok {
		t.Fatalf("Expected GameOverDetail summary, got %T", mockCtx.lastSummary)
	}
	if detail.IsDraw {
		t.Errorf("Expected IsDraw false for win, got true")
	}
	if len(detail.Players) != 2 || detail.Players[0].ID != "host_1" || detail.Players[1].ID != "guest_2" {
		t.Errorf("Expected winner 'host_1' then 'guest_2', got %+v", detail.Players)
	}

	// Verify finished State
	viewAfterWin := inst.GetState("host_1").(TicTacToeState)
	if viewAfterWin.Status != TicTacToeGameoverStatus {
		t.Errorf("Expected status %q, got %q", TicTacToeGameoverStatus, viewAfterWin.Status)
	}
	if viewAfterWin.Winner != "host_1" {
		t.Errorf("Expected winner 'host_1', got %q", viewAfterWin.Winner)
	}
	if !reflect.DeepEqual(viewAfterWin.WinningLine, []int{0, 1, 2}) {
		t.Errorf("Expected winningLine [0, 1, 2], got %v", viewAfterWin.WinningLine)
	}
}

func TestTicTacToeInstance_DrawGame(t *testing.T) {
	factory := NewGame()
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	}

	mockCtx := &mockRoomContext{}
	inst, err := factory.StartGame(mockCtx, players)
	if err != nil {
		t.Fatalf("Failed to start game: %v", err)
	}

	// Draw sequence:
	// p1: 0 (X), p2: 1 (O), p1: 2 (X)
	// p2: 4 (O), p1: 3 (X), p2: 5 (O)
	// p1: 7 (X), p2: 6 (O), p1: 8 (X)
	moves := []struct {
		player string
		cell   int
	}{
		{"p1", 0}, {"p2", 1}, {"p1", 2},
		{"p2", 4}, {"p1", 3}, {"p2", 5},
		{"p1", 7}, {"p2", 6}, {"p1", 8},
	}

	for _, m := range moves {
		err := inst.OnAction(m.player, "mark_cell", json.RawMessage(fmt.Sprintf(`{"cell": %d}`, m.cell)))
		if err != nil {
			t.Fatalf("Move by %s on cell %d failed: %v", m.player, m.cell, err)
		}
	}

	if mockCtx.endGameCount != 1 {
		t.Errorf("Expected endGameCount 1, got %d", mockCtx.endGameCount)
	}

	detail, ok := mockCtx.lastSummary.(game_plugin.GameOverDetail)
	if !ok {
		t.Fatalf("Expected GameOverDetail summary, got %T", mockCtx.lastSummary)
	}
	if !detail.IsDraw {
		t.Errorf("Expected IsDraw true for draw, got false")
	}
	if len(detail.Players) != 2 {
		t.Errorf("Expected 2 players in draw GameOverDetail, got %d", len(detail.Players))
	}
}

func TestTicTacToeInstance_TimeoutMovesToLobby(t *testing.T) {
	factory := NewGame()
	players := []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	}
	mockCtx := &mockRoomContext{}
	inst, err := factory.StartGame(mockCtx, players)
	if err != nil {
		t.Fatalf("StartGame failed: %v", err)
	}

	inst.OnTimeout("p1")

	if mockCtx.returnToLobbyCount != 1 {
		t.Errorf("Expected ReturnToLobby to be called once, got %d", mockCtx.returnToLobbyCount)
	}
	if mockCtx.lastSentPlayerID != "p2" {
		t.Errorf("Expected error to be sent to 'p2', got %q", mockCtx.lastSentPlayerID)
	}
	if mockCtx.lastSentRoute != "room" || mockCtx.lastSentAction != "error" {
		t.Errorf("Expected error on room channel, got route=%s, action=%s", mockCtx.lastSentRoute, mockCtx.lastSentAction)
	}
	errEvt, ok := mockCtx.lastSentPayload.(protocol.ErrorEvent)
	if !ok || errEvt.Error == nil || errEvt.Error.Code != protocol.ErrPlayerLeft {
		t.Errorf("Expected ErrPlayerLeft error payload, got %+v", mockCtx.lastSentPayload)
	}
	if errEvt.Error.Message != "Opponent left the match" {
		t.Errorf("Expected error message 'Opponent left the match', got %q", errEvt.Error.Message)
	}
}
