# 进度（按调用方分）

后端 `cmd/backend` 一套 API，两个客户端各自实现自己的部分。以后就按这个口径对进度。

- **Web** = 浏览器里的 Vue 前端（`web/`）：只做账号与资料，不做游戏
- **SSH** = 终端网关（`cmd/hub`）：只做游戏，账号交给后端

图例：✅ 已实现并验证 · 🟡 写了但没验证 · ⬜ 未开始

---

## Web（浏览器）

基础账号流程已接入。下表状态表示 Web 页面接入情况。PT 余额、密钥名称与最后使用时间、登录会话、密码登录开关都已在账号页展示或可操作；TUI 的设置菜单也能开/关密码登录。

| 状态 | 接口 | 用途 |
|---|---|---|
| ✅ | `POST /api/v1/accounts` | 注册 |
| ✅ | `POST /api/v1/auth/password` | 密码登录 |
| ✅ | `POST /api/v1/auth/ssh/challenges` | 发起 SSH 密钥登录 |
| ✅ | `POST /api/v1/auth/ssh/challenges/poll` | 轮询领取会话 |
| ✅ | `GET /api/v1/me` | 当前账号 |
| ✅ | `PUT /api/v1/me/password` | 设置/修改密码 |
| ✅ | `PUT /api/v1/me/password-login` | 开启/关闭密码登录 |
| ✅ | `GET /api/v1/me/points` | PT 余额（账号页标题旁的徽章） |
| ✅ | `GET /api/v1/me/keys` | 公钥列表 |
| ✅ | `POST /api/v1/me/keys` | 添加公钥 |
| ✅ | `DELETE /api/v1/me/keys/{fingerprint}` | 删除公钥 |
| ✅ | `POST /api/v1/auth/logout` | 退出 |
| ✅ | `GET /api/v1/me/keys/{fingerprint}` | 单个公钥详情与最后使用时间 |
| ✅ | `PATCH /api/v1/me/keys/{fingerprint}` | 修改密钥名称 |
| ✅ | `GET /api/v1/me/sessions` | 登录会话列表 |
| ✅ | `DELETE /api/v1/me/sessions/{id}` | 退出指定登录 |
| ✅ | `POST /api/v1/me/sessions/revoke-others` | 退出其他登录 |

页面：`/login`、`/register`、`/account`。

公钥列表已展示 name（可改名）和 last_used_at；账号页新增登录会话卡片，可单条退出或一次性退出其他登录；改密码时可勾选“同时退出其他设备”。删除公钥会自动撤销关联会话；会话撤销后现有 SSE 会失效。后端测试已覆盖跨账号隔离、并发撤销、旧数据库迁移和失败事务回滚。

认证限流已按来源、账号、操作与已认证网关分层，支持可信代理 CIDR、网关转发玩家 IP、实际 Retry-After 秒数；预检查与正式密钥登录分别计小额度。前端已读取 Retry-After，429 时直接告诉玩家还要等几秒。

**有意没做**：房间类接口（游戏在终端里打，网页不做牌桌）。战绩做 M3 时再谈。

---

## SSH 网关

游戏链路全部覆盖。14/15。

| 状态 | 接口 | 用途 |
|---|---|---|
| ✅ | `POST /api/v1/internal/auth/ssh` | 密钥登录换玩家令牌 |
| ✅ | `POST /api/v1/auth/password` | SSH 密码登录 |
| ✅ | `POST .../internal/auth/ssh/challenges/confirm` | `ssh hub auth <码>` |
| ✅ | `GET /api/v1/me/room` | 重连回原房间 |
| ✅ | `POST /api/v1/rooms` | 建房 |
| ✅ | `POST /api/v1/rooms/{code}/join` | 进房 |
| ✅ | `POST /api/v1/rooms/{code}/leave` | 退房 |
| ✅ | `POST /api/v1/rooms/{code}/start` | 开局 / 下一局 |
| ✅ | `POST /api/v1/rooms/{code}/bots` | 补电脑 |
| ✅ | `POST /api/v1/rooms/{code}/chat` | 预设快捷语 |
| ✅ | `POST /api/v1/rooms/{code}/restart-votes` | 重开投票 |
| ✅ | `POST /api/v1/rooms/{code}/actions` | 出牌等动作（带 revision） |
| ✅ | `GET /api/v1/rooms/{code}/events` | SSE 实时快照 |
| ✅ | `GET /api/v1/games` | 大厅列表（和本地挂载取交集） |
| ⬜ | `GET /api/v1/rooms/{code}` | 靠 SSE 首帧拿快照，不需要单独拉 |

**已完成**：连上来先进大厅选游戏（`hub/internal/lobby` + `hub/internal/gateway`），
选完才建房进游戏；重连会跳过大厅直接回原房间。

**还没做**：多个游戏同时挂载（现在只注册了 uno）。

---

## 两边都要的后端能力（未实现）

| 能力 | 影响 |
|---|---|
| 房间落库 | 现在房间纯内存，后端重启对局全丢；战绩（M3）依赖它 |
| 战绩接口 | 网页展示历史对局 |
| 好友 / 在线状态 | 社交（M4） |
| 观战 | 只读订阅，SSH 和 Web 都能用 |

---

## 约定

1. 说"进度"时按上面两张表走，不混着说。
2. 新增接口先想清楚它属于哪一边；两边都用的（比如观战）单独开一节。
3. 每次改完更新这个文件。
