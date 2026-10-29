package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChoose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("鉴权头不对: %q", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "m" || len(body.Messages) != 2 {
			t.Errorf("请求体不对: %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"我选 2"}}]}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if !c.Enabled() {
		t.Fatal("配置齐全应该是启用的")
	}
	pick, err := c.Choose(context.Background(), "sys", "user", 3)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if pick != 2 {
		t.Fatalf("应该选 2，实际 %d", pick)
	}
}

func TestChooseRejectsOutOfRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"9"}}]}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Model: "m"})
	if _, err := c.Choose(context.Background(), "s", "u", 3); err == nil {
		t.Fatal("超出候选范围的答案应该被拒")
	}
}

func TestChooseRespectsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"1"}}]}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Model: "m", Timeout: 30 * time.Millisecond})
	start := time.Now()
	if _, err := c.Choose(context.Background(), "s", "u", 3); err == nil {
		t.Fatal("超时应该报错")
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("超时没生效，等了 %v", elapsed)
	}
}

func TestFirstNumber(t *testing.T) {
	cases := map[string]int{
		"3":         3,
		"我选择 2。":    2,
		"答案：4":      4,
		"no number": -1,
		"100":       -1,
		"1和2":       1,
	}
	for in, want := range cases {
		if got := firstNumber(in); got != want {
			t.Fatalf("%q 应该解析成 %d，实际 %d", in, want, got)
		}
	}
}

func TestAvailableNilWhenUnconfigured(t *testing.T) {
	if c := Available(Config{}); c != nil {
		t.Fatal("没配置时应该返回 nil")
	}
	if c := Available(Config{BaseURL: "http://x", Model: "m"}); c == nil {
		t.Fatal("配置齐全时应该可用")
	}
}
