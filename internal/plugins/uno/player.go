package uno

// Implements the placer info
type UnoPlayer struct {
	Id    string
	Name  string
	Cards []Card
}

// Implements the player view state for clients
type PlayerState struct {
	Name  string `json:"name"`
	Cards int    `json:"cards"`
}
