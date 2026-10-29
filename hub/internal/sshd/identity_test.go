package sshd

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	k, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("包装公钥失败: %v", err)
	}
	out, err := ssh.ParsePublicKey(k.Marshal())
	if err != nil {
		t.Fatalf("解析公钥失败: %v", err)
	}
	return out
}

// TestIdentityIgnoresUsername 锁定"身份只看密钥"这条契约。
//
// 用户名只是昵称。如果它参与身份，就会出现这个死角：我换了个 ssh 用户名
// 重连 → 被当成新人 → 牌局已经开了所以进不去 → 而自己的旧座位还占着。
// 用户唯一稳定拥有的东西是那把私钥，所以身份只认指纹。
func TestIdentityIgnoresUsername(t *testing.T) {
	k := newKey(t)
	if Identity(k) != Identity(k) {
		t.Fatal("同一个公钥必须给出稳定身份，否则重连回不了座位")
	}
	// 身份里不应该混入用户名这类会变的东西。
	if Identity(k) == "" {
		t.Fatal("身份不能为空")
	}
}

func TestIdentityDistinguishesKeys(t *testing.T) {
	if Identity(newKey(t)) == Identity(newKey(t)) {
		t.Fatal("不同密钥必须是不同身份——否则大家共用一个密钥还能忍，各用各的反而撞了")
	}
}

func TestFingerprintIsOpenSSHFormat(t *testing.T) {
	k := newKey(t)
	fp := Fingerprint(k)
	if len(fp) < 10 || fp[:7] != "SHA256:" {
		t.Fatalf("指纹格式不对: %q", fp)
	}
	if Fingerprint(k) != fp {
		t.Fatal("同一个公钥的指纹必须稳定")
	}
	if Fingerprint(k) == Fingerprint(newKey(t)) {
		t.Fatal("不同公钥必须给出不同指纹")
	}
	if Identity(k) != fp {
		t.Fatal("身份就是指纹，两者必须一致")
	}
}
