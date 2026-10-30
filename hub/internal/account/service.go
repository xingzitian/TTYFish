// Package account contains account business rules, independent of HTTP and SSH gateways.
package account

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	RegisterNamed(string, string, string, string, string) (store.Account, error)
	LinkNamed(string, string, string, *string) error
	RenameIdentity(string, string, string) (bool, error)
	KeySession(string, string, time.Duration) (string, bool, error)
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
	AllowPollAttempt(string) bool
	NewAPIChallenge(string, time.Duration) (store.Challenge, error)
	ConfirmAPIChallenge(string, string, string) error
	RedeemAPIChallenge(string, time.Duration) (string, store.Account, string, error)
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
	Name        string     `json:"name"`
	Fingerprint string     `json:"fingerprint"`
	PublicKey   string     `json:"public_key"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
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

func (s *Service) Register(username, password, publicKey string, names ...string) (Account, error) {
	name := ""
	if len(names) > 0 {
		var err error
		name, err = validateKeyName(names[0])
		if err != nil {
			return Account{}, err
		}
	}
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
	if name != "" && fp == "" {
		return Account{}, fmt.Errorf("%w: key_name 需要同时提供公钥", ErrInvalid)
	}
	a, err := s.repo.RegisterNamed(username, password, fp, pub, name)
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
	a, fp, err := s.keyAccount(username, publicKey)
	if err != nil {
		return Session{}, err
	}
	token, ok, err := s.repo.KeySession(a.ID, fp, SessionTTL)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, ErrCredentials
	}
	return Session{token, "Bearer", int64(SessionTTL.Seconds()), view(a)}, nil
}

// CheckKey is a side-effect-free preflight for unsigned SSH public-key probes.
func (s *Service) CheckKey(username, publicKey string) (Account, error) {
	a, _, err := s.keyAccount(username, publicKey)
	if err != nil {
		return Account{}, err
	}
	return view(a), nil
}

func (s *Service) keyAccount(username, publicKey string) (store.Account, string, error) {
	fp, _, err := ParsePublicKey(publicKey)
	if err != nil {
		return store.Account{}, "", err
	}
	a, ok, err := s.repo.ByFingerprint(fp)
	if err != nil {
		return store.Account{}, "", err
	}
	if !ok || a.Handle != username {
		return store.Account{}, "", ErrCredentials
	}
	return a, fp, nil
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
		var used *time.Time
		if !k.LastUsed.IsZero() {
			t := k.LastUsed
			used = &t
		}
		keys = append(keys, Key{Name: k.Name, Fingerprint: k.Fingerprint, PublicKey: k.PublicKey, CreatedAt: k.CreatedAt, LastUsedAt: used})
	}
	return keys, nil
}
func (s *Service) AddKey(id, publicKey string, names ...string) (Key, error) {
	var name *string
	if len(names) > 0 {
		v, err := validateKeyName(names[0])
		if err != nil {
			return Key{}, err
		}
		name = &v
	}
	fp, pub, err := ParsePublicKey(publicKey)
	if err != nil {
		return Key{}, err
	}
	if fp == "" {
		return Key{}, fmt.Errorf("%w: 缺少公钥", ErrInvalid)
	}
	if err = s.repo.LinkNamed(id, fp, pub, name); err != nil {
		return Key{}, classifyConflict(err)
	}
	return s.Key(id, fp)
}

func (s *Service) Key(id, fp string) (Key, error) {
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

func validateKeyName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: 密钥名称必须是 UTF-8 文本", ErrInvalid)
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: 密钥名称不能包含控制字符", ErrInvalid)
		}
	}
	name := strings.TrimSpace(raw)
	if utf8.RuneCountInString(name) > 64 {
		return "", fmt.Errorf("%w: 密钥名称最多 64 个字符", ErrInvalid)
	}
	return name, nil
}

func (s *Service) RenameKey(id, fp, raw string) (Key, error) {
	name, err := validateKeyName(raw)
	if err != nil {
		return Key{}, err
	}
	ok, err := s.repo.RenameIdentity(id, fp, name)
	if err != nil {
		return Key{}, err
	}
	if !ok {
		return Key{}, ErrNotFound
	}
	return s.Key(id, fp)
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
