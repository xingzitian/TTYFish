# TTYFish

[![CI](https://github.com/xingzitian/TTYFish/actions/workflows/ci.yml/badge.svg)](https://github.com/xingzitian/TTYFish/actions/workflows/ci.yml)

用 SSH 当客户端、用公钥当账号的多人小游戏平台。

玩家先在网页注册固定用户名，再通过 SSH 密钥或可选的账号密码登录。注册、部署及测试步骤见 [账号说明](docs/ACCOUNTS.md)。

独立核心后端已提供账号认证 API：运行 `cd hub`、`go run ./cmd/backend`，默认监听 `127.0.0.1:8081`。分工边界、接口与联调示例见 [后端 API](docs/BACKEND_API.md)，机器可读契约位于 `/api/openapi.json`。

独立后端也已提供 [房间、UNO 动作与 SSE 状态推送](docs/ROOM_API.md)，支持成员权限、房主开局、机器人、版本校验与断线接管。原 `cmd/hub` 保留兼容运行，其房间暂不与独立后端互通。

```
ssh -p 2222 你的名字@你的服务器
```

平台负责账号、房间、大厅；游戏只是挂在上面的模块。第一个游戏是 UNO。

---

## 目录

```
sdk/          契约：Game 接口、客户端快照、界面小工具（第三方游戏只依赖它）
hub/          平台：SSH 接入、房间、账号（开发中）
games/uno/    UNO 游戏：规则引擎、AI、终端界面
docs/         需求、架构、游戏开发指南
```

三个独立 Go 模块，依赖方向单向：`games/* → sdk ← hub`。

**加一个游戏**只需要在 `games/` 下新建一个模块实现 `sdk/game.Game`，
再在 `hub/cmd/hub` 注册规则实例和 UI 工厂，平台内部无需改动。见 [docs/GAME_SDK.md](docs/GAME_SDK.md)。

---

## 跑起来

```bash
# 平台（会挂上 uno）
cd hub
go run ./cmd/hub -addr :2222 -host-key host_ed25519

# 另开一个终端
ssh -p 2222 你的名字@127.0.0.1
```

游戏规则开关（叠加、罚牌、长局、质疑）都在 `hub` 的命令行上，
具体见 `-h`。

---

## 文档

| 文档 | 内容 |
|---|---|
| [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) | 平台需求：账号、登录、大厅、社交、非功能指标 |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 分层、数据流、部署拓扑、关键不变式 |
| [docs/GAME_SDK.md](docs/GAME_SDK.md) | 怎么写一个游戏（给第三方作者） |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | 本次改动、本机测试和后续开发步骤 |

---

## 状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| M0 | 平台骨架：HTTP + SQLite + SSH 挑战登录 | ✅ |
| M1 | 固定用户名注册、可选密码、多公钥与 SSH 挑战登录 | 账号 API 已完成，原页面已有注册登录与添加公钥 |
| M2 | 游戏挂载、房间与游戏 API、多游戏大厅 | UNO 房间/动作/SSE API 已完成；大厅选择和网关迁移待做 |
| M3 | 战绩 | 未开始 |
| M4 | 社交 | 未开始 |

目前：原 `cmd/hub` 可通过 SSH 玩 UNO；独立 `cmd/backend` 已提供账号、房间、游戏动作与 SSE API。
还差：Web/SSH 统一接入独立后端、多游戏大厅、对局持久化、战绩与社交。

---

## 开发

```bash
cd sdk       && go build ./... && go vet ./...
cd games/uno && go test ./...
cd hub       && go test ./... && bash scripts/login-smoke.sh
```

`hub/scripts/login-smoke.sh` 是登录链路（M0）的端到端验收：起服务、发挑战、
真的用 ssh 确认、拿会话、看账号页、再挂第二把钥匙。

模块之间用 `replace` 指向本地目录，所以三个目录必须保持同级。

---

## 为什么这么分

- **游戏作者不碰平台**：不写网络、不写账号、不写终端
- **平台不认牌**：`hub` 里不该出现任何游戏术语
- **信息裁剪只有一处**：`Observe` 同时服务界面、网络、AI
- **以后能拆进程**：`Game` 接口就是天然的 RPC 边界
