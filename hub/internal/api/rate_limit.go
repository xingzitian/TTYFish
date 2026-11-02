package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/ratelimit"
)

// RateLimits are requests per minute. Zero selects the default; negative values are invalid.
type RateLimits struct {
	PublicAuthIP     int
	GatewayRequests  int
	RegistrationIP   int
	AccountClient    int
	AccountTotal     int
	KeyClient        int
	KeyAccount       int
	PollIP           int
	PollToken        int
	PasswordChanges  int
	InvalidGatewayIP int
}

func (r RateLimits) defaults() (RateLimits, error) {
	values := []struct {
		value    *int
		fallback int
	}{
		{&r.PublicAuthIP, 120}, {&r.GatewayRequests, 600}, {&r.RegistrationIP, 10},
		{&r.AccountClient, 10}, {&r.AccountTotal, 30}, {&r.KeyClient, 20}, {&r.KeyAccount, 60},
		{&r.PollIP, 1200}, {&r.PollToken, 60}, {&r.PasswordChanges, 10}, {&r.InvalidGatewayIP, 10},
	}
	for _, v := range values {
		if *v.value < 0 {
			return RateLimits{}, errors.New("限流预算不能为负数")
		}
		if *v.value == 0 {
			*v.value = v.fallback
		}
	}
	return r, nil
}
func bucket(scope, identity string, max int) ratelimit.Limit {
	sum := sha256.Sum256([]byte(identity))
	return ratelimit.Limit{Key: scope + ":" + hex.EncodeToString(sum[:]), Max: max, Window: time.Minute}
}
func (s *Server) take(w http.ResponseWriter, limits ...ratelimit.Limit) bool {
	wait := s.limiter.Allow(limits...)
	if wait <= 0 {
		return true
	}
	seconds := int64((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	failure(w, 429, "rate_limited", "请求过于频繁，请按 Retry-After 等待后重试")
	return false
}
func (s *Server) source(w http.ResponseWriter, r *http.Request, gateway bool) (string, bool) {
	ip, err := s.sources.ip(r, gateway)
	if err != nil {
		failure(w, 400, "invalid_request", err.Error())
		return "", false
	}
	return ip, true
}
func (s *Server) aggregate(w http.ResponseWriter, r *http.Request, gateway bool, scope string, max int) bool {
	ip, ok := s.source(w, r, gateway)
	if !ok {
		return false
	}
	if gateway {
		return s.take(w, bucket("gateway", "configured-gateway", s.rates.GatewayRequests))
	}
	return s.take(w, bucket(scope, ip, max))
}
func (s *Server) accountBudget(w http.ResponseWriter, r *http.Request, gateway bool, scope, username string, key bool) bool {
	ip, ok := s.source(w, r, gateway)
	if !ok {
		return false
	}
	client, total := s.rates.AccountClient, s.rates.AccountTotal
	if key {
		client, total = s.rates.KeyClient, s.rates.KeyAccount
	}
	// Authentication remains case-sensitive; only quota identities are normalized.
	username = strings.ToLower(strings.TrimSpace(username))
	return s.take(w, bucket(scope+":client", ip+"\x00"+username, client), bucket(scope+":account", username, total))
}
func (s *Server) gatewayTokenMatches(token string) bool {
	if s.cfg.GatewayToken == "" || token == "" {
		return false
	}
	supplied, expected := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(s.cfg.GatewayToken))
	return subtle.ConstantTimeCompare(supplied[:], expected[:]) == 1
}
func (s *Server) rejectGateway(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.sources.ip(r, false)
	if s.take(w, bucket("invalid-gateway", ip, s.rates.InvalidGatewayIP)) {
		failure(w, 401, "gateway_unauthorized", "无效的网关凭证")
	}
}
func (s *Server) passwordGateway(w http.ResponseWriter, r *http.Request) (gateway, ok bool) {
	values := r.Header.Values(gatewayHeader)
	if len(values) == 0 {
		return false, true
	}
	if len(values) != 1 || !s.gatewayTokenMatches(values[0]) {
		s.rejectGateway(w, r)
		return false, false
	}
	return true, true
}
