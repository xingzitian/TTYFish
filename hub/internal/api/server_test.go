package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"golang.org/x/crypto/ssh"
)

const gatewaySecret = "test-gateway-secret-at-least-32-bytes"

type fixture struct {
	t       *testing.T
	handler http.Handler
	service *account.Service
}

func setup(t *testing.T, cfg Config) fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	service := account.New(st)
	srv, err := New(service, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{t, srv.Handler(), service}
}
func (f fixture) request(method, path, token string, body any, want int) *httptest.ResponseRecorder {
	f.t.Helper()
	data := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		data = string(b)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		f.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func read[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func key(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

func TestAccountLifecycle(t *testing.T) {
	f := setup(t, Config{})
	registration := map[string]string{"username": "alice", "password": "alice-password-123"}
	a := read[account.Account](t, f.request("POST", "/api/v1/accounts", "", registration, 201))
	f.request("POST", "/api/v1/accounts", "", registration, 409)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "wrong"}, 401)
	session := read[account.Session](t, f.request("POST", "/api/v1/auth/password", "", registration, 200))
	if session.AccessToken == "" || session.Account.ID != a.ID || session.ExpiresIn != 86400 {
		t.Fatal("incorrect session")
	}
	current := read[account.Account](t, f.request("GET", "/api/v1/me", session.AccessToken, nil, 200))
	if current.Username != "alice" {
		t.Fatal("incorrect account")
	}
	f.request("GET", "/api/v1/me", "", nil, 401)
	pub := key(t)
	saved := read[account.Key](t, f.request("POST", "/api/v1/me/keys", session.AccessToken, map[string]string{"public_key": pub}, 201))
	var list struct {
		Items []account.Key `json:"items"`
	}
	list = read[struct {
		Items []account.Key `json:"items"`
	}](t, f.request("GET", "/api/v1/me/keys", session.AccessToken, nil, 200))
	if len(list.Items) != 1 || list.Items[0].Fingerprint != saved.Fingerprint {
		t.Fatal("incorrect key list")
	}
	f.request("DELETE", "/api/v1/me/keys/"+url.PathEscape(saved.Fingerprint), session.AccessToken, nil, 204)
	f.request("DELETE", "/api/v1/me/keys/"+url.PathEscape(saved.Fingerprint), session.AccessToken, nil, 404)
	f.request("PUT", "/api/v1/me/password", session.AccessToken, map[string]string{"password": "replacement-password"}, 204)
	f.request("POST", "/api/v1/auth/password", "", registration, 401)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "replacement-password"}, 200)
	f.request("POST", "/api/v1/auth/logout", session.AccessToken, nil, 204)
	f.request("GET", "/api/v1/me", session.AccessToken, nil, 401)
}

func TestGatewayAndKeyOwnership(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	pub := key(t)
	a := read[account.Account](t, f.request("POST", "/api/v1/accounts", "", map[string]string{"username": "alice", "public_key": pub}, 201))
	body := map[string]string{"username": "alice", "public_key": pub}
	f.request("POST", "/api/v1/internal/auth/ssh", "", body, 401)
	f.request("POST", "/api/v1/internal/auth/ssh", "wrong-token", body, 401)
	session := read[account.Session](t, f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, body, 200))
	if session.Account.ID != a.ID {
		t.Fatal("different account for key")
	}
	f.request("POST", "/api/v1/internal/auth/ssh", session.AccessToken, body, 401)
	f.request("GET", "/api/v1/me", gatewaySecret, nil, 401)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "wrong", "public_key": pub}, 401)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "alice", "public_key": key(t)}, 401)
	fp, _, _ := account.ParsePublicKey(pub)
	path := "/api/v1/me/keys/" + url.PathEscape(fp)
	f.request("DELETE", path, session.AccessToken, nil, 409)
	bob, err := f.service.Register("bob", "bob-password-123", "")
	if err != nil {
		t.Fatal(err)
	}
	bs, err := f.service.LoginPassword(bob.Username, "bob-password-123")
	if err != nil {
		t.Fatal(err)
	}
	f.request("POST", "/api/v1/me/keys", bs.AccessToken, map[string]string{"public_key": pub}, 409)
	f.request("DELETE", path, bs.AccessToken, nil, 404)
	f.request("PUT", "/api/v1/me/password", session.AccessToken, map[string]string{"password": "alice-password-123"}, 204)
	f.request("DELETE", path, session.AccessToken, nil, 204)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, body, 401)
}

func TestValidationAndTransport(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"http://localhost:5173"}})
	for _, body := range []map[string]string{
		{"username": "Alice", "password": "long-password"},
		{"username": "alice"},
		{"username": "alice", "password": "short"},
		{"username": "alice", "public_key": "not a key"},
	} {
		f.request("POST", "/api/v1/accounts", "", body, 400)
	}
	f.request("POST", "/api/v1/internal/auth/ssh", "", map[string]string{}, 503)
	for _, tc := range []struct {
		method, path, content, body, origin string
		status                              int
	}{
		{"POST", "/api/v1/accounts", "application/json", `{"username":"alice","unknown":true}`, "", 400},
		{"POST", "/api/v1/accounts", "application/json", `{} {}`, "", 400},
		{"POST", "/api/v1/accounts", "text/plain", `{}`, "", 415},
		{"GET", "/not-found", "", "", "", 404},
		{"PATCH", "/api/v1/me", "", "", "", 405},
		{"OPTIONS", "/api/v1/me", "", "", "http://localhost:5173", 204},
		{"OPTIONS", "/api/v1/me", "", "", "https://evil.example", 403},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.content)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if tc.status != 204 && !json.Valid(w.Body.Bytes()) {
			t.Fatal("non-JSON error")
		}
	}
}

func TestRequestLimitAndRateLimit(t *testing.T) {
	f := setup(t, Config{})
	r := httptest.NewRequest("POST", "/api/v1/accounts", strings.NewReader(`{"username":"`+strings.Repeat("a", 17000)+`"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("oversize request: %d", w.Code)
	}
	for i := 0; i < 9; i++ {
		f.request("POST", "/api/v1/accounts", "", map[string]string{}, 400)
	}
	f.request("POST", "/api/v1/accounts", "", map[string]string{}, 429)
}

func TestRejectWeakGatewaySecret(t *testing.T) {
	if _, err := New(nil, Config{GatewayToken: "short"}); err == nil {
		t.Fatal("weak gateway secret accepted")
	}
}

func TestOpenAPIContractIsServed(t *testing.T) {
	f := setup(t, Config{})
	w := f.request("GET", "/api/openapi.json", "", nil, 200)
	var spec struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.0.3" || len(spec.Paths) != 9 {
		t.Fatalf("unexpected contract: %s, %d paths", spec.OpenAPI, len(spec.Paths))
	}
}
