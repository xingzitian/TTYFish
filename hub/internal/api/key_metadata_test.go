package api

import (
	"net/url"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func TestKeyNamesUsageAndOwnership(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	alice := loginPlayer(t, f, "alice")
	bob := loginPlayer(t, f, "bob")
	pub := key(t)
	added := read[account.Key](t, f.request("POST", "/api/v1/me/keys", alice, map[string]string{"public_key": pub, "name": " 工作电脑 "}, 201))
	if added.Name != "工作电脑" || added.LastUsedAt != nil {
		t.Fatalf("incorrect initial metadata: %+v", added)
	}
	path := "/api/v1/me/keys/" + url.PathEscape(added.Fingerprint)
	f.request("GET", path, "", nil, 401)
	f.request("GET", path, bob, nil, 404)
	f.request("PATCH", path, bob, map[string]string{"name": "stolen"}, 404)
	f.request("PATCH", path, alice, map[string]string{"name": "备用电脑"}, 200)
	f.request("PATCH", path, alice, map[string]string{}, 400)
	f.request("PATCH", path, alice, map[string]string{"name": strings.Repeat("字", 65)}, 400)
	f.request("PATCH", path, alice, map[string]string{"name": "bad\nlabel"}, 400)
	repeated := read[account.Key](t, f.request("POST", "/api/v1/me/keys", alice, map[string]string{"public_key": pub}, 201))
	if repeated.Name != "备用电脑" || repeated.LastUsedAt != nil {
		t.Fatal("duplicate add reset label or marked usage")
	}
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]any{"username": "alice", "public_key": pub, "verify_only": true}, 200)
	checked := read[account.Key](t, f.request("GET", path, alice, nil, 200))
	if checked.LastUsedAt != nil {
		t.Fatal("unsigned probe marked key as used")
	}
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "bob", "public_key": pub}, 401)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "alice", "public_key": pub}, 200)
	used := read[account.Key](t, f.request("GET", path, alice, nil, 200))
	if used.LastUsedAt == nil {
		t.Fatal("successful key login not recorded")
	}
	renamed := read[account.Key](t, f.request("PATCH", path, alice, map[string]string{"name": ""}, 200))
	if renamed.Name != "" || renamed.LastUsedAt == nil || !renamed.LastUsedAt.Equal(*used.LastUsedAt) {
		t.Fatal("rename changed usage time")
	}
	other := read[account.Key](t, f.request("POST", "/api/v1/me/keys", alice, map[string]string{"public_key": key(t), "name": "第二把"}, 201))
	if other.LastUsedAt != nil {
		t.Fatal("one key login marked another key used")
	}
	list := read[struct {
		Items []account.Key `json:"items"`
	}](t, f.request("GET", "/api/v1/me/keys", alice, nil, 200))
	if len(list.Items) != 2 {
		t.Fatal("incorrect key list")
	}
}

func TestRegistrationKeyNameAndChallengeUsage(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	pub := key(t)
	f.request("POST", "/api/v1/accounts", "", map[string]string{"username": "alice", "public_key": pub, "key_name": "笔记本"}, 201)
	a, err := f.service.CheckKey("alice", pub)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := f.service.Keys(a.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "笔记本" || keys[0].LastUsedAt != nil {
		t.Fatal("registration metadata incorrect")
	}
	c := read[account.Challenge](t, f.request("POST", "/api/v1/auth/ssh/challenges", "", map[string]string{"username": "alice"}, 201))
	f.request("POST", "/api/v1/internal/auth/ssh/challenges/confirm", gatewaySecret, map[string]string{"username": "alice", "public_key": pub, "code": c.Code}, 204)
	keys, err = f.service.Keys(a.ID)
	if err != nil || keys[0].LastUsedAt == nil {
		t.Fatal("challenge confirmation not recorded")
	}
	f.request("POST", "/api/v1/accounts", "", map[string]string{"username": "empty", "password": "empty-password-123", "key_name": "no key"}, 400)
}
