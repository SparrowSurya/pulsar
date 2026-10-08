// Package server runs integration tests for the websocket chat server.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"pulsar/internal/config"
	game_plugin "pulsar/internal/plugin/game"
	_ "pulsar/internal/plugins/bingo"
	_ "pulsar/internal/plugins/chat"
	_ "pulsar/internal/plugins/tictactoe"
	_ "pulsar/internal/plugins/uno"
	"pulsar/internal/server/domain"
	"pulsar/internal/server/middleware"
	"pulsar/internal/server/protocol"

	"github.com/gorilla/websocket"
)

func init() {
	os.Setenv("ENV", "development")
	os.Setenv("HOSTNAMEKEY", "HOSTNAME")
	os.Setenv("HOSTNAME", "127.0.0.1")
	os.Setenv("RATE_LIMIT_RPS", "1000")
	os.Setenv("RATE_LIMIT_BURST", "1000")
	config.ResetEnv()
	middleware.ResetRateLimiter()
}

// performHandshake completes the initial 'hello' handshake on a connected websocket.
func performHandshake(t *testing.T, conn *websocket.Conn, requestedID, name string) protocol.ServerHiEvent {
	t.Helper()
	payloadBytes, _ := json.Marshal(protocol.ClientHelloEvent{
		ID:   requestedID,
		Name: name,
	})
	helloReq := protocol.ClientMessage{
		Type:    protocol.ActionHello,
		Content: string(payloadBytes),
	}
	if err := conn.WriteJSON(helloReq); err != nil {
		t.Fatalf("Failed to write hello message: %v", err)
	}

	var envelope protocol.ServerMessage
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("Failed to read hello response envelope: %v", err)
	}
	if envelope.Type != protocol.ActionHi {
		t.Fatalf("Expected hello response type 'hi', got %q", envelope.Type)
	}

	var helloResp protocol.ServerHiEvent
	if err := json.Unmarshal([]byte(envelope.Content), &helloResp); err != nil {
		t.Fatalf("Failed to unmarshal hello response content: %v", err)
	}
	if helloResp.ID == "" {
		t.Fatalf("Expected non-empty ID in hello response")
	}
	return helloResp
}

// TestChatApplication performs an integration test of the websocket room chat system.
func TestChatApplication(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// Connect Client 1
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 1: %v", err)
	}
	defer conn1.Close()

	// Complete handshake for Client 1
	resp1 := performHandshake(t, conn1, "", "Alice")
	if resp1.Name != "Alice" {
		t.Errorf("Expected name 'Alice', got %q", resp1.Name)
	}

	// Connect Client 2
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 2: %v", err)
	}
	defer conn2.Close()

	// Complete handshake for Client 2
	resp2 := performHandshake(t, conn2, "", "Bob")
	if resp2.Name != "Bob" {
		t.Errorf("Expected name 'Bob', got %q", resp2.Name)
	}

	// Client 1 creates a room via REST API
	createReqBody := fmt.Sprintf(`{"clientId":"%s","gameName":"Tic-Tac-Toe","visibility":"public","joinCode":false,"spectate":true}`, resp1.ID)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/rooms", strings.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create REST request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make REST request: %v", err)
	}
	defer resp.Body.Close()

	var createResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createResp)
	roomID := createResp["roomId"].(string)

	// Both clients join the room
	joinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	conn1.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(joinPayload)})
	conn2.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(joinPayload)})

	// Drain initial room join / state messages on conn2 until in room
	var msg protocol.ServerMessage
	for {
		err := conn2.ReadJSON(&msg)
		if err != nil {
			t.Fatalf("Failed to read message on client 2: %v", err)
		}
		if msg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	// Client 1 sends a chat message inside the room
	testContent := `{"message":"hello world"}`
	chatReq := protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionChat,
		Content: testContent,
	}
	err = conn1.WriteJSON(chatReq)
	if err != nil {
		t.Fatalf("Failed to send chat message: %v", err)
	}

	// Client 2 should receive the room chat message
	var bcMsg2 protocol.ServerMessage
	for {
		err = conn2.ReadJSON(&bcMsg2)
		if err != nil {
			t.Fatalf("Failed to read broadcast message on client 2: %v", err)
		}
		if bcMsg2.Type == protocol.ActionChat && strings.Contains(bcMsg2.Content, "hello world") {
			break
		}
	}
}

// TestLegacyRenameWithoutChannel verifies that an unchanneled rename message is handled by handleLegacyAction.
func TestLegacyRenameWithoutChannel(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer conn.Close()

	resp := performHandshake(t, conn, "", "OldName")

	// 1. Send rename with valid name using Name field
	renameReq := protocol.ClientRenameEvent{
		Type: protocol.ActionRename,
		Name: "NewName",
	}
	if err := conn.WriteJSON(renameReq); err != nil {
		t.Fatalf("Failed to send rename message: %v", err)
	}

	var renameResp protocol.ServerRenameEvent
	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read rename response: %v", err)
	}
	if renameResp.Name != "NewName" {
		t.Fatalf("Expected rename response {name: 'NewName'}, got %+v", renameResp)
	}
	if renameResp.Error != nil {
		t.Errorf("Expected nil error on valid rename, got %+v", renameResp.Error)
	}

	client, exists := s.hub.FindClientByID(resp.ID)
	if !exists {
		t.Fatalf("Expected client to exist in hub")
	}
	if client.GetName() != "NewName" {
		t.Errorf("Expected client name to be 'NewName', got %q", client.GetName())
	}

	// 2. Send rename with length > 20 (should trim to 20 chars)
	longName := "PlayerNameExceedingTwentyChars12345"
	if err := conn.WriteJSON(protocol.ClientRenameEvent{Type: protocol.ActionRename, Name: longName}); err != nil {
		t.Fatalf("Failed to send long rename: %v", err)
	}
	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read long rename response: %v", err)
	}
	expectedLong := string([]rune(longName)[:20])
	if renameResp.Name != expectedLong {
		t.Errorf("Expected truncated name %q, got %q", expectedLong, renameResp.Name)
	}
	if renameResp.Error != nil {
		t.Errorf("Expected nil error on long rename, got %+v", renameResp.Error)
	}
	if client.GetName() != expectedLong {
		t.Errorf("Expected client name to be %q, got %q", expectedLong, client.GetName())
	}

	// 3. Send rename with length < 3 (should reject, return current name and non-nil error)
	if err := conn.WriteJSON(protocol.ClientRenameEvent{Type: protocol.ActionRename, Name: "Ab"}); err != nil {
		t.Fatalf("Failed to send short rename: %v", err)
	}
	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read short rename response: %v", err)
	}
	if renameResp.Name != expectedLong {
		t.Errorf("Expected rejected rename to return previous name %q, got %q", expectedLong, renameResp.Name)
	}
	if renameResp.Error == nil || renameResp.Error.Message == "" {
		t.Errorf("Expected non-empty error string for short rename")
	}
	if renameResp.Error != nil && renameResp.Error.Code != protocol.ErrInvalidName {
		t.Errorf("Expected error code %q, got %q", protocol.ErrInvalidName, renameResp.Error.Code)
	}
	if client.GetName() != expectedLong {
		t.Errorf("Expected client name to remain %q, got %q", expectedLong, client.GetName())
	}

	// 4. Send rename with special allowed and disallowed characters
	if err := conn.WriteJSON(protocol.ClientRenameEvent{Type: protocol.ActionRename, Name: "Cool_User-99!@#$"}); err != nil {
		t.Fatalf("Failed to send special char rename: %v", err)
	}
	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read special char rename response: %v", err)
	}
	if renameResp.Name != "Cool_User-99" {
		t.Errorf("Expected sanitized name 'Cool_User-99', got %q", renameResp.Name)
	}
	if renameResp.Error != nil {
		t.Errorf("Expected nil error on special char rename, got %+v", renameResp.Error)
	}
	if client.GetName() != "Cool_User-99" {
		t.Errorf("Expected client name to be 'Cool_User-99', got %q", client.GetName())
	}
}

// TestHelloNameValidation verifies name validation during the initial hello handshake.
func TestHelloNameValidation(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Handshake with valid name
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn1.Close()
	resp1 := performHandshake(t, conn1, "", "ValidName_1")
	if resp1.Name != "ValidName_1" {
		t.Errorf("Expected name 'ValidName_1', got %q", resp1.Name)
	}

	// 2. Handshake with name > 20 chars
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn2.Close()
	longName := "SuperExtraLongNicknameForPlayer"
	resp2 := performHandshake(t, conn2, "", longName)
	expectedLong := string([]rune(longName)[:20])
	if resp2.Name != expectedLong {
		t.Errorf("Expected truncated name %q, got %q", expectedLong, resp2.Name)
	}

	// 3. Handshake with short name (< 3 chars) -> should default to "Guest"
	conn3, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn3.Close()
	resp3 := performHandshake(t, conn3, "", "X")
	if resp3.Name != "Guest" {
		t.Errorf("Expected fallback name 'Guest', got %q", resp3.Name)
	}

	// 4. Handshake with disallowed chars only -> should default to "Guest"
	conn4, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn4.Close()
	resp4 := performHandshake(t, conn4, "", "@#$%%^")
	if resp4.Name != "Guest" {
		t.Errorf("Expected fallback name 'Guest', got %q", resp4.Name)
	}
}

// TestSanitizeAndValidateNameUnit runs pure unit tests on validation functions.
func TestSanitizeAndValidateNameUnit(t *testing.T) {
	tests := []struct {
		input       string
		helloExpect string
		renameCur   string
		renameExp   string
		renameOk    bool
	}{
		{"Alice", "Alice", "Current", "Alice", true},
		{"Bob", "Bob", "Current", "Bob", true},
		{"Ab", "Guest", "Current", "Current", false},
		{"", "Guest", "Current", "Current", false},
		{"12345678901234567890EXTRA", "12345678901234567890", "Current", "12345678901234567890", true},
		{"User_Name-1 2", "User_Name-1 2", "Current", "User_Name-1 2", true},
		{"User!@#Name", "UserName", "Current", "UserName", true},
		{"!@#", "Guest", "Current", "Current", false},
	}

	for _, tc := range tests {
		h := protocol.ValidateHelloName(tc.input)
		if h != tc.helloExpect {
			t.Errorf("ValidateHelloName(%q) = %q, expected %q", tc.input, h, tc.helloExpect)
		}

		r, _, ok := protocol.ValidateRename(tc.input, tc.renameCur)
		if r != tc.renameExp || ok != tc.renameOk {
			t.Errorf("ValidateRename(%q, %q) = (%q, %v), expected (%q, %v)", tc.input, tc.renameCur, r, ok, tc.renameExp, tc.renameOk)
		}
	}
}

// TestRejoinWithIDParam verifies that a disconnected client can rejoin with their previous ID.
func TestRejoinWithIDParam(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// Connect Client 1 initially
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 1: %v", err)
	}

	resp1 := performHandshake(t, conn1, "", "Alice")
	savedID := resp1.ID

	// Disconnect Client 1
	conn1.Close()

	// Wait for unregister to process
	time.Sleep(50 * time.Millisecond)

	// Reconnect with &id=<savedID>
	rejoinURL := fmt.Sprintf("%s&id=%s", wsURL, savedID)
	connRejoin, _, err := websocket.DefaultDialer.Dial(rejoinURL, nil)
	if err != nil {
		t.Fatalf("Failed to reconnect with ID: %v", err)
	}
	defer connRejoin.Close()

	// Complete handshake
	rejoinResp := performHandshake(t, connRejoin, savedID, "AliceRejoined")
	if rejoinResp.ID != savedID {
		t.Errorf("Expected rejoined ID to be %q, got %q", savedID, rejoinResp.ID)
	}
	if rejoinResp.Name != "AliceRejoined" {
		t.Errorf("Expected name 'AliceRejoined', got %q", rejoinResp.Name)
	}
}

// TestConnectWithActiveIDParam verifies that requesting an active player's ID assigns a new ID.
func TestConnectWithActiveIDParam(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// Connect Client 1 (stays connected)
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 1: %v", err)
	}
	defer conn1.Close()

	resp1 := performHandshake(t, conn1, "", "Alice")
	activeID := resp1.ID

	// Connect Client 2 attempting to use Client 1's active ID
	imposterURL := fmt.Sprintf("%s&id=%s", wsURL, activeID)
	conn2, _, err := websocket.DefaultDialer.Dial(imposterURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 2: %v", err)
	}
	defer conn2.Close()

	resp2 := performHandshake(t, conn2, activeID, "Imposter")
	if resp2.ID == activeID {
		t.Errorf("Expected a new ID to be assigned, but got active player's ID %q", resp2.ID)
	}
}

// TestConnectWithUnknownIDParam verifies that providing an unknown ID assigns a new generated ID.
func TestConnectWithUnknownIDParam(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	unknownID := "NONEXISTENT_9999"
	connectURL := fmt.Sprintf("%s&id=%s", wsURL, unknownID)
	conn, _, err := websocket.DefaultDialer.Dial(connectURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect with unknown ID: %v", err)
	}
	defer conn.Close()

	resp := performHandshake(t, conn, unknownID, "NewUser")
	if resp.ID == unknownID {
		t.Errorf("Expected unknown ID to be replaced with a generated ID, got %q", resp.ID)
	}
	if resp.Name != "NewUser" {
		t.Errorf("Expected name 'NewUser', got %q", resp.Name)
	}
}

// TestHandshakeTimeout verifies that the server closes connection if hello message is not received within timeout.
func TestHandshakeTimeout(t *testing.T) {
	s := &Server{
		HandshakeTimeout: 100 * time.Millisecond,
	}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait without sending any hello message
	var msg protocol.ServerMessage
	err = conn.ReadJSON(&msg)
	if err == nil {
		t.Fatalf("Expected connection to be closed on timeout, but read succeeded: %v", msg)
	}
}

// TestHandshakeTimeoutFromEnv verifies that INIT_WAIT_SECS is read from environment.
func TestHandshakeTimeoutFromEnv(t *testing.T) {
	os.Setenv("INIT_WAIT_SECS", "1")
	config.ResetEnv()
	defer func() {
		os.Unsetenv("INIT_WAIT_SECS")
		config.ResetEnv()
	}()

	s := &Server{}
	s.Setup()

	if s.HandshakeTimeout != 1*time.Second {
		t.Errorf("Expected HandshakeTimeout to be 1s from INIT_WAIT_SECS, got %v", s.HandshakeTimeout)
	}
}

// TestHandshakeInvalidMessageType verifies that sending a non-hello message as the first message closes the connection.
func TestHandshakeInvalidMessageType(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send a chat message instead of hello
	invalidMsg := protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionChat,
		Content: "Premature message",
	}
	if err := conn.WriteJSON(invalidMsg); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Server should close the connection
	var resp protocol.ServerMessage
	err = conn.ReadJSON(&resp)
	if err == nil {
		t.Fatalf("Expected connection to be closed after invalid first message, but read succeeded: %v", resp)
	}
}

// TestHandshakeInvalidJSON verifies that sending invalid JSON as the first message closes the connection.
func TestHandshakeInvalidJSON(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send non-JSON text
	if err := conn.WriteMessage(websocket.TextMessage, []byte("NOT_JSON")); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Server should close the connection
	var resp protocol.ServerMessage
	err = conn.ReadJSON(&resp)
	if err == nil {
		t.Fatalf("Expected connection to be closed after invalid JSON, but read succeeded: %v", resp)
	}
}

func TestWebSocketOriginValidation(t *testing.T) {
	// Configure test environment allowed origins
	os.Setenv("ENV", "production")
	os.Setenv("ALLOWED_ORIGINS", "http://allowed-frontend.example.com")
	config.ResetEnv()
	defer func() {
		os.Setenv("ENV", "development")
		os.Unsetenv("ALLOWED_ORIGINS")
		config.ResetEnv()
		middleware.ResetRateLimiter()
	}()

	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect with allowed origin
	dialerAllowed := websocket.Dialer{}
	headersAllowed := make(http.Header)
	headersAllowed.Set("Origin", "http://allowed-frontend.example.com")
	connAllowed, _, err := dialerAllowed.Dial(wsURL, headersAllowed)
	if err != nil {
		t.Fatalf("Failed to connect with allowed origin: %v", err)
	}
	connAllowed.Close()

	// 2. Connect with disallowed origin
	dialerDisallowed := websocket.Dialer{}
	headersDisallowed := make(http.Header)
	headersDisallowed.Set("Origin", "http://malicious-origin.com")
	_, resp, err := dialerDisallowed.Dial(wsURL, headersDisallowed)
	if err == nil {
		t.Fatalf("Expected connection with disallowed origin to be rejected, but it succeeded")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected response status 403 Forbidden, got %d", resp.StatusCode)
	}
}

func TestRoomOperationsAndDisconnect(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// Connect Client 1
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 1: %v", err)
	}
	defer conn1.Close()

	// Handshake Client 1
	resp1 := performHandshake(t, conn1, "", "Player1")

	// Connect Client 2
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 2: %v", err)
	}
	defer conn2.Close()

	// Handshake Client 2
	performHandshake(t, conn2, "", "Player2")

	// Client 1 creates a room for Tic-Tac-Toe via REST API
	createReqBody := fmt.Sprintf(`{"clientId":"%s","gameName":"Tic-Tac-Toe","visibility":"public","joinCode":false,"spectate":true}`, resp1.ID)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/rooms", strings.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create REST request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make REST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created from room creation, got %d", resp.StatusCode)
	}

	var createResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("Failed to decode room creation response: %v", err)
	}
	roomID := createResp["roomId"].(string)

	// Client 1 joins the room via WebSocket
	joinPayload1, _ := json.Marshal(map[string]string{
		"roomId":   roomID,
		"joinCode": "",
	})
	err = conn1.WriteJSON(protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionJoinRoom,
		Content: string(joinPayload1),
	})
	if err != nil {
		t.Fatalf("Failed to send join_room action for client 1: %v", err)
	}

	// Client 1 should receive room info
	var roomInfoMsg protocol.ServerMessage
	for {
		err = conn1.ReadJSON(&roomInfoMsg)
		if err != nil {
			t.Fatalf("Failed to read room info message: %v", err)
		}
		if roomInfoMsg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	// Client 2 joins the room via WebSocket
	joinPayload2, _ := json.Marshal(map[string]string{
		"roomId":   roomID,
		"joinCode": "",
	})
	err = conn2.WriteJSON(protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionJoinRoom,
		Content: string(joinPayload2),
	})
	if err != nil {
		t.Fatalf("Failed to send join_room action for client 2: %v", err)
	}

	// Client 2 should receive room info
	var client2RoomMsg protocol.ServerMessage
	for {
		err = conn2.ReadJSON(&client2RoomMsg)
		if err != nil {
			t.Fatalf("Failed to read room info on client 2: %v", err)
		}
		if client2RoomMsg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	// Client 1 should receive updated room info
	var client1RoomMsg protocol.ServerMessage
	for {
		err = conn1.ReadJSON(&client1RoomMsg)
		if err != nil {
			t.Fatalf("Failed to read updated room info on client 1: %v", err)
		}
		if client1RoomMsg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	// Now client 2 disconnects
	conn2.Close()

	// Client 1 should receive updated room info (showing client 2 left)
	var client1LeaveMsg protocol.ServerMessage
	for {
		err = conn1.ReadJSON(&client1LeaveMsg)
		if err != nil {
			t.Fatalf("Failed to read leave update on client 1: %v", err)
		}
		if client1LeaveMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			err = json.Unmarshal([]byte(client1LeaveMsg.Content), &resp)
			if err != nil {
				t.Fatalf("Failed to unmarshal room info content: %v", err)
			}
			if resp.RoomDetails != nil && len(resp.RoomDetails.Players) == 1 {
				break
			}
		}
	}
}

// TestWebSocketClientQueryParamValidation verifies client query param verification on /ws.
func TestWebSocketClientQueryParamValidation(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	// 1. Missing ?client param -> 403 Forbidden
	wsNoParam := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsNoParam, nil)
	if err == nil {
		t.Fatalf("Expected connection without ?client param to fail, but succeeded")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403 Forbidden, got %d", resp.StatusCode)
	}

	// 2. Invalid ?client param -> 403 Forbidden
	wsWrongParam := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=browser"
	_, resp, err = websocket.DefaultDialer.Dial(wsWrongParam, nil)
	if err == nil {
		t.Fatalf("Expected connection with invalid ?client param to fail, but succeeded")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403 Forbidden, got %d", resp.StatusCode)
	}

	// 3. Valid ?client=nebula -> 200/Upgrade OK
	wsValid := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"
	conn, _, err := websocket.DefaultDialer.Dial(wsValid, nil)
	if err != nil {
		t.Fatalf("Expected connection with ?client=nebula to succeed, got %v", err)
	}
	conn.Close()
}

// TestLegacyCreateRoom verifies creating rooms via WebSocket unchanneled create_room action.
func TestLegacyCreateRoom(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer conn.Close()

	resp := performHandshake(t, conn, "", "RoomCreator")

	// 1. Create Room with join code
	type createRoomMsg struct {
		Type             string   `json:"type"`
		GameID           string   `json:"gameId"`
		Visibility       string   `json:"visibility"`
		Spectate         bool     `json:"spectate"`
		AllowPlugins     []string `json:"allowPlugins"`
		RequiredJoinCode bool     `json:"requiredJoinCode"`
	}

	req := createRoomMsg{
		Type:             protocol.ActionCreateRoom,
		GameID:           "tic-tac-toe",
		Visibility:       "private",
		Spectate:         true,
		AllowPlugins:     []string{"chat"},
		RequiredJoinCode: true,
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("Failed to send create_room message: %v", err)
	}

	var resMsg protocol.ServerMessage
	if err := conn.ReadJSON(&resMsg); err != nil {
		t.Fatalf("Failed to read create_room response: %v", err)
	}

	if resMsg.Channel != protocol.RoomChannel {
		t.Errorf("Expected channel 'room', got %q", resMsg.Channel)
	}
	if resMsg.Type != protocol.ActionCreateRoom {
		t.Errorf("Expected type 'create_room', got %q", resMsg.Type)
	}

	var res protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(resMsg.Content), &res); err != nil {
		t.Fatalf("Failed to unmarshal create_room payload: %v", err)
	}

	if res.Error != nil {
		t.Errorf("Expected nil error on room creation, got %+v", res.Error)
	}
	if res.RoomID == "" {
		t.Errorf("Expected non-empty roomId on room creation")
	}
	if res.JoinCode == "" {
		t.Errorf("Expected non-empty joinCode when visibility is private")
	}

	// Verify room exists in hub
	s.hub.Mu.RLock()
	var createdRoom *domain.Room
	for _, r := range s.hub.Rooms {
		if r.OwnerID == resp.ID {
			createdRoom = r
			break
		}
	}
	s.hub.Mu.RUnlock()

	if createdRoom == nil {
		t.Fatalf("Expected room created by %s to exist in hub", resp.ID)
	}
	if createdRoom.ID != res.RoomID {
		t.Errorf("Expected room ID %q, got %q", res.RoomID, createdRoom.ID)
	}
	if createdRoom.JoinCode != res.JoinCode {
		t.Errorf("Expected room JoinCode %q, got %q", res.JoinCode, createdRoom.JoinCode)
	}

	// 2. Duplicate room creation by same client -> should error
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("Failed to send duplicate create_room message: %v", err)
	}
	var dupMsg protocol.ServerMessage
	if err := conn.ReadJSON(&dupMsg); err != nil {
		t.Fatalf("Failed to read duplicate create_room response: %v", err)
	}
	if dupMsg.Channel != protocol.RoomChannel {
		t.Errorf("Expected channel 'room', got %q", dupMsg.Channel)
	}
	var dupRes protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(dupMsg.Content), &dupRes); err != nil {
		t.Fatalf("Failed to parse duplicate create_room content: %v", err)
	}
	if dupRes.Error == nil || dupRes.Error.Code != protocol.ErrPlayerAlreadyPlaying {
		t.Errorf("Expected ErrPlayerAlreadyPlaying, got %+v", dupRes.Error)
	}

	// 3. Connect another client and test invalid game ID
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 2: %v", err)
	}
	defer conn2.Close()
	performHandshake(t, conn2, "", "RoomCreator2")

	invalidReq := createRoomMsg{
		Type:   protocol.ActionCreateRoom,
		GameID: "non-existent-game-999",
	}
	if err := conn2.WriteJSON(invalidReq); err != nil {
		t.Fatalf("Failed to send invalid create_room message: %v", err)
	}
	var invalidMsg protocol.ServerMessage
	if err := conn2.ReadJSON(&invalidMsg); err != nil {
		t.Fatalf("Failed to read invalid create_room response: %v", err)
	}
	if invalidMsg.Channel != protocol.RoomChannel {
		t.Errorf("Expected channel 'room', got %q", invalidMsg.Channel)
	}
	var invalidRes protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(invalidMsg.Content), &invalidRes); err != nil {
		t.Fatalf("Failed to parse invalid create_room content: %v", err)
	}
	if invalidRes.Error == nil || invalidRes.Error.Code != protocol.ErrInvalidGameID {
		t.Errorf("Expected ErrInvalidGameID, got %+v", invalidRes.Error)
	}

	// 4. Test creating room via standard channeled ClientMessage
	conn3, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client 3: %v", err)
	}
	defer conn3.Close()
	performHandshake(t, conn3, "", "ChanneledRoomCreator")

	createPayloadBytes, _ := json.Marshal(protocol.ClientCreateRoomEvent{
		GameID:       "tic-tac-toe",
		Visibility:   protocol.RoomPublic,
		AllowPlugins: []string{"chat"},
	})
	channeledCreateMsg := protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionCreateRoom,
		Content: string(createPayloadBytes),
	}
	if err := conn3.WriteJSON(channeledCreateMsg); err != nil {
		t.Fatalf("Failed to send channeled create_room message: %v", err)
	}
	var channeledRespMsg protocol.ServerMessage
	if err := conn3.ReadJSON(&channeledRespMsg); err != nil {
		t.Fatalf("Failed to read channeled create_room response: %v", err)
	}
	if channeledRespMsg.Channel != protocol.RoomChannel {
		t.Errorf("Expected channel 'room', got %q", channeledRespMsg.Channel)
	}
	if channeledRespMsg.Type != protocol.ActionCreateRoom {
		t.Errorf("Expected type 'create_room', got %q", channeledRespMsg.Type)
	}
	var channeledRes protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(channeledRespMsg.Content), &channeledRes); err != nil {
		t.Fatalf("Failed to parse channeled create_room payload: %v", err)
	}
	if channeledRes.Error != nil {
		t.Errorf("Expected nil error from channeled create_room, got %+v", channeledRes.Error)
	}
	if channeledRes.RoomID == "" {
		t.Errorf("Expected non-empty RoomID from channeled create_room")
	}
}

// TestRenameDisallowedInRoom verifies that clients cannot rename their display name while in a room.
func TestRenameDisallowedInRoom(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer conn.Close()

	resp := performHandshake(t, conn, "", "InitialPlayer")

	// 1. Create a room
	createReqBody := fmt.Sprintf(`{"clientId":"%s","gameName":"Tic-Tac-Toe","visibility":"public","joinCode":false,"spectate":true}`, resp.ID)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/rooms", strings.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create REST request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make REST request: %v", err)
	}
	defer httpResp.Body.Close()

	var createResp map[string]interface{}
	json.NewDecoder(httpResp.Body).Decode(&createResp)
	roomID := createResp["roomId"].(string)

	// 2. Join the room
	joinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	if err := conn.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(joinPayload)}); err != nil {
		t.Fatalf("Failed to send join_room message: %v", err)
	}

	// Drain until join_room response
	var msg protocol.ServerMessage
	for {
		err := conn.ReadJSON(&msg)
		if err != nil {
			t.Fatalf("Failed to read message on join: %v", err)
		}
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// 3. Attempt rename while in the room -> should be rejected and retain InitialPlayer
	renameMsg := protocol.ClientRenameEvent{
		Type: protocol.ActionRename,
		Name: "AttemptedNewName",
	}
	if err := conn.WriteJSON(renameMsg); err != nil {
		t.Fatalf("Failed to send rename message in room: %v", err)
	}

	var renameResp protocol.ServerRenameEvent
	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read rename response: %v", err)
	}

	if renameResp.Name != "InitialPlayer" {
		t.Errorf("Expected rejected rename in room to return 'InitialPlayer', got %q", renameResp.Name)
	}
	if renameResp.Error == nil || renameResp.Error.Message != "Cannot rename while in a room" {
		t.Errorf("Expected 'Cannot rename while in a room' error, got %+v", renameResp.Error)
	}
	if renameResp.Error != nil && renameResp.Error.Code != protocol.ErrPlayerAlreadyPlaying {
		t.Errorf("Expected error code %q, got %q", protocol.ErrPlayerAlreadyPlaying, renameResp.Error.Code)
	}

	client, exists := s.hub.FindClientByID(resp.ID)
	if !exists {
		t.Fatalf("Expected client to exist in hub")
	}
	if client.GetName() != "InitialPlayer" {
		t.Errorf("Expected client name in hub to remain 'InitialPlayer', got %q", client.GetName())
	}

	// 4. Leave the room
	leaveMsg := protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionLeaveRoom,
	}
	if err := conn.WriteJSON(leaveMsg); err != nil {
		t.Fatalf("Failed to send leave_room message: %v", err)
	}

	// 5. Attempt rename again outside the room -> should succeed with nil error
	validRenameMsg := protocol.ClientRenameEvent{
		Type: protocol.ActionRename,
		Name: "AllowedLobbyName",
	}
	if err := conn.WriteJSON(validRenameMsg); err != nil {
		t.Fatalf("Failed to send rename message outside room: %v", err)
	}

	if err := conn.ReadJSON(&renameResp); err != nil {
		t.Fatalf("Failed to read lobby rename response: %v", err)
	}

	if renameResp.Name != "AllowedLobbyName" {
		t.Errorf("Expected lobby rename to return 'AllowedLobbyName', got %q", renameResp.Name)
	}
	if renameResp.Error != nil {
		t.Errorf("Expected nil error on lobby rename, got %+v", renameResp.Error)
	}
	if client.GetName() != "AllowedLobbyName" {
		t.Errorf("Expected client name in hub to be 'AllowedLobbyName', got %q", client.GetName())
	}
}

// TestJoinRoomInteractiveFlow tests missing join code challenge, wrong join code, and successful join.
func TestJoinRoomInteractiveFlow(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect Client 1 (Host)
	connHost, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect host: %v", err)
	}
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostPlayer")

	// Host creates private room via legacy message
	createMsg := struct {
		Type         string   `json:"type"`
		GameID       string   `json:"gameId"`
		Visibility   string   `json:"visibility"`
		Spectate     bool     `json:"spectate"`
		AllowPlugins []string `json:"allowPlugins"`
	}{
		Type:         protocol.ActionCreateRoom,
		GameID:       "tic-tac-toe",
		Visibility:   "private",
		Spectate:     true,
		AllowPlugins: []string{"chat"},
	}
	if err := connHost.WriteJSON(createMsg); err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	var hostCreateMsg protocol.ServerMessage
	if err := connHost.ReadJSON(&hostCreateMsg); err != nil {
		t.Fatalf("Failed to read create room response: %v", err)
	}
	if hostCreateMsg.Channel != protocol.RoomChannel || hostCreateMsg.Type != protocol.ActionCreateRoom {
		t.Fatalf("Expected create_room on channel room, got: %+v", hostCreateMsg)
	}
	var hostCreateResp protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp); err != nil {
		t.Fatalf("Failed to parse create room content: %v", err)
	}
	if hostCreateResp.RoomID == "" || hostCreateResp.JoinCode == "" {
		t.Fatalf("Expected non-empty roomID and joinCode, got: %+v", hostCreateResp)
	}
	roomID := hostCreateResp.RoomID
	correctCode := hostCreateResp.JoinCode

	// Host joins their room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})

	var hostJoinContent protocol.ServerJoinRoom
	for {
		var hostJoinMsg protocol.ServerMessage
		if err := connHost.ReadJSON(&hostJoinMsg); err != nil {
			t.Fatalf("Failed to read host join response: %v", err)
		}
		if hostJoinMsg.Type == protocol.ActionJoinRoom {
			if err := json.Unmarshal([]byte(hostJoinMsg.Content), &hostJoinContent); err != nil {
				t.Fatalf("Failed to parse host join content: %v", err)
			}
			break
		}
	}
	if hostJoinContent.Error != nil {
		t.Errorf("Expected nil error for host join, got %+v", hostJoinContent.Error)
	}
	if hostJoinContent.RoomDetails == nil || hostJoinContent.RoomDetails.JoinCode != correctCode {
		t.Errorf("Expected roomDetails with joinCode %q, got %+v", correctCode, hostJoinContent.RoomDetails)
	}
	if hostJoinContent.RoomDetails != nil {
		if !hostJoinContent.RoomDetails.Spectate {
			t.Errorf("Expected Spectate true, got false")
		}
		if len(hostJoinContent.RoomDetails.AllowPlugins) != 1 || hostJoinContent.RoomDetails.AllowPlugins[0] != "chat" {
			t.Errorf("Expected AllowPlugins ['chat'], got %v", hostJoinContent.RoomDetails.AllowPlugins)
		}
	}

	// 2. Connect Client 2 (Guest)
	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()
	performHandshake(t, connGuest, "", "GuestPlayer")

	// Step A: Guest attempts to join without join code -> should receive ErrMissingJoinCode
	noCodePayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, JoinCode: ""})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(noCodePayload)})

	var guestResp1 protocol.ServerMessage
	if err := connGuest.ReadJSON(&guestResp1); err != nil {
		t.Fatalf("Failed to read guest response 1: %v", err)
	}
	var joinContent1 protocol.ServerJoinRoom
	if err := json.Unmarshal([]byte(guestResp1.Content), &joinContent1); err != nil {
		t.Fatalf("Failed to parse guest join content 1: %v", err)
	}
	if joinContent1.Error == nil || joinContent1.Error.Code != protocol.ErrMissingJoinCode {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrMissingJoinCode, joinContent1.Error)
	}

	// Step B: Guest sends wrong join code -> should receive error "Invalid join code"
	wrongCodePayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, JoinCode: "WRONG"})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(wrongCodePayload)})

	var guestResp2 protocol.ServerMessage
	if err := connGuest.ReadJSON(&guestResp2); err != nil {
		t.Fatalf("Failed to read guest response 2: %v", err)
	}
	var joinContent2 protocol.ServerJoinRoom
	if err := json.Unmarshal([]byte(guestResp2.Content), &joinContent2); err != nil {
		t.Fatalf("Failed to parse guest join content 2: %v", err)
	}
	if joinContent2.Error == nil || joinContent2.Error.Code != protocol.ErrInvalidJoinCode {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrInvalidJoinCode, joinContent2.Error)
	}

	// Step C: Guest sends correct join code -> should succeed with roomDetails
	correctCodePayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, JoinCode: correctCode})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(correctCodePayload)})

	var joinContent3 protocol.ServerJoinRoom
	for {
		var guestResp3 protocol.ServerMessage
		if err := connGuest.ReadJSON(&guestResp3); err != nil {
			t.Fatalf("Failed to read guest response 3: %v", err)
		}
		if guestResp3.Type == protocol.ActionJoinRoom {
			if err := json.Unmarshal([]byte(guestResp3.Content), &joinContent3); err != nil {
				t.Fatalf("Failed to parse guest join content 3: %v", err)
			}
			break
		}
	}
	if joinContent3.Error != nil {
		t.Errorf("Expected nil error for valid join, got %+v", joinContent3.Error)
	}
	if joinContent3.RoomDetails == nil {
		t.Fatalf("Expected non-nil RoomDetails in successful join response")
	}
	if joinContent3.RoomDetails.GameID != "tic-tac-toe" {
		t.Errorf("Expected GameID 'tic-tac-toe', got %q", joinContent3.RoomDetails.GameID)
	}
	if joinContent3.RoomDetails.GameMode != game_plugin.ModeDuel {
		t.Errorf("Expected GameMode 'duel', got %q", joinContent3.RoomDetails.GameMode)
	}
	if joinContent3.RoomDetails.Status != game_plugin.StatusWaiting {
		t.Errorf("Expected Status 'waiting', got %q", joinContent3.RoomDetails.Status)
	}
	if len(joinContent3.RoomDetails.Players) != 2 {
		t.Errorf("Expected 2 players in room, got %d", len(joinContent3.RoomDetails.Players))
	}
	_ = hostResp
}

// TestSpectatorJoinAndDisallowed tests spectating permissions.
func TestSpectatorJoinAndDisallowed(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// Host connects and creates room with spectators disallowed
	connHost, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect host: %v", err)
	}
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "StrictHost")

	createMsg := struct {
		Type         string   `json:"type"`
		GameID       string   `json:"gameId"`
		Visibility   string   `json:"visibility"`
		Spectate     bool     `json:"spectate"`
		AllowPlugins []string `json:"allowPlugins"`
	}{
		Type:         protocol.ActionCreateRoom,
		GameID:       "tic-tac-toe",
		Visibility:   "public",
		Spectate:     false,
		AllowPlugins: []string{"chat"},
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	if err := connHost.ReadJSON(&hostCreateMsg); err != nil {
		t.Fatalf("Failed to read create room response: %v", err)
	}
	var hostCreateResp protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp); err != nil {
		t.Fatalf("Failed to parse create room content: %v", err)
	}
	roomID := hostCreateResp.RoomID

	// Guest attempts to spectate in no-spectator room
	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()
	performHandshake(t, connGuest, "", "Viewer")

	spectatePayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: true})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(spectatePayload)})

	var guestResp protocol.ServerMessage
	if err := connGuest.ReadJSON(&guestResp); err != nil {
		t.Fatalf("Failed to read guest response: %v", err)
	}
	var joinContent protocol.ServerJoinRoom
	json.Unmarshal([]byte(guestResp.Content), &joinContent)
	if joinContent.Error == nil || joinContent.Error.Code != protocol.ErrSpectatedNotAllowed {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrSpectatedNotAllowed, joinContent.Error)
	}
	_ = hostResp
}

// TestRoomDestroyOnEmptyAndOwnerLeave verifies that:
// 1. A room is NOT destroyed when the owner leaves if other participants remain.
// 2. When the last participant leaves and the room becomes empty, the room is destroyed immediately.
func TestRoomDestroyOnEmptyAndOwnerLeave(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect Owner
	connOwner, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect owner: %v", err)
	}
	defer connOwner.Close()
	ownerResp := performHandshake(t, connOwner, "", "RoomOwner")

	// 2. Connect Guest
	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()
	guestResp := performHandshake(t, connGuest, "", "RoomGuest")

	// 3. Owner creates a room
	createMsg := struct {
		Type         string   `json:"type"`
		GameID       string   `json:"gameId"`
		Visibility   string   `json:"visibility"`
		Spectate     bool     `json:"spectate"`
		AllowPlugins []string `json:"allowPlugins"`
	}{
		Type:         protocol.ActionCreateRoom,
		GameID:       "tic-tac-toe",
		Visibility:   "public",
		Spectate:     true,
		AllowPlugins: []string{"chat"},
	}
	if err := connOwner.WriteJSON(createMsg); err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	var hostCreateMsg protocol.ServerMessage
	if err := connOwner.ReadJSON(&hostCreateMsg); err != nil {
		t.Fatalf("Failed to read create room response: %v", err)
	}
	var hostCreateResp protocol.ServerRoomCreateEvent
	if err := json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp); err != nil {
		t.Fatalf("Failed to parse create room content: %v", err)
	}
	roomID := hostCreateResp.RoomID

	// 4. Owner joins room
	ownerJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connOwner.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(ownerJoinPayload)})

	for {
		var msg protocol.ServerMessage
		if err := connOwner.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read owner join: %v", err)
		}
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// 5. Guest joins room
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})

	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read guest join: %v", err)
		}
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Verify both clients are in the room
	s.hub.Mu.RLock()
	room, exists := s.hub.Rooms[roomID]
	s.hub.Mu.RUnlock()
	if !exists {
		t.Fatalf("Expected room %s to exist in hub", roomID)
	}

	room.Mu.RLock()
	clientCount := len(room.Clients)
	room.Mu.RUnlock()

	if clientCount != 2 {
		t.Errorf("Expected 2 clients in room, got %d", clientCount)
	}

	// 6. Owner leaves the room
	connOwner.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionLeaveRoom})
	time.Sleep(50 * time.Millisecond)

	// Verify room is NOT destroyed (Guest is still inside)
	s.hub.Mu.RLock()
	room, exists = s.hub.Rooms[roomID]
	s.hub.Mu.RUnlock()
	if !exists {
		t.Fatalf("Expected room %s to still exist after owner left, but it was destroyed", roomID)
	}

	room.Mu.RLock()
	clientCount = len(room.Clients)
	currentOwner := room.OwnerID
	room.Mu.RUnlock()

	if clientCount != 1 {
		t.Errorf("Expected 1 client (Guest) remaining in room, got %d", clientCount)
	}
	if currentOwner != guestResp.ID {
		t.Errorf("Expected owner to be transferred to guest %s, got %s", guestResp.ID, currentOwner)
	}

	// 7. Guest leaves the room -> room is now empty and should be destroyed immediately
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionLeaveRoom})
	time.Sleep(50 * time.Millisecond)

	// Verify room is destroyed in hub
	s.hub.Mu.RLock()
	_, exists = s.hub.Rooms[roomID]
	s.hub.Mu.RUnlock()
	if exists {
		t.Fatalf("Expected room %s to be destroyed immediately when empty, but it still exists", roomID)
	}

	_ = ownerResp
	_ = guestResp
}

// TestChangeRoleBetweenPlayerAndSpectator tests switching roles between player and spectator.
func TestChangeRoleBetweenPlayerAndSpectator(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect Host
	connHost, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect host: %v", err)
	}
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	// 2. Connect Guest
	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()
	guestResp := performHandshake(t, connGuest, "", "GuestUser")

	// Host creates public room with spectating allowed
	createMsg := struct {
		Type         string   `json:"type"`
		GameID       string   `json:"gameId"`
		Visibility   string   `json:"visibility"`
		Spectate     bool     `json:"spectate"`
		AllowPlugins []string `json:"allowPlugins"`
	}{
		Type:         protocol.ActionCreateRoom,
		GameID:       "tic-tac-toe",
		Visibility:   "public",
		Spectate:     true,
		AllowPlugins: []string{"chat"},
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})

	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest joins as Player
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})

	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest switches role to spectator
	changeRolePayload, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RoleSpectator})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeRolePayload)})

	// Both should receive room_detail broadcast showing Guest as spectator
	var detailMsg protocol.ServerMessage
	for {
		err := connGuest.ReadJSON(&detailMsg)
		if err != nil {
			t.Fatalf("Failed to read room_detail on role change: %v", err)
		}
		if detailMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(detailMsg.Content), &resp); err != nil {
				t.Fatalf("Failed to unmarshal room details: %v", err)
			}
			details := resp.RoomDetails
			if details.Spectators != 1 {
				t.Errorf("Expected 1 spectator, got %d", details.Spectators)
			}
			if len(details.Players) != 1 || details.Players[0].ID != hostResp.ID {
				t.Errorf("Expected only host in players list, got %+v", details.Players)
			}
			break
		}
	}

	// Guest switches back to player
	changeRoleBack, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RolePlayer})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeRoleBack)})

	for {
		err := connGuest.ReadJSON(&detailMsg)
		if err != nil {
			t.Fatalf("Failed to read room_detail on role switch back: %v", err)
		}
		if detailMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			json.Unmarshal([]byte(detailMsg.Content), &resp)
			details := resp.RoomDetails
			if details.Spectators != 0 {
				t.Errorf("Expected 0 spectators, got %d", details.Spectators)
			}
			var foundGuest bool
			for _, p := range details.Players {
				if p.ID == guestResp.ID {
					foundGuest = true
				}
			}
			if !foundGuest {
				t.Errorf("Expected guest in players list after switching to player")
			}
			break
		}
	}

	_ = hostResp
}

// TestPlayerReadyToggle tests player toggling ready status and receiving updated RoomDetails.
func TestPlayerReadyToggle(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect Host & Guest
	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest.Close()
	guestResp := performHandshake(t, connGuest, "", "GuestUser")

	// Host creates public room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest joins as Player
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest sets ready = true
	readyPayload, _ := json.Marshal(protocol.ClientReadyEvent{Ready: true})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionReady, Content: string(readyPayload)})

	// Verify room_detail update with ready = true for Guest
	for {
		var detailMsg protocol.ServerMessage
		err := connGuest.ReadJSON(&detailMsg)
		if err != nil {
			t.Fatalf("Failed to read room_detail after ready: %v", err)
		}
		if detailMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			json.Unmarshal([]byte(detailMsg.Content), &resp)
			details := resp.RoomDetails
			for _, p := range details.Players {
				if p.ID == hostResp.ID && !p.Ready {
					t.Errorf("Expected host to be ready, got ready=false")
				}
				if p.ID == guestResp.ID && !p.Ready {
					t.Errorf("Expected guest to be ready=true after ready action, got false")
				}
			}
			break
		}
	}

	// Guest sets ready = false
	unreadyPayload, _ := json.Marshal(protocol.ClientReadyEvent{Ready: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionReady, Content: string(unreadyPayload)})

	for {
		var detailMsg protocol.ServerMessage
		err := connGuest.ReadJSON(&detailMsg)
		if err != nil {
			t.Fatalf("Failed to read room_detail after unready: %v", err)
		}
		if detailMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			json.Unmarshal([]byte(detailMsg.Content), &resp)
			details := resp.RoomDetails
			for _, p := range details.Players {
				if p.ID == guestResp.ID && p.Ready {
					t.Errorf("Expected guest to be ready=false after cancel ready, got true")
				}
			}
			break
		}
	}
}

// TestExplicitRoomDetailQuery tests requesting room_detail on demand.
func TestExplicitRoomDetailQuery(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()
	userResp := performHandshake(t, conn, "", "QueryUser")

	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
	}
	conn.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	conn.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	joinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	conn.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(joinPayload)})
	for {
		var msg protocol.ServerMessage
		conn.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Explicitly query room_detail
	conn.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionRoomDetail})

	for {
		var detailMsg protocol.ServerMessage
		err := conn.ReadJSON(&detailMsg)
		if err != nil {
			t.Fatalf("Failed to read explicit room_detail response: %v", err)
		}
		if detailMsg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(detailMsg.Content), &resp); err != nil {
				t.Fatalf("Failed to unmarshal room_detail query response: %v", err)
			}
			details := resp.RoomDetails
			if details.RoomID != roomID {
				t.Errorf("Expected RoomID %q, got %q", roomID, details.RoomID)
			}
			if details.OwnerID != userResp.ID {
				t.Errorf("Expected OwnerID %q, got %q", userResp.ID, details.OwnerID)
			}
			if len(details.Players) != 1 || details.Players[0].ID != userResp.ID {
				t.Errorf("Expected 1 player with ID %q, got %v", userResp.ID, details.Players)
			}
			break
		}
	}
}

// TestRoomChannelErrorEvents tests that errors on the room and game channels without dedicated response structs return ErrorEvent.
func TestRoomChannelErrorEvents(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	// 1. Connect Host and Guest
	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest.Close()
	performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest joins room as Player
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Case 1: Non-host attempts to start game -> should receive ErrorEvent on room channel
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})
	var errEnvelope protocol.ServerMessage
	if err := connGuest.ReadJSON(&errEnvelope); err != nil {
		t.Fatalf("Failed to read start_game error response: %v", err)
	}
	if errEnvelope.Channel != protocol.RoomChannel {
		t.Errorf("Expected error channel %q, got %q", protocol.RoomChannel, errEnvelope.Channel)
	}
	if errEnvelope.Type != protocol.ActionError {
		t.Errorf("Expected error type %q, got %q", protocol.ActionError, errEnvelope.Type)
	}
	var errEvt protocol.ErrorEvent
	if err := json.Unmarshal([]byte(errEnvelope.Content), &errEvt); err != nil {
		t.Fatalf("Failed to unmarshal ErrorEvent content: %v", err)
	}
	if errEvt.Error == nil || errEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrNotPermitted, errEvt.Error)
	}
	if errEvt.Error != nil && errEvt.Error.Message != "Only the room host can start the game" {
		t.Errorf("Expected message 'Only the room host can start the game', got %q", errEvt.Error.Message)
	}

	// Case 2: Switch to spectator and attempt ready -> should receive ErrorEvent
	changeRolePayload, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RoleSpectator})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeRolePayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	readyPayload, _ := json.Marshal(protocol.ClientReadyEvent{Ready: true})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionReady, Content: string(readyPayload)})
	if err := connGuest.ReadJSON(&errEnvelope); err != nil {
		t.Fatalf("Failed to read spectator ready error: %v", err)
	}
	if errEnvelope.Channel != protocol.RoomChannel || errEnvelope.Type != protocol.ActionError {
		t.Errorf("Expected error on room channel, got channel=%s, type=%s", errEnvelope.Channel, errEnvelope.Type)
	}
	var readyErrEvt protocol.ErrorEvent
	if err := json.Unmarshal([]byte(errEnvelope.Content), &readyErrEvt); err != nil {
		t.Fatalf("Failed to unmarshal spectator ready error: %v", err)
	}
	if readyErrEvt.Error == nil || readyErrEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrNotPermitted, readyErrEvt.Error)
	}

	// Case 3: Spectator attempts game action -> should receive ErrorEvent on game channel
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":0}`})
	var gameErrEnvelope protocol.ServerMessage
	if err := connGuest.ReadJSON(&gameErrEnvelope); err != nil {
		t.Fatalf("Failed to read spectator game action error: %v", err)
	}
	if gameErrEnvelope.Channel != protocol.GameChannel || gameErrEnvelope.Type != protocol.ActionError {
		t.Errorf("Expected game channel error, got channel=%s, type=%s", gameErrEnvelope.Channel, gameErrEnvelope.Type)
	}
	var gameErrEvt protocol.ErrorEvent
	if err := json.Unmarshal([]byte(gameErrEnvelope.Content), &gameErrEvt); err != nil {
		t.Fatalf("Failed to unmarshal game action ErrorEvent: %v", err)
	}
	if gameErrEvt.Error == nil || gameErrEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrNotPermitted, gameErrEvt.Error)
	}

	// Case 4: Client not in room sends room action
	connNotInRoom, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connNotInRoom.Close()
	performHandshake(t, connNotInRoom, "", "LobbyUser")

	connNotInRoom.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionRoomDetail})
	var lobbyErrEnvelope protocol.ServerMessage
	if err := connNotInRoom.ReadJSON(&lobbyErrEnvelope); err != nil {
		t.Fatalf("Failed to read not in room error: %v", err)
	}
	var lobbyErrEvt protocol.ErrorEvent
	if err := json.Unmarshal([]byte(lobbyErrEnvelope.Content), &lobbyErrEvt); err != nil {
		t.Fatalf("Failed to unmarshal lobby ErrorEvent: %v", err)
	}
	if lobbyErrEvt.Error == nil || lobbyErrEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected error code %q, got %+v", protocol.ErrNotPermitted, lobbyErrEvt.Error)
	}
}

// TestStartGameAndEndGameBroadcastsRoomDetails verifies that starting a game and ending a game broadcasts updated RoomDetails.
func TestStartGameAndEndGameBroadcastsRoomDetails(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest.Close()
	performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest joins room
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Guest should receive RoomDetails with Status = "playing" on RoomChannel
	var receivedPlayingStatus bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read message on guest: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &resp); err != nil {
				t.Fatalf("Failed to unmarshal RoomDetails: %v", err)
			}
			if resp.RoomDetails != nil && resp.RoomDetails.Status == game_plugin.StatusPlaying {
				receivedPlayingStatus = true
				break
			}
		}
	}
	if !receivedPlayingStatus {
		t.Errorf("Expected RoomDetails with status 'playing' after host starts game")
	}
}

// TestGameStartAndStateBroadcast verifies that starting a game sends initial state and action updates on GameChannel.
func TestGameStartAndStateBroadcast(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest.Close()
	guestResp := performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host & Guest join room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Both should receive initial game state on GameChannel
	var hostGotState, guestGotState bool
	for {
		var msg protocol.ServerMessage
		if err := connHost.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read host message: %v", err)
		}
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			hostGotState = true
			break
		}
	}
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read guest message: %v", err)
		}
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			guestGotState = true
			break
		}
	}

	if !hostGotState || !guestGotState {
		t.Errorf("Expected both host and guest to receive initial game state")
	}

	// Host makes move (cell 0)
	movePayload := `{"cell":0}`
	connHost.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "mark_cell",
		Content: movePayload,
	})

	// Guest receives state update with move applied
	var moveApplied bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read guest message after move: %v", err)
		}
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			type TTTView struct {
				Board [9]string `json:"board"`
				Turn  string    `json:"turn"`
			}
			var view TTTView
			if err := json.Unmarshal([]byte(msg.Content), &view); err == nil {
				if view.Board[0] != "" && view.Turn == guestResp.ID {
					moveApplied = true
					break
				}
			}
		}
	}

	if !moveApplied {
		t.Errorf("Expected move to be applied and turn switched to guest")
	}
	_ = hostResp
}

// TestReconnectionDuringActiveGame verifies that a player disconnecting from an active game can reconnect, rejoin as player, and receive game state.
func TestReconnectionDuringActiveGame(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	guestResp := performHandshake(t, connGuest, "", "GuestUser")
	guestID := guestResp.ID

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Both join
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Drain game start on host
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			break
		}
	}

	// Host plays move 0
	connHost.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "mark_cell",
		Content: `{"cell":0}`,
	})

	// Guest disconnects unexpectedly
	connGuest.Close()
	time.Sleep(50 * time.Millisecond)

	// Guest reconnects with the same ID
	rejoinWSURL := fmt.Sprintf("%s&id=%s", wsURL, guestID)
	connGuest2, _, err := websocket.DefaultDialer.Dial(rejoinWSURL, nil)
	if err != nil {
		t.Fatalf("Failed to reconnect guest: %v", err)
	}
	defer connGuest2.Close()

	rejoinHi := performHandshake(t, connGuest2, guestID, "GuestUser")
	if rejoinHi.ID != guestID {
		t.Fatalf("Expected rejoined ID %q, got %q", guestID, rejoinHi.ID)
	}

	// Guest rejoins the active game room
	rejoinRoomPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connGuest2.WriteJSON(protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionJoinRoom,
		Content: string(rejoinRoomPayload),
	})

	// Guest should receive JoinRoom response with role = player, status = playing, and current GameState
	var rejoinJoinResp protocol.ServerJoinRoom
	var guestGotStateAfterRejoin bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest2.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read rejoin response: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionJoinRoom {
			if err := json.Unmarshal([]byte(msg.Content), &rejoinJoinResp); err != nil {
				t.Fatalf("Failed to unmarshal join room response: %v", err)
			}
		}
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			type TTTView struct {
				Board [9]string `json:"board"`
			}
			var view TTTView
			json.Unmarshal([]byte(msg.Content), &view)
			if view.Board[0] != "" {
				guestGotStateAfterRejoin = true
			}
		}
		if rejoinJoinResp.RoomDetails != nil && guestGotStateAfterRejoin {
			break
		}
	}

	if rejoinJoinResp.Error != nil {
		t.Fatalf("Expected nil error on rejoin, got %+v", rejoinJoinResp.Error)
	}
	if rejoinJoinResp.RoomDetails == nil {
		t.Fatalf("Expected RoomDetails on rejoin")
	}
	if rejoinJoinResp.RoomDetails.Status != game_plugin.StatusPlaying {
		t.Errorf("Expected room status 'playing', got %s", rejoinJoinResp.RoomDetails.Status)
	}

	// Find guest in players
	var guestFound bool
	for _, p := range rejoinJoinResp.RoomDetails.Players {
		if p.ID == guestID {
			guestFound = true
		}
	}
	if !guestFound {
		t.Errorf("Expected reconnected player to be present in players list")
	}

	if !guestGotStateAfterRejoin {
		t.Errorf("Expected reconnected player to receive game state with existing moves")
	}
	_ = hostResp
}

// TestConfigurableTimeoutsAndDurations verifies reading timeout configs from environment variables.
func TestConfigurableTimeoutsAndDurations(t *testing.T) {
	// Set custom environment variables
	os.Setenv("DISCONNECT_GRACE_SECS", "45")
	os.Setenv("ROOM_INACTIVITY_SECS", "900")
	os.Setenv("WS_WRITE_WAIT_SECS", "5")
	os.Setenv("WS_PONG_WAIT_SECS", "30")
	os.Setenv("WS_MAX_MESSAGE_SIZE_BYTES", "1048576")
	config.ResetEnv()

	defer func() {
		os.Unsetenv("DISCONNECT_GRACE_SECS")
		os.Unsetenv("ROOM_INACTIVITY_SECS")
		os.Unsetenv("WS_WRITE_WAIT_SECS")
		os.Unsetenv("WS_PONG_WAIT_SECS")
		os.Unsetenv("WS_MAX_MESSAGE_SIZE_BYTES")
		os.Setenv("RATE_LIMIT_RPS", "1000")
		os.Setenv("RATE_LIMIT_BURST", "1000")
		config.ResetEnv()
		middleware.ResetRateLimiter()
	}()

	cfg := config.LoadEnv()
	if cfg.DisconnectGraceSecs != 45 {
		t.Errorf("Expected DisconnectGraceSecs = 45, got %d", cfg.DisconnectGraceSecs)
	}
	if cfg.RoomInactivitySecs != 900 {
		t.Errorf("Expected RoomInactivitySecs = 900, got %d", cfg.RoomInactivitySecs)
	}
	if cfg.WsWriteWaitSecs != 5 {
		t.Errorf("Expected WsWriteWaitSecs = 5, got %d", cfg.WsWriteWaitSecs)
	}
	if cfg.WsPongWaitSecs != 30 {
		t.Errorf("Expected WsPongWaitSecs = 30, got %d", cfg.WsPongWaitSecs)
	}
	if cfg.WsMaxMessageSizeBytes != 1048576 {
		t.Errorf("Expected WsMaxMessageSizeBytes = 1048576, got %d", cfg.WsMaxMessageSizeBytes)
	}

	// Verify game plugin InactivityDuration uses config
	ttt, err := game_plugin.Get("Tic-Tac-Toe")
	if err != nil {
		t.Fatalf("Failed to get Tic-Tac-Toe game plugin: %v", err)
	}
	tttInst, err := ttt.StartGame(nil, []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	})
	if err != nil {
		t.Fatalf("Failed to start Tic-Tac-Toe game: %v", err)
	}
	if tttInst.InactivityDuration() != 900 {
		t.Errorf("Expected Tic-Tac-Toe InactivityDuration = 900, got %d", tttInst.InactivityDuration())
	}

	uno, err := game_plugin.Get("Uno")
	if err != nil {
		t.Fatalf("Failed to get uno game plugin: %v", err)
	}
	unoInst, err := uno.StartGame(nil, []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	})
	if err != nil {
		t.Fatalf("Failed to start uno game: %v", err)
	}
	if unoInst.InactivityDuration() != 900 {
		t.Errorf("Expected Uno InactivityDuration = 900, got %d", unoInst.InactivityDuration())
	}

	bingo, err := game_plugin.Get("Bingo")
	if err != nil {
		t.Fatalf("Failed to get bingo game plugin: %v", err)
	}
	bingoInst, err := bingo.StartGame(nil, []game_plugin.PlayerInfo{
		{ID: "p1", Name: "Player 1"},
		{ID: "p2", Name: "Player 2"},
	})
	if err != nil {
		t.Fatalf("Failed to start bingo game: %v", err)
	}
	if bingoInst.InactivityDuration() != 900 {
		t.Errorf("Expected Bingo InactivityDuration = 900, got %d", bingoInst.InactivityDuration())
	}
}

func TestBingoGameFlow(t *testing.T) {
	middleware.ResetRateLimiter()
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect host: %v", err)
	}
	defer connHost.Close()

	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()

	hostHi := performHandshake(t, connHost, "", "HostUser")
	guestHi := performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room for Bingo
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "bingo",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Both join room
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Both should receive initial game state in "setup" status
	var hostSetupState map[string]any
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			json.Unmarshal([]byte(msg.Content), &hostSetupState)
			break
		}
	}
	if hostSetupState["status"] != "setup" {
		t.Fatalf("expected initial state status to be 'setup', got '%v'", hostSetupState["status"])
	}

	// Both players send ready
	readyPayload := `{"random": true}`
	connHost.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "ready",
		Content: readyPayload,
	})
	connGuest.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "ready",
		Content: readyPayload,
	})

	// Once both ready, state becomes "playing"
	var playingState map[string]any
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			var state map[string]any
			json.Unmarshal([]byte(msg.Content), &state)
			if state["status"] == "playing" {
				playingState = state
				break
			}
		}
	}
	if playingState["status"] != "playing" {
		t.Fatalf("expected state status to be 'playing', got '%v'", playingState["status"])
	}

	// Turn player marks number 12
	turnPlayerID := playingState["currentTurn"].(string)
	markConn := connHost
	if turnPlayerID == guestHi.ID {
		markConn = connGuest
	}
	_ = hostHi

	markConn.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "mark",
		Content: `{"number": 12}`,
	})

	// Check updated state has marked number 12
	var markedState map[string]any
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			var state map[string]any
			json.Unmarshal([]byte(msg.Content), &state)
			nums, ok := state["markedNumbers"].([]any)
			if ok && len(nums) > 0 {
				markedState = state
				break
			}
		}
	}
	nums := markedState["markedNumbers"].([]any)
	if len(nums) != 1 || int(nums[0].(float64)) != 12 {
		t.Fatalf("expected markedNumbers to contain [12], got %v", nums)
	}
}

func TestRoomStatusTransitions(t *testing.T) {
	middleware.ResetRateLimiter()
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect host: %v", err)
	}
	defer connHost.Close()

	connGuest, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect guest: %v", err)
	}
	defer connGuest.Close()

	hostHi := performHandshake(t, connHost, "", "HostUser")
	guestHi := performHandshake(t, connGuest, "", "GuestUser")
	_ = hostHi
	_ = guestHi

	// 1. Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest joins -> verify room status is "waiting"
	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			var joinResp protocol.ServerJoinRoom
			json.Unmarshal([]byte(msg.Content), &joinResp)
			if joinResp.RoomDetails != nil {
				if joinResp.RoomDetails.Status != game_plugin.StatusWaiting {
					t.Fatalf("Expected room status 'waiting', got %s", joinResp.RoomDetails.Status)
				}
			}
			break
		}
	}

	// 2. Host starts game -> verify transitions to starting, then playing
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	var gotStarting, gotPlaying bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read guest message: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var detailResp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &detailResp); err == nil && detailResp.RoomDetails != nil {
				if detailResp.RoomDetails.Status == game_plugin.StatusStarting {
					gotStarting = true
				} else if detailResp.RoomDetails.Status == game_plugin.StatusPlaying {
					gotPlaying = true
					break
				}
			}
		}
	}

	if !gotStarting {
		t.Errorf("Expected to receive room_detail with status 'starting'")
	}
	if !gotPlaying {
		t.Errorf("Expected to receive room_detail with status 'playing'")
	}

	// 3. Attempt lobby action (ready) while playing -> must receive ErrorEvent
	connGuest.WriteJSON(protocol.ClientMessage{
		Channel: protocol.RoomChannel,
		Type:    protocol.ActionReady,
		Content: `{"ready": false}`,
	})
	var errEnvelope protocol.ServerMessage
	for {
		if err := connGuest.ReadJSON(&errEnvelope); err != nil {
			t.Fatalf("Failed to read message on guest: %v", err)
		}
		if errEnvelope.Type == protocol.ActionError {
			break
		}
	}
	var errEvt protocol.ErrorEvent
	json.Unmarshal([]byte(errEnvelope.Content), &errEvt)
	if errEvt.Error == nil || errEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected ErrNotPermitted when attempting ready while playing, got %+v", errEvt.Error)
	}

	// 4. Play game moves to completion: Host(0), Guest(3), Host(1), Guest(4), Host(2) -> Host wins
	// Drain host messages until playing state
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			break
		}
	}

	// Host: 0
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":0}`})
	time.Sleep(20 * time.Millisecond)
	// Guest: 3
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":3}`})
	time.Sleep(20 * time.Millisecond)
	// Host: 1
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":1}`})
	time.Sleep(20 * time.Millisecond)
	// Guest: 4
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":4}`})
	time.Sleep(20 * time.Millisecond)
	// Host: 2 -> Wins!
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.GameChannel, Type: "mark_cell", Content: `{"cell":2}`})

	// 5. Verify transitions through ending to gameover, then to waiting
	var gotEnding, gotGameOverStatus, gotGameOverAction, gotWaitingStatus bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read message on guest: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var detailResp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &detailResp); err == nil && detailResp.RoomDetails != nil {
				if detailResp.RoomDetails.Status == game_plugin.StatusEnding {
					gotEnding = true
				} else if detailResp.RoomDetails.Status == game_plugin.StatusGameOver {
					gotGameOverStatus = true
				} else if detailResp.RoomDetails.Status == game_plugin.StatusWaiting && gotGameOverStatus {
					gotWaitingStatus = true
					if gotGameOverAction {
						break
					}
				}
			}
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionGameover {
			gotGameOverAction = true
			var gameOverEvt protocol.ServerGameoverDetails
			if err := json.Unmarshal([]byte(msg.Content), &gameOverEvt); err != nil {
				t.Fatalf("Failed to unmarshal ServerGameoverDetails: %v", err)
			}
			if gameOverEvt.GameoverDetails == nil {
				t.Fatalf("Expected GameoverDetails in ServerGameoverDetails, got nil")
			}
			details := gameOverEvt.GameoverDetails
			if details.IsDraw {
				t.Errorf("Expected IsDraw false for host win, got true")
			}
			if len(details.Players) != 2 {
				t.Fatalf("Expected 2 players in GameOverEvent, got %d", len(details.Players))
			}
			if details.Players[0].ID != hostHi.ID {
				t.Errorf("Expected winner player[0] ID %q, got %q", hostHi.ID, details.Players[0].ID)
			}
			if details.Players[1].ID != guestHi.ID {
				t.Errorf("Expected loser player[1] ID %q, got %q", guestHi.ID, details.Players[1].ID)
			}
			if gotWaitingStatus {
				break
			}
		}
	}

	if !gotEnding {
		t.Errorf("Expected to receive room_detail with status 'ending'")
	}
	if !gotGameOverStatus {
		t.Errorf("Expected to receive room_detail with status 'gameover'")
	}
	if !gotGameOverAction {
		t.Errorf("Expected to receive gameover action on room channel")
	}
	if !gotWaitingStatus {
		t.Errorf("Expected to receive room_detail with status 'waiting' after gameover")
	}
}

// TestDisconnectTimeoutMovesToLobbyWithError verifies that when a player disconnects and times out,
// the remaining player receives an error event (ErrPlayerLeft) on the room channel and the room transitions back to waiting lobby.
func TestDisconnectTimeoutMovesToLobbyWithError(t *testing.T) {
	os.Setenv("DISCONNECT_GRACE_SECS", "1")
	config.ResetEnv()
	defer func() {
		os.Unsetenv("DISCONNECT_GRACE_SECS")
		config.ResetEnv()
	}()

	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Both join
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Drain game start on host
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			break
		}
	}

	// Host plays move 0
	connHost.WriteJSON(protocol.ClientMessage{
		Channel: protocol.GameChannel,
		Type:    "mark_cell",
		Content: `{"cell":0}`,
	})

	// Guest disconnects unexpectedly
	connGuest.Close()

	// Host should receive:
	// 1. ErrorEvent on room channel with ErrPlayerLeft ("Opponent left the match")
	// 2. RoomDetail on room channel with Status = waiting
	var gotPlayerLeftError, gotLobbyWaiting bool
	for {
		var msg protocol.ServerMessage
		if err := connHost.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read message on host: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionError {
			var errEvt protocol.ErrorEvent
			if err := json.Unmarshal([]byte(msg.Content), &errEvt); err == nil && errEvt.Error != nil {
				if errEvt.Error.Code == protocol.ErrPlayerLeft {
					gotPlayerLeftError = true
					if errEvt.Error.Message != "Opponent left the match" {
						t.Errorf("Expected message 'Opponent left the match', got %q", errEvt.Error.Message)
					}
				}
			}
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var detailResp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &detailResp); err == nil && detailResp.RoomDetails != nil {
				if detailResp.RoomDetails.Status == game_plugin.StatusWaiting {
					gotLobbyWaiting = true
				}
			}
		}
		if gotPlayerLeftError && gotLobbyWaiting {
			break
		}
	}

	if !gotPlayerLeftError {
		t.Errorf("Expected host to receive ErrPlayerLeft error event on room channel")
	}
	if !gotLobbyWaiting {
		t.Errorf("Expected room to transition back to waiting status in lobby")
	}
}

// TestOwnerTimeoutTransfersOwnershipToGuestAndResetsReady verifies that when the owner disconnects and times out during a match:
// 1. Ready statuses of players were reset when game started.
// 2. The remaining guest receives the ErrPlayerLeft error event.
// 3. Ownership is transferred to the remaining guest in the lobby.
func TestOwnerTimeoutTransfersOwnershipToGuestAndResetsReady(t *testing.T) {
	os.Setenv("DISCONNECT_GRACE_SECS", "1")
	config.ResetEnv()
	defer func() {
		os.Unsetenv("DISCONNECT_GRACE_SECS")
		config.ResetEnv()
	}()

	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	performHandshake(t, connHost, "", "HostUser")

	connGuest, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest.Close()
	guestResp := performHandshake(t, connGuest, "", "GuestUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Both join
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	guestJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guestJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Guest sets ready = true in lobby
	readyPayload, _ := json.Marshal(protocol.ClientReadyEvent{Ready: true})
	connGuest.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionReady, Content: string(readyPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			break
		}
	}

	// Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// Drain game start on guest
	for {
		var msg protocol.ServerMessage
		connGuest.ReadJSON(&msg)
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			break
		}
	}

	// Host disconnects during active game
	connHost.Close()

	// Guest should receive:
	// 1. ErrorEvent on room channel with ErrPlayerLeft ("Opponent left the match")
	// 2. RoomDetail on room channel with Status = waiting AND OwnerID = guestResp.ID
	var gotPlayerLeftError, gotNewOwnerLobby bool
	for {
		var msg protocol.ServerMessage
		if err := connGuest.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read message on guest: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionError {
			var errEvt protocol.ErrorEvent
			if err := json.Unmarshal([]byte(msg.Content), &errEvt); err == nil && errEvt.Error != nil {
				if errEvt.Error.Code == protocol.ErrPlayerLeft {
					gotPlayerLeftError = true
					if errEvt.Error.Message != "Opponent left the match" {
						t.Errorf("Expected message 'Opponent left the match', got %q", errEvt.Error.Message)
					}
				}
			}
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var detailResp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &detailResp); err == nil && detailResp.RoomDetails != nil {
				if detailResp.RoomDetails.Status == game_plugin.StatusWaiting && detailResp.RoomDetails.OwnerID == guestResp.ID {
					gotNewOwnerLobby = true
				}
			}
		}
		if gotPlayerLeftError && gotNewOwnerLobby {
			break
		}
	}

	if !gotPlayerLeftError {
		t.Errorf("Expected guest to receive ErrPlayerLeft error event on room channel")
	}
	if !gotNewOwnerLobby {
		t.Errorf("Expected guest to become new room owner in waiting lobby")
	}
}

// TestOwnerAsSpectatorFlow verifies that a room host can join as a spectator,
// retain exclusive start_game authority, and start a game with active players.
func TestOwnerAsSpectatorFlow(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	connGuest1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest1.Close()
	guest1Resp := performHandshake(t, connGuest1, "", "GuestOne")

	connGuest2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connGuest2.Close()
	guest2Resp := performHandshake(t, connGuest2, "", "GuestTwo")

	// 1. Host creates room with spectate: true
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// 2. Host joins room as spectator
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: true})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})

	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			var resp protocol.ServerJoinRoom
			json.Unmarshal([]byte(msg.Content), &resp)
			if resp.RoomDetails == nil {
				t.Fatalf("Expected RoomDetails in host join response")
			}
			if resp.RoomDetails.Spectators != 1 {
				t.Errorf("Expected 1 spectator, got %d", resp.RoomDetails.Spectators)
			}
			if len(resp.RoomDetails.Players) != 0 {
				t.Errorf("Expected 0 players, got %d", len(resp.RoomDetails.Players))
			}
			if resp.RoomDetails.OwnerID != hostResp.ID {
				t.Errorf("Expected OwnerID %q, got %q", hostResp.ID, resp.RoomDetails.OwnerID)
			}
			break
		}
	}

	// 3. Guest 1 joins as player
	guest1JoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest1.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guest1JoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest1.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// 4. Guest 1 attempts start_game -> should fail (only host can start)
	connGuest1.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})
	var errEnvelope protocol.ServerMessage
	connGuest1.ReadJSON(&errEnvelope)
	var errEvt protocol.ErrorEvent
	json.Unmarshal([]byte(errEnvelope.Content), &errEvt)
	if errEvt.Error == nil || errEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected ErrNotPermitted for guest start_game, got %+v", errEvt.Error)
	}

	// 5. Host attempts start_game with only 1 player -> should fail (need 2 players)
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})
	connHost.ReadJSON(&errEnvelope)
	json.Unmarshal([]byte(errEnvelope.Content), &errEvt)
	if errEvt.Error == nil || errEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected ErrNotPermitted when starting with 1 player, got %+v", errEvt.Error)
	}

	// 6. Guest 2 joins as player
	guest2JoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connGuest2.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(guest2JoinPayload)})
	for {
		var msg protocol.ServerMessage
		connGuest2.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// 7. Host starts game
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionStartGame})

	// 8. Verify host (spectator) receives room_detail playing status and game state update
	var hostGotPlaying, hostGotGameState bool
	for {
		var msg protocol.ServerMessage
		if err := connHost.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read on host: %v", err)
		}
		if msg.Channel == protocol.RoomChannel && msg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			if err := json.Unmarshal([]byte(msg.Content), &resp); err == nil && resp.RoomDetails != nil {
				if resp.RoomDetails.Status == game_plugin.StatusPlaying {
					hostGotPlaying = true
				}
			}
		}
		if msg.Channel == protocol.GameChannel && msg.Type == protocol.ActionStateUpdate {
			hostGotGameState = true
		}
		if hostGotPlaying && hostGotGameState {
			break
		}
	}

	if !hostGotPlaying {
		t.Errorf("Expected host to receive playing status in room_detail")
	}
	if !hostGotGameState {
		t.Errorf("Expected spectator host to receive game state update")
	}

	// Suppress unused variable warnings
	_ = guest1Resp
	_ = guest2Resp
}

// TestOwnerSwitchToSpectatorInLobby verifies role switching for the room owner.
func TestOwnerSwitchToSpectatorInLobby(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	hostResp := performHandshake(t, connHost, "", "HostUser")

	// Host creates room
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   bool   `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   true,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host joins room as Player
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})
	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host switches to Spectator
	changeRolePayload, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RoleSpectator})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeRolePayload)})

	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			json.Unmarshal([]byte(msg.Content), &resp)
			if resp.RoomDetails != nil {
				if resp.RoomDetails.Spectators == 1 && len(resp.RoomDetails.Players) == 0 {
					break
				}
			}
		}
	}

	// Host switches back to Player
	changeToPlayerPayload, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RolePlayer})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeToPlayerPayload)})

	for {
		var msg protocol.ServerMessage
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionRoomDetail {
			var resp protocol.ServerRoomDetail
			json.Unmarshal([]byte(msg.Content), &resp)
			if resp.RoomDetails != nil {
				if resp.RoomDetails.Spectators == 0 && len(resp.RoomDetails.Players) == 1 {
					if resp.RoomDetails.Players[0].ID == hostResp.ID && resp.RoomDetails.Players[0].Ready {
						break
					}
				}
			}
		}
	}
}

// TestOwnerCannotSpectateWhenSpectateDisabled verifies that spectating cannot be used if room spectate is false.
func TestOwnerCannotSpectateWhenSpectateDisabled(t *testing.T) {
	s := &Server{}
	s.Setup()

	ts := httptest.NewServer(s.SetupRouter())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?client=nebula"

	connHost, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer connHost.Close()
	performHandshake(t, connHost, "", "HostUser")

	// Host creates room with spectate: false
	spectateFalse := false
	createMsg := struct {
		Type       string `json:"type"`
		GameID     string `json:"gameId"`
		Visibility string `json:"visibility"`
		Spectate   *bool  `json:"spectate"`
	}{
		Type:       protocol.ActionCreateRoom,
		GameID:     "tic-tac-toe",
		Visibility: "public",
		Spectate:   &spectateFalse,
	}
	connHost.WriteJSON(createMsg)

	var hostCreateMsg protocol.ServerMessage
	connHost.ReadJSON(&hostCreateMsg)
	var hostCreateResp protocol.ServerRoomCreateEvent
	json.Unmarshal([]byte(hostCreateMsg.Content), &hostCreateResp)
	roomID := hostCreateResp.RoomID

	// Host attempts to join as spectator -> rejected
	hostJoinPayload, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: true})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayload)})

	var msg protocol.ServerMessage
	connHost.ReadJSON(&msg)
	var joinResp protocol.ServerJoinRoom
	json.Unmarshal([]byte(msg.Content), &joinResp)
	if joinResp.Error == nil || joinResp.Error.Code != protocol.ErrSpectatedNotAllowed {
		t.Errorf("Expected ErrSpectatedNotAllowed, got %+v", joinResp.Error)
	}

	// Host joins as player -> succeeds
	hostJoinPayloadPlayer, _ := json.Marshal(protocol.ClientJoinRoomEvent{RoomID: roomID, Spectate: false})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionJoinRoom, Content: string(hostJoinPayloadPlayer)})
	for {
		connHost.ReadJSON(&msg)
		if msg.Type == protocol.ActionJoinRoom {
			break
		}
	}

	// Host attempts change_role to spectator -> rejected
	changeRolePayload, _ := json.Marshal(protocol.ClientChangeRoleEvent{Role: game_plugin.RoleSpectator})
	connHost.WriteJSON(protocol.ClientMessage{Channel: protocol.RoomChannel, Type: protocol.ActionChangeRole, Content: string(changeRolePayload)})

	connHost.ReadJSON(&msg)
	var errEvt protocol.ErrorEvent
	json.Unmarshal([]byte(msg.Content), &errEvt)
	if errEvt.Error == nil || errEvt.Error.Code != protocol.ErrNotPermitted {
		t.Errorf("Expected ErrNotPermitted for change_role to spectator when disabled, got %+v", errEvt.Error)
	}
}
