#!/usr/bin/env bash
# 服务器侧自助更新：每分钟看一眼 GitHub 上有没有新的 Release，有就下载、校验、
# 原子替换、重启、健康检查，失败自动回滚。
#
# 设计取舍：
#   - 下载 CI 构建好的二进制，不在生产机上编译（省 CPU，产物就是 CI 验过的那份）
#   - 走本机 mihomo 代理，跨境下载才快
#   - 有人在游戏里就等下一分钟，绝不把正在打的人踢掉
#   - 编译/启动失败的版本记进 .bad，不会每分钟重试同一个坏版本
set -uo pipefail

REPO="xingzitian/TTYFish"
BIN_DIR="/opt/ttyfish"
STATE="$BIN_DIR/.version"
BAD="$BIN_DIR/.bad"
LOCK="$BIN_DIR/.update.lock"
SSH_PORT=12331
HEALTH="http://127.0.0.1:12332/healthz"
PROXY="http://127.0.0.1:7890"

export https_proxy="$PROXY" http_proxy="$PROXY"
export GIT_TERMINAL_PROMPT=0

log() { echo "[$(date '+%F %T')] $*"; }

# 防止两次更新叠着跑（一次下载+重启可能超过一分钟）
exec 9>"$LOCK"
if ! flock -n 9; then
	log "上一次更新还没结束，跳过"
	exit 0
fi

# 1. 最新 tag（用 git ls-remote，不消耗 GitHub API 配额）
LATEST=$(git ls-remote --tags --refs "https://github.com/$REPO.git" 2>/dev/null |
	sed 's#.*refs/tags/##' | sort -V | tail -1)
if [ -z "$LATEST" ]; then
	log "查不到远端 tag（网络问题？），等下一分钟"
	exit 0
fi

CUR=$(cat "$STATE" 2>/dev/null || true)
[ "$LATEST" = "$CUR" ] && exit 0

# 坏版本不重试，等有更新的 tag 再说
if [ -f "$BAD" ] && [ "$(cat "$BAD")" = "$LATEST" ]; then
	exit 0
fi

# 2. 有人在玩就等
ACTIVE=$(ss -Htn state established "( sport = :$SSH_PORT )" 2>/dev/null | wc -l)
if [ "$ACTIVE" -gt 0 ]; then
	log "$LATEST 待部署，但 12331 上还有 $ACTIVE 个连接，等下一分钟"
	exit 0
fi

log "发现新版本 $LATEST（当前 ${CUR:-无}）"

# 3. 下载 release 资产（CI 可能还在构建，404 就静默等下一分钟）
BASE="https://github.com/$REPO/releases/download/$LATEST"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if ! curl -fsSL --max-time 300 -o "$TMP/hub" "$BASE/hub-linux-amd64"; then
	log "下载失败（可能 CI 还在构建），等下一分钟"
	exit 0
fi
if ! curl -fsSL --max-time 60 -o "$TMP/sum" "$BASE/hub-linux-amd64.sha256"; then
	log "下载校验文件失败，放弃这一轮"
	exit 0
fi

# 4. 校验 sha256——下载来的东西必须和 CI 算出来的一致
EXPECT=$(cut -d' ' -f1 "$TMP/sum")
ACTUAL=$(sha256sum "$TMP/hub" | cut -d' ' -f1)
if [ "$EXPECT" != "$ACTUAL" ]; then
	log "校验失败：期望 $EXPECT 实际 $ACTUAL，不部署"
	echo "$LATEST" >"$BAD"
	exit 1
fi
chmod +x "$TMP/hub"

# 5. 原子替换 + 重启
[ -f "$BIN_DIR/hub" ] && cp -p "$BIN_DIR/hub" "$BIN_DIR/hub.prev"
mv "$TMP/hub" "$BIN_DIR/hub"
sudo -n systemctl restart ttyfish

# 6. 健康检查，失败回滚
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sf --max-time 3 "$HEALTH" >/dev/null; then
		echo "$LATEST" >"$STATE"
		log "已切换到 $LATEST"
		exit 0
	fi
	sleep 1
done

log "健康检查失败，回滚"
if [ -f "$BIN_DIR/hub.prev" ]; then
	mv "$BIN_DIR/hub.prev" "$BIN_DIR/hub"
	sudo -n systemctl restart ttyfish
fi
echo "$LATEST" >"$BAD"
exit 1
