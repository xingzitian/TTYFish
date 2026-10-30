// Package remote 是"房间 API 的客户端"：SSH 网关用它把后端当成房间服务器。
//
// 它实现 sdk/client.Client，所以游戏界面完全不用改——界面以为自己在跟一个
// 本地房间打交道，实际上每次读写都走 HTTP + SSE。
package remote

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/games/uno/protocol"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// Client 是一个玩家在某个后端会话里的房间客户端。
type Client struct {
	base  string
	token string
	game  string
	code  string
	http  *http.Client

	mu     sync.Mutex
	snap   client.Snapshot
	rev    uint64
	ch     chan client.Snapshot
	closed bool

	stop chan struct{}
}

// 后端快照的线格式。字段名和后端 internal/play 的 Snapshot 一致。
type wireSnapshot struct {
	ProtocolVersion int                    `json:"protocol_version"`
	GameID          string                 `json:"game_id"`
	Code            string                 `json:"code"`
	Revision        uint64                 `json:"revision"`
	Phase           string                 `json:"phase"`
	Players         []client.Player        `json:"players"`
	MySeat          int                    `json:"my_seat"`
	MaxSeats        int                    `json:"max_seats"`
	IsHost          bool                   `json:"is_host"`
	View            json.RawMessage        `json:"view"`
	Timeline        []client.TimelineEntry `json:"timeline"`
	Vote            *client.Vote           `json:"vote"`
	Notice          string                 `json:"notice"`
	PendingLeftMS   int64                  `json:"pending_left_ms"`
	Kicked          bool                   `json:"kicked"`
}

// New 建一个客户端。base 是后端地址，例如 http://127.0.0.1:8081。
func New(base, token, gameID string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		game:  gameID,
		http:  &http.Client{Timeout: 15 * time.Second},
		ch:    make(chan client.Snapshot, 4),
		stop:  make(chan struct{}),
	}
}

// ---- sdk/client.Client ----

func (c *Client) Snapshot() client.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *Client) Updates() <-chan client.Snapshot { return c.ch }

// Join 进房间：code 为空表示新建一间。
func (c *Client) Join(code string) error {
	var (
		snap wireSnapshot
		err  error
	)
	if code == "" {
		snap, err = c.doCreateRoom()
	} else {
		snap, err = c.doRoomAction("POST", "/api/v1/rooms/"+strings.ToUpper(code)+"/join", nil)
	}
	if err != nil {
		return err
	}
	c.apply(snap)
	c.Watch()
	return nil
}

func (c *Client) Leave() error {
	c.stopWatch()
	code := c.currentCode()
	if code == "" {
		return nil
	}
	_, err := c.doRoomAction("POST", "/api/v1/rooms/"+code+"/leave", nil)
	c.mu.Lock()
	c.code = ""
	c.mu.Unlock()
	return err
}

func (c *Client) Start() error {
	_, err := c.doRoomAction("POST", "/api/v1/rooms/"+c.mustCode()+"/start", nil)
	return err
}

func (c *Client) AddBot() error {
	_, err := c.doRoomAction("POST", "/api/v1/rooms/"+c.mustCode()+"/bots", nil)
	return err
}

// Act 把游戏动作编成线格式，带最新 revision 提交。
func (c *Client) Act(action any) error {
	code := c.currentCode()
	if code == "" {
		return fmt.Errorf("还没进房间")
	}
	a, ok := action.(engine.Action)
	if !ok {
		return fmt.Errorf("不支持的动作类型 %T", action)
	}
	wire := protocol.Codec{}.EncodeAction(a)
	body := map[string]any{
		"expected_revision": c.Revision(),
		"type":              wire.Type,
		"payload":           wire.Payload,
	}
	snap, err := c.doRoomAction("POST", "/api/v1/rooms/"+code+"/actions", body)
	if err != nil {
		return err
	}
	// 动作成功会带回新快照；SSE 也会推一帧，版本号保证不会重复处理。
	c.apply(snap)
	return nil
}

func (c *Client) Chat(preset int) error {
	_, err := c.doRoomAction("POST", "/api/v1/rooms/"+c.mustCode()+"/chat",
		map[string]any{"preset": preset})
	return err
}

func (c *Client) VoteRestart(agree bool) error {
	_, err := c.doRoomAction("POST", "/api/v1/rooms/"+c.mustCode()+"/restart-votes",
		map[string]any{"agree": agree})
	return err
}

// ---- 额外能力（网关用） ----

// Resume 恢复当前账号所在房间；没有房间时返回 false。
func (c *Client) Resume() (bool, error) {
	status, body, err := c.do("GET", "/api/v1/me/room", nil)
	if err != nil {
		return false, err
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status != http.StatusOK {
		return false, apiError(status, body)
	}
	var snap wireSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return false, err
	}
	c.apply(snap)
	c.Watch()
	return true, nil
}

// Revision 是本地看到的最新房间版本。
func (c *Client) Revision() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rev
}

// Close 只断开推送，不离开房间——SSH 断开不该把座位丢掉。
func (c *Client) Close() {
	c.stopWatch()
}

// Watch 建立 SSE 长连接，把后续快照推进 Updates()。可重复调用，只保留一条。
func (c *Client) Watch() {
	c.stopWatch()
	code := c.currentCode()
	if code == "" {
		return
	}
	stop := make(chan struct{})
	c.mu.Lock()
	c.stop = stop
	c.closed = false
	c.mu.Unlock()

	go c.stream(code, stop)
}

func (c *Client) stopWatch() {
	c.mu.Lock()
	stop := c.stop
	c.stop = nil
	c.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// stream 解析 SSE：累积到空行才算一帧。
func (c *Client) stream(code string, stop chan struct{}) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/rooms/"+code+"/events", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	reader := bufio.NewReader(resp.Body)
	var event string
	var data strings.Builder

	for {
		select {
		case <-stop:
			return
		default:
		}

		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case line == "":
			if data.Len() > 0 {
				c.handleEvent(event, data.String())
			}
			event = ""
			data.Reset()
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}

		if readErr == io.EOF {
			c.closeChannel()
			return
		}
	}
}

func (c *Client) handleEvent(event, data string) {
	switch event {
	case "snapshot", "kicked":
		var snap wireSnapshot
		if err := json.Unmarshal([]byte(data), &snap); err != nil {
			return
		}
		c.apply(snap)
	case "auth_expired":
		c.closeChannel()
	}
}

func (c *Client) closeChannel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
}

// apply 把线格式转成界面要的快照并推给界面；版本旧于已处理过的就丢掉。
func (c *Client) apply(w wireSnapshot) {
	c.mu.Lock()
	if w.Revision < c.rev {
		c.mu.Unlock()
		return
	}
	c.rev = w.Revision
	c.code = w.Code

	snap := client.Snapshot{
		Code:          w.Code,
		Phase:         phaseOf(w.Phase),
		Players:       w.Players,
		MySeat:        w.MySeat,
		MaxSeats:      w.MaxSeats,
		IsHost:        w.IsHost,
		Timeline:      w.Timeline,
		Vote:          w.Vote,
		Notice:        w.Notice,
		PendingLeftMS: w.PendingLeftMS,
		Kicked:        w.Kicked,
		View:          decodeView(w.View, w.MySeat),
	}
	c.snap = snap
	closed := c.closed
	ch := c.ch
	c.mu.Unlock()

	if closed {
		return
	}
	// 界面读得慢就丢中间帧，保留最新状态——和 SSE 的语义一致
	select {
	case ch <- snap:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- snap:
		default:
		}
	}
}

func phaseOf(s string) client.Phase {
	switch s {
	case "playing":
		return client.Playing
	case "over":
		return client.Over
	default:
		return client.Lobby
	}
}

// decodeView 把协议里的视图还原成 UNO 的 Observation，包括合法动作。
func decodeView(raw json.RawMessage, seat int) any {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var payload struct {
		engine.Observation
		Legal []protocol.Action `json:"legal"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	obs := payload.Observation
	obs.Legal = nil
	codec := protocol.Codec{}
	for _, a := range payload.Legal {
		rawPayload, err := json.Marshal(a.Payload)
		if err != nil {
			continue
		}
		act, err := codec.DecodeAction(a.Type, rawPayload, seat)
		if err != nil {
			continue
		}
		if ea, ok := act.(engine.Action); ok {
			obs.Legal = append(obs.Legal, ea)
		}
	}
	return obs
}

// ---- HTTP ----

func (c *Client) currentCode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code
}

func (c *Client) mustCode() string { return c.currentCode() }

func (c *Client) doCreateRoom() (wireSnapshot, error) {
	body := map[string]any{"game_id": c.game}
	status, raw, err := c.do("POST", "/api/v1/rooms", body)
	if err != nil {
		return wireSnapshot{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return wireSnapshot{}, apiError(status, raw)
	}
	var snap wireSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return wireSnapshot{}, err
	}
	return snap, nil
}

func (c *Client) doRoomAction(method, path string, body any) (wireSnapshot, error) {
	status, raw, err := c.do(method, path, body)
	if err != nil {
		return wireSnapshot{}, err
	}
	if status != http.StatusOK {
		return wireSnapshot{}, apiError(status, raw)
	}
	var snap wireSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return wireSnapshot{}, err
	}
	return snap, nil
}

func (c *Client) do(method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连不上后端：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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

func apiError(status int, raw []byte) error {
	var e apiErrorBody
	if err := json.Unmarshal(raw, &e); err == nil && e.Error.Message != "" {
		msg := strings.TrimPrefix(e.Error.Message, e.Error.Code+": ")
		if e.Error.Code == "stale_state" {
			return fmt.Errorf("状态已经变了，重新看一眼再操作")
		}
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("后端返回 HTTP %d", status)
}
