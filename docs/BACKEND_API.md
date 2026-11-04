# 核心后端 API：账号认证

独立后端已提供账号 API，以及 [房间、游戏动作和 SSE 推送 API](ROOM_API.md)。你负责核心业务与 HTTP 接口，同事可以按 OpenAPI 开发 Web、SSH 接入。

## 服务边界

```text
浏览器 / Web 服务 ── HTTP JSON ──┐
                               ├── backend → account → store → SQLite
SSH 玩家 ── SSH 网关 ── HTTP ────┘
```

后端是账号数据的所有者：注册、凭证校验、用户名规则、公钥归属和会话撤销都在后端完成。Web 负责页面和用户操作；SSH 网关负责 SSH 协议、私钥持有证明、终端输入和渲染。两个接入层都不应直接访问数据库。

目录：

| 路径 | 职责 |
|---|---|
| `hub/cmd/backend` | 独立进程入口，只启动账号 API |
| `hub/internal/account` | 账号业务服务与持久化接口，不依赖 Web、SSH 网关或游戏 |
| `hub/internal/api` | JSON、路由、Bearer 认证、CORS、错误码与 OpenAPI |
| `hub/internal/store` | SQLite 持久化、密码哈希、事务 |

独立运行入口仍复用 `hub` Go 模块，暂不新增仓库或模块。现有 `cmd/hub` 是兼容入口，仍运行原有网页和游戏 SSH 服务，尚未改成调用新 API；本次不是完整网关迁移。原 Web 对 SSH 服务包的依赖已移除，公钥解析复用账号服务。

## 本地启动

在项目根目录打开 PowerShell：

```powershell
cd hub
go run ./cmd/backend -addr 127.0.0.1:8081 -db backend.db -cors-origins http://localhost:5173
```

- API 地址：`http://127.0.0.1:8081`。
- 健康检查：`GET /healthz`，检查进程可响应。
- 服务器版本：`GET /api/v1/version`，无需认证，返回当前服务器发布版本。
- OpenAPI：`GET /api/openapi.json`，可导入支持 OpenAPI 3.0 的 API 工具。
- [OpenAPI 源文件](../hub/internal/api/openapi.json)。
- 默认数据库为 `backend.db`。如需使用现有账号，先备份，再显式指定 `-db hub.db`。数据表向后兼容，不会自动改用户名。
- Web 来源必须准确匹配 `-cors-origins`；多个来源用逗号分隔。默认不允许带 Origin 的浏览器跨源请求；命令行及服务端请求可直接调用。转发头默认不可信，代理部署可通过 `-trusted-proxies` 配置明确的代理 CIDR，详见下方限流说明。
- 公网部署通过 HTTPS 提供 API。网关凭证只放在服务端环境变量中。

## 接口清单

| 方法 | 路径 | 认证 | 用途 |
|---|---|---|---|
| GET | `/api/v1/version` | 无 | 查询当前服务器版本 |
| GET | `/api/v1/me/points` | 玩家 Bearer 会话 | 查询自己的 PT 余额 |
| POST | `/api/v1/accounts` | 无 | 注册，返回账号，不自动创建登录会话 |
| POST | `/api/v1/auth/password` | 无 | 用户名和密码登录 |
| GET | `/api/v1/me` | 玩家 Bearer | 当前账号 |
| POST | `/api/v1/auth/logout` | 玩家 Bearer | 撤销当前会话 |
| GET | `/api/v1/me/sessions` | 玩家 Bearer | 查询自己的活跃登录会话 |
| DELETE | `/api/v1/me/sessions/{id}` | 玩家 Bearer | 撤销指定会话，可撤销当前会话 |
| POST | `/api/v1/me/sessions/revoke-others` | 玩家 Bearer | 退出其他会话，保留当前会话 |
| PUT | `/api/v1/me/password` | 玩家 Bearer | 设置或替换密码 |
| GET | `/api/v1/me/keys` | 玩家 Bearer | 列出自己的公钥 |
| POST | `/api/v1/me/keys` | 玩家 Bearer | 添加公钥 |
| GET | `/api/v1/me/keys/{fingerprint}` | 玩家 Bearer | 查询单个公钥、名称和最后使用时间 |
| PATCH | `/api/v1/me/keys/{fingerprint}` | 玩家 Bearer | 设置或清除公钥名称 |
| DELETE | `/api/v1/me/keys/{fingerprint}` | 玩家 Bearer | 删除自己的公钥 |
| POST | `/api/v1/internal/auth/ssh` | 网关 Bearer | 私钥证明成功后换取玩家会话 |
| POST | `/api/v1/auth/ssh/challenges` | 无 | 为固定用户名生成网页登录挑战 |
| POST | `/api/v1/internal/auth/ssh/challenges/confirm` | 网关 Bearer | 验证 SSH 私钥后确认挑战 |
| POST | `/api/v1/auth/ssh/challenges/poll` | 浏览器 poll_token | 查询状态并一次性领取会话 |

### PT 积分与 UNO 奖励

`GET /api/v1/me/points` 使用已有玩家 Bearer 会话，不接受其他账号 ID。成功返回 `200 OK`：

```json
{"balance_tenths":68,"balance_pt":"6.8"}
```

`balance_tenths` 是非负 int64 整数，以 **0.1 PT** 为单位；`balance_pt` 是精确到一位小数的字符串，适合直接展示，避免客户端浮点计算。未获奖励的新旧账号都返回 `{"balance_tenths":0,"balance_pt":"0.0"}`。会话缺失、无效、过期或撤销返回 `401 invalid_credentials`，读取失败返回 `500 internal_error`，不会用零余额掩盖数据库错误。接口仅供查询，不提供充值、转账或任意修改余额能力。

UNO 保留现有规则分数：赢家获得其他玩家剩余手牌的分值总和，**游戏分数 : PT = 10 : 1**，因此赢得 68 分增加 6.8 PT，1 分增加 0.1 PT，不取整、不使用浮点数。默认单局在该局结束时奖励；多局长赛只在达到目标分、整场结束时奖励最终赢家，使用该赢家整场累计分数（不是只取最后一局，也不是目标分封顶）。其他玩家余额和规则分数不变。原生机器人没有账号，不奖励；玩家断线后由机器人托管仍归属原账号。

数据库打开时沿用 `CREATE TABLE IF NOT EXISTS` 模式增加 `pt_balances` 和 `uno_pt_rewards`，无需手动迁移旧账号。每次新比赛生成随机独立 ID，长赛各小局共享该 ID，重开则生成新 ID。入账在同一 SQLite 事务中记录唯一比赛结果并累加整数余额；相同结果重复处理或服务重启后再次处理不会重复奖励，账号或分数不匹配的重复结果会报错。余额溢出或写入失败会回滚整个事务，不会只留下奖励记录。

真人动作、机器人/断线托管、回合超时及限时义务都经过相同最终结算入口。写入失败时记录服务端错误并在房间通知中提示，后台重试同一结果，即使所有玩家离线也继续重试；期间禁止重开投票、开始新比赛和回收该房间，防止覆盖待入账结果。

已提交的余额及奖励去重记录持久保存在 SQLite 中。**比赛状态本身仍只在内存中**：强制重启/崩溃会丢失未结束的比赛，以及因数据库持续不可用而尚未成功提交的结果；关闭房间时会再尝试一次并记录仍未入账的错误。此功能不提供历史比赛回放、跨进程房间恢复或补发工具。

### 服务器版本

`GET /api/v1/version` 不需要请求体或登录凭证，成功返回 `200 OK`：

```json
{"version":"v0.1.8"}
```

响应为 `application/json; charset=utf-8`，沿用 API 的 `Cache-Control: no-store` 和 CORS 策略。`version` 保留 `v` 前缀，表示当前服务器发布版本，不是 OpenAPI 的 `info.version`、前端包版本或游戏协议版本。版本值统一定义在 `hub/internal/api/server.go` 的 `serverVersion` 常量中，发布新版本时更新该值及相关测试、文档示例。

```powershell
Invoke-RestMethod 'http://127.0.0.1:8081/api/v1/version'
```

### 密码登录开关

使用 `PUT /api/v1/me/password-login`，携带玩家会话并提交 `{"enabled":false}` 关闭当前账号的密码登录；提交 `{"enabled":true}` 重新开启。成功返回 `204 No Content`。关闭只禁用密码认证，不删除密码，SSH 公钥登录及 SSH 挑战网页登录不受影响。密码登录默认开启，兼容已有账号。

关闭前必须绑定至少一把 SSH 公钥；否则返回 `409 last_credential`。密码登录关闭时也不能删除最后一把 SSH 公钥。`GET /api/v1/me` 和登录响应中的账号对象包含 `password_login_enabled` 与 `has_ssh_key`，分别表示当前策略和是否至少绑定一把公钥。该策略适用于网页/API 密码登录与 SSH 密码认证。

### 账号约定

账号 ID 是稳定身份，用户名固定且区分大小写：新用户名只接受小写字母，3–20 位，允许数字、下划线和短横线，以字母开头。SSH 网关必须使用后端返回的账号 ID 标识玩家，不能使用客户端自报用户名或公钥指纹作为游戏账号 ID。

注册至少提供密码或公钥。密码为 10–72 个 UTF-8 字节，使用 bcrypt 哈希；不支持清空密码。公钥必须是单个标准 OpenSSH 公钥，不接受私钥、证书、多个公钥或 authorized_keys 选项。相同公钥不能绑定多个账号。同账号重复添加公钥幂等，返回 201。删除公钥必须保留至少一种登录凭证；已有密码的账号可以删除最后一把公钥。

账号、公钥时间均为 RFC 3339。列表格式为 `{ "items": [] }`，无数据返回空数组。接口暂不提供分页、改用户名、删除账号或找回密码。

### 公钥名称与使用时间

添加公钥可传 `{"public_key":"ssh-ed25519 AAAA...","name":"工作电脑"}`，注册时可传 `key_name` 为初始公钥命名。名称允许中文，去除首尾空白后最多 64 个 Unicode 字符，不允许控制字符；名称仅作备注，不要求唯一，不参与认证。

使用 `PATCH /api/v1/me/keys/{fingerprint}`，请求体 `{"name":"备用笔记本"}` 修改名称，`{"name":""}` 清除名称。缺少 name 或传 null 返回 400。添加同一把公钥时，省略 name 保留原名称；显式传 name 则更新名称。

列表、添加、单个查询和改名响应均包含以下字段：

```json
{
  "fingerprint": "SHA256:...",
  "public_key": "ssh-ed25519 AAAA...",
  "name": "工作电脑",
  "created_at": "2026-10-30T15:00:00+08:00",
  "last_used_at": null
}
```

新公钥未使用时 last_used_at 为 null。可信网关完成密钥认证登录，或成功确认 SSH 登录挑战后，后端更新实际使用的那一把公钥；密码登录、查询、改名、重复添加和失败认证均不刷新。正式密钥登录时，时间更新与会话写入在同一事务内完成，写入失败会回滚。

旧数据库自动添加名称字段，原账号、公钥和已记录时间保留。早期版本的 last_used_at 可能记录的是添加或绑定时间，无法追溯重建历史认证时间；升级后的新认证按上述规则记录。指纹中可能包含 `/` 和 `+`，必须将整个指纹作为一个路径段 URL 编码。只能查询或修改自己的公钥，跨账号操作返回 404。

```powershell
$fingerprint = [Uri]::EscapeDataString($savedKey.fingerprint)
$keyUrl = "$apiBase/api/v1/me/keys/$fingerprint"
$rename = @{ name = '工作电脑' } | ConvertTo-Json
Invoke-RestMethod $keyUrl -Method Patch -ContentType 'application/json' -Headers $headers -Body $rename
Invoke-RestMethod $keyUrl -Headers $headers
```

## Web 联调示例

下面的 PowerShell 示例使用本机测试密码：

```powershell
$apiBase = 'http://127.0.0.1:8081'
$registration = @{ username = 'alice'; password = 'local-test-password-123' } | ConvertTo-Json
Invoke-RestMethod "$apiBase/api/v1/accounts" -Method Post -ContentType 'application/json' -Body $registration

$login = Invoke-RestMethod "$apiBase/api/v1/auth/password" -Method Post -ContentType 'application/json' -Body $registration
$headers = @{ Authorization = "Bearer $($login.access_token)" }
Invoke-RestMethod "$apiBase/api/v1/me" -Headers $headers

$newPassword = @{ password = 'replacement-test-password-123' } | ConvertTo-Json
Invoke-RestMethod "$apiBase/api/v1/me/password" -Method Put -ContentType 'application/json' -Headers $headers -Body $newPassword

# 使用自己真实的 .pub 文件路径。只传公钥，不传私钥。
$publicKey = @{ public_key = Get-Content "$env:USERPROFILE/.ssh/id_ed25519.pub" -Raw } | ConvertTo-Json
$savedKey = Invoke-RestMethod "$apiBase/api/v1/me/keys" -Method Post -ContentType 'application/json' -Headers $headers -Body $publicKey
Invoke-RestMethod "$apiBase/api/v1/me/keys" -Headers $headers

$fingerprint = [Uri]::EscapeDataString($savedKey.fingerprint)
Invoke-RestMethod "$apiBase/api/v1/me/keys/$fingerprint" -Method Delete -Headers $headers
Invoke-RestMethod "$apiBase/api/v1/auth/logout" -Method Post -Headers $headers
```

密码登录返回：

```json
{
  "access_token": "<随机会话令牌>",
  "token_type": "Bearer",
  "expires_in": 86400,
  "account": {
    "id": "<账号ID>",
    "username": "alice",
    "display_name": "alice",
    "created_at": "2026-10-30T12:00:00+08:00"
  }
}
```

后续请求使用 `Authorization: Bearer <access_token>`。API 不读取登录 Cookie，不把令牌放进 URL。新会话有效期为 24 小时，无刷新令牌；过期后重新登录。退出只撤销当前令牌。修改密码默认保留会话，可通过 revoke_other_sessions 选择退出其他登录；删除公钥会同时撤销由该公钥登录的已关联会话。

## 登录会话管理

使用 `GET /api/v1/me/sessions` 查询当前账号所有未过期、未撤销的会话，按创建时间降序、同时间按 ID 降序返回：

```json
{
  "items": [{
    "id": "0123456789abcdef01234567",
    "auth_method": "ssh_key",
    "credential_fingerprint": "SHA256:...",
    "created_at": "2026-11-02T10:00:00+08:00",
    "expires_at": "2026-11-03T10:00:00+08:00",
    "last_seen_at": "2026-11-02T10:05:00+08:00",
    "source_ip": "127.0.0.1",
    "user_agent": "TTYFish-SSH-Gateway",
    "current": true
  }]
}
```

- `id` 是独立生成的 24 位十六进制管理 ID，不是登录令牌，也不包含令牌哈希。不能用此 ID 访问账号或游戏接口。
- `auth_method` 为 `password`、`ssh_key`、`ssh_challenge` 或历史来源不明的 `unknown`。
- `credential_fingerprint` 为实际认证使用的公钥指纹。密码会话和无法追溯的历史会话返回 null。
- `last_seen_at` 是后端最近成功验证此会话的时间，包含普通请求和 SSE 心跳，不代表用户最后按键时间。
- `source_ip` 和 `user_agent` 在会话创建时记录，后续请求不覆盖。来源默认采用直连 IP；只有配置的可信代理转发链、或已认证 SSH 网关提供的玩家 IP 才会替代它。旧记录不回填。User-Agent 是客户端声明，不是可信的设备识别。未知值为空字符串。
- Web 每个浏览器配置使用本地随机设备 ID。密码登录和 SSH 挑战领取会话时发送 `X-Device-ID`；列表把同一账号、同一浏览器配置的多个有效登录令牌合并为一行，以最近登录信息展示。多个标签页的令牌仍分别有效；撤销该行会退出此浏览器的所有登录。“退出其他所有登录”也按设备组保留当前浏览器，并以其他设备/未分组会话组数返回。不同浏览器配置、不同账号彼此独立；没有提供该头的旧客户端/SSH 网关仍按每次登录单独记录。
- 设备 ID 是浏览器生成且可伪造的分组标识，不是认证凭证或可信设备证明。升级前建立且没有设备 ID 的历史会话无法可靠追溯归并，会保留到过期或手动撤销。`current` 表示此设备组中至少有一个令牌正用于当前请求。

使用 `DELETE /api/v1/me/sessions/{id}` 撤销指定会话，成功返回 204。只允许撤销自己的会话，其他账号的 ID、不存在、已过期或已撤销的 ID 都返回 404；ID 格式不正确返回 400。允许撤销当前会话，成功响应后客户端应清除本地令牌，后续请求将返回 401。

使用 `POST /api/v1/me/sessions/revoke-others` 退出其他设备/浏览器，无需请求体，返回 `{"revoked_count":2}`。数量统计实际撤销的其他活跃设备组；对没有设备标识的旧客户端会按单个会话计数。重复调用返回 0，当前浏览器设备组保留，其他账号不受影响。

### 密码与公钥的关联策略

设置密码时可以显式退出其他登录：

```http
PUT /api/v1/me/password
Authorization: Bearer <access_token>
Content-Type: application/json

{"password":"replacement-password-123","revoke_other_sessions":true}
```

省略该字段或传 false，维持原行为，其他会话仍有效。传 true 时，密码写入与撤销其他会话在同一个事务内完成，保留当前会话；写入失败不会提前踢掉其他登录。撤销范围包含其他密码、SSH 密钥和 SSH 挑战会话。

删除公钥时，会在删除事务中撤销 credential_fingerprint 与该公钥匹配的会话，并清理由它确认但尚未领取的挑战。不会撤销其他公钥或密码会话。如果当前会话就是用被删除的公钥登录的，删除成功后它也立即失效。最后一种凭证的删除仍被拒绝，且不会撤销任何会话。

普通请求在下一次认证时返回 401。已经建立的 SSE 在下一次状态事件或最长约 5 秒后的心跳中发送 auth_expired 并关闭；网关应按现有失效流程处理。已经通过认证、正在执行的请求可能完成，撤销不回滚已提交的游戏动作。

### 数据升级与联调

启动后端会自动升级旧 sessions 表，给历史会话生成稳定的独立 ID，保留令牌有效性和原过期时间。能根据旧记录确认的登录方式会保留；无法追溯的方式标记 unknown，公钥关联、来源和客户端信息不猜测补齐。历史 last_seen_at 初始采用创建时间，后续认证时更新。缺少公钥关联的历史会话不能自动按密钥撤销，可通过会话管理接口主动撤销。

```powershell
$sessions = Invoke-RestMethod "$apiBase/api/v1/me/sessions" -Headers $headers
$sessions.items
# 保留当前登录，退出其余登录
Invoke-RestMethod "$apiBase/api/v1/me/sessions/revoke-others" -Method Post -Headers $headers
```

此阶段只提供后端 API，Web 仍需添加会话列表、退出指定登录，以及修改密码时的退出选项。刷新令牌尚未实现；认证限流策略见下文。

## SSH 网关联调约定

启动后端前，在同一个 PowerShell 窗口设置服务端凭证：

```powershell
$env:TTYFISH_GATEWAY_TOKEN = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
go run ./cmd/backend -addr 127.0.0.1:8081 -db backend.db
```

将同一个凭证通过服务端配置交给可信 SSH 网关；不提交到 Git、不放入前端代码。不配置时，内部密钥登录接口返回 `503 gateway_disabled`；非空但不足 32 字节时后端拒绝启动。

网关必须先验证 SSH 握手签名，确认玩家持有对应私钥，再请求：

```http
POST /api/v1/internal/auth/ssh
Authorization: Bearer <TTYFISH_GATEWAY_TOKEN>
Content-Type: application/json

{"username":"alice","public_key":"ssh-ed25519 AAAA..."}
```

后端检查用户名与该公钥所属账号完全匹配，成功返回与密码登录相同格式的玩家会话。网关凭证与玩家会话不能相互替代。此内部接口信任网关完成签名验证，本身不校验 SSH 签名；不能把它代理成任何人可调用的“提交公钥登录”接口。网关凭证具有代表玩家登录的权限。

SSH 握手包含未签名的公钥探测阶段。这个阶段应在同一接口中传 `verify_only:true`，仅返回 `{"account":{...}}`，不发放令牌、不记录使用时间。签名验证成功后，用默认 verify_only=false 完成正式登录。项目 SSH 网关已将正式登录移至握手完成后的会话阶段。预检查与正式登录的小额度分别计数，两者仍各消耗一次网关总预算。

网关调用密码登录时，在公共 `/api/v1/auth/password` 入口提供 `X-TTYFish-Gateway-Token`，内部密钥与挑战接口继续使用 Authorization Bearer 网关凭证。网关可同时提供 `X-TTYFish-Client-IP`（单个 IP，不含端口）；只有凭证正确时才信任该值。项目网关已从 SSH 连接的 RemoteAddr 提供玩家 IP，密码和密钥登录均使用独立网关总预算。普通客户端伪造来源头或 User-Agent 不会获得网关待遇，错误网关凭证也不能回退为公共密码尝试。

浏览器通过下方挑战流程使用 SSH 密钥登录，无需设置密码。现有 `cmd/hub` 仍使用原来的挑战链路，尚未迁移为独立 API 客户端；两套挑战凭证按用途隔离，不能相互确认或领取。

## 浏览器 SSH 挑战登录

1. 浏览器提交固定用户名，创建挑战：

   ```http
   POST /api/v1/auth/ssh/challenges
   Content-Type: application/json

   {"username":"alice"}
   ```

   返回 `201`：

   ```json
   {"username":"alice","code":"ABCD-2345","poll_token":"<64位小写十六进制凭证>","expires_in":90,"poll_interval_ms":1500}
   ```

   账号必须已存在且绑定公钥。浏览器根据部署配置显示命令，例如 `ssh -p 2222 alice@服务器地址 auth ABCD-2345`。后端不拼接服务器地址；此命令需要配套的 API 模式 SSH 网关支持。

2. 玩家在自己的电脑上执行命令。SSH 网关必须先完成私钥认证，再用服务端网关凭证确认：

   ```http
   POST /api/v1/internal/auth/ssh/challenges/confirm
   Authorization: Bearer <TTYFISH_GATEWAY_TOKEN>
   Content-Type: application/json

   {"username":"alice","public_key":"ssh-ed25519 AAAA...","code":"ABCD-2345"}
   ```

   `username` 和 `public_key` 必须来自已通过认证的 SSH 连接，不能从命令参数中直接信任。确认码允许小写和首尾空白；成功返回 `204`，终端提示“已确认，请回浏览器继续”。只接受密钥身份，不接受密码认证连接冒充密钥确认。确认接口不返回玩家会话或浏览器查询凭证。

3. 浏览器每隔至少 `poll_interval_ms` 查询：

   ```http
   POST /api/v1/auth/ssh/challenges/poll
   Content-Type: application/json

   {"poll_token":"<创建挑战时返回的凭证>"}
   ```

   返回 `200`，状态如下：

   - `{"state":"pending"}`：等待终端确认，继续轮询。
   - `{"state":"ok","session":{...}}`：已确认，`session` 与密码登录响应格式相同，保存会话后停止轮询。
   - `{"state":"expired"}`：凭证不存在、已过期、已领取或确认的公钥已被删除，停止轮询并提示重新获取。

挑战从创建开始总计 90 秒有效，确认不会延长时间。终端确认码与浏览器查询凭证分开，数据库中都只保存哈希。不要把 `poll_token` 放入 URL、终端命令或传给 SSH 网关。

会话写入和挑战消费在同一个数据库事务完成。并发轮询只能有一个请求获得会话；再次领取返回 `expired`。如果浏览器在成功响应返回时断网，不能重放领取令牌，需要重新发起挑战。确认的公钥被撤销后，尚未领取的挑战也不能换取会话。

## 错误与请求限制

错误统一为：

```json
{"error":{"code":"invalid_credentials","message":"账号或登录凭证错误"}}
```

| HTTP | 常见 code | 客户端处理 |
|---|---|---|
| 400 | `invalid_request` | 修正字段或 JSON |
| 401 | `invalid_credentials` | 提示登录失败或重新登录 |
| 401 | `gateway_unauthorized` | 检查网关服务端凭证 |
| 403 | `origin_denied` | 检查 CORS 来源配置 |
| 404 | `not_found` | 资源不存在或不属于当前账号 |
| 405 | `method_not_allowed` | 检查 HTTP 方法 |
| 409 | `account_conflict` / `last_credential` | 提示用户名、公钥冲突或需要保留凭证 |
| 409 | `challenge_invalid` | 确认码无效、已使用、已过期或账号公钥不匹配 |
| 413 / 415 | `request_too_large` / `unsupported_media_type` | 限制请求体、使用 JSON |
| 429 | `rate_limited` | 等待 Retry-After 指定的秒数 |
| 500 | `internal_error` | 提示稍后重试，服务端查看日志 |
| 503 | `gateway_disabled` | 配置网关凭证 |

JSON 请求体最多 16 KiB，不接受未知字段或多个 JSON 值。认证限流已从原来的共享 10 次预算改为下列分层策略，成功与失败请求均计数。

| 操作 | 默认每分钟预算 |
|---|---|
| 注册 | 每来源 10 次，独立于登录 |
| 公共密码登录、创建 SSH 挑战 | 每来源合计 120 次 |
| 密码登录 | 每来源与账号组合 10 次；同账号跨来源合计 30 次 |
| 已认证网关的密码、密钥预检查、密钥登录、挑战确认 | 同一配置网关凭证总计 600 次 |
| 密钥预检查 | 每来源与账号组合 20 次；同账号跨来源 60 次 |
| 正式密钥登录 | 独立于预检查，每来源与账号组合 20 次；同账号跨来源 60 次 |
| 创建挑战、确认挑战 | 两个操作分别每来源与账号组合 10 次；同账号跨来源 30 次 |
| 修改密码 | 每个已认证账号 10 次，不与其他账号共享小额度 |
| 挑战轮询 | 每来源合计 1200 次；每个 poll_token 跨来源 60 次 |
| 错误网关凭证 | 每来源 10 次，与正常公共登录预算分开 |

网关总预算不是无限放行，账号和来源组合的小额度仍然生效。用户名只在配额键中去除首尾空白、转换小写；实际认证仍按原固定用户名严格匹配。账号级预算可以限制更换 IP 的连续尝试，达到预算后需等待窗口结束。

每个计数器窗口从首次请求起持续 60 秒。429 返回 `rate_limited` 和实际剩余时间的 `Retry-After`，按秒向上取整，不再固定写 60。跨源响应通过 Access-Control-Expose-Headers 暴露该头；网关 SDK 返回携带 RetryAfterSeconds 的 RateLimitError，不自动重试密码、一次性确认等请求。前端可读取该头或按 API 客户端能力实现等待提示。

限流计数在单个后端进程内存中，重启后重置，不是永久封禁，也不是分布式限流。最多保存 20000 个计数器；满额时清理过期项，不能通过淘汰活跃项来重置额度，无法分配新额度时返回 429。

### 可信代理与来源解析

默认忽略 X-Forwarded-For、X-Real-IP 和未认证的网关来源声明。仅当直连地址属于 `-trusted-proxies` 时，才从 X-Forwarded-For 的右侧向左遍历，跳过已知代理，选择第一个非可信地址；不会直接采用客户端可伪造的最左值。就近地址格式异常或链条过长时保守回退到直连地址。

只信任实际反向代理使用的明确 CIDR，不接受 `0.0.0.0/0` 或 `::/0`。代理必须覆盖来源头，或将实际连接地址追加到链条右端，不能原样透传用户提供的值。SSH 网关来源头另由网关凭证授权，不依赖代理 CIDR；旧网关未提供玩家来源时，兼容采用直连/可信代理解析地址，但建议同时升级网关。

例如反向代理确实从本机回环地址连接后端时：

```powershell
go run ./cmd/backend -addr 127.0.0.1:8081 -trusted-proxies '127.0.0.1/32,::1/128' -rate-limit-auth-ip 120 -rate-limit-gateway 600
```

两个总预算参数可按部署容量调整，0 表示使用默认值，负数会拒绝启动。其余预算可通过 Go 的 api.Config.RateLimits 配置。生产代理可能来自容器网段，应按实际直连地址配置，不要照抄回环地址或信任整个公网；CORS 来源白名单仍需独立配置。

## 验证与下一阶段

```powershell
cd hub
go test ./... -count=1
```

API 测试使用真实 SQLite，覆盖账号生命周期、网关权限、玩家令牌隔离、公钥归属、最后凭证保护、密码修改、CORS、JSON 校验与限流。旧 Web/SSH 测试继续运行，防止引入回归。

挑战测试覆盖浏览器发起 → 网关确认 → 浏览器领取会话、错误身份、重放、过期、公钥撤销、旧协议隔离、两个数据库连接并发领取，以及会话写入失败后可重试。

房间和游戏 API 已接入同一后端，详见 [房间协议](ROOM_API.md)。下一步把原 `cmd/hub` 的接入层迁移到 API，停止直接访问数据库与房间实现。
