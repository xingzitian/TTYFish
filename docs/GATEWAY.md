# 网关改造：SSH 接后端 API（进行中）

## 目标

`cmd/hub` 不再自己建房间、不再碰数据库，只做三件事：

1. 处理 SSH 协议（认证、PTY、窗口尺寸）
2. 拿玩家令牌，把按键变成 API 调用
3. 把后端推来的快照渲染成终端界面

```
玩家终端 ──SSH──► cmd/hub（网关） ──HTTP+SSE──► cmd/backend（房间与规则）
                        │
                        └─ 只负责渲染，不执行 UNO 规则
```

## 已完成

**`hub/internal/remote`** —— 房间 API 的客户端，实现了 `sdk/client.Client`：

| 方法 | 对应的 API |
|---|---|
| `Join("")` | `POST /api/v1/rooms`（新建） |
| `Join(code)` | `POST /api/v1/rooms/{code}/join` |
| `Resume()` | `GET /api/v1/me/room` |
| `Act(action)` | `POST /api/v1/rooms/{code}/actions`（带 `expected_revision`） |
| `Start/AddBot/Chat/VoteRestart/Leave` | 同名控制接口 |
| `Watch()` | `GET /api/v1/rooms/{code}/events`（SSE） |

关键点：

- **快照转译**：后端的 `view` 是协议编码过的 JSON，这里解码回
  `engine.Observation`，并把 `view.legal` 里的 `{type,payload}` 用
  `protocol.DecodeAction` 还原成 `engine.Action`——界面靠它判断某张牌能不能出。
- **版本号**：旧的 SSE 帧会被丢弃，只推最新的；界面读得慢也不积压。
- **断线不丢座位**：`Close()` 只断开 SSE，不调用 leave。

`games/uno/protocol` 增加了一个 `EncodeAction`，让网关能把本地动作编成线格式。

## 待完成：`internal/sshd` 的认证迁移

现在是这三处直接读 SQLite，改造后都要走 API：

| 位置 | 现在 | 改造后 |
|---|---|---|
| banner | `store.ByHandle` 判断用户名是否存在 | 直接去掉，或换成固定文案（避免账号枚举） |
| `WithPasswordAuth` | `store.AuthenticatePassword` | `POST /api/v1/auth/password` |
| `WithPublicKeyAuth` | `store.ByFingerprint` 校验用户名匹配 | `POST /api/v1/internal/auth/ssh`（网关凭证） |
| `handleAuth`（`ssh hub auth <码>`） | `store.Confirm` | `POST /api/v1/internal/auth/ssh/challenges/confirm` |

拿到玩家令牌后，`session.New(reg, …)` 换成 `remote.New(base, token, "uno")`，
界面工厂的 `UIContext.Key` 用**账号 ID**（不是指纹），这样换钥匙不丢偏好。

## 需要定的一件事

**SSH 登录横幅要不要保留？**

现在连上来会提示「账号错误：请先在网页注册」。这是查库判断的，改造后网关不查库了，两个选择：

- **A（推荐）**：去掉横幅，认证失败时只回 `Permission denied`。少泄露"这个用户名存在不存在"。
- **B**：保留一句固定文案（比如「请用注册时的用户名登录」），不对具体账号做判断。

另外，**密码登录 SSH** 要不要保留？后端 `/internal/auth/ssh` 只接受密钥身份，
密码得调用 `/api/v1/auth/password` 拿令牌。可以保留，也可以干脆只支持密钥
（网页端反正有密码登录）。
