package sshd

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"

	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
)

// prefs 记每个玩家的个人偏好，按公钥指纹索引，落盘成一个很小的 JSON。
//
// 渲染虽然发生在服务端（每个会话单独算一帧 ANSI 发给终端），但"用哪种牌面"
// 是玩家自己的选择，不该因为换条连接就被重置回服务端默认值。
//
// 读写失败一律降级：偏好读不出来就当没设置过，写不进去只记日志——
// 一个显示偏好不应该影响能不能打牌。
type prefs struct {
	mu    sync.Mutex
	path  string
	faces map[string]string
}

func loadPrefs(path string) *prefs {
	p := &prefs{path: path, faces: map[string]string{}}
	if path == "" {
		return p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	stored := map[string]string{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		slog.Warn("偏好文件解析失败，本次按默认处理", "path", path, "err", err)
		return p
	}
	p.faces = stored
	return p
}

// face 返回这个身份上次选的牌面样式；没选过时 ok=false，交给服务端默认值。
func (p *prefs) mode(key string) (sdkui.ColorMode, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	slug, exists := p.faces[key]
	if !exists {
		return sdkui.ColorBlock, false
	}
	f, valid := sdkui.ParseColorMode(slug)
	if !valid {
		return sdkui.ColorBlock, false
	}
	return f, true
}

func (p *prefs) setMode(key string, f sdkui.ColorMode) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.faces[key] = f.Slug()
	p.flushLocked()
}

// flushLocked 原子落盘：先写临时文件再 rename，避免写到一半断电留下半个 JSON。
func (p *prefs) flushLocked() {
	if p.path == "" {
		return
	}
	raw, err := json.MarshalIndent(p.faces, "", "  ")
	if err != nil {
		return
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		slog.Warn("偏好写盘失败（不影响游戏）", "path", p.path, "err", err)
		return
	}
	if err := os.Rename(tmp, p.path); err != nil {
		slog.Warn("偏好文件替换失败（不影响游戏）", "path", p.path, "err", err)
	}
}
