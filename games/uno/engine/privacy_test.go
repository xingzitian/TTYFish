package engine

import "testing"

func TestObservationOnlyRevealsOwnDrawnCard(t *testing.T) {
	s := NewGame(2, 42, Rules{})
	s.Turn = 0
	s.DrawnID = 88
	if Observe(s, 0).DrawnID != 88 {
		t.Fatal("owner cannot see drawn card")
	}
	if Observe(s, 1).DrawnID != -1 || Observe(s, -1).DrawnID != -1 {
		t.Fatal("private drawn card ID leaked")
	}
}
