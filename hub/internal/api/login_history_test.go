package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func TestLoginHistoryAuthenticatedAccountAndPagination(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"https://example.com"}})
	for _, name := range []string{"alice", "bob"} {
		if _, err := f.service.Register(name, "test-password-123", ""); err != nil {
			t.Fatal(err)
		}
	}
	login := func(name, device string) account.Session {
		t.Helper()
		r := httptest.NewRequest("POST", "https://example.com/api/v1/auth/password", strings.NewReader(`{"username":"`+name+`","password":"test-password-123"}`))
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Device-ID", strings.Repeat(device, 64))
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return read[account.Session](t, w)
	}
	login("alice", "a")
	alice := login("alice", "a")
	login("alice", "b")
	bob := login("bob", "c")
	path := "/api/v1/me/login-history?channel=web&limit=1"
	raw := f.request("GET", path, alice.AccessToken, nil, 200)
	type historyPage struct {
		Total int `json:"total"`
		Items []struct {
			Count  int `json:"count"`
			Events []struct {
				ID int `json:"id"`
			} `json:"events"`
		} `json:"items"`
	}
	page := read[historyPage](t, raw)
	second := read[historyPage](t, f.request("GET", path+"&offset=1", alice.AccessToken, nil, 200))
	if page.Total != 2 || len(page.Items) != 1 || len(second.Items) != 1 || page.Items[0].Count+second.Items[0].Count != 3 {
		t.Fatalf("bad grouped pagination: %+v %+v", page, second)
	}
	other := read[historyPage](t, f.request("GET", path, bob.AccessToken, nil, 200))
	if other.Total != 1 || other.Items[0].Count != 1 {
		t.Fatalf("account history leaked: %+v", other)
	}
	f.request("GET", "/api/v1/me/login-history?channel=ssh", alice.AccessToken, nil, 200)
	f.request("GET", "/api/v1/me/login-history?channel=bad", alice.AccessToken, nil, 400)
	f.request("GET", "/api/v1/me/login-history?channel=web&limit=51", alice.AccessToken, nil, 400)
	f.request("GET", path, "", nil, 401)
}

func TestSSHChallengeCreatesOnlyOneWebLoginEvent(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, AllowedOrigins: []string{"https://example.com"}})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	device := strings.Repeat("a", 64)
	first := loginWithDeviceInfo(t, f, "alice", "192.0.2.1:1234", "Browser", device)
	publicKey := key(t)
	f.request("POST", "/api/v1/me/keys", first.AccessToken, map[string]string{"public_key": publicKey, "name": "laptop"}, 201)
	challenge := read[account.Challenge](t, f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 201))
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{"username": "alice", "public_key": publicKey, "code": challenge.Code}, 204)
	body, _ := json.Marshal(map[string]string{"poll_token": challenge.PollToken})
	r := httptest.NewRequest("POST", "https://example.com/api/v1/auth/ssh/challenges/poll", strings.NewReader(string(body)))
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Device-ID", device)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	result := read[account.ChallengeResult](t, w)
	if result.Session == nil {
		t.Fatal("challenge missing session")
	}
	type item struct {
		Count  int `json:"count"`
		Events []struct {
			AuthMethod string `json:"auth_method"`
			KeyName    string `json:"key_name"`
		} `json:"events"`
	}
	type page struct {
		Total int    `json:"total"`
		Items []item `json:"items"`
	}
	web := read[page](t, f.request("GET", "/api/v1/me/login-history?channel=web", result.Session.AccessToken, nil, 200))
	if web.Total != 1 || len(web.Items) != 1 || web.Items[0].Count != 2 {
		t.Fatalf("web events: %+v", web)
	}
	found := false
	for _, e := range web.Items[0].Events {
		if e.AuthMethod == "ssh_challenge" && e.KeyName == "laptop" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing challenge event or key name")
	}
	ssh := read[page](t, f.request("GET", "/api/v1/me/login-history?channel=ssh", result.Session.AccessToken, nil, 200))
	if ssh.Total != 0 {
		t.Fatal("challenge incorrectly counted as interactive SSH login")
	}
}
