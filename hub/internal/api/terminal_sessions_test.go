package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTerminalEndpointsRequireGatewayAndReplaceLease(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret, RateLimits: RateLimits{GatewayRequests: 1}})
	first := loginPlayer(t, f, "alice")
	secondSession, err := f.service.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, token, gateway, lease string) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"lease_id": lease})
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if gateway != "" {
			r.Header.Set("X-TTYFish-Gateway-Token", gateway)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/internal/terminal/claim"
	if w := call(path, first, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("claim without gateway: %d", w.Code)
	}
	w := call(path, first, gatewaySecret, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	lease1 := read[struct {
		Lease string `json:"lease_id"`
	}](t, w).Lease
	w = call(path, secondSession.AccessToken, gatewaySecret, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	lease2 := read[struct {
		Lease string `json:"lease_id"`
	}](t, w).Lease
	if lease1 == lease2 {
		t.Fatal("lease did not change")
	}
	if w = call("/api/v1/internal/terminal/heartbeat", first, gatewaySecret, lease1); w.Code != 401 {
		t.Fatalf("old terminal heartbeat: %d", w.Code)
	}
	if w = call("/api/v1/internal/terminal/heartbeat", secondSession.AccessToken, gatewaySecret, lease2); w.Code != 204 {
		t.Fatalf("current terminal heartbeat: %d", w.Code)
	}
	f.request("GET", "/api/v1/me/terminal", secondSession.AccessToken, nil, 200)
	if w = call("/api/v1/internal/terminal/release", secondSession.AccessToken, gatewaySecret, lease2); w.Code != 204 {
		t.Fatalf("release: %d", w.Code)
	}
	f.request("GET", "/api/v1/me", secondSession.AccessToken, nil, 401)
}

func TestTerminalEventsNotifyTakeover(t *testing.T) {
	f := setup(t, Config{GatewayToken: gatewaySecret})
	first := loginPlayer(t, f, "alice")
	second, err := f.service.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	claim := func(token string) string {
		t.Helper()
		r, _ := http.NewRequest("POST", server.URL+"/api/v1/internal/terminal/claim", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(gatewayHeader, gatewaySecret)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("claim: %d", res.StatusCode)
		}
		var body struct {
			Lease string `json:"lease_id"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Lease
	}
	lease := claim(first)
	body, _ := json.Marshal(map[string]string{"lease_id": lease})
	r, _ := http.NewRequest("POST", server.URL+"/api/v1/internal/terminal/events", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+first)
	r.Header.Set(gatewayHeader, gatewaySecret)
	r.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: ready\n" {
		t.Fatalf("missing ready: %q %v", line, err)
	}
	claim(second.AccessToken)
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "event: revoked\n" {
			break
		}
	}
}
