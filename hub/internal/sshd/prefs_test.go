package sshd

import (
	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
	"os"
	"path/filepath"
	"testing"
)

func TestPrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")

	p := loadPrefs(path)
	if _, ok := p.mode("fp-1"); ok {
		t.Fatal("没设置过不该返回 ok")
	}

	p.setMode("fp-1", sdkui.ColorText)

	// 重新加载：应该读回来。
	q := loadPrefs(path)
	f, ok := q.mode("fp-1")
	if !ok || f != sdkui.ColorText {
		t.Fatalf("应该读回 text，实际 %v ok=%v", f, ok)
	}

	// 另一个身份没设置过。
	if _, ok := q.mode("fp-2"); ok {
		t.Fatal("别的身份不该有偏好")
	}
}

func TestPrefsBadFileFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := os.WriteFile(path, []byte("{ 这不是 json"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := loadPrefs(path)
	if _, ok := p.mode("fp-1"); ok {
		t.Fatal("坏文件应该降级成空偏好，而不是崩掉")
	}

	// 写一次应该把坏文件覆盖成正常的。
	p.setMode("fp-1", sdkui.ColorNone)
	q := loadPrefs(path)
	if f, ok := q.mode("fp-1"); !ok || f != sdkui.ColorNone {
		t.Fatalf("写回之后应该能读出来，实际 %v ok=%v", f, ok)
	}
}
