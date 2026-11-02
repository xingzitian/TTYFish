package api

import (
	"net/http/httptest"
	"testing"
)

func TestSourceTrustBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded, gatewayIP, want string
		trusted                                  []string
		gateway, wantError                       bool
	}{
		{name: "untrusted forwarded header", remote: "192.0.2.10:123", forwarded: "203.0.113.9", want: "192.0.2.10"},
		{name: "untrusted gateway header", remote: "192.0.2.10:123", gatewayIP: "203.0.113.9", want: "192.0.2.10"},
		{name: "trusted proxy", remote: "127.0.0.1:123", forwarded: "203.0.113.9", want: "203.0.113.9", trusted: []string{"127.0.0.1/32"}},
		{name: "ignore forged leftmost", remote: "127.0.0.1:123", forwarded: "1.2.3.4, 203.0.113.9", want: "203.0.113.9", trusted: []string{"127.0.0.1/32"}},
		{name: "multiple trusted proxies", remote: "10.0.0.1:123", forwarded: "203.0.113.9, 172.16.0.2", want: "203.0.113.9", trusted: []string{"10.0.0.0/8", "172.16.0.0/12"}},
		{name: "malformed nearest hop", remote: "127.0.0.1:123", forwarded: "203.0.113.9, bad", want: "127.0.0.1", trusted: []string{"127.0.0.1/32"}},
		{name: "gateway authenticated", remote: "127.0.0.1:123", gatewayIP: "203.0.113.9", want: "203.0.113.9", gateway: true},
		{name: "bad gateway IP", remote: "127.0.0.1:123", gatewayIP: "203.0.113.9:123", gateway: true, wantError: true},
		{name: "gateway mapped IPv4", remote: "127.0.0.1:123", gatewayIP: "::ffff:203.0.113.9", want: "203.0.113.9", gateway: true},
		{name: "mapped peer", remote: "[::ffff:192.0.2.10]:123", want: "192.0.2.10"},
		{name: "mapped proxy prefix", remote: "127.0.0.1:123", forwarded: "2001:db8::10", want: "2001:db8::10", trusted: []string{"::ffff:127.0.0.1/128"}},
		{name: "unknown peer", remote: "not-an-ip:123", forwarded: "203.0.113.9", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := newSourceResolver(tc.trusted)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			if tc.gatewayIP != "" {
				r.Header.Set(gatewayIPHeader, tc.gatewayIP)
			}
			got, err := resolver.ip(r, tc.gateway)
			if (err != nil) != tc.wantError || got != tc.want {
				t.Fatalf("got %q err=%v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestInvalidTrustedProxyConfiguration(t *testing.T) {
	for _, cidr := range []string{"127.0.0.1", "0.0.0.0/0", "::/0", "bad", "::ffff:0.0.0.0/96"} {
		if _, err := New(nil, Config{TrustedProxies: []string{cidr}}); err == nil {
			t.Fatalf("accepted %q", cidr)
		}
	}
	if _, err := New(nil, Config{RateLimits: RateLimits{GatewayRequests: -1}}); err == nil {
		t.Fatal("negative budget accepted")
	}
}
