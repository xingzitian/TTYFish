package room

import (
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// obs 取出快照里 UNO 的观察结果（测试专用：平台不解释 View 的内容）。
func obs(snap Snapshot) engine.Observation {
	v, _ := snap.View.(engine.Observation)
	return v
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestRoom(t *testing.T) (*Room, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	r := New("ABC234", uno.New(engine.Rules{}), Options{
		TurnTimeout: time.Minute,
		Grace:       time.Minute,
		BotLag:      time.Second,
		Now:         clk.now,
		NoLoop:      true,
	})
	t.Cleanup(r.Close)
	return r, clk
}

func mustJoin(t *testing.T, r *Room, fp, name string) int {
	t.Helper()
	seat, err := r.Join(fp, name)
	if err != nil {
		t.Fatalf("Join(%s) 失败: %v", name, err)
	}
	return seat
}

func mustStart(t *testing.T, r *Room, hostFP string) {
	t.Helper()
	if err := r.Start(hostFP); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
}

func TestJoinSeatsAndHost(t *testing.T) {
	r, _ := newTestRoom(t)

	if seat := mustJoin(t, r, "fp-a", "阿甲"); seat != 0 {
		t.Fatalf("第一个人应该坐 0 号位，实际 %d", seat)
	}
	if seat := mustJoin(t, r, "fp-b", "阿乙"); seat != 1 {
		t.Fatalf("第二个人应该坐 1 号位，实际 %d", seat)
	}

	snap, err := r.SnapshotFor("fp-a")
	if err != nil {
		t.Fatalf("取快照失败: %v", err)
	}
	if !snap.IsHost {
		t.Fatal("第一个人应该是房主")
	}
	if snap.Phase != Lobby {
		t.Fatalf("还没开局应该是 Lobby，实际 %v", snap.Phase)
	}
	if len(snap.Players) != 2 {
		t.Fatalf("应该有两个人，实际 %d", len(snap.Players))
	}

	snapB, _ := r.SnapshotFor("fp-b")
	if snapB.IsHost {
		t.Fatal("第二个人不该是房主")
	}
	if snapB.MySeat != 1 {
		t.Fatalf("第二个人座位应该是 1，实际 %d", snapB.MySeat)
	}
}

func TestStartRules(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")

	if err := r.Start("fp-a"); err == nil {
		t.Fatal("一个人开局应该被拒")
	}

	mustJoin(t, r, "fp-b", "阿乙")
	if err := r.Start("fp-b"); err != ErrNotHost {
		t.Fatalf("非房主开局应该被拒，实际 %v", err)
	}

	mustStart(t, r, "fp-a")
	snap, _ := r.SnapshotFor("fp-a")
	if snap.Phase != Playing {
		t.Fatalf("开局后应该是 Playing，实际 %v", snap.Phase)
	}
}

func TestNoJoinAfterStart(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	if _, err := r.Join("fp-c", "阿丙"); err != ErrStarted {
		t.Fatalf("开局后不许新人加入，实际 %v", err)
	}
}

func TestSeatSpoofRejected(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	// 甲自称自己是 1 号位，必须被拒——座位以房间记录为准。
	err := r.Act("fp-a", engine.DrawCard{Seat: 1})
	if err != ErrSeat {
		t.Fatalf("冒用别人座位应该被拒，实际 %v", err)
	}
}

func TestSnapshotOnlyShowsOwnHand(t *testing.T) {
	r, _ := newTestRoom(t)
	for _, fp := range []string{"fp-a", "fp-b", "fp-c", "fp-d"} {
		mustJoin(t, r, fp, "玩家"+fp)
	}
	mustStart(t, r, "fp-a")

	snap, _ := r.SnapshotFor("fp-b")
	if len(obs(snap).Hand) != 7 {
		t.Fatalf("应该只看到自己 7 张手牌，实际 %d", len(obs(snap).Hand))
	}
	if len(obs(snap).HandCounts) != 4 {
		t.Fatalf("四个人应该有四条张数记录，实际 %d", len(obs(snap).HandCounts))
	}
	for i, n := range obs(snap).HandCounts {
		if n != 7 {
			t.Fatalf("座位 %d 应该有 7 张，实际 %d", i, n)
		}
	}
}

func TestDisconnectBotTakesOver(t *testing.T) {
	r, clk := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	snap, _ := r.SnapshotFor("fp-a")
	turn := obs(snap).Turn
	who := []string{"fp-a", "fp-b"}[turn]
	before := len(obs(snap).Log)

	r.Disconnect(who)

	// 还没到电脑的思考时间，不该动。
	r.tick()
	mid, _ := r.SnapshotFor("fp-a")
	if len(obs(mid).Log) != before {
		t.Fatal("电脑还没到思考时间就出手了")
	}

	clk.add(2 * time.Second)
	r.tick()

	after, _ := r.SnapshotFor("fp-a")
	if len(obs(after).Log) == before {
		t.Fatal("掉线玩家的回合应该由电脑接手，但没有任何动作")
	}

	// 座位还在，只是没人操作。
	found := false
	for _, p := range after.Players {
		if p.Seat == turn {
			found = true
			if p.Connected {
				t.Fatal("掉线的人不该显示为在线")
			}
			if !p.Bot {
				t.Fatal("掉线的人应该标记为电脑代打")
			}
		}
	}
	if !found {
		t.Fatal("掉线不应该丢掉座位")
	}
}

func TestReconnectKeepsSeat(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	r.Disconnect("fp-b")

	seat, err := r.Join("fp-b", "阿乙")
	if err != nil {
		t.Fatalf("重连失败: %v", err)
	}
	if seat != 1 {
		t.Fatalf("重连应该回到 1 号位，实际 %d", seat)
	}

	snap, _ := r.SnapshotFor("fp-b")
	for _, p := range snap.Players {
		if p.Seat == 1 {
			if !p.Connected {
				t.Fatal("重连后应该显示在线")
			}
			if p.Bot {
				t.Fatal("重连后不该继续由电脑代打")
			}
		}
	}
}

func TestVoteRestart(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustJoin(t, r, "fp-c", "阿丙")
	mustStart(t, r, "fp-a")

	// 丙掉线，剩下两个人。
	r.Disconnect("fp-c")

	if err := r.VoteRestart("fp-a", true); err != nil {
		t.Fatalf("发起投票失败: %v", err)
	}
	snap, _ := r.SnapshotFor("fp-a")
	if snap.Vote == nil {
		t.Fatal("应该有一个进行中的投票")
	}
	if snap.Vote.Need != 2 {
		t.Fatalf("两个在线玩家，需要 2 票，实际 %d", snap.Vote.Need)
	}

	if err := r.VoteRestart("fp-a", true); err == nil {
		t.Fatal("同一个人不能投两次")
	}

	if err := r.VoteRestart("fp-b", true); err != nil {
		t.Fatalf("第二票失败: %v", err)
	}

	after, _ := r.SnapshotFor("fp-a")
	if after.Phase != Lobby {
		t.Fatalf("投票通过后应该回到 Lobby，实际 %v", after.Phase)
	}
}

func TestVoteRejectedByMajority(t *testing.T) {
	r, _ := newTestRoom(t)
	for _, fp := range []string{"fp-a", "fp-b", "fp-c", "fp-d"} {
		mustJoin(t, r, fp, "玩家"+fp)
	}
	mustStart(t, r, "fp-a")
	r.Disconnect("fp-d")

	// 三个人在线，需要 2 票通过；先投两张反对就直接作废。
	if err := r.VoteRestart("fp-a", false); err != nil {
		t.Fatalf("投票失败: %v", err)
	}
	if err := r.VoteRestart("fp-b", false); err != nil {
		t.Fatalf("投票失败: %v", err)
	}

	snap, _ := r.SnapshotFor("fp-a")
	if snap.Phase != Playing {
		t.Fatalf("反对票够多应该继续打，实际阶段 %v", snap.Phase)
	}
	if snap.Vote != nil {
		t.Fatal("投票应该已经作废")
	}
}

func TestPresetChat(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")

	if err := r.Chat("fp-a", 0); err != nil {
		t.Fatalf("发快捷语失败: %v", err)
	}
	snap, _ := r.SnapshotFor("fp-b")

	// 聊天现在进时间线，和出牌记录混在一起，这样宽屏面板里也看得见。
	var chats []TimelineEntry
	for _, e := range snap.Timeline {
		if e.Chat {
			chats = append(chats, e)
		}
	}
	if len(chats) != 1 {
		t.Fatalf("时间线里应该正好有一条聊天，实际 %d", len(chats))
	}
	if chats[0].Seat != 0 || chats[0].Text != Presets[0] {
		t.Fatalf("快捷语内容不对: %+v", chats[0])
	}

	if err := r.Chat("fp-a", -1); err == nil {
		t.Fatal("非法索引应该被拒")
	}
	if err := r.Chat("fp-a", len(Presets)); err == nil {
		t.Fatal("越界索引应该被拒")
	}
	if err := r.Chat("fp-陌生人", 0); err != ErrNotIn {
		t.Fatalf("不在房间里的人不能发言，实际 %v", err)
	}
}

func TestLobbyDisconnectRemovesSeat(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustJoin(t, r, "fp-c", "阿丙")

	// 大厅里房主走了，房主应该转给下一个人，座位重新编号。
	r.Disconnect("fp-a")

	snap, err := r.SnapshotFor("fp-b")
	if err != nil {
		t.Fatalf("取快照失败: %v", err)
	}
	if len(snap.Players) != 2 {
		t.Fatalf("大厅里离开应该就地移除，实际剩 %d 人", len(snap.Players))
	}
	if !snap.IsHost {
		t.Fatal("房主离开后应该由下一个人接手")
	}
	if snap.MySeat != 0 {
		t.Fatalf("重新编号后应该坐 0 号位，实际 %d", snap.MySeat)
	}
}

func TestRegistryJoinByCode(t *testing.T) {
	reg := NewRegistry(uno.New(engine.Rules{}), Options{NoLoop: true, Now: time.Now})

	rm, seat, err := reg.Join("", "fp-a", "阿甲")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}
	if seat != 0 {
		t.Fatalf("新房第一个人应该坐 0 号位，实际 %d", seat)
	}
	if !ValidCode(rm.Code) {
		t.Fatalf("自动生成的房间码不合法: %q", rm.Code)
	}
	t.Cleanup(rm.Close)

	rm2, seat2, err := reg.Join(rm.Code, "fp-b", "阿乙")
	if err != nil {
		t.Fatalf("按码加入失败: %v", err)
	}
	if rm2 != rm {
		t.Fatal("同一个码应该进同一个房间")
	}
	if seat2 != 1 {
		t.Fatalf("第二个人应该坐 1 号位，实际 %d", seat2)
	}
}

func TestRegistryRejectsBadCode(t *testing.T) {
	reg := NewRegistry(uno.New(engine.Rules{}), Options{NoLoop: true, Now: time.Now})
	if _, _, err := reg.Join("XYZ", "fp-a", "阿甲"); err != ErrCode {
		t.Fatalf("三位房间码应该被拒，实际 %v", err)
	}
	// 容易混淆的字符不在字母表里。
	if _, _, err := reg.Join("ABC01I", "fp-a", "阿甲"); err != ErrCode {
		t.Fatalf("含 0/1/I 的房间码应该被拒，实际 %v", err)
	}
}

func TestRegistrySweepEmptyRooms(t *testing.T) {
	reg := NewRegistry(uno.New(engine.Rules{}), Options{NoLoop: true, Now: time.Now})
	rm, _, err := reg.Join("", "fp-a", "阿甲")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}
	rm.Disconnect("fp-a") // 大厅里掉线会被直接移除
	if n := reg.Sweep(); n != 1 {
		t.Fatalf("应该回收 1 个空房间，实际 %d", n)
	}
	if _, ok := reg.Get(rm.Code); ok {
		t.Fatal("空房间应该已经从注册表里删掉")
	}
}

// TestSubscribeLastWins 验证"同一个座位被重复订阅"时让新连接接管，
// 而不是把新来的人挡在门外。旧连接必须被关掉，否则座位会被它一直占着。
func TestSubscribeLastWins(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")

	ch1, cancel1, err := r.Subscribe("fp-a")
	if err != nil {
		t.Fatalf("首次订阅失败: %v", err)
	}
	defer cancel1()

	ch2, cancel2, err := r.Subscribe("fp-a")
	if err != nil {
		t.Fatalf("第二台设备应该能接管座位，实际 %v", err)
	}
	defer cancel2()

	deadline := time.After(2 * time.Second)
	for closed := false; !closed; {
		select {
		case _, ok := <-ch1:
			closed = !ok
		case <-deadline:
			t.Fatal("旧连接没有被关闭，座位会一直被它占着")
		}
	}

	// 新连接必须还能收到更新。
	if err := r.Chat("fp-a", 0); err != nil {
		t.Fatalf("发快捷语失败: %v", err)
	}
	drain := time.After(2 * time.Second)
	for {
		select {
		case snap, ok := <-ch2:
			if !ok {
				t.Fatal("新连接被意外关闭")
			}
			hasChat := false
			for _, e := range snap.Timeline {
				if e.Chat {
					hasChat = true
				}
			}
			if hasChat {
				return
			}
		case <-drain:
			t.Fatal("新连接收不到房间更新")
		}
	}
}

func TestNormalizeAndValidCode(t *testing.T) {
	if got := NormalizeCode(" ab-c 234 "); got != "ABC234" {
		t.Fatalf("归一化结果不对: %q", got)
	}
	if !ValidCode("ABC234") {
		t.Fatal("合法房间码被误判")
	}
	if ValidCode("abc234") {
		t.Fatal("小写应该先归一化再判断")
	}
}
