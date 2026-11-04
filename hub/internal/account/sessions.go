package account

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/store"
)

type ClientInfo = store.ClientInfo

func firstClient(clients []ClientInfo) ClientInfo {
	if len(clients) > 0 {
		return clients[0]
	}
	return ClientInfo{}
}

type SessionInfo struct {
	ID                    string    `json:"id"`
	AuthMethod            string    `json:"auth_method"`
	CredentialFingerprint *string   `json:"credential_fingerprint"`
	CreatedAt             time.Time `json:"created_at"`
	ExpiresAt             time.Time `json:"expires_at"`
	LastSeenAt            time.Time `json:"last_seen_at"`
	SourceIP              string    `json:"source_ip"`
	UserAgent             string    `json:"user_agent"`
	Current               bool      `json:"current"`
}

var sessionIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func sessionError(err error) error {
	if errors.Is(err, store.ErrSessionInvalid) {
		return ErrCredentials
	}
	return err
}

func (s *Service) Sessions(id, token string) ([]SessionInfo, error) {
	sessions, err := s.repo.Sessions(id, token)
	if err != nil {
		return nil, sessionError(err)
	}
	items := make([]SessionInfo, 0, len(sessions))
	byDevice := make(map[string]int, len(sessions))
	for _, session := range sessions {
		var fp *string
		if session.CredentialFingerprint != "" {
			v := session.CredentialFingerprint
			fp = &v
		}
		key := "session:" + session.ID
		if session.DeviceID != "" {
			key = "device:" + session.DeviceID
		}
		if index, exists := byDevice[key]; exists {
			item := &items[index]
			item.Current = item.Current || session.Current
			if session.LastSeenAt.After(item.LastSeenAt) {
				item.LastSeenAt = session.LastSeenAt
			}
			if session.ExpiresAt.After(item.ExpiresAt) {
				item.ExpiresAt = session.ExpiresAt
			}
			continue
		}
		byDevice[key] = len(items)
		items = append(items, SessionInfo{session.ID, session.AuthMethod, fp, session.CreatedAt, session.ExpiresAt, session.LastSeenAt, session.SourceIP, session.UserAgent, session.Current})
	}
	return items, nil
}
func (s *Service) RevokeSession(id, token, sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("%w: 会话 ID 格式错误", ErrInvalid)
	}
	ok, err := s.repo.RevokeSession(id, token, sessionID)
	if err != nil {
		return sessionError(err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
func (s *Service) RevokeOtherSessions(id, token string) (int64, error) {
	count, err := s.repo.RevokeOtherSessions(id, token)
	return count, sessionError(err)
}
func (s *Service) SetPasswordWithPolicy(id, token, password string, revokeOthers bool) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	if !revokeOthers {
		return s.repo.SetPassword(id, password)
	}
	return sessionError(s.repo.SetPasswordAndRevokeOthers(id, token, password))
}
