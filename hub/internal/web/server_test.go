package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, "hub 127.0.0.1"), st
}

func TestLoginPageRenders(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("首页应该 200，实际 %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "用 SSH 登录") {
		t.Fatalf("首页应该有登录按钮:\n%s", body)
	}
	if strings.Contains(body, "密码") && strings.Contains(body, "忘记密码") {
		t.Fatal("不该出现密码相关的东西")
	}
}

// TestChallengeLoginEndToEnd 是 M0 的核心验收：
// 浏览器发起 → 拿到短码 → 终端确认 → 浏览器轮询拿到会话。
func TestChallengeLoginEndToEnd(t *testing.T) {
	s, st := newTestServer(t)
	h := s.Handler()

	// 1. 浏览器发起
	req := httptest.NewRequest("POST", "/auth/start", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("发起登录失败: %d %s", w.Code, w.Body.String())
	}
	code, poll := extractCode(t, w.Body.String())
	if code == "" || poll == "" {
		t.Fatal("页面里应该有短码和轮询 token")
	}

	// 2. 还没确认时轮询是 pending
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/auth/poll?p="+url.QueryEscape(poll), nil))
	if !strings.Contains(w.Body.String(), `"pending"`) {
		t.Fatalf("应该是 pending，实际 %s", w.Body.String())
	}

	// 3. 终端确认（这里直接调 store，等价于 ssh 那条命令）
	fp := "SHA256:endtoend"
	purpose, _, err := st.Confirm(code, fp)
	if err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if purpose != "login" {
		t.Fatalf("purpose 不对: %q", purpose)
	}

	// 4. 轮询拿到会话
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/auth/poll?p="+url.QueryEscape(poll), nil))
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("应该登录成功，实际 %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != cookieName {
		t.Fatal("应该设置会话 cookie")
	}
	if !cookies[0].HttpOnly {
		t.Fatal("会话 cookie 必须是 HttpOnly")
	}

	// 5. 用这个 cookie 访问账号页
	req = httptest.NewRequest("GET", "/account", nil)
	req.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "公钥") {
		t.Fatalf("账号页应该显示公钥列表:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SHA256:endtoend"[:12]) {
		t.Fatalf("账号页应该显示这把公钥:\n%s", w.Body.String())
	}
}

// TestShortCodeIsSingleUse 同一个码不能登两次。
func TestShortCodeIsSingleUse(t *testing.T) {
	s, st := newTestServer(t)
	req := httptest.NewRequest("POST", "/auth/start", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	code, poll := extractCode(t, w.Body.String())

	if _, _, err := st.Confirm(code, "fp1"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/auth/poll?p="+url.QueryEscape(poll), nil))
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("第一次应该成功: %s", w.Body.String())
	}

	// 再轮询一次：挑战已经消费，应该 expired
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/auth/poll?p="+url.QueryEscape(poll), nil))
	if !strings.Contains(w.Body.String(), `"expired"`) {
		t.Fatalf("重复轮询应该失效: %s", w.Body.String())
	}
}

func TestCrossSitePostRejected(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/auth/start", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Host = "example.com"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨站 POST 应该被拒，实际 %d", w.Code)
	}
}

func TestAccountRequiresLogin(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/account", nil))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("未登录应该跳转到首页，实际 %d", w.Code)
	}
}

// extractCode 从"等待确认"页面里抠出短码和轮询 token。
func extractCode(t *testing.T, body string) (code, poll string) {
	t.Helper()
	// 命令行长这样：ssh hub 127.0.0.1 auth ABCD-2345
	const marker = " auth "
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("页面里没有 auth 命令:\n%s", body)
	}
	rest := body[i+len(marker):]
	end := strings.IndexAny(rest, "< \n")
	if end < 0 {
		end = len(rest)
	}
	code = strings.TrimSpace(rest[:end])

	// var poll = "....";
	const pm = "var poll = "
	j := strings.Index(body, pm)
	if j < 0 {
		return code, ""
	}
	rest = body[j+len(pm):]
	end = strings.IndexAny(rest, ";")
	if end < 0 {
		return code, ""
	}
	poll = strings.Trim(strings.TrimSpace(rest[:end]), `"`)
	return code, poll
}
