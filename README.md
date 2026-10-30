# TTYFish

[![CI](https://github.com/xingzitian/TTYFish/actions/workflows/ci.yml/badge.svg)](https://github.com/xingzitian/TTYFish/actions/workflows/ci.yml)

用 SSH 终端玩多人小游戏的平台。网页负责注册和账号管理，核心后端负责账号、房间与游戏规则，SSH 网关负责终端交互。当前可玩的游戏是 UNO。

玩家先在网页注册固定用户名，提供 SSH 公钥或设置密码，至少选择一种。之后所有登录都使用该用户名；同一账号的多把密钥和密码对应同一个游戏身份。

## 当前架构

```text
浏览器 ── Vue 页面 ── HTTP JSON ──────────────┐
                                            ├── backend ── SQLite（账号、凭证、会话）
玩家终端 ── SSH ── hub 网关 ── HTTP + SSE ────┘       └── 内存房间、UNO 规则与机器人
```

| 部分 | 入口 / 目录 | 职责 |
|---|---|---|
| 核心后端 | `hub/cmd/backend` | 账号认证、公钥管理、房间、游戏动作、计时与状态推送 |
| SSH 网关 | `hub/cmd/hub` | SSH 密钥/密码认证、游戏选择、按键输入、终端渲染；通过 API 操作后端 |
| Web 前端 | `web/` | Vue 3 + TypeScript + Element Plus；注册、登录、账号设置 |
| 游戏模块 | `games/uno/` | UNO 规则、机器人、JSON 动作协议与终端界面 |
| SDK | `sdk/` | 游戏规则接口、客户端快照、界面所需契约 |

后端和 SSH 网关已经分为两个进程。`cmd/hub` 不直接访问账号数据库，也不在本地执行对局规则。Web 当前只做账号相关页面，游戏在 SSH 终端中进行。

## 已完成与待完成

| 能力 | 当前状态 |
|---|---|
| 固定用户名注册 | 已完成；用户名 3–20 位，小写字母开头，允许数字、下划线和短横线 |
| 密码与 SSH 密钥登录 | Web 和 SSH 均已接入账号 API；密码可选 |
| 网页 SSH 挑战登录 | 已接通生成挑战、终端确认、浏览器领取会话；90 秒有效且只能领取一次 |
| 多公钥管理 | API 支持添加、删除、命名、改名、查询最后使用时间；Web 已接增删，名称与时间展示待接入 |
| SSH 大厅 | 已有游戏选择入口，当前只注册 UNO |
| 房间与 UNO | 已有建房、进退房、房主开局、机器人、预设聊天、重开投票和游戏动作 API |
| 实时状态与重连 | 网关通过 SSE 接收玩家快照，支持断线托管、恢复座位与新连接接管 |
| 动作与信息校验 | 后端确定座位，按房间版本校验动作，仅返回该玩家允许看到的游戏信息 |
| CI 与发布 | Go 测试、前端构建、账号 API 冒烟测试，以及 Release 部署工作流 |

接下来主要补齐：

- Web 的密钥名称、改名与最后使用时间展示。
- 多游戏实际接入，以及网关游戏列表与后端开放列表的同步。
- 对局持久化、战绩与统计、好友和在线状态、观战。

**当前房间保存在单个后端进程的内存中。** 后端重启会丢失对局，账号与凭证保留在 SQLite 中；尚不支持多个后端实例共同承载房间。

## 本地运行

需要 Go 1.26、Node.js 22、npm 和 OpenSSH 客户端。Go 模块使用本地 `replace`，请保持 `sdk/`、`games/uno/`、`hub/` 的目录关系。

以下示例使用 PowerShell，分别在三个终端中从仓库根目录开始执行。示例网关凭证仅用于本机联调；生产环境使用随机凭证，长度至少 32 字节。

### 1. 启动核心后端

```powershell
cd hub
$env:TTYFISH_GATEWAY_TOKEN = 'ttyfish-local-development-token-32-bytes'
go run ./cmd/backend -addr 127.0.0.1:8081 -db backend.db -cors-origins 'http://localhost:5173,http://127.0.0.1:5173'
```

默认数据库位于当前目录的 `backend.db`。可访问：

- 健康检查：`http://127.0.0.1:8081/healthz`
- OpenAPI：`http://127.0.0.1:8081/api/openapi.json`

### 2. 启动 SSH 网关

```powershell
cd hub
$env:TTYFISH_GATEWAY_TOKEN = 'ttyfish-local-development-token-32-bytes'
go run ./cmd/hub -addr :2222 -host-key host_ed25519 -backend http://127.0.0.1:8081
```

网关和后端必须使用相同的 `TTYFISH_GATEWAY_TOKEN`。它是服务之间的凭证，不能传给浏览器或当作玩家密码。网关启动时会检查后端是否可达。

### 3. 启动 Web 前端

```powershell
cd web
npm ci
$env:VITE_SSH_HOST = '127.0.0.1'
$env:VITE_SSH_PORT = '2222'
npm run dev -- --strictPort
```

访问 `http://localhost:5173`，先注册，再登录。Vite 将 `/api` 代理到本地后端。

**代理不会取消浏览器的 Origin 请求头。** 后端按来源白名单校验，所以即使使用 Vite 代理，也要保留上面的 `-cors-origins`。若更换前端地址或端口，同步修改该配置。

### 4. 进入游戏

注册完成后，在另一个终端连接；将 `alice` 替换为注册时的用户名：

```powershell
ssh -t -p 2222 alice@127.0.0.1
```

指定密钥，或直接使用账号密码：

```powershell
ssh -t -p 2222 -i "$env:USERPROFILE/.ssh/id_ed25519" alice@127.0.0.1
ssh -t -p 2222 -o PubkeyAuthentication=no alice@127.0.0.1
```

登录后先进入游戏选择大厅，再按游戏界面提示创建或加入房间。未设置密码的账号只能使用已绑定的密钥。网页登录时则使用网页生成的 `ssh ... auth <确认码>` 命令，不是上面的游戏连接命令。

## API 与协作开发

核心后端使用 HTTP JSON API 和玩家 Bearer 会话，网关内部认证另用服务端凭证。房间变化通过 SSE 发送完整玩家快照，游戏动作携带 `expected_revision`，由后端拒绝过期状态上的操作。

接口覆盖：

- 账号注册、密码登录、当前账号、设置密码、退出。
- 公钥增删、命名和单个查询；`last_used_at` 对新密钥未使用时为 null。
- SSH 挑战创建、网关确认和浏览器一次性领取会话。
- 游戏列表、当前房间、建房和加入、房主控制、游戏动作与 SSE。

接口契约以 [OpenAPI 源文件](hub/internal/api/openapi.json) 为准，服务运行时可从 `/api/openapi.json` 获取。新增能力先约定请求、响应和错误码，再由 Web 或 SSH 网关接入。

新增游戏需要三部分：实现 `sdk/game.Game`，在后端注册规则和 JSON 协议适配器，在网关注册终端 UI 与客户端协议适配。不能只注册一个 UI 就假定后端已经支持该游戏。

## 目录

```text
sdk/                      游戏与客户端契约（独立 Go 模块）
games/uno/                UNO 规则、协议、机器人与界面（独立 Go 模块）
hub/                      后端与 SSH 网关（独立 Go 模块）
  cmd/backend/            核心后端入口
  cmd/hub/                SSH 网关入口
  internal/account/       账号业务
  internal/api/           JSON / SSE 接口及 OpenAPI
  internal/play/          后端游戏注册与账号房间关系
  internal/room/          房间状态、计时、订阅
  internal/store/         SQLite 数据层
  internal/backend/       网关使用的账号 API 客户端
  internal/remote/        网关使用的房间 API / SSE 客户端
  internal/gateway/       SSH 会话与游戏界面衔接
  internal/lobby/         终端游戏选择大厅
web/                      Vue 网页前端
deploy/                   服务器部署脚本
docs/                     API、协议、开发与部署说明
```

## 测试与构建

从仓库根目录执行：

```powershell
go -C sdk test ./... -count=1
go -C games/uno test ./... -count=1
go -C hub test ./... -count=1
go -C sdk vet ./...
go -C games/uno vet ./...
go -C hub vet ./...
npm --prefix web ci
npm --prefix web run build
```

前端构建会先运行 TypeScript 类型检查，产物位于 `web/dist/`。

Linux 或已准备好相应工具的 Bash 环境还可运行：

```bash
bash hub/scripts/login-smoke.sh
```

该脚本启动后端，验证注册、密码登录、公钥增删、SSH 挑战 API 与跨源拒绝；需要 Bash、Go、curl、OpenSSH 工具和 Python 3。它不启动 SSH 网关，真实 SSH 认证由网关测试覆盖。

## 发布与生产配置

`.github/workflows/deploy.yml` 通过 `v*` tag 或手动触发发布，生成后端、网关、前端三个 Release 资产，由服务器脚本按“后端 → 前端 → 网关”顺序更新并检查。发布操作和回滚方式见 [部署说明](docs/DEPLOY.md)。推送普通 main 提交会触发 CI，不等于创建版本发布。

仓库记录的生产服务配置如下。systemd unit 由服务器维护，不在仓库中；调整部署时应核对服务器上的实际文件。

| 服务 | 配置位置 | 用途 |
|---|---|---|
| `ttyfish-backend` | `/etc/systemd/system/ttyfish-backend.service` | 后端 `127.0.0.1:8082` |
| `ttyfish` | `/etc/systemd/system/ttyfish.service` | SSH 网关 `:12331` |

后端的关键配置：

```ini
EnvironmentFile=/etc/ttyfish/gateway.env
ExecStart=/opt/ttyfish/backend -addr 127.0.0.1:8082 -db /var/lib/ttyfish/backend.db -cors-origins http://159.75.78.7:12332
```

网关的关键配置：

```ini
EnvironmentFile=/etc/ttyfish/gateway.env
ExecStart=/opt/ttyfish/hub -addr :12331 -host-key /var/lib/ttyfish/host_ed25519 -backend http://127.0.0.1:8082
```

两个服务共用 `/etc/ttyfish/gateway.env` 中的随机网关凭证。生产前端来源变化时，需要同步更新 `-cors-origins`；否则浏览器请求会被拒绝。

前端构建时通过 `VITE_SSH_HOST` 和 `VITE_SSH_PORT` 写入玩家应连接的地址。当前部署脚本将静态文件写到 `/opt/1panel/www/sites/TTYFish/index`；OpenResty 容器只挂载了相应站点目录，不能假定它能读取其他宿主机路径。Web 服务需将 `/api` 代理到后端，并为 Vue 路由配置 SPA 回退。

发布重启会断开 SSH 连接；后端重启还会丢失内存中的对局，发布前应选择合适的时间窗口。

## 文档

| 文档 | 内容 |
|---|---|
| [后端 API](docs/BACKEND_API.md) | 账号、密钥元数据、SSH 挑战与认证约定 |
| [房间与游戏协议](docs/ROOM_API.md) | 房间接口、UNO 动作、版本校验与 SSE |
| [前端说明](docs/FRONTEND.md) | Vue 页面和构建方式 |
| [游戏开发指南](docs/GAME_SDK.md) | 游戏接口与终端界面契约 |
| [部署说明](docs/DEPLOY.md) | Release 资产、服务器更新与回滚 |
| [分工与进度记录](docs/PROGRESS.md) | Web、SSH 与后端分工 |

`REQUIREMENTS.md`、`ARCHITECTURE.md`、`DEVELOPMENT.md`、`GATEWAY.md` 等保留了早期设计或改造过程，部分描述尚未同步。本 README 提供当前概览；具体接口以代码和 OpenAPI 为准，避免继续按“自动建号、无密码、网关本地运行规则”的旧方案开发。
