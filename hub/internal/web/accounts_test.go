package web

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestWebRegistrationAndCredentials(t *testing.T) {
	s, st := newTestServer(t)
	post := func(path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	values := url.Values{"username": {"alice"}, "password": {"alice-password-123"}}
	w := post("/register", values, nil)
	if w.Code != 303 || len(w.Result().Cookies()) == 0 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if w = post("/register", values, nil); w.Code != 400 {
		t.Fatal("duplicate registration accepted")
	}
	if w = post("/auth/password", values, nil); w.Code != 303 {
		t.Fatal("password login rejected")
	}
	if w = post("/auth/password", url.Values{"username": {"alice"}, "password": {"wrong"}}, nil); w.Code != 401 {
		t.Fatal("wrong password accepted")
	}
	if w = post("/account/password", url.Values{"password": {"replacement-password"}}, nil); w.Code != 401 {
		t.Fatal("anonymous password change accepted")
	}
	if w = post("/account/password", url.Values{"password": {"replacement-password"}}, cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	if _, ok := st.AuthenticatePassword("alice", "replacement-password"); !ok {
		t.Fatal("password was not changed")
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if w = post("/account/key", url.Values{"public_key": {string(ssh.MarshalAuthorizedKey(k))}}, cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	a, ok, _ := st.ByFingerprint(ssh.FingerprintSHA256(k))
	if !ok || a.Handle != "alice" {
		t.Fatal("key not linked")
	}
	if w = post("/auth/start", url.Values{"username": {"alice"}}, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "alice@127.0.0.1") {
		t.Fatal("incorrect SSH command", w.Body.String())
	}
	if w = post("/register", url.Values{"username": {"bob"}, "public_key": {string(ssh.MarshalAuthorizedKey(k))}}, nil); w.Code != 400 {
		t.Fatal("another account claimed key")
	}
}

func TestCredentialMutationsRejectCrossOrigin(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{"/register", "/auth/password", "/account/password", "/account/key"} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
