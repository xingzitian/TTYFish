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
- OpenAPI：`GET /api/openapi.json`，可导入支持 OpenAPI 3.0 的 API 工具。
- [OpenAPI 源文件](../hub/internal/api/openapi.json)。
- 默认数据库为 `backend.db`。如需使用现有账号，先备份，再显式指定 `-db hub.db`。数据表向后兼容，不会自动改用户名。
- Web 来源必须准确匹配 `-cors-origins`；多个来源用逗号分隔。默认不允许带 Origin 的浏览器跨源请求；命令行及服务端请求可直接调用。不信任 `X-Forwarded-For`，限流按直连来源 IP 计算。
- 公网部署通过 HTTPS 提供 API。网关凭证只放在服务端环境变量中。

## 接口清单

| 方法 | 路径 | 认证 | 用途 |
|---|---|---|---|
| POST | `/api/v1/accounts` | 无 | 注册，返回账号，不自动创建登录会话 |
| POST | `/api/v1/auth/password` | 无 | 用户名和密码登录 |
| GET | `/api/v1/me` | 玩家 Bearer | 当前账号 |
| POST | `/api/v1/auth/logout` | 玩家 Bearer | 撤销当前会话 |
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

后续请求使用 `Authorization: Bearer <access_token>`。API 不读取登录 Cookie，不把令牌放进 URL。会话有效期为 24 小时，无刷新令牌；过期后重新登录。退出只撤销当前令牌。修改密码和删除公钥影响后续认证，不撤销此前发放的令牌。

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

SSH 握手包含未签名的公钥探测阶段。这个阶段应在同一接口中传 `verify_only:true`，仅返回 `{"account":{...}}`，不发放令牌、不记录使用时间。签名验证成功后，用默认 verify_only=false 完成正式登录。项目 SSH 网关已将正式登录移至握手完成后的会话阶段。预检查与正式登录各计一次认证预算。

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

JSON 请求体最多 16 KiB，不接受未知字段或多个 JSON 值。注册、密码登录、密码设置、网关密钥登录、挑战创建与确认按直连来源 IP 共用每分钟 10 次预算；失败请求也计数。挑战轮询使用独立的每分钟 120 次预算，不会消耗登录预算。Web 反向代理和 SSH 网关的所有玩家暂时会共用该入口 IP 的预算，扩展网关接入时需要调整策略。

## 验证与下一阶段

```powershell
cd hub
go test ./... -count=1
```

API 测试使用真实 SQLite，覆盖账号生命周期、网关权限、玩家令牌隔离、公钥归属、最后凭证保护、密码修改、CORS、JSON 校验与限流。旧 Web/SSH 测试继续运行，防止引入回归。

挑战测试覆盖浏览器发起 → 网关确认 → 浏览器领取会话、错误身份、重放、过期、公钥撤销、旧协议隔离、两个数据库连接并发领取，以及会话写入失败后可重试。

房间和游戏 API 已接入同一后端，详见 [房间协议](ROOM_API.md)。下一步把原 `cmd/hub` 的接入层迁移到 API，停止直接访问数据库与房间实现。
