package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/store"
)

type failingRefreshRepo struct {
	account.Repository
	fail bool
}

type expiredAccessRepo struct{ account.Repository }

func (r *expiredAccessRepo) StartWebSession(access, device string, _ time.Duration, refreshTTL time.Duration) (string, error) {
	return r.Repository.StartWebSession(access, device, -time.Second, refreshTTL)
}

func (r *failingRefreshRepo) RotateWebSessionWithAccount(token string, ttl time.Duration) (string, string, store.Account, error) {
	if r.fail {
		return "", "", store.Account{}, errors.New("temporary database failure")
	}
	return r.Repository.RotateWebSessionWithAccount(token, ttl)
}

func TestWebCookieRefreshLogoutAndOrigin(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"https://example.com"}})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	call := func(path, origin, token string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "https://example.com"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if path == "/api/v1/auth/password" {
			r.Header.Set("X-Device-ID", strings.Repeat("a", 64))
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	login := call("/api/v1/auth/password", "https://example.com", "", nil, `{"username":"alice","password":"test-password-123"}`)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	session := read[account.Session](t, login)
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || session.ExpiresIn != 900 {
		t.Fatalf("unexpected session or cookie: %+v %+v", session, cookies)
	}
	old := cookies[0]
	denied := call("/api/v1/auth/refresh", "https://evil.example", "", old, "")
	if denied.Code != 403 {
		t.Fatalf("cross-origin refresh: %d", denied.Code)
	}
	refreshed := call("/api/v1/auth/refresh", "https://example.com", "", old, "")
	if refreshed.Code != 200 {
		t.Fatal(refreshed.Code, refreshed.Body.String())
	}
	newSession := read[account.Session](t, refreshed)
	newCookie := refreshed.Result().Cookies()[0]
	if old.Value == newCookie.Value || session.AccessToken == newSession.AccessToken {
		t.Fatal("credentials were not rotated")
	}
	logout := call("/api/v1/auth/logout", "https://example.com", newSession.AccessToken, newCookie, "")
	if logout.Code != 204 || logout.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("logout did not clear cookie")
	}
	if w := call("/api/v1/auth/refresh", "https://example.com", "", newCookie, ""); w.Code != 401 {
		t.Fatalf("refresh after logout: %d", w.Code)
	}
	f.request("GET", "/api/v1/me", newSession.AccessToken, nil, 401)
}

func TestRefreshServerErrorPreservesCookie(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := &failingRefreshRepo{Repository: st}
	service := account.New(repo)
	if _, err = service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	server, err := New(service, Config{AllowedOrigins: []string{"https://example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "https://example.com"+path, strings.NewReader(body))
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set("Content-Type", "application/json")
		if path == "/api/v1/auth/password" {
			r.Header.Set("X-Device-ID", strings.Repeat("a", 64))
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	login := call("/api/v1/auth/password", nil, `{"username":"alice","password":"test-password-123"}`)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	repo.fail = true
	failed := call("/api/v1/auth/refresh", cookie, "")
	if failed.Code != 500 || len(failed.Result().Cookies()) != 0 {
		t.Fatalf("temporary error changed cookie: %d %v", failed.Code, failed.Header().Values("Set-Cookie"))
	}
	repo.fail = false
	if retried := call("/api/v1/auth/refresh", cookie, ""); retried.Code != 200 {
		t.Fatalf("retry after temporary error: %d %s", retried.Code, retried.Body.String())
	}
}

func TestLogoutRejectsMismatchedBrowserCredentials(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"https://example.com"}})
	for _, name := range []string{"alice", "bob"} {
		if _, err := f.service.Register(name, "test-password-123", ""); err != nil {
			t.Fatal(err)
		}
	}
	login := func(name, device string) (account.Session, *http.Cookie) {
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
		return read[account.Session](t, w), w.Result().Cookies()[0]
	}
	a, _ := login("alice", "a")
	b, cookieB := login("bob", "b")
	r := httptest.NewRequest("POST", "https://example.com/api/v1/auth/logout", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Authorization", "Bearer "+a.AccessToken)
	r.AddCookie(cookieB)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("mismatched logout: %d %s", w.Code, w.Body.String())
	}
	f.request("GET", "/api/v1/me", a.AccessToken, nil, 200)
	f.request("GET", "/api/v1/me", b.AccessToken, nil, 200)
}

func TestLogoutRevokesCookieAfterAccessWasSwept(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	service := account.New(&expiredAccessRepo{Repository: st})
	if _, err = service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	server, err := New(service, Config{AllowedOrigins: []string{"https://example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "https://example.com/api/v1/auth/password", strings.NewReader(`{"username":"alice","password":"test-password-123"}`))
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Device-ID", strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	session, cookie := read[account.Session](t, w), w.Result().Cookies()[0]
	st.Sweep()
	r = httptest.NewRequest("POST", "https://example.com/api/v1/auth/logout", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Authorization", "Bearer "+session.AccessToken)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 204 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "https://example.com/api/v1/auth/refresh", nil)
	r.Header.Set("Origin", "https://example.com")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("refresh after logout: %d %s", w.Code, w.Body.String())
	}
}
