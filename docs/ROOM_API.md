# 房间、游戏动作与状态推送协议 v1

独立后端 `hub/cmd/backend` 现在管理账号、房间、UNO 规则执行、机器人、计时和玩家快照。Web 与 SSH 网关通过同一套 HTTP 接口操作，不直接访问数据库或房间对象。接口契约位于 `/api/openapi.json`（OpenAPI 文档版本 1.2.0）。

当前默认注册 UNO，规则为不叠加、允许摸后出牌、允许质疑 +4、单局，不启用漏喊 UNO 罚牌。规则由后端注册时确定，本版不接受客户端自定义规则。添加其他游戏需要注册规则实现和 JSON 协议适配器，核心 API 不依赖具体游戏的 UI。

## 启动与接口

```powershell
cd hub
go run ./cmd/backend -cors-origins http://localhost:5173
```

默认监听 `127.0.0.1:8081`，仍使用 [账号 API](BACKEND_API.md) 发放的 `Authorization: Bearer <access_token>`。所有房间接口，包括游戏列表与 SSE，都要求玩家会话；网关服务端凭证不能代替玩家令牌。

| 方法 | 路径 | 含义 |
|---|---|---|
| GET | `/api/v1/games` | 游戏列表、人数限制、协议版本、聊天预设 |
| POST | `/api/v1/rooms` | `{"game_id":"uno"}` 创建房间并加入 |
| GET | `/api/v1/me/room` | 恢复当前账号所在房间，无房间返回 404 |
| GET | `/api/v1/rooms/{code}` | 当前账号在该房间的完整私有快照 |
| POST | `/api/v1/rooms/{code}/join` | 加入已有房间，空请求体 |
| POST | `/api/v1/rooms/{code}/leave` | 离开当前房间，空请求体，成功 204 |
| POST | `/api/v1/rooms/{code}/start` | 房主开局或推进下一局，空请求体 |
| POST | `/api/v1/rooms/{code}/bots` | 房主在大厅添加一个机器人，空请求体 |
| POST | `/api/v1/rooms/{code}/chat` | `{"preset":0}` 发送预设快捷语 |
| POST | `/api/v1/rooms/{code}/restart-votes` | `{"agree":true}` 发起或参与重开投票 |
| POST | `/api/v1/rooms/{code}/actions` | 带版本号提交游戏动作 |
| GET | `/api/v1/rooms/{code}/events` | SSE 实时推送完整快照 |

创建成功返回 201，其余查询、加入、开局、机器人、聊天、投票和动作成功返回 200，响应为同一格式的完整快照。输入房间码可忽略大小写和首尾空白；不存在的码不会自动创建房间。每个账号同时只有一个当前房间，切换前须显式离开。

只有房间成员可以获取视图、订阅、聊天、投票或操作。只有房主可以开局或添加机器人。已开局的房间拒绝新成员，已有座位的原账号仍可重新加入。没有公开房间目录或旁观接口。

## 快照格式

下面是大厅快照的结构示例：

```json
{
  "protocol_version": 1,
  "game_id": "uno",
  "code": "ABC234",
  "revision": 1,
  "phase": "lobby",
  "players": [{"seat":0,"name":"alice","host":true,"connected":true,"bot":false,"offline":0}],
  "my_seat": 0,
  "max_seats": 6,
  "is_host": true,
  "view": null,
  "timeline": [],
  "vote": null,
  "notice": "alice 加入",
  "pending_left_ms": 0,
  "kicked": false
}
```

- `phase` 为 `lobby`、`playing` 或 `over`，与游戏内部阶段分开。
- `revision` 是当前房间生命周期内单调递增的广播版本。机器人、玩家、加入退出、聊天等状态变化都会推进版本。不同房间的版本不能互相比。
- `view` 在大厅为 null，开局后由对应游戏的 `Observe` 裁剪，再由协议适配器编码。只包含该座位允许看到的信息。
- UNO 的 `view.hand` 是自己的手牌，`hand_counts` 仅是每人的数量；不会发送其他玩家手牌、牌堆顺序或随机种子。`drawn_id` 只对本回合摸牌的玩家显示，其余为 -1。
- UNO 的 `view.legal` 是带 `type` 与 `payload` 的合法动作列表，可以直接复制到动作请求。喊 UNO、抓漏喊等非回合动作仍按规则判断，不保证都出现在回合合法列表中。
- `players[].bot` 可能表示原生机器人，也可能是断线后代打的玩家；原账号重新连接可接管自己的座位。
- `timeline` 是最近最多 60 条公开战报与预设聊天。客户端不应依赖其作为持久对局记录。

## 动作协议

客户端提交意图，后端负责验证和推进规则。动作统一封装为：

```json
{
  "expected_revision": 12,
  "type": "play_card",
  "payload": {"card_id": 38, "color": 0, "uno": false}
}
```

`expected_revision` 必须使用最新快照的版本。版本校验与规则执行在同一个房间锁内完成。客户端不能传 `seat`，后端从已认证账号在房间的记录确定座位；未知 payload 字段会被拒绝。

| UNO type | payload |
|---|---|
| `play_card` | `card_id`、`color`，以及可选 `uno` |
| `draw_card` | `{}` |
| `pass` | `{}` |
| `call_uno` | `{}` |
| `catch_uno` | `{"target":1}` |
| `resolve_challenge` | `{"challenge":true}`，false 表示认罚 |

颜色编号：0 红、1 黄、2 绿、3 蓝。出牌按稳定 `card_id`，不使用手牌数组下标。完整字段和游戏视图枚举见 OpenAPI。

成功返回更新后的当前玩家快照。`409 stale_state` 表示状态已变化，动作没有执行，重新取快照再由玩家决定。相同动作请求再次提交也会因版本过期被拒绝；本版不缓存请求 ID，也不把重复请求伪装成成功。遇到网络中断时先拉取状态，不自动重放。机器人可能在动作处理完成后继续行动，因此响应可以包含更新的后续状态。

开局、加机器人、投票等控制接口也不应自动重试；重试前先查询状态。版本号针对游戏动作做原子校验，不是所有 HTTP 操作的通用事务 ID。

## SSE 推送

通过带玩家令牌的 GET 建立长连接：

```http
GET /api/v1/rooms/ABC234/events
Authorization: Bearer <access_token>
Accept: text/event-stream
```

正常事件是完整快照，不是增量补丁：

```text
id: 12
event: snapshot
data: {"protocol_version":1,"game_id":"uno","code":"ABC234","revision":12,...}

```

上面 data 为格式示意，实际是一行完整 JSON。连接建立后立即发送当前快照，后续房间变化继续推送。慢客户端允许跳过中间快照，但保留最新状态，不阻塞其他玩家。`id` 对应房间版本，仅供诊断；不提供历史事件补发，不使用 `Last-Event-ID`。重连后第一帧就是当前完整状态。

| event | data | 客户端行为 |
|---|---|---|
| `snapshot` | 完整玩家快照 | 替换本地状态，忽略同房间已经处理过的旧版本 |
| `heartbeat` | `{"server_time_ms":...}`，每 5 秒 | 保持连接，不推进房间版本 |
| `kicked` | `kicked=true` 的快照 | 新连接已接管，停止旧连接的自动重连 |
| `auth_expired` | `{"code":"invalid_credentials"}` | 会话失效或已退出，停止重连并重新登录 |

`kicked` 与认证事件必须按事件类型处理，即使它们的版本与上一帧相同。后端在状态事件和心跳时重新验证会话；退出登录后，原 SSE 最迟在下一次心跳时被关闭。

浏览器原生 `EventSource` 不方便设置 Authorization，请使用 `fetch` 读取响应流并按 SSE 帧解析，或由同源 Web 服务在服务端转发。不能把令牌放进查询参数。SSE 为 UTF-8 文本，网络分块不等于事件边界，客户端须缓存到空行后再处理完整帧。

## 在线状态、重连与回收

- 创建或加入后应立即建立 SSE。纯 REST 操作会刷新该账号的一分钟活动租约；既没有 SSE、又一分钟没有操作的玩家会被标记断线。
- SSE 断开后，大厅中的玩家立即移除；对局中的玩家保留座位，由机器人代打。仍有当前成员关系时，重新建立 SSE 会恢复同一座位。
- 同一账号只保留最新的一条 SSE。旧连接得到 kicked；旧连接的清理不会把新连接标记为断线。
- 显式 leave 会清除当前成员关系并关闭推送。对局不能凭空删掉座位，原座位继续托管；账号可加入其他房间。若之后主动加入旧房间，仍可认回旧座位。
- 没有真人在线的房间在十分钟后回收；机器人不会阻止回收。空房间也会回收。
- 后端关闭时停止房间时钟和推送，不残留后台房间循环。

## 手动联调：单人加机器人

先按账号文档注册登录，获得 `$login.access_token`：

```powershell
$apiBase = 'http://127.0.0.1:8081'
$headers = @{ Authorization = "Bearer $($login.access_token)" }
Invoke-RestMethod "$apiBase/api/v1/games" -Headers $headers
$create = @{ game_id = 'uno' } | ConvertTo-Json
$room = Invoke-RestMethod "$apiBase/api/v1/rooms" -Method Post -ContentType 'application/json' -Headers $headers -Body $create
$roomBase = "$apiBase/api/v1/rooms/$($room.code)"
Invoke-RestMethod "$roomBase/bots" -Method Post -Headers $headers
$snapshot = Invoke-RestMethod "$roomBase/start" -Method Post -Headers $headers
# 玩家回合时，从 $snapshot.view.legal 选取动作；若机器人正在行动，先等推送或重新查询。
$snapshot = Invoke-RestMethod $roomBase -Headers $headers
if ($snapshot.view.my_turn -and $snapshot.view.legal.Count -gt 0) {
  $move = $snapshot.view.legal[0]
  $body = @{ expected_revision = $snapshot.revision; type = $move.type; payload = $move.payload } | ConvertTo-Json -Depth 10
  Invoke-RestMethod "$roomBase/actions" -Method Post -ContentType 'application/json' -Headers $headers -Body $body
}
```

SSE 可用支持自定义 Header 的 API 客户端测试；另开客户端修改房间状态，确认事件实时出现。多人测试须注册不同账号，同一账号会互相接管。

## 当前部署边界

本版房间在单个 backend 进程内存中运行，重启会丢失对局；账号和凭证仍在 SQLite 中持久化。没有对局存盘、跨实例路由或横向扩容支持，暂时只能由一个 backend 实例承载同一组房间。

原 `cmd/hub` 保留兼容路径，它自己的房间与独立 backend 的房间不互通。现阶段已经开放后端游戏能力，下一步由 Web/SSH 网关接入这些 API，才能让用户统一进入 backend 的房间。网关仅处理输入渲染，不再本地创建房间或执行 UNO 规则。
