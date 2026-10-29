// Package session 把"一个玩家的一条连接"接到房间上，并实现 sdk 的 Client。
//
// 游戏的界面只看到 Client 接口；房间、订阅、断线托管都在这一层处理。
package session

import (
	"fmt"
	"sync"

	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// Client 是一个连接在平台侧的代理。
type Client struct {
	reg  *room.Registry
	fp   string
	name string

	mu     sync.Mutex
	rm     *room.Room
	sub    <-chan client.Snapshot
	cancel func()
	snap   client.Snapshot
}

func New(reg *room.Registry, fp, name string) *Client {
	return &Client{reg: reg, fp: fp, name: name}
}

// Resume 如果这个身份还在某个房间里，直接接上去（断线重连）。
func (c *Client) Resume() bool {
	rm := c.reg.FindRoom(c.fp)
	if rm == nil {
		return false
	}
	if _, err := rm.Join(c.fp, c.name); err != nil {
		return false
	}
	return c.attach(rm)
}

func (c *Client) attach(rm *room.Room) bool {
	sub, cancel, err := rm.Subscribe(c.fp)
	if err != nil {
		return false
	}
	c.mu.Lock()
	c.rm, c.sub, c.cancel = rm, sub, cancel
	c.mu.Unlock()
	// 订阅会立刻推一帧，先取走，保证第一次渲染就有内容。
	if s, ok := <-sub; ok {
		c.mu.Lock()
		c.snap = s
		c.mu.Unlock()
	}
	return true
}

func (c *Client) Join(code string) error {
	rm, _, err := c.reg.Join(code, c.fp, c.name)
	if err != nil {
		return err
	}
	if !c.attach(rm) {
		return fmt.Errorf("进房间失败")
	}
	return nil
}

func (c *Client) Leave() error {
	c.mu.Lock()
	rm, cancel := c.rm, c.cancel
	c.rm, c.cancel, c.sub = nil, nil, nil
	c.snap = client.Snapshot{}
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if rm != nil {
		rm.Disconnect(c.fp)
	}
	return nil
}

func (c *Client) Snapshot() client.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *Client) Updates() <-chan client.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sub == nil {
		return nil
	}
	return c.sub
}

func (c *Client) Start() error {
	rm, err := c.room()
	if err != nil {
		return err
	}
	return rm.Start(c.fp)
}

func (c *Client) AddBot() error {
	rm, err := c.room()
	if err != nil {
		return err
	}
	_, err = rm.AddBot("")
	return err
}

func (c *Client) Act(a any) error {
	rm, err := c.room()
	if err != nil {
		return err
	}
	return rm.Act(c.fp, a)
}

func (c *Client) Chat(i int) error {
	rm, err := c.room()
	if err != nil {
		return err
	}
	return rm.Chat(c.fp, i)
}

func (c *Client) VoteRestart(agree bool) error {
	rm, err := c.room()
	if err != nil {
		return err
	}
	return rm.VoteRestart(c.fp, agree)
}

func (c *Client) room() (*room.Room, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rm == nil {
		return nil, fmt.Errorf("还没进房间")
	}
	return c.rm, nil
}
