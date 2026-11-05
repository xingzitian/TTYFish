package account

import "github.com/xingzitian/TTYFish/hub/internal/store"

func (s *Service) LoginHistory(id, channel string, offset, limit int) ([]store.LoginGroup, int, error) {
	return s.repo.LoginHistory(id, channel, offset, limit)
}
