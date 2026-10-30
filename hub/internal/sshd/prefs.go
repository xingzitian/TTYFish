package sshd

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"

	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
)

// prefs 记每个玩家的个人偏好，按账号索引，落盘成一个很小的 JSON。
//
// 渲染虽然发生在服务端（每个会话单独算一帧 ANSI 发给终端），但"用哪种牌面"
// 和"界面要多精简"是玩家自己的选择，不该因为换条连接就被重置回服务端默认值。
//
// 读写失败一律降级：偏好读不出来就当没设置过，写不进去只记日志——
// 一个显示偏好不应该影响能不能打牌。
type prefs struct {
	mu    sync.Mutex
	path  string
	items map[string]prefEntry
}

// prefEntry 是一个账号的偏好。
//
// 老版本只存了颜色（JSON 里直接是字符串），所以解析时要兼容两种形状，
// 否则升级后所有人的牌面设置都会被清掉。
type prefEntry struct {
	Mode   string `json:"mode,omitempty"`
	Layout string `json:"layout,omitempty"`
}

func loadPrefs(path string) *prefs {
	p := &prefs{path: path, items: map[string]prefEntry{}}
	if path == "" {
		return p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return p
	}

	var stored map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		slog.Warn("偏好文件解析失败，本次按默认处理", "path", path, "err", err)
		return p
	}
	for key, value := range stored {
		trimmed := strings.TrimSpace(string(value))
		if strings.HasPrefix(trimmed, "{") {
			var e prefEntry
			if err := json.Unmarshal(value, &e); err == nil {
				p.items[key] = e
			}
			continue
		}
		// 老格式：直接是一个颜色名
		var mode string
		if err := json.Unmarshal(value, &mode); err == nil {
			p.items[key] = prefEntry{Mode: mode}
		}
	}
	return p
}

// mode 返回这个账号上次选的颜色深浅；没选过时 ok=false，交给默认值。
func (p *prefs) mode(key string) (sdkui.ColorMode, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, exists := p.items[key]
	if !exists || e.Mode == "" {
		return sdkui.ColorBlock, false
	}
	f, valid := sdkui.ParseColorMode(e.Mode)
	if !valid {
		return sdkui.ColorBlock, false
	}
	return f, true
}

func (p *prefs) setMode(key string, f sdkui.ColorMode) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e := p.items[key]
	e.Mode = f.Slug()
	p.items[key] = e
	p.flushLocked()
}

// layout 返回这个账号上次选的界面密度。
func (p *prefs) layout(key string) (sdkui.Layout, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, exists := p.items[key]
	if !exists || e.Layout == "" {
		return sdkui.LayoutRich, false
	}
	l, valid := sdkui.ParseLayout(e.Layout)
	if !valid {
		return sdkui.LayoutRich, false
	}
	return l, true
}

func (p *prefs) setLayout(key string, l sdkui.Layout) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e := p.items[key]
	e.Layout = l.Slug()
	p.items[key] = e
	p.flushLocked()
}

// flushLocked 原子落盘：先写临时文件再 rename，避免写到一半断电留下半个 JSON。
func (p *prefs) flushLocked() {
	if p.path == "" {
		return
	}
	raw, err := json.MarshalIndent(p.items, "", "  ")
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
