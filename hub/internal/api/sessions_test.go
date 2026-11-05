package api

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

type sessionList struct {
	Items []account.SessionInfo `json:"items"`
}

func sessionsFor(t *testing.T, f fixture, token string) []account.SessionInfo {
	t.Helper()
	return read[sessionList](t, f.request("GET", "/api/v1/me/sessions", token, nil, 200)).Items
}
func currentSessionID(t *testing.T, items []account.SessionInfo) string {
	t.Helper()
	id := ""
	for _, item := range items {
		if item.Current {
			if id != "" {
				t.Fatal("multiple current sessions")
			}
			id = item.ID
		}
	}
	if id == "" {
		t.Fatal("no current session")
	}
	return id
}
func loginWithInfo(t *testing.T, f fixture, name, remote, agent string) account.Session {
	return loginWithDeviceInfo(t, f, name, remote, agent, "")
}

func loginWithDeviceInfo(t *testing.T, f fixture, name, remote, agent, device string) account.Session {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": name, "password": "test-password-123"})
	r := httptest.NewRequest("POST", "/api/v1/auth/password", strings.NewReader(string(body)))
	r.RemoteAddr = remote
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", agent)
	if device != "" {
		r.Header.Set("X-Device-ID", device)
		r.Header.Set("Origin", "https://example.com")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return read[account.Session](t, w)
}

func TestPasswordLoginsAreUniquePerBrowserDevice(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"https://example.com"}})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	deviceA, deviceB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	first := loginWithDeviceInfo(t, f, "alice", "192.0.2.1:1234", "Browser A", deviceA)
	again := loginWithDeviceInfo(t, f, "alice", "192.0.2.2:1234", "Browser A updated", deviceA)
	f.request("GET", "/api/v1/me", first.AccessToken, nil, 401)
	other := loginWithDeviceInfo(t, f, "alice", "192.0.2.3:1234", "Browser B", deviceB)
	items := sessionsFor(t, f, again.AccessToken)
	if len(items) != 2 {
		t.Fatalf("got %d rows for two browsers", len(items))
	}
	byCurrent, currentCount := "", 0
	for _, item := range items {
		if item.Current {
			currentCount++
			byCurrent = item.UserAgent
		}
	}
	if currentCount != 1 || byCurrent != "Browser A updated" {
		t.Fatalf("same-device login metadata/current marker incorrect: %+v", items)
	}
	f.request("GET", "/api/v1/me", other.AccessToken, nil, 200)

	// Requests without a browser identity retain independent session semantics.
	legacy1 := loginWithInfo(t, f, "alice", "192.0.2.4:1234", "CLI")
	legacy2 := loginWithInfo(t, f, "alice", "192.0.2.4:1234", "CLI")
	f.request("GET", "/api/v1/me", legacy1.AccessToken, nil, 200)
	if len(sessionsFor(t, f, legacy2.AccessToken)) != 4 {
		t.Fatal("sessions without device identifiers were merged")
	}
	items = sessionsFor(t, f, other.AccessToken)
	var deviceSessionID string
	for _, item := range items {
		if item.UserAgent == "Browser A updated" {
			deviceSessionID = item.ID
		}
	}
	if deviceSessionID == "" {
		t.Fatal("device session missing from list")
	}
	f.request("DELETE", "/api/v1/me/sessions/"+deviceSessionID, other.AccessToken, nil, 204)
	f.request("GET", "/api/v1/me", first.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", again.AccessToken, nil, 401)
}

func TestSSHChallengeLoginReusesBrowserSession(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, AllowedOrigins: []string{"https://example.com"}})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	device := strings.Repeat("c", 64)
	password := loginWithDeviceInfo(t, f, "alice", "192.0.2.1:1234", "Browser", device)
	publicKey := key(t)
	f.request("POST", "/api/v1/me/keys", password.AccessToken, map[string]string{"public_key": publicKey}, 201)
	challenge := read[account.Challenge](t, f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 201))
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{
		"username": "alice", "public_key": publicKey, "code": challenge.Code,
	}, 204)

	body, _ := json.Marshal(map[string]string{"poll_token": challenge.PollToken})
	r := httptest.NewRequest("POST", "/api/v1/auth/ssh/challenges/poll", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Device-ID", device)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("User-Agent", "Browser")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("poll: got %d: %s", w.Code, w.Body.String())
	}
	result := read[account.ChallengeResult](t, w)
	if result.Session == nil {
		t.Fatal("challenge did not issue a session")
	}
	f.request("GET", "/api/v1/me", password.AccessToken, nil, 401)
	items := sessionsFor(t, f, result.Session.AccessToken)
	if len(items) != 1 || !items[0].Current || items[0].AuthMethod != "ssh_challenge" || items[0].UserAgent != "Browser" {
		t.Fatalf("SSH login did not stay in one browser session row: %+v", items)
	}
}

func TestSessionListAndTargetedRevocation(t *testing.T) {
	f := setup(t, Config{})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	first := loginWithInfo(t, f, "alice", "192.0.2.10:4321", "Browser One")
	second := loginWithInfo(t, f, "alice", "[2001:db8::10]:4321", "Browser Two")
	bob := loginPlayer(t, f, "bob")
	f.request("GET", "/api/v1/me/sessions", "", nil, 401)
	raw := f.request("GET", "/api/v1/me/sessions", first.AccessToken, nil, 200)
	items := read[sessionList](t, raw).Items
	if len(items) != 2 {
		t.Fatal("wrong session count")
	}
	firstID := currentSessionID(t, items)
	f.request("GET", "/api/v1/me", firstID, nil, 401)
	secondID := currentSessionID(t, sessionsFor(t, f, second.AccessToken))
	for _, item := range items {
		if len(item.ID) != 24 || item.AuthMethod != "password" || item.CredentialFingerprint != nil || item.LastSeenAt.Before(item.CreatedAt) || !item.ExpiresAt.After(item.CreatedAt) {
			t.Fatalf("incorrect metadata: %+v", item)
		}
		if item.Current && (item.SourceIP != "192.0.2.10" || item.UserAgent != "Browser One") {
			t.Fatal("request metadata incorrect or forwarded header trusted")
		}
		if !item.Current && (item.SourceIP != "2001:db8::10" || item.UserAgent != "Browser Two") {
			t.Fatal("IPv6 metadata incorrect")
		}
	}
	if strings.Contains(raw.Body.String(), first.AccessToken) || strings.Contains(raw.Body.String(), second.AccessToken) || strings.Contains(raw.Body.String(), "token_hash") {
		t.Fatal("secret leaked in session list")
	}
	bobID := currentSessionID(t, sessionsFor(t, f, bob))
	f.request("DELETE", "/api/v1/me/sessions/"+bobID, first.AccessToken, nil, 404)
	f.request("DELETE", "/api/v1/me/sessions/not-valid", first.AccessToken, nil, 400)
	f.request("DELETE", "/api/v1/me/sessions/"+secondID, first.AccessToken, nil, 204)
	f.request("GET", "/api/v1/me", second.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", bob, nil, 200)
	if len(sessionsFor(t, f, first.AccessToken)) != 1 {
		t.Fatal("revoked session still listed")
	}
	f.request("DELETE", "/api/v1/me/sessions/"+secondID, first.AccessToken, nil, 404)
	f.request("DELETE", "/api/v1/me/sessions/"+firstID, first.AccessToken, nil, 204)
	f.request("GET", "/api/v1/me/sessions", first.AccessToken, nil, 401)
}

func TestRevokeOthersAndPasswordPolicy(t *testing.T) {
	f := setup(t, Config{})
	first := loginPlayer(t, f, "alice")
	bob := loginPlayer(t, f, "bob")
	second, err := f.service.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	count := read[struct {
		Count int64 `json:"revoked_count"`
	}](t, f.request("POST", "/api/v1/me/sessions/revoke-others", first, nil, 200))
	if count.Count != 1 {
		t.Fatal("incorrect revocation count")
	}
	f.request("GET", "/api/v1/me", first, nil, 200)
	f.request("GET", "/api/v1/me", second.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", bob, nil, 200)
	count = read[struct {
		Count int64 `json:"revoked_count"`
	}](t, f.request("POST", "/api/v1/me/sessions/revoke-others", first, nil, 200))
	if count.Count != 0 {
		t.Fatal("repeat should revoke zero")
	}
	other, err := f.service.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	f.request("PUT", "/api/v1/me/password", first, map[string]any{"password": "replacement-password-123"}, 204)
	f.request("GET", "/api/v1/me", other.AccessToken, nil, 200)
	f.request("PUT", "/api/v1/me/password", first, map[string]any{"password": "final-password-123", "revoke_other_sessions": true}, 204)
	f.request("GET", "/api/v1/me", other.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", first, nil, 200)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "final-password-123"}, 200)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "replacement-password-123"}, 401)
}

func TestDeletingKeyRevokesOnlyLinkedSessions(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	password := loginPlayer(t, f, "alice")
	pubA, pubB := key(t), key(t)
	keyA := read[account.Key](t, f.request("POST", "/api/v1/me/keys", password, map[string]string{"public_key": pubA}, 201))
	f.request("POST", "/api/v1/me/keys", password, map[string]string{"public_key": pubB}, 201)
	loginA := read[account.Session](t, f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "alice", "public_key": pubA}, 200))
	loginB := read[account.Session](t, f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "alice", "public_key": pubB}, 200))
	c := read[account.Challenge](t, f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 201))
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{"username": "alice", "public_key": pubA, "code": c.Code}, 204)
	result := read[account.ChallengeResult](t, f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": c.PollToken}, 200))
	if result.Session == nil {
		t.Fatal("missing challenge session")
	}
	items := sessionsFor(t, f, password)
	var keys, challenges, passwords int
	for _, item := range items {
		switch item.AuthMethod {
		case "ssh_key":
			keys++
			if item.CredentialFingerprint == nil {
				t.Fatal("key fingerprint missing")
			}
		case "ssh_challenge":
			challenges++
			if item.CredentialFingerprint == nil || *item.CredentialFingerprint != keyA.Fingerprint {
				t.Fatal("challenge fingerprint incorrect")
			}
		case "password":
			passwords++
		}
	}
	if keys != 0 || challenges != 1 || passwords != 1 {
		t.Fatal("authentication methods not captured")
	}
	f.request("DELETE", "/api/v1/me/keys/"+url.PathEscape(keyA.Fingerprint), password, nil, 204)
	f.request("GET", "/api/v1/me", loginA.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", result.Session.AccessToken, nil, 401)
	f.request("GET", "/api/v1/me", loginB.AccessToken, nil, 200)
	f.request("GET", "/api/v1/me", password, nil, 200)
	if len(sessionsFor(t, f, password)) != 1 {
		t.Fatal("unexpected surviving sessions")
	}
}
