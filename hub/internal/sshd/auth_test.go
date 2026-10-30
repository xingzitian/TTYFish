package sshd

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

func TestAccountSSHAuthentication(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	signer := func() gossh.Signer {
		_, priv, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		k, e := gossh.NewSignerFromKey(priv)
		if e != nil {
			t.Fatal(e)
		}
		return k
	}
	key, other := signer(), signer()
	a, err := st.Register("alice", "alice-password-123", Fingerprint(key.PublicKey()), string(gossh.MarshalAuthorizedKey(key.PublicKey())))
	if err != nil {
		t.Fatal(err)
	}
	contexts := make(chan registry.UIContext, 4)
	srv, err := New(Config{Store: st, HostKeyPath: filepath.Join(t.TempDir(), "host"), Game: registry.Entry{ID: "test", Game: mountGame{}, NewUI: func(c registry.UIContext) registry.Model {
		contexts <- c
		return &mountModel{closed: make(chan struct{})}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	for _, tc := range []struct {
		name, user string
		auth       gossh.AuthMethod
		want       bool
	}{
		{"key", "alice", gossh.PublicKeys(key), true},
		{"password", "alice", gossh.Password("alice-password-123"), true},
		{"wrong username", "other", gossh.PublicKeys(key), false},
		{"wrong key", "alice", gossh.PublicKeys(other), false},
		{"wrong password", "alice", gossh.Password("wrong"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e := gossh.Dial("tcp", ln.Addr().String(), &gossh.ClientConfig{User: tc.user, Auth: []gossh.AuthMethod{tc.auth}, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
			if !tc.want {
				if e == nil {
					c.Close()
					t.Fatal("unauthorized login accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			session, e := c.NewSession()
			if e != nil {
				t.Fatal(e)
			}
			defer session.Close()
			if e = session.RequestPty("xterm", 24, 80, nil); e != nil {
				t.Fatal(e)
			}
			if e = session.Shell(); e != nil {
				t.Fatal(e)
			}
			select {
			case ctx := <-contexts:
				if ctx.Key != "account:"+a.ID || ctx.Name != "alice" {
					t.Fatalf("incorrect identity: %+v", ctx)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("game not mounted")
			}
		})
	}
}
