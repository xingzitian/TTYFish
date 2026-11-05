package engine

import "testing"

func TestPeekMetadataReflectsResolutionWithoutExposingCards(t *testing.T) {
	s, _ := NewGame(2, 1)
	s.Hands[0] = []Card{{ID: 100, Kind: SeeTheFuture}}
	if err := Apply(s, Action{Type: Play, Seat: 0, CardIDs: []int{100}}); err != nil {
		t.Fatal(err)
	}
	before := Observe(s, 1)
	if before.PeekSeat != -1 || before.PeekSeq != 0 || len(before.Future) != 0 {
		t.Fatal("pending prophecy exposed a resolved peek")
	}
	for seat := range s.Alive {
		if err := Apply(s, Action{Type: Pass, Seat: seat}); err != nil {
			t.Fatal(err)
		}
	}
	actor, observer := Observe(s, 0), Observe(s, 1)
	if actor.PeekSeq != 1 || actor.PeekSeat != 0 || len(actor.Future) != 3 ||
		observer.PeekSeq != 1 || observer.PeekSeat != 0 || len(observer.Future) != 0 {
		t.Fatal("peek resolution metadata or privacy broken")
	}
	if err := Apply(s, Action{Type: Draw, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	if o := Observe(s, 1); o.PeekSeq != 1 || o.PeekSeat != -1 {
		t.Fatal("drawing did not end public peek")
	}
}
