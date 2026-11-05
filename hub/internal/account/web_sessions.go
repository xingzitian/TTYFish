package account

import "time"

const WebAccessTTL = 15 * time.Minute
const WebRefreshTTL = 30 * 24 * time.Hour

func (s *Service) StartWebSession(session *Session, device string) (string, error) {
	refresh, err := s.repo.StartWebSession(session.AccessToken, device, WebAccessTTL, WebRefreshTTL)
	if err != nil {
		return "", sessionError(err)
	}
	session.ExpiresIn = int64(WebAccessTTL.Seconds())
	return refresh, nil
}

func (s *Service) RefreshWebSession(refresh string) (Session, string, error) {
	access, next, a, err := s.repo.RotateWebSessionWithAccount(refresh, WebAccessTTL)
	if err != nil {
		return Session{}, "", sessionError(err)
	}
	return Session{access, "Bearer", int64(WebAccessTTL.Seconds()), view(a)}, next, nil
}

func (s *Service) LogoutWeb(access, refresh string) error {
	return sessionError(s.repo.RevokeWebSession(access, refresh))
}
