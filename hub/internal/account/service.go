// Package account contains account business rules, independent of HTTP and SSH gateways.
package account

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/store"
	"golang.org/x/crypto/ssh"
)

var (
	ErrInvalid        = errors.New("invalid_request")
	ErrCredentials    = errors.New("invalid_credentials")
	ErrConflict       = errors.New("account_conflict")
	ErrNotFound       = errors.New("not_found")
	ErrLastCredential = errors.New("last_credential")
	usernamePattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,19}$`)
)

const SessionTTL = 24 * time.Hour

// Repository is the persistence boundary; gateways never access it directly.
type Repository interface {
	Register(string, string, string, string) (store.Account, error)
	ByHandle(string) (store.Account, bool, error)
	ByFingerprint(string) (store.Account, bool, error)
	AuthenticatePassword(string, string) (store.Account, bool)
	NewSession(string, string, time.Duration) (string, error)
	SessionAccount(string) (store.Account, bool, error)
	DropSession(string) error
	SetPassword(string, string) error
	Identities(string) ([]store.Identity, error)
	Link(string, string, string) error
	RemoveIdentity(string, string) (bool, error)
	AllowPasswordAttempt(string) bool
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

type Account struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}
type Session struct {
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	ExpiresIn   int64   `json:"expires_in"`
	Account     Account `json:"account"`
}
type Key struct {
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"public_key"`
	CreatedAt   time.Time `json:"created_at"`
}

func view(a store.Account) Account { return Account{a.ID, a.Handle, a.Display, a.CreatedAt} }

// ParsePublicKey accepts exactly one authorized_keys entry without options.
func ParsePublicKey(text string) (string, string, error) {
	if strings.TrimSpace(text) == "" {
		return "", "", nil
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil || len(options) > 0 || strings.TrimSpace(string(rest)) != "" {
		return "", "", fmt.Errorf("%w: 请提供一把完整的 SSH 公钥，不含 authorized_keys 选项", ErrInvalid)
	}
	if _, ok := key.(*ssh.Certificate); ok {
		return "", "", fmt.Errorf("%w: 暂不支持 SSH 证书", ErrInvalid)
	}
	return ssh.FingerprintSHA256(key), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), nil
}

func validatePassword(password string) error {
	if len(password) < 10 || len(password) > 72 || strings.TrimSpace(password) == "" {
		return fmt.Errorf("%w: 密码须为 10–72 字节", ErrInvalid)
	}
	return nil
}

func (s *Service) Register(username, password, publicKey string) (Account, error) {
	if !usernamePattern.MatchString(username) {
		return Account{}, fmt.Errorf("%w: 用户名须为 3–20 位小写字母、数字、下划线或短横线，以字母开头", ErrInvalid)
	}
	if password != "" {
		if err := validatePassword(password); err != nil {
			return Account{}, err
		}
	}
	fp, pub, err := ParsePublicKey(publicKey)
	if err != nil {
		return Account{}, err
	}
	if fp == "" && password == "" {
		return Account{}, fmt.Errorf("%w: 密码和公钥至少提供一项", ErrInvalid)
	}
	a, err := s.repo.Register(username, password, fp, pub)
	if err != nil {
		return Account{}, classifyConflict(err)
	}
	return view(a), nil
}

func classifyConflict(err error) error {
	if errors.Is(err, store.ErrConflict) {
		return ErrConflict
	}
	return err
}

func (s *Service) issue(a store.Account) (Session, error) {
	token, err := s.repo.NewSession(a.ID, "api", SessionTTL)
	return Session{token, "Bearer", int64(SessionTTL.Seconds()), view(a)}, err
}

func (s *Service) LoginPassword(username, password string) (Session, error) {
	a, ok := s.repo.AuthenticatePassword(username, password)
	if !ok {
		return Session{}, ErrCredentials
	}
	return s.issue(a)
}

// LoginVerifiedKey may only be called by a trusted gateway after SSH proof of possession.
func (s *Service) LoginVerifiedKey(username, publicKey string) (Session, error) {
	fp, _, err := ParsePublicKey(publicKey)
	if err != nil {
		return Session{}, err
	}
	a, ok, err := s.repo.ByFingerprint(fp)
	if err != nil {
		return Session{}, err
	}
	if !ok || a.Handle != username {
		return Session{}, ErrCredentials
	}
	return s.issue(a)
}

func (s *Service) Current(token string) (Account, error) {
	if token == "" {
		return Account{}, ErrCredentials
	}
	a, ok, err := s.repo.SessionAccount(token)
	if err != nil {
		return Account{}, err
	}
	if !ok {
		return Account{}, ErrCredentials
	}
	return view(a), nil
}
func (s *Service) Logout(token string) error       { return s.repo.DropSession(token) }
func (s *Service) AllowAttempt(remote string) bool { return s.repo.AllowPasswordAttempt(remote) }
func (s *Service) SetPassword(id, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	return s.repo.SetPassword(id, password)
}
func (s *Service) Keys(id string) ([]Key, error) {
	items, err := s.repo.Identities(id)
	if err != nil {
		return nil, err
	}
	keys := make([]Key, 0, len(items))
	for _, k := range items {
		keys = append(keys, Key{k.Fingerprint, k.PublicKey, k.CreatedAt})
	}
	return keys, nil
}
func (s *Service) AddKey(id, publicKey string) (Key, error) {
	fp, pub, err := ParsePublicKey(publicKey)
	if err != nil {
		return Key{}, err
	}
	if fp == "" {
		return Key{}, fmt.Errorf("%w: 缺少公钥", ErrInvalid)
	}
	if err = s.repo.Link(id, fp, pub); err != nil {
		return Key{}, classifyConflict(err)
	}
	keys, err := s.Keys(id)
	if err != nil {
		return Key{}, err
	}
	for _, k := range keys {
		if k.Fingerprint == fp {
			return k, nil
		}
	}
	return Key{}, ErrNotFound
}
func (s *Service) DeleteKey(id, fp string) error {
	ok, err := s.repo.RemoveIdentity(id, fp)
	if errors.Is(err, store.ErrLastCredential) {
		return ErrLastCredential
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
