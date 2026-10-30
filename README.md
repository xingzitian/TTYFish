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
hub/          平台：SSH 接入、房间、账号 API（cmd/backend）
web/          网页前端：Vue 3 + TypeScript + Element Plus
games/uno/    UNO 游戏：规则引擎、AI、终端界面
deploy/       服务器侧部署脚本
docs/         需求、架构、游戏开发指南、部署、前端
```

三个 Go 模块 + 一个前端工程，依赖方向单向：`games/* → sdk ← hub`，
前端只通过 HTTP JSON API 和 `hub` 通信。

**加一个游戏**只需要在 `games/` 下新建一个模块实现 `sdk/game.Game`，
再在 `hub/cmd/hub` 注册规则实例和 UI 工厂，平台内部无需改动。见 [docs/GAME_SDK.md](docs/GAME_SDK.md)。

---

## 跑起来

本地跑要起**两个进程**（一个是后端 API，一个是 SSH 网关）：

```bash
# 1. 后端：账号、房间、UNO 规则
cd hub
TTYFISH_GATEWAY_TOKEN=dev-token go run ./cmd/backend -addr 127.0.0.1:8081

# 2. 网关：只渲染，不跑规则
TTYFISH_GATEWAY_TOKEN=dev-token go run ./cmd/hub \
    -addr :2222 -backend http://127.0.0.1:8081

# 3. 前端（开发模式，代理到 8081）
cd web && npm run dev
```

两个进程的 `TTYFISH_GATEWAY_TOKEN` 必须**是同一个值**：后端用它签发玩家令牌，
网关用它换令牌。

## 服务器上的启动参数（改之前先看这段）

生产环境用 systemd 托管，**unit 是手工放的、不在本仓库里**。改参数请直接改线上文件：

`/etc/systemd/system/ttyfish-backend.service`

```ini
EnvironmentFile=/etc/ttyfish/gateway.env
ExecStart=/opt/ttyfish/backend -addr 127.0.0.1:8082 -db /var/lib/ttyfish/backend.db -cors-origins http://159.75.78.7:12332
```

`/etc/systemd/system/ttyfish.service`

```ini
EnvironmentFile=/etc/ttyfish/gateway.env
ExecStart=/opt/ttyfish/hub -addr :12331 -host-key /var/lib/ttyfish/host_ed25519 -backend http://127.0.0.1:8082
```

⚠️ **`-cors-origins` 不能删**。浏览器发 POST 时**即使同源也会带 `Origin` 头**，
后端只认白名单——删了之后网页登录会直接 403 `origin_denied`，
而且报错出现在浏览器里，很难联想到是这个参数。

`/etc/ttyfish/gateway.env` 里放 `TTYFISH_GATEWAY_TOKEN=<随机串>`，两个服务共用。

前端静态文件的正本是 `/opt/1panel/www/sites/TTYFish/index`——openresty 跑在**容器**里，
只挂载了 `/opt/1panel/www`，文件放别处它**看不见**（曾经把文件放 `/opt/ttyfish/web`
再软链过去，容器里是断链，直接 500）。部署脚本直接写这个目录。

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
