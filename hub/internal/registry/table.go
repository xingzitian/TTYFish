package registry

import (
	"fmt"
	"sort"
)

// Registry 是网关挂载的游戏表：id → Entry。
//
// 它只存"这个进程能渲染哪些游戏"。后端那边有自己的游戏表（`GET /api/v1/games`），
// 两边取交集才是能玩的——网关没挂 UI 的游戏不该出现在大厅里。
type Registry struct {
	entries map[string]Entry
}

func New() *Registry {
	return &Registry{entries: map[string]Entry{}}
}

// Register 挂载一个游戏；重复标识会报错，避免后注册的悄悄覆盖前面的。
func (r *Registry) Register(e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if _, exists := r.entries[e.ID]; exists {
		return fmt.Errorf("游戏 %q 已经注册过了", e.ID)
	}
	r.entries[e.ID] = e
	return nil
}

// Get 按标识取一个游戏。
func (r *Registry) Get(id string) (Entry, bool) {
	e, ok := r.entries[id]
	return e, ok
}

// List 按标识排序返回全部游戏，供大厅展示。
func (r *Registry) List() []Entry {
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len 是挂载数量。
func (r *Registry) Len() int { return len(r.entries) }
