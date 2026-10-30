#!/usr/bin/env bash
# 一次性安装：在服务器上以 root 跑，装好自助更新。
#
#   sudo bash deploy/install.sh
#
# 装完之后：打 tag → CI 建 Release → 服务器一分钟内自己切过去。
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
	echo "用 root 跑：sudo bash deploy/install.sh"
	exit 1
fi

HERE=$(cd "$(dirname "$0")" && pwd)
SRC=${SRC:-/opt/ttyfish/src}

# 运行账号和目录（已经建过就跳过）
id ttyfish >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d /var/lib/ttyfish ttyfish
id deploy >/dev/null 2>&1 || { echo "没有 deploy 用户，先按文档建一个"; exit 1; }
mkdir -p /opt/ttyfish /var/lib/ttyfish
chown ttyfish:ttyfish /var/lib/ttyfish

# 更新脚本要能写 /opt/ttyfish（换二进制、写 .version）
chown deploy:deploy /opt/ttyfish
install -m 755 "$HERE/update.sh" /opt/ttyfish/update.sh

# 只给 deploy 重启本服务的权限
cat >/etc/sudoers.d/ttyfish-deploy <<'EOF'
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart ttyfish, /usr/bin/systemctl status ttyfish, /usr/bin/systemctl is-active ttyfish
EOF
chmod 440 /etc/sudoers.d/ttyfish-deploy
visudo -cf /etc/sudoers.d/ttyfish-deploy >/dev/null

install -m 644 "$HERE/ttyfish-update.service" /etc/systemd/system/
install -m 644 "$HERE/ttyfish-update.timer" /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now ttyfish-update.timer

echo
echo "装好了。状态："
systemctl list-timers ttyfish-update.timer --no-pager | head -3
echo
echo "手动跑一次看看：sudo -u deploy /opt/ttyfish/update.sh"
echo "看日志：journalctl -u ttyfish-update -f"
echo
echo "源码目录（可选，只用于看代码）：$SRC"
