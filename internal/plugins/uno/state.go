package uno

import (
	"encoding/json"
	"fmt"
	"slices"
	game_plugin "pulsar/internal/plugin/game"
)

// UnoState manages the state transitions and rules of uno game.
type UnoState struct {
	drawPile     []Card                // players draw card from this pile
	discardPile  []Card                // players discards their cards in this pile
	players      map[string]*UnoPlayer // playerID -> uno player
	disconnected map[string]bool       // playerID -> disconnected
	playerIDs    []string              // player IDs in order of turn
	turn         string                // current turn (refers to playerID)
	status       Status                // game status
	passTurn     bool                  // Know if current turn can be passed
	finished     bool                  // Know if game is finished
}

// NewState instantiates a new uno state with participating players.
func NewState(players []game_plugin.PlayerInfo) *UnoState {
	s := &UnoState{
		players:      make(map[string]*UnoPlayer),
		disconnected: make(map[string]bool),
		playerIDs:    make([]string, 0, len(players)),
		status:       playing,
		finished:     false,
		passTurn:     false,
	}

	for _, p := range players {
		s.playerIDs = append(s.playerIDs, p.ID)
		s.players[p.ID] = &UnoPlayer{
			Id:   p.ID,
			Name: p.Name,
		}
	}

	s.Init()
	return s
}

// Init initialises the state with cards and its distribution to players.
func (s *UnoState) Init() {
	deck := NewDeck()
	Shuffle(deck)
	s.drawPile = deck
	s.discardPile = make([]Card, 0)
	if len(s.playerIDs) > 0 {
		s.turn = s.playerIDs[0]
	}
	s.status = playing
	s.finished = false
	s.passTurn = false
}

// Describe the in game actions
type Action string

const (
	startGame Action = "start_game"
	drawCard  Action = "draw_card"
	putCard   Action = "put_card"
	passTurn  Action = "pass_turn"
)

// MakeMove handles player actions in the game. It only commits action if its played by
// current turn player. It returns two values. First value tells if state has been
// mutated. Second value provides error. If there is error the state is not mutated.
func (s *UnoState) MakeMove(playerID string, action Action, payload json.RawMessage) (bool, error) {
	player := s.players[playerID]
	if player.Id != playerID {
		return false, nil
	}

	var updated bool
	var err error

	switch action {
	case startGame:
		var detail DrawCardDetail
		if err := json.Unmarshal(payload, &detail); err != nil {
			return false, fmt.Errorf("Invalid action payload: %w", err)
		}
		updated, err = s.DrawCard(playerID, detail)
	case drawCard:
		var detail DrawCardDetail
		if err := json.Unmarshal(payload, &detail); err != nil {
			return false, fmt.Errorf("Invalid action payload: %w", err)
		}
		updated, err = s.DrawCard(playerID, detail)
	case putCard:
	}

	return updated, err
}

// RefillCards places more cards in the drawPile.
func (s *UnoState) RefillCards() {
	deck := NewDeck()
	Shuffle(deck)
	s.drawPile = append(s.drawPile, deck...)
}

// NextTurn switches to the turn of next player.
func (s *UnoState) NextTurn() {
	s.passTurn = false

	count := len(s.playerIDs)
	start := slices.Index(s.playerIDs, s.turn)
	for i := range count {
		index := (start + i) % count
		player := s.players[s.playerIDs[index]]
		if len(player.Cards) != 0 {
			s.turn = player.Id
			break
		}
	}

	// Check gameover when only one player has cards
}

type StartGameDetail struct{}

// StartGame starts the game.
func (s *UnoState) StartGame(playerID string, detail StartGameDetail) (bool, error) {
	if GameMinPlayers < len(s.playerIDs) {
		return false, fmt.Errorf("Minimum %d players required to start game", GameMinPlayers)
	}

	s.Init()
	return true, nil
}

// Details about draw card action
type DrawCardDetail struct {
}

func (s *UnoState) DrawCard(playerID string, detail DrawCardDetail) (bool, error) {
	if len(s.drawPile) == 0 {
		s.RefillCards()
	}

	// TODO: draw cards based on the last cards

	card := s.drawPile[0]
	player := s.players[playerID]
	player.Cards = append(player.Cards, card)

	s.passTurn = true

	return true, nil
}

// Details about put card action
type PutCardDetail struct {
	Index int `json:"index"` // Index of the card in player cards
}

func (s *UnoState) PutCard(playerID string, detail PutCardDetail) (bool, error) {
	// TODO: check if this card can be put or not

	player := s.players[playerID]
	cards := player.Cards
	index := detail.Index
	player.Cards = append(cards[:index], cards[index+1:]...)

	s.NextTurn()

	return true, nil
}

type PassTurnDetail struct{}

// PassTurn passes the turn to next player if current turn player has drawn card
func (s *UnoState) PassTurn() (bool, error) {
	if s.passTurn {
		s.NextTurn()
		return true, nil
	}

	return false, nil
}
