package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestEnsureAccountIsTOFU(t *testing.T) {
	s := openTest(t)

	a, err := s.EnsureAccount("SHA256:aaa", "ssh-ed25519 AAAA", "Alice")
	if err != nil {
		t.Fatalf("建号失败: %v", err)
	}
	if a.Handle != "alice" {
		t.Fatalf("handle 应该被规范成小写 alice，实际 %q", a.Handle)
	}

	// 同一个指纹再来一次：还是同一个账号，不会新建。
	b, err := s.EnsureAccount("SHA256:aaa", "ssh-ed25519 AAAA", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID {
		t.Fatalf("同一把公钥应该还是同一个账号")
	}

	// 另一把：新账号，但 handle 冲突要自动加后缀。
	c, err := s.EnsureAccount("SHA256:bbb", "ssh-ed25519 BBBB", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == a.ID || c.Handle == "alice" {
		t.Fatalf("重名应该换个 handle，实际 %q", c.Handle)
	}
}

func TestLinkAndUnlink(t *testing.T) {
	s := openTest(t)
	a, _ := s.EnsureAccount("SHA256:a1", "k1", "alice")

	if err := s.Link(a.ID, "SHA256:a2", "k2"); err != nil {
		t.Fatalf("挂第二把钥匙失败: %v", err)
	}
	ids, _ := s.Identities(a.ID)
	if len(ids) != 2 {
		t.Fatalf("应该有两把钥匙，实际 %d", len(ids))
	}

	// 第二把钥匙现在也应该能登进同一个账号。
	got, ok, _ := s.ByFingerprint("SHA256:a2")
	if !ok || got.ID != a.ID {
		t.Fatal("第二把钥匙应该指向同一个账号")
	}

	// 删掉一把可以。
	if err := s.Unlink(a.ID, "SHA256:a2"); err != nil {
		t.Fatalf("删钥匙失败: %v", err)
	}

	// 删最后一把不行——那样人就永远进不来了。
	if err := s.Unlink(a.ID, "SHA256:a1"); err == nil {
		t.Fatal("最后一把钥匙应该删不掉")
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := openTest(t)
	a, _ := s.EnsureAccount("SHA256:s1", "k", "alice")

	tok, err := s.NewSession(a.ID, "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.SessionAccount(tok)
	if err != nil || !ok || got.ID != a.ID {
		t.Fatalf("会话应该能换回账号: %v %v", ok, err)
	}

	if err := s.DropSession(tok); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.SessionAccount(tok); ok {
		t.Fatal("注销之后会话应该失效")
	}

	// 未知 token 不该认。
	if _, ok, _ := s.SessionAccount("不存在"); ok {
		t.Fatal("随便编的 token 不该有效")
	}
}

func TestSessionExpires(t *testing.T) {
	s := openTest(t)
	a, _ := s.EnsureAccount("SHA256:e1", "k", "alice")
	tok, _ := s.NewSession(a.ID, "", time.Millisecond)

	time.Sleep(5 * time.Millisecond)
	if _, ok, _ := s.SessionAccount(tok); ok {
		t.Fatal("过期的会话应该失效")
	}
}

func TestChallengeFlow(t *testing.T) {
	s := openTest(t)
	a, _ := s.EnsureAccount("SHA256:c1", "k", "alice")

	c, err := s.NewChallenge("login", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if c.Code == "" || c.Poll == "" || c.Code == c.Poll {
		t.Fatal("code 和 poll 必须是两个不同的秘密")
	}

	// 还没确认时是 pending。
	state, _, _, ok, _ := s.Poll(c.Poll)
	if !ok || state != "pending" {
		t.Fatalf("应该是 pending，实际 %q ok=%v", state, ok)
	}

	// 终端确认（这里用指纹代替）。
	purpose, target, err := s.Confirm(c.Code, "SHA256:c1")
	if err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if purpose != "login" || target != "" {
		t.Fatalf("purpose/target 不对: %q %q", purpose, target)
	}

	state, _, fp, ok, _ := s.Poll(c.Poll)
	if !ok || state != "confirmed" || fp != "SHA256:c1" {
		t.Fatalf("应该已确认，实际 %q %q", state, fp)
	}

	// 用完即删。
	s.Consume(c.Poll)
	if _, _, _, ok, _ := s.Poll(c.Poll); ok {
		t.Fatal("消费之后应该查不到")
	}

	// 同一个 code 不能再用。
	if _, _, err := s.Confirm(c.Code, "SHA256:c1"); err == nil {
		t.Fatal("码不能重复使用")
	}
	_ = a
}

func TestChallengeExpires(t *testing.T) {
	s := openTest(t)
	c, _ := s.NewChallenge("login", "", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	if _, _, err := s.Confirm(c.Code, "fp"); err == nil {
		t.Fatal("过期之后不该确认成功")
	}
}

func TestShortCodeHasNoConfusingChars(t *testing.T) {
	for i := 0; i < 50; i++ {
		c := shortCode()
		for _, r := range c {
			if r == '-' {
				continue
			}
			if r == '0' || r == 'O' || r == '1' || r == 'I' {
				t.Fatalf("短码里不该出现易混字符: %q", c)
			}
		}
	}
}
