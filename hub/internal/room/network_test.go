package room

import (
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

func TestSlowSubscriberReceivesLatestSnapshot(t *testing.T) {
	rm := New("ABC234", uno.New(engine.Rules{}), Options{NoLoop: true})
	defer rm.Close()
	_, _ = rm.Join("alice", "alice")
	updates, cancel, err := rm.Subscribe("alice")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for i := 0; i < 20; i++ {
		if err := rm.Chat("alice", i%len(Presets)); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := rm.SnapshotFor("alice")
	var last uint64
	for len(updates) > 0 {
		last = (<-updates).Revision
	}
	if last != current.Revision {
		t.Fatalf("lost latest frame: %d != %d", last, current.Revision)
	}
}

func TestBotsDoNotPreventAbandonedRoomCollection(t *testing.T) {
	now := time.Now()
	rm := New("ABC234", uno.New(engine.Rules{}), Options{NoLoop: true, Now: func() time.Time { return now }})
	defer rm.Close()
	_, _ = rm.Join("alice", "alice")
	_, _ = rm.AddBot("")
	_ = rm.Start("alice")
	rm.Disconnect("alice")
	now = now.Add(11 * time.Minute)
	if !rm.Abandoned(10 * time.Minute) {
		t.Fatal("bots kept an abandoned room alive")
	}
}
