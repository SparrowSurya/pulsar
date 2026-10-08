package uno

import (
	"fmt"
	"math/rand/v2"
)

type CardColor string

const (
	redColor    CardColor = "red"
	yellowColor CardColor = "yellow"
	greenColor  CardColor = "green"
	blueColor   CardColor = "blue"
	wildColor   CardColor = "wild"
)

type CardType string

const (
	zero  CardType = "0"
	one   CardType = "1"
	two   CardType = "2"
	three CardType = "3"
	four  CardType = "4"
	five  CardType = "5"
	six   CardType = "6"
	seven CardType = "7"
	eight CardType = "8"
	nine  CardType = "9"

	skip    CardType = "skip"
	reverse CardType = "reverse"
	drawTwo CardType = "draw_two"

	wildColorChange CardType = "wild_color_change"
	wildDrawFour    CardType = "wild_draw_four"
)

type Card struct {
	ID    string    `json:"id"`
	Color CardColor `json:"color"`
	Type  CardType  `json:"type"`
}

func NewDeck() []Card {
	colors := []CardColor{redColor, yellowColor, greenColor, blueColor}
	numbers := []CardType{
		zero, one, two, three, four,
		five, six, seven, eight, nine,
	}

	deck := make([]Card, 0, 108)

	id := 0
	addCard := func(color CardColor, t CardType) {
		id++
		deck = append(deck, Card{
			ID:    fmt.Sprintf("%03d", id),
			Color: color,
			Type:  t,
		})
	}

	for _, color := range colors {
		addCard(color, zero)

		for _, number := range numbers[1:] {
			addCard(color, number)
			addCard(color, number)
		}

		for range 2 {
			addCard(color, skip)
			addCard(color, reverse)
			addCard(color, drawTwo)
		}
	}

	for range 4 {
		addCard(wildColor, wildColorChange)
		addCard(wildColor, wildDrawFour)
	}

	return deck
}

func Shuffle(deck []Card) {
	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}

func (c *Card) IsNumberCard() bool {
	switch c.Type {
	case zero, one, two, three, four, five, six, seven, eight, nine:
		return true
	default:
		return false
	}
}
