package remote

import "testing"

func TestSettlementRevisionSurvivesWireUpdates(t *testing.T) {
	c := New("http://unused", "", "uno")
	c.apply(wireSnapshot{Code: "ABC234", Revision: 7, Phase: "over", SettlementRevision: 1})
	snap := <-c.Updates()
	if snap.SettlementRevision != 1 || snap.Revision != 7 {
		t.Fatalf("wire metadata lost: %+v", snap)
	}
	c.apply(wireSnapshot{Code: "ABC234", Revision: 8, Phase: "playing", SettlementRevision: 1})
	if snap := <-c.Updates(); snap.SettlementRevision != 1 {
		t.Fatalf("new match lost settlement revision: %+v", snap)
	}
	c.apply(wireSnapshot{Code: "ABC234", Revision: 6, Phase: "playing"})
	if c.Snapshot().SettlementRevision != 1 {
		t.Fatal("stale snapshot overwrote settlement revision")
	}
}
