// Package agent 是"让模型来做决定"的通用接入层。
//
// 它不认任何游戏：游戏把当前局面翻译成一段文字，再给出一组编好号的候选动作，
// 这里只负责问模型"选第几个"。模型的答案永远要经过游戏自己的合法性校验，
// 一旦超时、报错或者答非所问，调用方就该退回原本的启发式——AI 只做增强，
// 不能成为单点故障。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Config 是 OpenAI 兼容接口的连接参数。
// 任何兼容 /chat/completions 的服务都能用（官方、各类本地推理服务、网关都行）。
type Config struct {
	BaseURL   string        // 例如 https://api.openai.com/v1，不带结尾斜杠
	APIKey    string        // 本地服务可以留空
	Model     string        // 例如 gpt-4o-mini / qwen2.5:7b
	Timeout   time.Duration // 单次请求上限，默认 1.5s
	MaxTokens int           // 默认 16，只需要一个数字
}

// Client 是一个很薄的 chat/completions 客户端。
type Client struct {
	cfg Config
	hc  *http.Client
}

func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 1500 * time.Millisecond
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 16
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}
}

// Enabled 表示配置看起来是完整的。没配齐就当没开 AI。
func (c *Client) Enabled() bool { return c != nil && c.cfg.BaseURL != "" && c.cfg.Model != "" }

// Choose 让模型从 1..n 里选一个编号。返回 -1 表示没选出来。
//
// 注意这里**不重试**：AI 只是增强，超时就该退回启发式，
// 重试只会把一次卡顿变成两次。
func (c *Client) Choose(ctx context.Context, system, user string, n int) (int, error) {
	if !c.Enabled() {
		return -1, fmt.Errorf("agent: 未配置")
	}
	if n <= 0 {
		return -1, fmt.Errorf("agent: 没有候选动作")
	}

	body := map[string]any{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":  c.cfg.MaxTokens,
		"temperature": 0,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return -1, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return -1, fmt.Errorf("agent: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return -1, err
	}
	if len(out.Choices) == 0 {
		return -1, fmt.Errorf("agent: 空回复")
	}

	pick := firstNumber(out.Choices[0].Message.Content)
	if pick < 1 || pick > n {
		return -1, fmt.Errorf("agent: 回复里没有合法编号 %q", out.Choices[0].Message.Content)
	}
	return pick, nil
}

// firstNumber 从模型回复里抠出第一个 1~99 的整数。
// 模型经常回"我选择 3。"或者"3"，两种都要认。
func firstNumber(s string) int {
	start := -1
	for i, r := range s {
		if r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if v, err := strconv.Atoi(s[start:i]); err == nil && v <= 99 {
				return v
			}
			start = -1
		}
	}
	if start >= 0 {
		if v, err := strconv.Atoi(s[start:]); err == nil && v <= 99 {
			return v
		}
	}
	return -1
}

// Available 按环境变量构造客户端，没配置就返回 nil。
func Available(cfg Config) *Client {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil
	}
	c := New(cfg)
	if !c.Enabled() {
		return nil
	}
	return c
}
