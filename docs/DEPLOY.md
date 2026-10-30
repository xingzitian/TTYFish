# 部署

线上：`159.75.78.7`，Debian 12，systemd 托管。

## 日常发布

```bash
git tag v0.1.3 && git push origin v0.1.3
```

一次发布带三样东西：

| 资产 | 内容 |
|---|---|
| `backend-linux-amd64` | 账号 + 房间 + 规则 |
| `web.tar.gz` | 前端构建产物（`VITE_SSH_HOST` 在构建时注入） |
| `hub-linux-amd64` | SSH 网关 |

CI 做三件事：

1. 构建后端和网关，`npm ci && npm run build` 构建前端，一起发布成 GitHub Release
2. SSH 进服务器，把 `deploy/deploy.sh` 推上去
3. 执行 `deploy.sh <tag>`：下载三个资产 → 逐个校验 → 有人在玩记一行日志 →
   **按 后端 → 前端 → 网关 的顺序**替换并检查 → 任一步失败自动回滚

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
| CI 直接 `scp` 15MB 二进制 | 跨境只有几十 KB/s，传一次十几分钟 |
| 服务器定时轮询 Release | CI 本来就能连上机器，多一条链路没有收益 |
| 在服务器上编译 | 能跑，但占生产机 CPU，而且产物不是 CI 验过的那份 |
| 直连 GitHub 下载 | 实测只有 **5 KB/s**，15MB 传不完 |
| Actions artifact | 下载需要鉴权，90 天过期 |

## 下载速度实测（2026-10-30，159.75.78.7）

| 路径 | 速度 | 说明 |
|---|---|---|
| 直连 GitHub | 5 KB/s | 2.4MB 的包 55 秒没下完 |
| 走本机 mihomo（127.0.0.1:7890） | 3 MB/s | 13MB 的包 4.3 秒 |

所以 `deploy.sh` 里**必须走代理**：先试 `127.0.0.1:7890`，失败才直连。
15MB 的二进制走代理约 5 秒；直连那一档实际上等于不可用。

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

- **`scp` 的端口是大写 `-P`**，写成 `-p` 会把端口当成文件名
- **`authorized_keys` 里要放公钥内容**，不是 `ssh-keygen -lf` 打印的指纹
- **私钥从 Windows 粘进 secret 会带 `\r`**，`ssh` 会拒绝；脚本里统一 `tr -d '\r'`
- **`hub/go.mod` 要 1.26**，服务器和 CI 都得是 1.26
