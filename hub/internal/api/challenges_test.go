package api

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func TestSSHChallengeBrowserGatewayFlow(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	pub := key(t)
	a, err := f.service.Register("alice", "", pub)
	if err != nil {
		t.Fatal(err)
	}
	c := read[account.Challenge](t, f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 201))
	if c.Username != "alice" || c.Code == "" || len(c.PollToken) != 64 || c.ExpiresIn != 90 || c.PollIntervalMS != 1500 {
		t.Fatalf("incorrect challenge: %+v", c)
	}
	poll := map[string]string{"poll_token": c.PollToken}
	pending := read[account.ChallengeResult](t, f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", poll, 200))
	if pending.State != "pending" || pending.Session != nil {
		t.Fatal("session issued before confirmation")
	}
	confirm := map[string]string{"username": "alice", "public_key": pub, "code": strings.ToLower(c.Code)}
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", "", confirm, 401)
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", "wrong-secret", confirm, 401)
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{"username": "bob", "public_key": pub, "code": c.Code}, 409)
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{"username": "alice", "public_key": key(t), "code": c.Code}, 409)
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, confirm, 204)
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, confirm, 409)
	result := read[account.ChallengeResult](t, f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", poll, 200))
	if result.State != "ok" || result.Session == nil || result.Session.Account.ID != a.ID || result.Session.AccessToken == "" {
		t.Fatal("browser did not receive account session")
	}
	f.request("GET", "/api/v1/me", result.Session.AccessToken, nil, 200)
	replay := read[account.ChallengeResult](t, f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", poll, 200))
	if replay.State != "expired" || replay.Session != nil {
		t.Fatal("poll token replay issued a session")
	}
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", result.Session.AccessToken, confirm, 401)
}

func TestSSHChallengeValidationAndIndependentPollBudget(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	if _, err := f.service.Register("alice", "alice-password-123", ""); err != nil {
		t.Fatal(err)
	}
	f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{}, 400)
	f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "unknown"}, 401)
	f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 400)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": "code-is-not-a-poll-token"}, 400)
	unknown := read[account.ChallengeResult](t, f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": strings.Repeat("0", 64)}, 200))
	if unknown.State != "expired" {
		t.Fatal("unknown poll token was valid")
	}
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{}, 400)
	// Polling at the documented interval must not exhaust the login budget.
	for i := 0; i < 20; i++ {
		f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": strings.Repeat("0", 64)}, 200)
	}
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "alice-password-123"}, 200)
	for i := 22; i < 120; i++ {
		f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": strings.Repeat("0", 64)}, 200)
	}
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": strings.Repeat("0", 64)}, 429)
}
