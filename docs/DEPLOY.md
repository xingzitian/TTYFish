# 部署

线上：`159.75.78.7`，Debian 12，systemd 托管。

## 日常测试部署

推送到 `main` 后，CI 构建并验证后端、网关和前端。CI 成功后，部署工作流从**同一 CI run**
下载 Actions artifacts，校验后上传到私有 Cloudflare R2 bucket；服务器通过短时效预签名 URL
拉取产物。整个部署不会重新编译或创建 Release/prerelease，单纯推送 `v*` tag 不会触发部署。

CI 为同一 commit 上传三样部署产物（保留 7 天）：

| 资产 | 内容 |
|---|---|
| `backend-linux-amd64` | 账号 + 房间 + 规则 |
| `web.tar.gz` | 前端构建产物（`VITE_SSH_HOST` 在构建时注入） |
| `hub-linux-amd64` | SSH 网关 |

部署工作流会：

1. 仅接受 `main` 上成功且由 push 触发的 CI run；不处理 PR 或其他事件
2. 下载该 run 的二进制和前端 artifacts，校验 SHA256，再将它们打包
3. 把压缩产物上传到私有 R2；生成 15 分钟有效的预签名 URL，经 SSH 传给服务器
4. 服务器从 R2 下载后再次校验 SHA256 → 有人在玩记一行日志 →
   **按 后端 → 前端 → 网关 的顺序**替换并检查 → 任一步失败自动回滚

### R2 一次性配置

部署工作流使用 Cloudflare Account ID `c7271c3053a80ea7bf1206cbb25cf45e` 和 bucket `ttyfish`。
bucket 应保持私有，不要启用公开访问。Cloudflare Dashboard → R2 → Manage R2 API Tokens
创建仅对 `ttyfish` bucket 有 **Object Read & Write** 权限的 token；不需要账户级管理权限。

在 GitHub 仓库 `Settings → Secrets and variables → Actions → New repository secret` 中添加：

| Secret 名称 | 值 |
|---|---|
| `R2_ACCESS_KEY_ID` | R2 S3 API token 的 Access Key ID |
| `R2_SECRET_ACCESS_KEY` | 对应的 Secret Access Key |

Secret Access Key 只在创建时显示；直接粘贴到 GitHub Secret，不要提交到仓库或放在服务器上。
部署 SSH key 仍使用现有 `TTYFISH_DEPLOY` secret。建议在 R2 bucket 配置对象生命周期规则，
删除 `deploy/` 前缀下超过 7 天的对象，避免积累过期部署包。

## 正式版本发布

正式 Release 由操作者显式发布；`.github/workflows/release.yml` 会为已发布 tag 构建并上传资产，
但不会部署到服务器。测试部署工作流不会创建或发布 GitHub Release。

**为什么是这个顺序**：前端要用后端的新接口，所以排后端之后；网关重启会踢掉正在
玩的人，所以压最后。前端的健康检查是 `curl 127.0.0.1:12332/` 和 `/account` 都要 200
——后者能顺带验证 SPA 回退有没有配错。

`VITE_SSH_HOST` / `VITE_SSH_PORT` 取仓库变量，没设就默认 `159.75.78.7` / `12331`。
它们决定登录页显示的那条 `ssh` 命令，改错的表现是"所有人都说连不上"。

失败时看 Actions 里的「部署到服务器」那一步，或者上机器：

```bash
journalctl -u ttyfish -n 50 --no-pager
```

**有人在玩也照推**——发版是自己挑的时间窗口，脚本只记一行日志方便事后对账，不会拦。重启会把在线的 SSH 会话踢掉，这是预期行为。

## 为什么不走别的路子

| 方案 | 问题 |
|---|---|
| 服务器直接下载 Actions artifact | 需要给服务器提供 GitHub API 凭证，因此不采用 |
| 在服务器上编译 | 占用生产机 CPU，而且部署的不是 CI 验过的那份 |
| 从 Release 下载 | 与构建及 CI run 脱节，且测试部署不应创建 Release |

产物内容由服务器从 R2 下载，避免将大型文件跨境 SCP。Actions 只通过 SSH 发送短时效签名 URL；
服务器不持有 R2 长期凭证。R2 对象按 commit/run 唯一命名，建议配合上述生命周期规则自动清理。

## 服务器上的一次性设置

现在是**两个进程**：

| 服务 | 作用 | 监听 |
|---|---|---|
| `ttyfish-backend` | 账号、房间、UNO 规则 | `127.0.0.1:8082` |
| `ttyfish` | SSH 网关（只渲染，不跑规则） | `0.0.0.0:12331` |

网关启动时要连后端，所以 `ttyfish.service` 里写了 `Requires=ttyfish-backend.service`。

```bash
# 运行账号（两个服务共用，无 shell）
useradd -r -s /usr/sbin/nologin -d /var/lib/ttyfish ttyfish
mkdir -p /opt/ttyfish /var/lib/ttyfish
chown ttyfish:ttyfish /var/lib/ttyfish

# 部署账号：能换二进制、能重启这两个服务
useradd -r -m -s /bin/bash deploy
chown deploy:deploy /opt/ttyfish
# ~/.ssh/authorized_keys 放 CI 那把公钥，权限 600

# 网关凭证：后端用它签发玩家令牌，网关用它换令牌，两边必须是同一个
mkdir -p /etc/ttyfish
echo "TTYFISH_GATEWAY_TOKEN=$(openssl rand -hex 32)" > /etc/ttyfish/gateway.env
chmod 600 /etc/ttyfish/gateway.env

cat >/etc/sudoers.d/ttyfish-deploy <<'EOF'
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart ttyfish, /usr/bin/systemctl restart ttyfish-backend, /usr/bin/systemctl status ttyfish, /usr/bin/systemctl status ttyfish-backend, /usr/bin/systemctl is-active ttyfish, /usr/bin/systemctl is-active ttyfish-backend
EOF
chmod 440 /etc/sudoers.d/ttyfish-deploy && visudo -cf /etc/sudoers.d/ttyfish-deploy
```

两个 unit 的完整内容在服务器 `/etc/systemd/system/ttyfish{,-backend}.service`，
要点：都跑在 `ttyfish` 账号下、都 `EnvironmentFile=/etc/ttyfish/gateway.env`、
都用 `StateDirectory=ttyfish` 把 `/var/lib/ttyfish` 挂成可写。

端口：SSH `12331`（对外）、后端 `8082`（只绑回环，不需要放行）。
腾讯云安全组和 1Panel 防火墙只要放行 `12331`。

## 踩过的坑

认证限流采用来源、账号与已认证网关的分层预算。backend 可用 `-rate-limit-auth-ip`（默认 120/分钟）和 `-rate-limit-gateway`（默认 600/分钟）调整总预算。网关应同步升级，以便转发实际 SSH 客户端 IP。

反向代理后若要按玩家 IP 限流，需在 backend 启动参数中显式设置 `-trusted-proxies` 为真实代理地址的 CIDR。默认忽略转发头；不能信任所有公网地址，也不能根据请求中的 User-Agent 判断网关身份。代理必须覆盖来源头或在链条右端追加实际连接地址，例如 Nginx 的 `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`。容器代理的地址未必是回环地址，请以实际连接为准。具体默认额度、来源解析与 Retry-After 约定见 [后端 API](BACKEND_API.md)。

- **`scp` 的端口是大写 `-P`**，写成 `-p` 会把端口当成文件名
- **`authorized_keys` 里要放公钥内容**，不是 `ssh-keygen -lf` 打印的指纹
- **私钥从 Windows 粘进 secret 会带 `\r`**，`ssh` 会拒绝；脚本里统一 `tr -d '\r'`
- **`hub/go.mod` 要 1.26**，服务器和 CI 都得是 1.26
