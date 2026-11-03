package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayCredentialsAndOriginalClientIP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/v1/auth/password" {
			if r.Header.Get("X-TTYFish-Gateway-Token") != "gateway-secret" || r.Header.Get("Authorization") != "" {
				t.Error("password gateway credential missing or used wrong header")
			}
			if r.Header.Get("X-TTYFish-Client-IP") != "198.51.100.1" {
				t.Error("wrong IPv4 client source")
			}
		} else {
			if r.Header.Get("Authorization") != "Bearer gateway-secret" {
				t.Error("internal gateway credential missing")
			}
			if r.Header.Get("X-TTYFish-Client-IP") != "2001:db8::1" {
				t.Error("wrong IPv6 client source")
			}
		}
		if r.URL.Path == "/api/v1/internal/auth/ssh/challenges/confirm" {
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"player-token","account":{"id":"account-id","username":"alice"}}`))
	}))
	defer server.Close()
	c := New(server.URL, "gateway-secret")
	ctx := context.Background()
	if _, err := c.LoginPassword(ctx, "alice", "password", "198.51.100.1:123"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckKey(ctx, "alice", "pub", "[2001:db8::1]:123"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LoginKey(ctx, "alice", "pub", "[2001:db8::1]:123"); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmChallenge(ctx, "alice", "pub", "CODE", "[2001:db8::1]:123"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("unexpected request count")
	}
}

func TestRateLimitErrorIncludesRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"wait"}}`))
	}))
	defer server.Close()
	_, err := New(server.URL, "gateway-secret").LoginPassword(context.Background(), "alice", "password")
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfterSeconds != 17 || !strings.Contains(err.Error(), "17") {
		t.Fatalf("waiting time lost: %v", err)
	}
}

func TestSetPasswordLoginEnabledUsesPlayerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/me/password-login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer player-token" {
			t.Error("player token was not sent")
		}
		var body map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["enabled"] {
			t.Error("request should disable password login")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := New(server.URL, "").SetPasswordLoginEnabled(context.Background(), "player-token", false); err != nil {
		t.Fatal(err)
	}
}
