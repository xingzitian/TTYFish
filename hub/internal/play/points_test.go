package play

import (
	"errors"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/room"
)

type rewardRecorder struct {
	calls       int
	id, account string
	score       int64
	err         error
}

func (r *rewardRecorder) AwardUNO(id, account string, score int64) error {
	r.calls++
	r.id, r.account, r.score = id, account, score
	return r.err
}

func TestUNORewardsIdentityAndErrors(t *testing.T) {
	repo := &rewardRecorder{}
	settle := UNORewards(repo)
	if err := settle(room.MatchResult{ID: "match", Score: 68}); err != nil || repo.calls != 0 {
		t.Fatal("native bot awarded")
	}
	for _, identity := range []string{"account:", "someone"} {
		if err := settle(room.MatchResult{ID: "match", Identity: identity, Score: 68}); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	if err := settle(room.MatchResult{ID: "match", Identity: "account:alice", Score: 68}); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.id != "match" || repo.account != "alice" || repo.score != 68 {
		t.Fatalf("incorrect reward %+v", repo)
	}
	repo.err = errors.New("database unavailable")
	if err := settle(room.MatchResult{ID: "match", Identity: "account:alice", Score: 68}); !errors.Is(err, repo.err) {
		t.Fatal("persistence failure swallowed")
	}
}
