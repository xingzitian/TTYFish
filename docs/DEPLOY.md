# 部署

线上：`159.75.78.7`，Debian 12，systemd 托管。

## 日常发布

```bash
git tag v0.1.3 && git push origin v0.1.3
```

然后 CI 做三件事：

1. 构建 `linux/amd64`，发布成 GitHub Release（资产名固定：`hub-linux-amd64`）
2. SSH 进服务器，把 `deploy/deploy.sh` 推上去
3. 执行 `deploy.sh <tag>`：下载资产 → 校验 sha256 → 有人在玩就拒绝 → 原子替换 → 重启 → 健康检查 → 失败自动回滚

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

已经做过了，换机器时照抄：

```bash
# 运行账号（无 shell）
useradd -r -s /usr/sbin/nologin -d /var/lib/ttyfish ttyfish
mkdir -p /opt/ttyfish /var/lib/ttyfish
chown ttyfish:ttyfish /var/lib/ttyfish

# 部署账号
useradd -r -m -s /bin/bash deploy
chown deploy:deploy /opt/ttyfish          # 要能换二进制
# ~/.ssh/authorized_keys 放 CI 那把公钥，权限 600

# 只给它重启本服务的权限
cat >/etc/sudoers.d/ttyfish-deploy <<'EOF'
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart ttyfish, /usr/bin/systemctl status ttyfish, /usr/bin/systemctl is-active ttyfish
EOF
chmod 440 /etc/sudoers.d/ttyfish-deploy && visudo -cf /etc/sudoers.d/ttyfish-deploy

# 服务
cat >/etc/systemd/system/ttyfish.service <<'EOF'
[Unit]
Description=TTYFish platform (SSH games + web)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ttyfish
Group=ttyfish
WorkingDirectory=/opt/ttyfish
ExecStart=/opt/ttyfish/hub -addr :12331 -http :12332 -db /var/lib/ttyfish/hub.db -host-key /var/lib/ttyfish/host_ed25519 -ssh-hint "-p 12331 你的用户名@159.75.78.7"
Restart=always
RestartSec=2
StateDirectory=ttyfish
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
MemoryMax=512M
TasksMax=1024

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload && systemctl enable --now ttyfish
```

端口：SSH `12331`、HTTP `12332`。腾讯云安全组和 1Panel 防火墙都要放行。

## 踩过的坑

- **`scp` 的端口是大写 `-P`**，写成 `-p` 会把端口当成文件名
- **`authorized_keys` 里要放公钥内容**，不是 `ssh-keygen -lf` 打印的指纹
- **私钥从 Windows 粘进 secret 会带 `\r`**，`ssh` 会拒绝；脚本里统一 `tr -d '\r'`
- **`hub/go.mod` 要 1.26**，服务器和 CI 都得是 1.26
