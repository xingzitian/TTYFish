// Package engine implements Original Edition rules without network, UI or clocks.
//
// Rules reference: https://www.explodingkittens.com/pages/rules-kittens
// (2025 Original Edition instructions). Optional fast-game deck removal is not used.
package engine

type Kind string

const (
	ExplodingKitten Kind = "exploding_kitten"
	Defuse          Kind = "defuse"
	Attack          Kind = "attack"
	Skip            Kind = "skip"
	Reverse         Kind = "reverse"
	Favor           Kind = "favor"
	Shuffle         Kind = "shuffle"
	SeeTheFuture    Kind = "see_the_future"
	Nope            Kind = "nope"
	TacoCat         Kind = "taco_cat"
	Cattermelon     Kind = "cattermelon"
	HairyPotatoCat  Kind = "hairy_potato_cat"
	BeardCat        Kind = "beard_cat"
	RainbowCat      Kind = "rainbow_cat"
)

type Card struct {
	ID   int  `json:"id"`
	Kind Kind `json:"kind"`
}

func (k Kind) valid() bool {
	if k == Reverse {
		return true
	}
	for _, entry := range deckComposition {
		if entry.kind == k {
			return true
		}
	}
	return false
}

var deckComposition = []struct {
	kind  Kind
	count int
}{
	{ExplodingKitten, 4}, {Defuse, 6}, {Attack, 4}, {Skip, 4},
	{Favor, 4}, {Shuffle, 4}, {SeeTheFuture, 5}, {Nope, 5},
	{TacoCat, 4}, {Cattermelon, 4}, {HairyPotatoCat, 4}, {BeardCat, 4}, {RainbowCat, 4},
}

func NewDeck() []Card {
	cards := make([]Card, 0, 56)
	for _, entry := range deckComposition {
		for i := 0; i < entry.count; i++ {
			cards = append(cards, Card{ID: len(cards), Kind: entry.kind})
		}
	}
	return cards
}
