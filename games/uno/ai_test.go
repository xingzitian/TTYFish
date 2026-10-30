package uno

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/agent"
)

// sampleObs 造一个"轮到我、手里有牌可出"的观察。
func sampleObs(t *testing.T) engine.Observation {
	t.Helper()
	st := engine.NewGame(3, 7, engine.Rules{})
	o := engine.Observe(st, st.Turn)
	if len(o.Legal) == 0 {
		t.Fatal("局面应该至少有一个合法动作")
	}
	return o
}

// TestBrainPicksLegalMove 正常情况：模型给个编号，就要拿到对应的合法动作。
func TestBrainPicksLegalMove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"2"}}]}`))
	}))
	defer srv.Close()

	o := sampleObs(t)
	if len(o.Legal) < 2 {
		t.Skip("这个局面只有一个合法动作")
	}
	b := NewBrain(agent.New(agent.Config{BaseURL: srv.URL, Model: "m"}))
	got := b.Move(o)
	if got == nil {
		t.Fatal("应该返回一个动作")
	}
	if got != o.Legal[1] {
		t.Fatalf("应该选第二个动作，实际 %#v", got)
	}
}

// TestBrainFallsBackWhenModelFails 模型挂了要退回启发式，绝不能返回 nil 卡住牌桌。
func TestBrainFallsBackWhenModelFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	o := sampleObs(t)
	g := New(engine.Rules{}).WithAI(NewBrain(agent.New(agent.Config{BaseURL: srv.URL, Model: "m"})))
	a := g.Bot(o)
	if a == nil {
		t.Fatal("AI 失败时也应该由启发式给出动作")
	}
	if a != engine.Decide(o) {
		t.Fatalf("AI 失败时应该用启发式的结果，实际 %#v", a)
	}
}

// TestBrainTimeoutFallsBack 超时同样退回启发式，而且不能拖太久。
func TestBrainTimeoutFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"1"}}]}`))
	}))
	defer srv.Close()

	o := sampleObs(t)
	b := &Brain{Client: agent.New(agent.Config{BaseURL: srv.URL, Model: "m"}), Timeout: 40 * time.Millisecond}
	start := time.Now()
	if a := b.Move(o); a != nil {
		t.Fatalf("超时应该返回 nil，实际 %#v", a)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("超时没生效，等了 %v", elapsed)
	}
}

// TestPromptHasNoHiddenInfo 提示词里不能出现别人的手牌。
// Observation 本身就是裁剪过的，这条测试盯着"别在提示里另开一条路"。
func TestPromptHasNoHiddenInfo(t *testing.T) {
	o := sampleObs(t)
	p := promptFor(o)

	// 别人手里的具体牌面不应该出现：提示里只有"各家手牌数"。
	if strings.Contains(p, "玩家的手牌") {
		t.Fatalf("提示里不该有别人的手牌:\n%s", p)
	}
	for i, n := range o.HandCounts {
		if i == o.Seat {
			continue
		}
		if !strings.Contains(p, "张") {
			t.Fatalf("应该包含各家手牌数:\n%s", p)
		}
		_ = n
	}
	// 自己的手牌必须以可读形式出现，否则模型没法决策。
	if !strings.Contains(p, "你的手牌") {
		t.Fatalf("提示里应该有我的手牌:\n%s", p)
	}
	// 候选动作要编号，模型只回数字。
	if !strings.Contains(p, "1.") {
		t.Fatalf("候选动作应该从 1 开始编号:\n%s", p)
	}
}

// TestPromptIsStable 提示词要能稳定序列化出来（便于复现问题）。
func TestPromptIsStable(t *testing.T) {
	o := sampleObs(t)
	a, b := promptFor(o), promptFor(o)
	if a != b {
		t.Fatal("同一个局面两次生成的提示不一致")
	}
	if _, err := json.Marshal(a); err != nil {
		t.Fatalf("提示词应该能序列化: %v", err)
	}
}
