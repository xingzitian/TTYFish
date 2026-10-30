package account

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/store"
)

const ChallengeTTL = 90 * time.Second

var ErrChallengeInvalid = errors.New("challenge_invalid")
var challengeCodePattern = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)
var pollTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Challenge struct {
	Username       string `json:"username"`
	Code           string `json:"code"`
	PollToken      string `json:"poll_token"`
	ExpiresIn      int64  `json:"expires_in"`
	PollIntervalMS int    `json:"poll_interval_ms"`
}
type ChallengeResult struct {
	State   string   `json:"state"`
	Session *Session `json:"session,omitempty"`
}

func (s *Service) StartSSHChallenge(username string) (Challenge, error) {
	if username == "" {
		return Challenge{}, fmt.Errorf("%w: 请填写固定用户名", ErrInvalid)
	}
	a, ok, err := s.repo.ByHandle(username)
	if err != nil {
		return Challenge{}, err
	}
	if !ok {
		return Challenge{}, ErrCredentials
	}
	keys, err := s.repo.Identities(a.ID)
	if err != nil {
		return Challenge{}, err
	}
	if len(keys) == 0 {
		return Challenge{}, fmt.Errorf("%w: 此账号尚未绑定 SSH 公钥", ErrInvalid)
	}
	c, err := s.repo.NewAPIChallenge(a.ID, ChallengeTTL)
	if err != nil {
		return Challenge{}, err
	}
	return Challenge{a.Handle, c.Code, c.Poll, int64(ChallengeTTL.Seconds()), 1500}, nil
}

// ConfirmSSHChallenge trusts only the gateway's verified SSH identity, never browser input.
func (s *Service) ConfirmSSHChallenge(username, publicKey, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if username == "" || !challengeCodePattern.MatchString(code) {
		return fmt.Errorf("%w: 用户名或确认码格式错误", ErrInvalid)
	}
	fp, _, err := ParsePublicKey(publicKey)
	if err != nil {
		return err
	}
	if fp == "" {
		return fmt.Errorf("%w: 缺少公钥", ErrInvalid)
	}
	err = s.repo.ConfirmAPIChallenge(code, username, fp)
	if errors.Is(err, store.ErrChallengeInvalid) {
		return ErrChallengeInvalid
	}
	return err
}

func (s *Service) PollSSHChallenge(token string) (ChallengeResult, error) {
	if !pollTokenPattern.MatchString(token) {
		return ChallengeResult{}, fmt.Errorf("%w: 查询凭证格式错误", ErrInvalid)
	}
	state, a, accessToken, err := s.repo.RedeemAPIChallenge(token, SessionTTL)
	if err != nil {
		return ChallengeResult{}, err
	}
	result := ChallengeResult{State: state}
	if state == "ok" {
		result.Session = &Session{accessToken, "Bearer", int64(SessionTTL.Seconds()), view(a)}
	}
	return result, nil
}
func (s *Service) AllowPollAttempt(remote string) bool { return s.repo.AllowPollAttempt(remote) }
