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

## 已完成（第二轮）

- `internal/sshd` 不再依赖 `*store.Store`：认证全部改成调后端
  - 密码登录 → `POST /api/v1/auth/password`
  - `ssh hub auth <码>` → `POST .../challenges/confirm`
  - 横幅置空（钩子保留）
- `cmd/hub` 变成纯网关：不打开数据库、不建房间、不跑规则
- 新包 `internal/backend`：账号 API 的客户端（含错误码到中文提示的映射）
- 测试换成 `httptest` 假后端：不再需要 SQLite 也能测网关

## 验证过的东西

网关 + 后端 + UNO 界面在真机上跑通了：

```
ssh -p 12333 alice2@127.0.0.1   →  输入房间码界面
```

服务端日志：

```
INFO 密钥登录 user=alice2 fingerprint=SHA256:4COv5NEz... account=a567bdcf...
```

排查过程中踩的两个坑（都不是网关的问题，是测试环境）：

1. **测试密钥带密码短语**：`ssh-keygen -N '""'` 在 PowerShell 里传进去的是两个引号字符，
   密钥就被"加密"了。客户端拿不到私钥签名，表现成 `Permission denied`——
   看起来特别像服务端拒绝，其实是客户端签不出来。
2. **Windows 上私钥权限太松**：文件从 WSL 拷出来是 777，OpenSSH 直接拒用，
   报 `UNPROCESSABLE PRIVATE KEY FILE`。要用 `icacls` 收紧到只有自己可读。

## 下一步

- 网页前端：账号页已好；战绩/好友等 M3/M4 再补
- 后端房间 API 的分页/列表、观战等都还没有