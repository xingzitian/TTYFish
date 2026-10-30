// Package backend 是账号 API 的客户端，供 SSH 网关使用。
//
// 网关自己不存账号、不查库：它只负责证明"这条 SSH 连接背后是谁"，
// 然后拿后端发的玩家令牌去操作房间。
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Account 是后端返回的账号信息。
type Account struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
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
func (c *Client) LoginKey(ctx context.Context, username, publicKey string) (Player, error) {
	body := map[string]string{"username": username, "public_key": publicKey}
	return c.playerCall(ctx, "/api/v1/internal/auth/ssh", body, true)
}

// CheckKey checks key ownership without issuing a token or recording key usage.
func (c *Client) CheckKey(ctx context.Context, username, publicKey string) (Account, error) {
	status, raw, err := c.do(ctx, http.MethodPost, "/api/v1/internal/auth/ssh", map[string]any{"username": username, "public_key": publicKey, "verify_only": true}, true)
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
func (c *Client) LoginPassword(ctx context.Context, username, password string) (Player, error) {
	body := map[string]string{"username": username, "password": password}
	return c.playerCall(ctx, "/api/v1/auth/password", body, false)
}

// ConfirmChallenge 确认网页登录用的一次性码。
func (c *Client) ConfirmChallenge(ctx context.Context, username, publicKey, code string) error {
	body := map[string]string{
		"username":   username,
		"public_key": publicKey,
		"code":       code,
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/api/v1/internal/auth/ssh/challenges/confirm", body, true)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return apiError(status, raw)
	}
	return nil
}

func (c *Client) playerCall(ctx context.Context, path string, body map[string]string, gateway bool) (Player, error) {
	status, raw, err := c.do(ctx, http.MethodPost, path, body, gateway)
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

func (c *Client) do(ctx context.Context, method, path string, body any, gateway bool) (int, []byte, error) {
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if gateway {
		if c.gatewayToken == "" {
			return 0, nil, fmt.Errorf("没有配置网关凭证")
		}
		req.Header.Set("Authorization", "Bearer "+c.gatewayToken)
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
	return resp.StatusCode, raw, nil
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
