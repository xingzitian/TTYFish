// Package backend 是账号 API 的客户端，供 SSH 网关使用。
//
// 网关自己不存账号、不查库：它只负责证明"这条 SSH 连接背后是谁"，
// 然后拿后端发的玩家令牌去操作房间。
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrTerminalRevoked = errors.New("终端租约已失效")

// Account 是后端返回的账号信息。
type Account struct {
	ID                   string `json:"id"`
	Username             string `json:"username"`
	DisplayName          string `json:"display_name"`
	CreatedAt            string `json:"created_at"`
	PasswordLoginEnabled bool   `json:"password_login_enabled"`
	HasSSHKey            bool   `json:"has_ssh_key"`
}

// Player 是一次成功认证的结果：用 Token 代表这个玩家访问房间接口。
type Player struct {
	Token   string  `json:"access_token"`
	Account Account `json:"account"`
}

// Client 调后端的账号相关接口。
type Client struct {
	base         string
	gatewayToken string
	hc           *http.Client
}

func New(base, gatewayToken string) *Client {
	return &Client{
		base:         strings.TrimRight(base, "/"),
		gatewayToken: gatewayToken,
		hc:           &http.Client{Timeout: 15 * time.Second},
	}
}

// Health 用于启动时检查后端是否可达。
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("后端健康检查返回 %d", resp.StatusCode)
	}
	return nil
}

// LoginKey 用"用户名 + 已通过 SSH 签名验证的公钥"换玩家令牌。
//
// 前提是网关**已经**完成了 SSH 握手签名验证——这个接口信任网关的结论，
// 不能把它当成"任何人提交公钥就能登录"的入口。
func (c *Client) LoginKey(ctx context.Context, username, publicKey string, clients ...string) (Player, error) {
	body := map[string]string{"username": username, "public_key": publicKey}
	return c.playerCall(ctx, "/api/v1/internal/auth/ssh", body, true, clients...)
}

// CheckKey checks key ownership without issuing a token or recording key usage.
func (c *Client) CheckKey(ctx context.Context, username, publicKey string, clients ...string) (Account, error) {
	status, raw, err := c.do(ctx, http.MethodPost, "/api/v1/internal/auth/ssh", map[string]any{"username": username, "public_key": publicKey, "verify_only": true}, true, clients...)
	if err != nil {
		return Account{}, err
	}
	if status != http.StatusOK {
		return Account{}, apiError(status, raw)
	}
	var result struct {
		Account Account `json:"account"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return Account{}, err
	}
	if result.Account.ID == "" {
		return Account{}, fmt.Errorf("后端没有返回账号")
	}
	return result.Account, nil
}

// LoginPassword 用用户名密码换玩家令牌。
func (c *Client) LoginPassword(ctx context.Context, username, password string, clients ...string) (Player, error) {
	body := map[string]string{"username": username, "password": password}
	return c.playerCall(ctx, "/api/v1/auth/password", body, false, clients...)
}

func (c *Client) SetPasswordLoginEnabled(ctx context.Context, token string, enabled bool) error {
	status, raw, err := c.doAuthorized(ctx, http.MethodPut, "/api/v1/me/password-login", map[string]bool{"enabled": enabled}, token)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return apiError(status, raw)
	}
	return nil
}

func (c *Client) terminalCall(ctx context.Context, path, token, lease string) ([]byte, error) {
	var body io.Reader
	if lease != "" {
		raw, _ := json.Marshal(map[string]string{"lease_id": lease})
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-TTYFish-Gateway-Token", c.gatewayToken)
	if lease != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrTerminalRevoked
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, apiError(resp.StatusCode, raw)
	}
	return raw, nil
}
func (c *Client) ClaimTerminal(ctx context.Context, token string) (string, error) {
	raw, err := c.terminalCall(ctx, "/api/v1/internal/terminal/claim", token, "")
	if err != nil {
		return "", err
	}
	var result struct {
		Lease string `json:"lease_id"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Lease == "" {
		return "", fmt.Errorf("后端未返回终端租约")
	}
	return result.Lease, nil
}
func (c *Client) HeartbeatTerminal(ctx context.Context, token, lease string) error {
	_, err := c.terminalCall(ctx, "/api/v1/internal/terminal/heartbeat", token, lease)
	return err
}
func (c *Client) ReleaseTerminal(ctx context.Context, token, lease string) error {
	_, err := c.terminalCall(ctx, "/api/v1/internal/terminal/release", token, lease)
	return err
}

// WatchTerminal waits for a takeover notification; callers should reconnect on
// transport errors and continue heartbeat-based lease checks independently.
func (c *Client) WatchTerminal(ctx context.Context, token, lease string) error {
	raw, err := json.Marshal(map[string]string{"lease_id": lease})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/internal/terminal/events", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-TTYFish-Gateway-Token", c.gatewayToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrTerminalRevoked
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("终端事件返回 HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if scanner.Text() == "event: revoked" {
			return ErrTerminalRevoked
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// Points 是账号的 PT 余额。
type Points struct {
	// BalanceTenths 以 0.1 PT 为单位；BalancePT 是带一位小数的字符串，
	// 后端直接给了展示用的值，客户端不要自己去算浮点。
	BalanceTenths int64  `json:"balance_tenths"`
	BalancePT     string `json:"balance_pt"`
}

// PlayerPoints 查当前玩家的 PT 余额。
func (c *Client) PlayerPoints(ctx context.Context, token string) (Points, error) {
	status, raw, err := c.doAuthorized(ctx, http.MethodGet, "/api/v1/me/points", nil, token)
	if err != nil {
		return Points{}, err
	}
	if status != http.StatusOK {
		return Points{}, apiError(status, raw)
	}
	var p Points
	if err := json.Unmarshal(raw, &p); err != nil {
		return Points{}, err
	}
	return p, nil
}

// ConfirmChallenge 确认网页登录用的一次性码。
func (c *Client) ConfirmChallenge(ctx context.Context, username, publicKey, code string, clients ...string) error {
	body := map[string]string{
		"username":   username,
		"public_key": publicKey,
		"code":       code,
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/api/v1/internal/auth/ssh/challenges/confirm", body, true, clients...)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return apiError(status, raw)
	}
	return nil
}

func (c *Client) playerCall(ctx context.Context, path string, body map[string]string, gateway bool, clients ...string) (Player, error) {
	status, raw, err := c.do(ctx, http.MethodPost, path, body, gateway, clients...)
	if err != nil {
		return Player{}, err
	}
	if status != http.StatusOK {
		return Player{}, apiError(status, raw)
	}
	var p Player
	if err := json.Unmarshal(raw, &p); err != nil {
		return Player{}, err
	}
	if p.Token == "" {
		return Player{}, fmt.Errorf("后端没有返回令牌")
	}
	return p, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, gateway bool, clients ...string) (int, []byte, error) {
	authorization := ""
	if gateway {
		if c.gatewayToken == "" {
			return 0, nil, fmt.Errorf("没有配置网关凭证")
		}
		authorization = c.gatewayToken
	}
	status, raw, err := c.doAuthorized(ctx, method, path, body, authorization, clients...)
	if err != nil {
		return status, raw, err
	}
	return status, raw, nil
}

func (c *Client) doAuthorized(ctx context.Context, method, path string, body any, authorization string, clients ...string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "TTYFish-SSH-Gateway")
	if path == "/api/v1/auth/password" && c.gatewayToken != "" {
		req.Header.Set("X-TTYFish-Gateway-Token", c.gatewayToken)
	}
	if len(clients) > 0 {
		host, _, err := net.SplitHostPort(clients[0])
		if err != nil {
			host = clients[0]
		}
		if ip := net.ParseIP(host); ip != nil {
			req.Header.Set("X-TTYFish-Client-IP", ip.String())
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", "Bearer "+authorization)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连不上后端：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || seconds < 1 {
			seconds = 60
		}
		return resp.StatusCode, raw, &RateLimitError{RetryAfterSeconds: seconds}
	}
	return resp.StatusCode, raw, nil
}

type RateLimitError struct{ RetryAfterSeconds int }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("尝试太频繁，请在 %d 秒后重试", e.RetryAfterSeconds)
}

type apiErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// apiError 把后端的错误转成给终端显示的一句话。
func apiError(status int, raw []byte) error {
	var e apiErrorBody
	if err := json.Unmarshal(raw, &e); err == nil && e.Error.Code != "" {
		switch e.Error.Code {
		case "invalid_credentials":
			return fmt.Errorf("用户名、密码或密钥不匹配")
		case "gateway_unauthorized":
			return fmt.Errorf("网关凭证无效，检查 -gateway-token")
		case "gateway_disabled":
			return fmt.Errorf("后端没有启用网关登录")
		case "rate_limited":
			return fmt.Errorf("尝试太频繁了，等一会儿再试")
		}
		msg := strings.TrimPrefix(e.Error.Message, e.Error.Code+": ")
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("后端返回 HTTP %d", status)
}

// Game 是后端游戏列表里的一项。
type Game struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MinPlayers      int    `json:"min_players"`
	MaxPlayers      int    `json:"max_players"`
	ProtocolVersion int    `json:"protocol_version"`
}

// Games 取后端开放的游戏列表。网关拿它和本地挂载的 UI 取交集。
func (c *Client) Games(ctx context.Context, token string) ([]Game, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/games", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上后端：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, raw)
	}
	var body struct {
		Items []Game `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body.Items, nil
}
