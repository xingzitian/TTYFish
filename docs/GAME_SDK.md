# 写一个游戏

> 目标读者：想给 TTYFish 加游戏的人。
> 读完你应该能写一个能被人玩的游戏，而不需要碰账号、房间、网络、终端。

---

## 1. 你会拿到什么

平台已经处理好：

- SSH 接入、公钥认证、账号
- 房间码、加入/离开、房主、掉线托管、重连、顶号
- 投票重开、快捷语、时间线（谁在什么时候做了什么）
- 终端渲染的会话管理、窗口大小变化、颜色深浅
- 信息裁剪的强制边界

你要写的只有：**规则** 和 **这个游戏长什么样**。

---

## 2. 游戏 = 一个 Go 模块

```
yourgame/
  go.mod          module github.com/you/yourgame
  game.go         实现 sdk/game.Game
  engine/         纯规则（推荐，但不强制）
  ui/             bubbletea 界面
```

`go.mod` 依赖 `sdk`：

```
require github.com/xingzitian/TTYFish/sdk v0.1.0
```

---

## 3. 接口

```go
type Game interface {
    Name() string
    MinSeats() int
    MaxSeats() int

    NewMatch(seeds []PlayerSeed, seed int64) any
    NextRound(state any) error

    Apply(state any, action any) error
    Observe(state any, seat int) any
    SeatOf(action any) int

    Phase(state any) Phase
    Turn(state any) int

    Bot(view any) any
    TimeoutAction(view any) any

    RoundWinner(state any) int
    MatchWinner(state any) int
    Scores(state any) []int
    MatchTarget() int

    Events(state any, since int) ([]Event, int)
    Pending(state any) (token string, within time.Duration, action any)
}
```

### 3.1 状态和动作用 `any`

平台不解释它们，只是原样传递和存储。你的 `Apply` 里做类型断言：

```go
func (g Game) Apply(state any, action any) error {
    s, ok := state.(*State)
    if !ok { return fmt.Errorf("状态类型不对") }
    a, ok := action.(Action)
    if !ok { return fmt.Errorf("动作类型不对") }
    return apply(s, a)
}
```

### 3.2 `Observe` 是全项目最重要的方法

它决定"某个座位此刻能看到什么"。平台拿它的返回值去：

- 渲染界面（玩家自己的 UI）
- 给 AI 做决策
- （以后）给观战者看

**唯一的信息出口。** 所以：

- 别人的手牌、底牌、私有信息，绝不能出现在里面
- 返回类型建议是值类型（快照语义），改它不会影响真实状态

### 3.3 限时义务 `Pending`

有些规则带"必须在 N 秒内做某事"。平台不认这些规则，只认这个约定：

```go
// 返回空 token 表示当前没有义务
func (g Game) Pending(state any) (token string, within time.Duration, action any)
```

- `token` 变化 → 平台开始计时
- 到点还没解除 → 平台替你执行 `action`
- token 变回空 → 平台取消计时

例：UNO 剩一张牌没喊 UNO，token = `"uno:座位号"`，3 秒后执行"被抓"。

**游戏不碰时钟**，所以这个逻辑在测试里可以用假时钟精确验证。

### 3.4 `Events`

返回 `seq` 之后的新战报，平台负责打时间戳、存储、展示。

```go
type Event struct {
    Seq    int
    Seat   int    // -1 表示系统事件
    Text   string // "出牌"、"摸了一张牌"
    Detail any    // 给 UI 用的额外信息（UNO 放牌面）
}
```

`Detail` 里别放 nil 指针——塞进 `any` 之后它不是 nil 接口，
下游用 `!= nil` 判断会漏过去，一解引用就 panic。（真实踩过。）

---

## 4. 界面

游戏自带 UI，平台只负责把它挂到 SSH 会话上。

```go
// 平台给你的东西
type Client interface {
    Snapshot() sdk.Snapshot          // 当前这一帧
    Updates() <-chan sdk.Snapshot    // 后续每一帧
    Act(action any) error            // 发一个动作
    Chat(preset int) error
    VoteRestart() error
}
```

`sdk.Snapshot` 里有：

```go
type Snapshot struct {
    Code      string
    Phase     Phase
    Players   []Player        // 座位、昵称、是否在线、是否电脑
    MySeat    int
    IsHost    bool
    View      any             // 你自己的 Observe 返回值
    Timeline  []TimelineEntry // 已打时间戳的战报 + 聊天
    Vote      *Vote
    Notice    string
    PendingLeftMS int64       // 限时义务剩余毫秒
    Kicked    bool
}
```

**界面必须是响应式的**：终端可能只有 40 列，也可能 200 列 9 行。
参考 UNO 的四种布局（宽高 / 宽矮 / 窄高 / 窄矮）。

---

## 5. 注册

在平台的 `cmd/hub/main.go` 里加一行：

```go
reg.Register(registry.Entry{
    Name: "uno",
    Game: uno.New(rules),
    NewUI: func(c hub.Client) tea.Model { return ui.New(c) },
})
```

大厅列表里就会出现这个游戏。

---

## 6. 必须遵守的三条

1. **不做 I/O**：规则层不读文件、不连网、不看时钟。
   随机性全部从 `NewMatch` 的 `seed` 推导——这样任意状态都能存档重放。
2. **不信任客户端**：`Apply` 里重新校验一切。客户端说"这张牌能出"不算数。
3. **`Observe` 里别漏也别人手牌**。裁剪漏了就是作弊，不是 bug。

---

## 7. 测试建议

```go
func TestApplyIsDeterministic(t *testing.T) {
    a := NewMatch(seeds, 42)
    b := NewMatch(seeds, 42)
    // 同样一串动作，两边状态必须完全一致
}
```

再配一个 `simulate`：用机器人自己跟自己打 1000 局，断言不变量
（总牌数守恒、不能出非法牌、必须能结束）。

平台的 `selfcheck.sh` 会跑这些。
