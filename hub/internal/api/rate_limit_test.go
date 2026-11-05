package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/ratelimit"
)

func requestFrom(t *testing.T, f fixture, path, token string, body any, remote string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(string(data)))
	r.RemoteAddr = remote
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("got %d want %d: %s", w.Code, status, w.Body.String())
	}
}

func TestGatewayUsersDoNotShareThePublicBudget(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{PublicAuthIP: 2}})
	for i := 0; i < 2; i++ {
		f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "missing", "password": "wrong"}, 401)
	}
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "missing", "password": "wrong"}, 429)
	for i := 0; i < 15; i++ {
		username := fmt.Sprintf("player%02d", i)
		pub := key(t)
		if _, err := f.service.Register(username, "", pub); err != nil {
			t.Fatal(err)
		}
		w := requestFrom(t, f, "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": username, "public_key": pub}, "192.0.2.1:123", map[string]string{gatewayIPHeader: fmt.Sprintf("198.51.100.%d", i+1)})
		wantStatus(t, w, 200)
	}
	if _, err := f.service.Register("passworduser", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	w := requestFrom(t, f, "/api/v1/auth/password", "", map[string]string{"username": "passworduser", "password": "test-password-123"}, "192.0.2.1:123", map[string]string{gatewayHeader: gatewaySecret, gatewayIPHeader: "198.51.100.20"})
	wantStatus(t, w, 200)
	token := read[account.Session](t, w).AccessToken
	w = requestFrom(t, f, "/api/v1/internal/terminal/claim", token, nil, "192.0.2.1:123", map[string]string{gatewayHeader: gatewaySecret})
	wantStatus(t, w, 200)
	terminal := read[struct {
		Terminal struct {
			SourceIP string `json:"source_ip"`
		} `json:"terminal"`
	}](t, f.request("GET", "/api/v1/me/terminal", token, nil, 200))
	if terminal.Terminal.SourceIP != "198.51.100.20" {
		t.Fatal("verified gateway source not recorded")
	}
}

func TestAccountBudgetsBlockRotatingSourcesWithoutBlockingOthers(t *testing.T) {
	f := setup(t, Config{RateLimits: RateLimits{AccountClient: 2, AccountTotal: 3}})
	if _, err := f.service.Register("alice", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Register("bob", "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{"username": "alice", "password": "wrong"}
	for i := 0; i < 2; i++ {
		wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "192.0.2.1:123", nil), 401)
	}
	w := requestFrom(t, f, "/api/v1/auth/password", "", bad, "192.0.2.1:999", map[string]string{"X-Forwarded-For": "203.0.113.9", gatewayIPHeader: "203.0.113.9"})
	wantStatus(t, w, 429)
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "192.0.2.2:123", nil), 401)
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "192.0.2.3:123", nil), 429)
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", map[string]string{"username": "bob", "password": "test-password-123"}, "192.0.2.1:123", nil), 200)
}

func TestKeyProbeAndLoginHaveSeparateBudgets(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{KeyClient: 2, KeyAccount: 3}})
	pub := key(t)
	if _, err := f.service.Register("alice", "", pub); err != nil {
		t.Fatal(err)
	}
	check := map[string]any{"username": "alice", "public_key": pub, "verify_only": true}
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, check, 200)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, check, 200)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, check, 429)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]string{"username": "alice", "public_key": pub}, 200)
}

func TestTrustedProxyPartitionsClientBudgets(t *testing.T) {
	f := setup(t, Config{TrustedProxies: []string{"127.0.0.1/32"}, RateLimits: RateLimits{PublicAuthIP: 1}})
	bad := map[string]string{"username": "missing", "password": "wrong"}
	h := map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.1"}
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "127.0.0.1:123", h), 401)
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "127.0.0.1:234", h), 429)
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", bad, "127.0.0.1:123", map[string]string{"X-Forwarded-For": "198.51.100.2"}), 401)
}

func TestInvalidGatewayCannotUseHigherQuota(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{InvalidGatewayIP: 1}})
	body := map[string]string{"username": "missing", "password": "wrong"}
	h := map[string]string{gatewayHeader: "wrong", gatewayIPHeader: "198.51.100.1"}
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", body, "192.0.2.1:123", h), 401)
	h[gatewayIPHeader] = "198.51.100.2"
	wantStatus(t, requestFrom(t, f, "/api/v1/auth/password", "", body, "192.0.2.1:123", h), 429)
	f.request("POST", "/api/v1/auth/password", "", body, 401)
}

func TestRetryAfterTracksWindowAndIsExposedToBrowser(t *testing.T) {
	f := setup(t, Config{AllowedOrigins: []string{"http://localhost:5173"}})
	// Use the real API with a controlled clock instead of sleeping through a minute.
	s, err := New(f.service, Config{AllowedOrigins: []string{"http://localhost:5173"}, RateLimits: RateLimits{PublicAuthIP: 1}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	s.limiter = ratelimit.New(20, func() time.Time { return now })
	f.handler = s.Handler()
	body := map[string]string{"username": "missing", "password": "wrong"}
	f.request("POST", "/api/v1/auth/password", "", body, 401)
	now = now.Add(35*time.Second + 200*time.Millisecond)
	w := requestFrom(t, f, "/api/v1/auth/password", "", body, "192.0.2.1:123", map[string]string{"Origin": "http://localhost:5173"})
	wantStatus(t, w, 429)
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || seconds != 25 {
		t.Fatal("incorrect Retry-After", w.Header())
	}
	if w.Header().Get("Access-Control-Expose-Headers") != "Retry-After" {
		t.Fatal("browser cannot read Retry-After")
	}
	now = now.Add(24800 * time.Millisecond)
	f.request("POST", "/api/v1/auth/password", "", body, 401)
}

func TestPollBudgetsArePerTokenAndIndependentOfLogin(t *testing.T) {
	f := setup(t, Config{RateLimits: RateLimits{PollIP: 5, PollToken: 2}})
	first := map[string]string{"poll_token": strings.Repeat("0", 64)}
	second := map[string]string{"poll_token": strings.Repeat("1", 64)}
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", first, 200)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", first, 200)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", first, 429)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", second, 200)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", second, 200)
	f.request("POST", "/api/v1/auth/ssh/challenges/poll", "", map[string]string{"poll_token": strings.Repeat("2", 64)}, 429)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "missing", "password": "wrong"}, 401)
}

func TestPasswordChangesArePerAccount(t *testing.T) {
	f := setup(t, Config{RateLimits: RateLimits{PasswordChanges: 1}})
	alice := loginPlayer(t, f, "alice")
	bob := loginPlayer(t, f, "bob")
	body := map[string]string{"password": "replacement-password-123"}
	f.request("PUT", "/api/v1/me/password", alice, body, 204)
	f.request("PUT", "/api/v1/me/password", alice, body, 429)
	f.request("PUT", "/api/v1/me/password", bob, body, 204)
	f.request("POST", "/api/v1/auth/password", "", map[string]string{"username": "alice", "password": "replacement-password-123"}, 200)
}

func TestGatewayAggregateBudgetStillApplies(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{GatewayRequests: 1}})
	pub := key(t)
	if _, err := f.service.Register("alice", "", pub); err != nil {
		t.Fatal(err)
	}
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]any{"username": "alice", "public_key": pub, "verify_only": true}, 200)
	f.request("POST", "/api/v1/internal/auth/ssh", gatewaySecret, map[string]any{"username": "alice", "public_key": pub}, 429)
}

func TestUserAgentAndClientHeaderCannotImpersonateGateway(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{PublicAuthIP: 1}})
	body := map[string]string{"username": "missing", "password": "wrong"}
	f.request("POST", "/api/v1/auth/password", "", body, 401)
	w := requestFrom(t, f, "/api/v1/auth/password", "", body, "192.0.2.1:234", map[string]string{"User-Agent": "TTYFish-SSH-Gateway", gatewayIPHeader: "198.51.100.99"})
	wantStatus(t, w, 429)
}
