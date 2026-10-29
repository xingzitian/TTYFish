# TTYFish

用 SSH 当客户端、用公钥当账号的多人小游戏平台。

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
| M1 | 账号：自动建号、多公钥、网页可见 | ✅（删钥匙的界面还没做） |
| M2 | 游戏挂载：`ssh hub` 进大厅选游戏 | 部分（通用挂载已完成，仍直接进 uno） |
| M3 | 战绩 | 未开始 |
| M4 | 社交 | 未开始 |

目前：SSH 进来能直接玩 uno；网页能登录、能看自己的公钥、能加第二把钥匙。
还差：大厅选游戏（现在连上来直接进 uno）、战绩、社交。

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
