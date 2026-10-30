package room

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/game"
)

const (
	codeLen     = 6
	maxTimeline = 60
	codeChars   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉 I/O/0/1 这些易混字符
)

var (
	ErrNotFound = errors.New("房间不存在")
	ErrFull     = errors.New("房间满了")
	ErrStarted  = errors.New("这一局已经开始了，等打完再进（房主按 s 开下一局）")
	ErrNotIn    = errors.New("你不在这个房间里")
	ErrNotHost  = errors.New("只有房主能开始")
	ErrPhase    = errors.New("现在不能做这个操作")
	ErrCode     = errors.New("房间码是 6 位字母数字，且不含 0/O/1/I 这些易混字符")
	ErrSeat     = errors.New("这个动作不是你这个座位发的")
	ErrStale    = errors.New("房间状态已更新，请获取最新快照后重试")
)

// 平台对外的一帧、阶段、玩家信息都来自 sdk/client——
// 这样游戏的界面只依赖 SDK，不依赖这个包。
type (
	Phase         = client.Phase
	Player        = client.Player
	Vote          = client.Vote
	TimelineEntry = client.TimelineEntry
	Snapshot      = client.Snapshot
)

const (
	Lobby   = client.Lobby
	Playing = client.Playing
	Over    = client.Over
)

// Presets 是预设快捷语。只做固定短语，不做自由输入——
// 自由文本要处理 ANSI 注入（别人能往你终端里塞控制序列），这个先不碰。
var Presets = client.Presets

// Options 是房间的时序参数。
type Options struct {
	TurnTimeout time.Duration // 一个真人想多久，超时自动走保底动作
	Grace       time.Duration // 掉线后多久算"彻底托管"
	BotLag      time.Duration // 电脑每步思考多久
	Now         func() time.Time

	// NoLoop 关掉后台时钟，测试里手动调 tick() 用。
	NoLoop bool
}

// DefaultOptions 是默认时序。60 秒掉线宽限是"去倒杯水还能回来"的量级；
// 电脑每步 800ms——再快的话，一桌电脑连着走，人根本看不清谁出了什么。
func DefaultOptions() Options {
	return Options{
		TurnTimeout: 45 * time.Second,
		Grace:       60 * time.Second,
		BotLag:      800 * time.Millisecond,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.TurnTimeout <= 0 {
		o.TurnTimeout = d.TurnTimeout
	}
	if o.Grace <= 0 {
		o.Grace = d.Grace
	}
	if o.BotLag <= 0 {
		o.BotLag = d.BotLag
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

type seat struct {
	seat      int
	fp        string
	name      string
	host      bool
	connected bool
	bot       bool
	offlineAt time.Time
	yes       bool // 这一轮投票投了赞成
	voted     bool
}

type voteState struct {
	kind   string
	endsAt time.Time
	need   int
}

// Room 是一间房。所有导出方法都是并发安全的。
//
// 房间不认识任何具体游戏：它只知道"有个 Game 能开局、能裁决、能裁剪视图"。
type Room struct {
	Code string
	Game game.Game

	mu          sync.Mutex
	opts        Options
	phase       Phase
	state       any
	seats       []*seat
	byFP        map[string]*seat
	subs        map[*seat]chan Snapshot
	turnAt      time.Time
	lastActive  time.Time
	abandonedAt time.Time
	startedAt   time.Time
	timeline    []TimelineEntry
	logSeq      int
	pendingID   string
	pendingEnd  time.Time
	pendingAct  any
	vote        *voteState
	notice      string
	noticeAt    time.Time
	closed      bool
	revision    uint64
	stop        chan struct{}
}

// New 建一间空房。
func New(code string, g game.Game, opts Options) *Room {
	r := &Room{
		Code:  code,
		Game:  g,
		opts:  opts.withDefaults(),
		phase: Lobby,
		byFP:  make(map[string]*seat),
		subs:  make(map[*seat]chan Snapshot),
		stop:  make(chan struct{}),
	}
	r.lastActive = r.opts.Now()
	if !r.opts.NoLoop {
		go r.loop()
	}
	return r
}

// Join 让一个身份进入房间，返回座位号。同一指纹再次进来就是"重连"。
func (r *Room) Join(fp, name string) (int, error) {
	if fp == "" {
		return 0, fmt.Errorf("缺少身份")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.byFP[fp]; ok {
		r.abandonedAt = time.Time{}
		s.connected = true
		s.bot = false
		s.offlineAt = time.Time{}
		if name != "" {
			s.name = name
		}
		r.setNotice(fmt.Sprintf("%s 回来了", s.name))
		r.broadcastLocked()
		return s.seat, nil
	}

	if r.phase != Lobby {
		return 0, ErrStarted
	}
	if len(r.seats) >= r.Game.MaxSeats() {
		return 0, ErrFull
	}
	if name == "" {
		name = fmt.Sprintf("P%d", len(r.seats)+1)
	}
	name = r.uniqueNameLocked(name)

	s := &seat{
		seat:      len(r.seats),
		fp:        fp,
		name:      name,
		host:      len(r.seats) == 0,
		connected: true,
	}
	r.seats = append(r.seats, s)
	r.byFP[fp] = s
	r.abandonedAt = time.Time{}
	r.setNotice(fmt.Sprintf("%s 加入", name))
	r.broadcastLocked()
	return s.seat, nil
}

// Disconnect 处理一条连接断开。座位保留，交给电脑代打。
func (r *Room) Disconnect(fp string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byFP[fp]
	if !ok {
		return
	}
	s.connected = false
	s.offlineAt = r.opts.Now()

	if r.phase == Lobby {
		// 还没开局就直接把人挪走，别留空座位。
		r.removeLocked(s)
	} else {
		s.bot = true
		r.setNotice(fmt.Sprintf("%s 掉线了，先由电脑代打", s.name))
	}
	humansOnline := false
	for _, player := range r.seats {
		if player.fp != "" && player.connected {
			humansOnline = true
			break
		}
	}
	if !humansOnline && r.abandonedAt.IsZero() {
		r.abandonedAt = r.opts.Now()
	}
	r.broadcastLocked()
}

// Start 由房主开局。
func (r *Room) Start(fp string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byFP[fp]
	if !ok {
		return ErrNotIn
	}
	if !s.host {
		return ErrNotHost
	}
	if r.phase != Lobby && r.phase != Over {
		return ErrPhase
	}
	if len(r.seats) < r.Game.MinSeats() {
		// 带上实际人数：现场最常遇到的是"三个人都在，但进的是不同的房间"。
		return fmt.Errorf("这个房间里只有 %d 个人，开局至少要 %d 个——确认大家输的是同一个房间码", len(r.seats), r.Game.MinSeats())
	}

	// 长局：上一局打完了但还没到目标分，接着开下一局，分数保留。
	if r.phase == Over && r.state != nil &&
		r.Game.Phase(r.state) == game.RoundOver && r.Game.MatchTarget() > 0 {
		r.nextRoundLocked()
		r.broadcastLocked()
		return nil
	}

	r.startLocked()
	r.broadcastLocked()
	return nil
}

// nextRoundLocked 在长局里开下一局：保留分数，只重置牌桌。
func (r *Room) nextRoundLocked() {
	if err := r.Game.NextRound(r.state); err != nil {
		return
	}
	r.phase = Playing
	r.turnAt = r.opts.Now()
	r.clearPendingLocked()
	r.vote = nil
	for _, s := range r.seats {
		s.yes, s.voted = false, false
	}
	r.absorbLogLocked()
	r.setNotice(fmt.Sprintf("下一局开始（先到 %d 分）", r.Game.MatchTarget()))
}

// AddBot 在大厅里补一个电脑玩家（单机模式用）。
func (r *Room) AddBot(name string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addBotLocked(name)
}

// AddBotBy enforces the host permission in the same lock as the mutation.
func (r *Room) AddBotBy(fp string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byFP[fp]
	if !ok {
		return 0, ErrNotIn
	}
	if !s.host {
		return 0, ErrNotHost
	}
	return r.addBotLocked("")
}

func (r *Room) addBotLocked(name string) (int, error) {

	if r.phase != Lobby {
		return 0, ErrPhase
	}
	if len(r.seats) >= r.Game.MaxSeats() {
		return 0, ErrFull
	}
	if name == "" {
		name = fmt.Sprintf("电脑%d", len(r.seats))
	}
	s := &seat{
		seat:      len(r.seats),
		name:      name,
		connected: true,
		bot:       true,
	}
	r.seats = append(r.seats, s)
	r.broadcastLocked()
	return s.seat, nil
}

func (r *Room) startLocked() {
	seed := r.opts.Now().UnixNano()
	seeds := make([]game.PlayerSeed, 0, len(r.seats))
	for _, s := range r.seats {
		seeds = append(seeds, game.PlayerSeed{Seat: s.seat, Name: s.name, Bot: s.bot})
	}
	r.state = r.Game.NewMatch(seeds, seed)
	r.phase = Playing
	r.turnAt = r.opts.Now()
	r.startedAt = r.turnAt
	r.timeline = nil
	r.logSeq = 0
	r.clearPendingLocked()
	r.vote = nil
	for _, s := range r.seats {
		s.yes, s.voted = false, false
	}
	r.absorbLogLocked()
	r.setNotice("开局！")
}

// absorbLogLocked 把游戏新增的战报搬到房间时间线上，顺便打时间戳。
func (r *Room) absorbLogLocked() {
	if r.state == nil {
		return
	}
	now := r.elapsedLocked()
	events, seq := r.Game.Events(r.state, r.logSeq)
	r.logSeq = seq
	for _, e := range events {
		r.timeline = append(r.timeline, TimelineEntry{
			AtMS:   now,
			Seat:   e.Seat,
			Detail: e.Detail,
			Text:   e.Text,
		})
	}
	r.trimTimelineLocked()
	r.syncPendingLocked()
}

// syncPendingLocked 维护"限时义务"的窗口。
//
// 游戏只负责报告"现在有什么义务、多长时限、超时后执行什么动作"
// （UNO 就是"剩一张牌 3 秒内要喊 UNO"），计时和判定都在平台这一侧，
// 游戏本身不碰时钟，测试里也就能手动推进时间。
func (r *Room) syncPendingLocked() {
	if r.state == nil {
		return
	}
	token, within, action := r.Game.Pending(r.state)
	if token == "" {
		r.clearPendingLocked()
		return
	}
	if token != r.pendingID {
		r.pendingID = token
		r.pendingEnd = r.opts.Now().Add(within)
		r.pendingAct = action
	}
}

func (r *Room) clearPendingLocked() {
	r.pendingID = ""
	r.pendingEnd = time.Time{}
	r.pendingAct = nil
}

// elapsedLocked 返回从开局算起的毫秒数；还没开局就是 0。
func (r *Room) elapsedLocked() int64 {
	if r.startedAt.IsZero() {
		return 0
	}
	return r.opts.Now().Sub(r.startedAt).Milliseconds()
}

func (r *Room) trimTimelineLocked() {
	if n := len(r.timeline); n > maxTimeline {
		r.timeline = r.timeline[n-maxTimeline:]
	}
}

// Act 执行一个玩家的动作。座位以房间记录为准，客户端自称的座位不作数。
func (r *Room) Act(fp string, a any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actLocked(fp, a)
}

func (r *Room) ActAtRevision(fp string, revision uint64, a any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revision != revision {
		return ErrStale
	}
	return r.actLocked(fp, a)
}

func (r *Room) actLocked(fp string, a any) error {

	s, ok := r.byFP[fp]
	if !ok {
		return ErrNotIn
	}
	if r.phase != Playing || r.state == nil {
		return ErrPhase
	}
	if r.Game.SeatOf(a) != s.seat {
		return ErrSeat
	}
	if err := r.Game.Apply(r.state, a); err != nil {
		return err
	}

	r.turnAt = r.opts.Now()
	r.afterStateLocked()
	r.absorbLogLocked()
	r.broadcastLocked()
	return nil
}

// Chat 发一条预设快捷语。它和出牌记录进同一条时间线，
// 这样宽屏下也能看见，不用在面板里再开一块。
func (r *Room) Chat(fp string, idx int) error {
	if idx < 0 || idx >= len(Presets) {
		return fmt.Errorf("没有这条快捷语")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byFP[fp]
	if !ok {
		return ErrNotIn
	}
	r.timeline = append(r.timeline, TimelineEntry{
		AtMS: r.elapsedLocked(),
		Seat: s.seat,
		Chat: true,
		Text: Presets[idx],
	})
	r.trimTimelineLocked()
	r.broadcastLocked()
	return nil
}

// VoteRestart 对"现在就重开"投票。agree=false 是明确反对。
func (r *Room) VoteRestart(fp string, agree bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byFP[fp]
	if !ok {
		return ErrNotIn
	}
	if r.phase != Playing && r.phase != Over {
		return ErrPhase
	}

	if r.vote == nil {
		r.vote = &voteState{
			kind:   "restart",
			endsAt: r.opts.Now().Add(30 * time.Second),
			need:   r.connectedCountLocked()/2 + 1,
		}
		for _, p := range r.seats {
			p.yes, p.voted = false, false
		}
	}
	if s.voted {
		return fmt.Errorf("你已经投过了")
	}
	s.voted = true
	s.yes = agree

	r.checkVoteLocked()
	r.broadcastLocked()
	return nil
}

func (r *Room) checkVoteLocked() {
	if r.vote == nil {
		return
	}
	yes, voted := 0, 0
	for _, s := range r.seats {
		if s.voted {
			voted++
			if s.yes {
				yes++
			}
		}
	}
	// 反对票一多就直接作废，别让人干等。
	if voted-yes >= r.vote.need {
		r.vote = nil
		r.setNotice("重开投票没通过")
		return
	}
	if yes >= r.vote.need {
		r.vote = nil
		r.phase = Lobby
		r.state = nil
		r.turnAt = time.Time{}
		for _, s := range r.seats {
			s.yes, s.voted = false, false
			if s.connected {
				s.bot = false
			}
		}
		r.setNotice("投票通过，重开房主可以再开一局")
	}
}

// Subscribe 订阅这个座位的快照流。返回的 cancel 必须在连接断开时调用。
//
// 同一个座位被重复订阅 = 同一个人在第二台设备登录，或者大家的密钥撞了。
// 这时候让**新连接接管**（last-wins），旧连接收到关闭信号后自己退出。
// 直接报错是不行的：报错只能把新来的人挡在门外，座位还占着，
// 现场表现就是"我怎么都进不去"。
func (r *Room) Subscribe(fp string) (<-chan Snapshot, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byFP[fp]
	if !ok {
		return nil, nil, ErrNotIn
	}
	if old, exists := r.subs[s]; exists {
		// 先摘掉再关，避免旧连接的 cancel 把新 channel 也关掉。
		delete(r.subs, s)
		// 给旧连接留一帧说明，别让它只看到"连接已断开"还得猜原因。
		kicked := r.snapshotLocked(s)
		kicked.Kicked = true
		kicked.Notice = "这个身份在别处登录了"
		select {
		case old <- kicked:
		default:
			select {
			case <-old:
			default:
			}
			old <- kicked
		}
		close(old)
		r.setNotice(fmt.Sprintf("%s 在别处登录，旧连接已断开", s.name))
		// 现场最常见的情况是几个人共用了一个密钥，于是不停互相顶号。
		// 日志里明确写出来，省得靠猜。
		slog.Warn("同一身份再次连接，旧连接被顶掉",
			"room", r.Code, "name", s.name, "identity", fp,
			"hint", "多人共用一个密钥时会这样，每人用自己的密钥")
	}

	ch := make(chan Snapshot, 4)
	r.subs[s] = ch
	ch <- r.snapshotLocked(s)

	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if c, ok := r.subs[s]; ok && c == ch {
			delete(r.subs, s)
			close(ch)
		}
	}
	return ch, cancel, nil
}

// Close 停掉房间的后台时钟。
func (r *Room) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.stop)
	for s, ch := range r.subs {
		delete(r.subs, s)
		close(ch)
	}
	r.mu.Unlock()
}

// ---- 内部 ----

func (r *Room) loop() {
	t := time.NewTicker(r.opts.BotLag / 2)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			r.tick()
		}
	}
}

// tick 推进一切与时间有关的状态：电脑出牌、回合超时、掉线宽限、投票到期。
func (r *Room) tick() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	now := r.opts.Now()

	// 通知只显示几秒。
	if r.notice != "" && now.Sub(r.noticeAt) > 4*time.Second {
		r.notice = ""
	}

	// 投票到期就作废。
	if r.vote != nil && now.After(r.vote.endsAt) {
		r.vote = nil
		r.setNotice("重开投票超时作废")
		r.broadcastLocked()
	}

	changed := false

	// 掉线宽限到期：座位保留，但彻底交给电脑。
	for _, s := range r.seats {
		if !s.connected && now.Sub(s.offlineAt) > r.opts.Grace {
			// 已经是托管状态，这里只用于发一次通知。
			if !s.bot {
				s.bot = true
				r.setNotice(fmt.Sprintf("%s 还没回来，这一局由电脑接手", s.name))
				changed = true
			}
		}
	}

	// 一个人都不在线就冻结：别让电脑互相打到天荒地老。
	if r.connectedCountLocked() == 0 {
		return
	}

	// 漏喊 UNO 的宽限到了：直接按"被抓"结算，别让座位一直挂着。
	// 限时义务到点：执行游戏给的处理动作（UNO 就是"漏喊算被抓"）。
	if r.phase == Playing && r.state != nil &&
		r.pendingAct != nil && !r.pendingEnd.IsZero() && now.After(r.pendingEnd) {
		act := r.pendingAct
		if err := r.Game.Apply(r.state, act); err == nil {
			r.clearPendingLocked()
			r.absorbLogLocked()
			changed = true
		}
	}

	if r.phase == Playing && r.state != nil && r.Game.Phase(r.state) == game.Playing {
		cur := r.seats[r.Game.Turn(r.state)]
		waited := now.Sub(r.turnAt)

		switch {
		case cur.bot || !cur.connected:
			// 电脑座位，或者掉线玩家的座位：交给电脑，节奏跟电脑一样快。
			if waited >= r.opts.BotLag {
				r.botStepLocked(cur.seat)
				changed = true
			}
		case waited >= r.opts.TurnTimeout:
			// 真人在想太久：帮他走一步保底动作，别把桌子卡死。
			if r.timeoutStepLocked(cur.seat) {
				changed = true
			}
		}
	}

	if changed {
		r.broadcastLocked()
	}
}

func (r *Room) botStepLocked(seat int) {
	view := r.Game.Observe(r.state, seat)
	a := r.Game.Bot(view)
	if a == nil {
		return
	}
	if err := r.Game.Apply(r.state, a); err != nil {
		// 机器人算错了也别卡住：走一步保底动作。
		if fallback := r.Game.TimeoutAction(view); fallback != nil {
			_ = r.Game.Apply(r.state, fallback)
		}
	}
	r.turnAt = r.opts.Now()
	r.afterStateLocked()
	r.absorbLogLocked()
}

func (r *Room) timeoutStepLocked(seat int) bool {
	// 真人迟迟不动（或在质疑 +4 时不决定），由游戏给出保底动作。
	a := r.Game.TimeoutAction(r.Game.Observe(r.state, seat))
	if a == nil {
		return false
	}
	if err := r.Game.Apply(r.state, a); err != nil {
		return false
	}
	r.turnAt = r.opts.Now()
	r.afterStateLocked()
	r.absorbLogLocked()
	return true
}

func (r *Room) afterStateLocked() {
	if r.Game.Phase(r.state) == game.Playing {
		return
	}
	r.phase = Over
	r.vote = nil
	w := r.Game.RoundWinner(r.state)
	if w >= 0 && w < len(r.seats) {
		r.setNotice(fmt.Sprintf("%s 打完手牌！", r.seats[w].name))
	}
}

func (r *Room) removeLocked(s *seat) {
	// 电脑座位没有指纹，别把 "" 从索引里删脏了。
	if s.fp != "" {
		delete(r.byFP, s.fp)
	}
	if ch, ok := r.subs[s]; ok {
		delete(r.subs, s)
		close(ch)
	}
	kept := r.seats[:0]
	for _, p := range r.seats {
		if p != s {
			kept = append(kept, p)
		}
	}
	r.seats = kept

	// 重新编号，并保证总有一个人是房主。
	hasHost := false
	for _, p := range r.seats {
		if p.host {
			hasHost = true
		}
	}
	for i, p := range r.seats {
		p.seat = i
		if !hasHost && i == 0 {
			p.host = true
			hasHost = true
		}
	}
}

func (r *Room) connectedCountLocked() int {
	n := 0
	for _, s := range r.seats {
		if s.connected {
			n++
		}
	}
	return n
}

// uniqueNameLocked 处理重名：两个人都叫 play 时，后来的变成 play#2。
func (r *Room) uniqueNameLocked(name string) string {
	taken := func(n string) bool {
		for _, s := range r.seats {
			if s.name == n {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s#%d", name, i)
		if !taken(cand) {
			return cand
		}
	}
	return name
}

func (r *Room) setNotice(msg string) {
	r.notice = msg
	r.noticeAt = r.opts.Now()
}

func (r *Room) broadcastLocked() {
	r.revision++
	// 有任何一次广播就说明房间还活着，用它来驱动回收计时。
	r.lastActive = r.opts.Now()
	for s, ch := range r.subs {
		select {
		case ch <- r.snapshotLocked(s):
		default:
			// Drop the oldest queued full snapshot; retain the latest state.
			select {
			case <-ch:
			default:
			}
			ch <- r.snapshotLocked(s)
		}
	}
}

// Abandoned 报告房间是否已经没人了、且安静了很久。
// 只看 Population 是不够的：掉线玩家的座位还占着，房间永远不会变成 0 人。
func (r *Room) Abandoned(ttl time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, player := range r.seats {
		if player.fp != "" && player.connected {
			return false
		}
	}
	if !r.abandonedAt.IsZero() {
		return r.opts.Now().Sub(r.abandonedAt) > ttl
	}
	return r.opts.Now().Sub(r.lastActive) > ttl
}

func (r *Room) snapshotLocked(s *seat) Snapshot {
	snap := Snapshot{
		Revision: r.revision,
		Code:     r.Code,
		Phase:    r.phase,
		MySeat:   s.seat,
		MaxSeats: r.Game.MaxSeats(),
		IsHost:   s.host,
		Notice:   r.notice,
		Timeline: append([]TimelineEntry(nil), r.timeline...),
		Players:  make([]Player, 0, len(r.seats)),
	}
	if r.phase == Playing || r.phase == Over {
		snap.View = r.Game.Observe(r.state, s.seat)
	}
	now := r.opts.Now()
	if r.state != nil && r.pendingAct != nil && !r.pendingEnd.IsZero() {
		if left := r.pendingEnd.Sub(now).Milliseconds(); left > 0 {
			snap.PendingLeftMS = left
		}
	}
	for _, p := range r.seats {
		pl := Player{
			Seat:      p.seat,
			Name:      p.name,
			Host:      p.host,
			Connected: p.connected,
			Bot:       p.bot,
		}
		if !p.connected {
			pl.Offline = int(now.Sub(p.offlineAt).Seconds())
		}
		snap.Players = append(snap.Players, pl)
	}
	if r.vote != nil {
		yes := 0
		for _, p := range r.seats {
			if p.voted && p.yes {
				yes++
			}
		}
		snap.Vote = &Vote{
			Kind:   r.vote.kind,
			Yes:    yes,
			No:     r.connectedCountLocked() - yes,
			Need:   r.vote.need,
			EndsIn: int(r.vote.endsAt.Sub(now).Seconds()),
		}
	}
	return snap
}

// ---- 房间注册表 ----

// Registry 是按房间码索引的房间表。
type Registry struct {
	mu    sync.Mutex
	rooms map[string]*Room
	game  game.Game
	opts  Options
}

func NewRegistry(g game.Game, opts Options) *Registry {
	return &Registry{
		rooms: make(map[string]*Room),
		game:  g,
		opts:  opts.withDefaults(),
	}
}

// Joining 返回房间和座位号。code 为空时自动开一间新房。
func (reg *Registry) Join(code, fp, name string) (*Room, int, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	code = NormalizeCode(code)
	if code == "" {
		code = reg.newCodeLocked()
	} else if !ValidCode(code) {
		return nil, 0, ErrCode
	}

	rm, ok := reg.rooms[code]
	if !ok {
		rm = New(code, reg.game, reg.opts)
		reg.rooms[code] = rm
	}
	if rm.IsClosed() {
		delete(reg.rooms, code)
		rm = New(code, reg.game, reg.opts)
		reg.rooms[code] = rm
	}

	seat, err := rm.Join(fp, name)
	if err != nil {
		// 新建的房里进不去（比如已经开了），别留垃圾房间。
		if rm.Population() == 0 {
			delete(reg.rooms, code)
			rm.Close()
		}
		return nil, 0, err
	}
	// 现场排障用：谁进了哪间房、坐几号位、房里现在几个人。
	slog.Info("进房", "room", code, "seat", seat+1, "seats", rm.Population(), "name", name, "identity", fp)
	return rm, seat, nil
}

// Get 按房间码取房间。
func (reg *Registry) Get(code string) (*Room, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	rm, ok := reg.rooms[NormalizeCode(code)]
	return rm, ok
}

// FindRoom 找一个包含这个身份的房，用于断线后自动回到原座位。
// 有多个候选时（比如测试留下的旧房还没回收）回**最近活跃**的那一间，
// 而不是 map 遍历到的第一个——否则重连会随机落进一个旧房间。
func (reg *Registry) FindRoom(fp string) *Room {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var best *Room
	var bestAt time.Time
	for _, rm := range reg.rooms {
		if !rm.HasMember(fp) {
			continue
		}
		at := rm.LastActive()
		if best == nil || at.After(bestAt) {
			best, bestAt = rm, at
		}
	}
	return best
}

// LastActive 返回房间最后一次有动静的时间。
func (r *Room) LastActive() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastActive
}

// HasMember 报告这个身份是否在房间里（含已掉线的座位）。
func (r *Room) HasMember(fp string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byFP[fp]
	return ok
}

// Count 返回当前房间数量（诊断用）。
func (reg *Registry) Count() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.rooms)
}

// Sweep 回收已经没人的房间。返回回收数量。
func (reg *Registry) Sweep() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	n := 0
	for code, rm := range reg.rooms {
		if rm.Population() == 0 || rm.Abandoned(10*time.Minute) {
			delete(reg.rooms, code)
			rm.Close()
			n++
		}
	}
	return n
}

func (reg *Registry) newCodeLocked() string {
	for i := 0; i < 100; i++ {
		code := randomCode()
		if _, exists := reg.rooms[code]; !exists {
			return code
		}
	}
	return randomCode()
}

// IsClosed 报告房间是否已关闭。
func (r *Room) IsClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Population 返回房间里的人数。
func (r *Room) Population() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seats)
}

// SnapshotFor 直接取一帧，用于开局的第一次渲染。
func (r *Room) SnapshotFor(fp string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byFP[fp]
	if !ok {
		return Snapshot{}, ErrNotIn
	}
	return r.snapshotLocked(s), nil
}

// NormalizeCode 统一成大写、去掉空格和连字符。
func NormalizeCode(s string) string {
	out := make([]rune, 0, len(s))
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c-32)
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			out = append(out, c)
		}
	}
	return string(out)
}

// ValidCode 判断房间码格式。
func ValidCode(code string) bool {
	if len(code) != codeLen {
		return false
	}
	for _, c := range code {
		if !containsRune(codeChars, c) {
			return false
		}
	}
	return true
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func randomCode() string {
	b := make([]byte, codeLen)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下的兜底，保证不会返回空码。
		return "UNO234"
	}
	out := make([]byte, codeLen)
	for i, v := range b {
		out[i] = codeChars[int(v)%len(codeChars)]
	}
	return string(out)
}
