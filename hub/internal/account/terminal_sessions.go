package account

import (
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"time"
)

const TerminalTTL = 60 * time.Second

func (s *Service) OnlineTerminal(id string) (*store.TerminalInfo, error) {
	return s.repo.OnlineTerminal(id)
}

func (s *Service) ClaimTerminal(id, token string) (string, error) {
	lease, err := s.repo.ClaimTerminal(id, token, TerminalTTL)
	return lease, sessionError(err)
}
func (s *Service) HeartbeatTerminal(id, token, lease string) error {
	return sessionError(s.repo.HeartbeatTerminal(id, token, lease, TerminalTTL))
}
func (s *Service) ReleaseTerminal(id, token, lease string) error {
	return s.repo.ReleaseTerminal(id, token, lease)
}
