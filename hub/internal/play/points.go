package play

import (
	"fmt"
	"strings"

	"github.com/xingzitian/TTYFish/hub/internal/room"
)

type UNORewardRepository interface {
	AwardUNO(matchID, accountID string, score int64) error
}

func UNORewards(repo UNORewardRepository) func(room.MatchResult) error {
	return func(result room.MatchResult) error {
		// Native bots have no account; disconnected humans retain their identity.
		if result.Identity == "" {
			return nil
		}
		accountID, ok := strings.CutPrefix(result.Identity, "account:")
		if !ok || accountID == "" {
			return fmt.Errorf("UNO reward requires an account identity")
		}
		return repo.AwardUNO(result.ID, accountID, result.Score)
	}
}
