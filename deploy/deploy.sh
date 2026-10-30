#!/usr/bin/env bash
# 服务器侧的部署脚本：把指定 tag 的 Release 资产换上来。
#
#   /opt/ttyfish/deploy.sh v0.1.3
#
# 由 CI 通过 SSH 调用（也方便手动执行）。它只做四件事：
#   下载 → 校验 sha256 → 有人在玩就拒绝 → 原子替换 + 重启 + 健康检查 + 失败回滚
#
# 不在生产机上编译，也不从 CI 传 15MB 的二进制过来——这个方向跨境只有几十 KB/s。
set -uo pipefail

TAG=${1:-}
if [ -z "$TAG" ]; then
	echo "用法: $0 <tag>（例如 v0.1.3）" >&2
	exit 2
fi

REPO="xingzitian/TTYFish"
BIN_DIR="/opt/ttyfish"
SSH_PORT=12331
HEALTH="http://127.0.0.1:12332/healthz"
PROXY="http://127.0.0.1:7890"

log() { echo "[$(date '+%F %T')] $*"; }

# 优先走 mihomo；代理不通就直连再试一次
fetch() {
	if curl -fsSL --max-time 300 -o "$2" "$1"; then
		return 0
	fi
	log "经代理失败，改直连重试"
	curl -fsSL --max-time 300 --noproxy '*' -o "$2" "$1"
}

# 发版是自己挑的时间窗口，有人在玩也照推——只留一行日志，方便事后对账
ACTIVE=$(ss -Htn state established "( sport = :$SSH_PORT )" 2>/dev/null | wc -l)
if [ "$ACTIVE" -gt 0 ]; then
	log "注意：12331 上还有 $ACTIVE 个连接，仍按计划强推（会踢掉这些人）"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

BASE="https://github.com/$REPO/releases/download/$TAG"
export https_proxy="$PROXY" http_proxy="$PROXY"

log "下载 $TAG"
if ! fetch "$BASE/hub-linux-amd64" "$TMP/hub"; then
	log "下载二进制失败（Release 还没建好？）"
	exit 1
fi
if ! fetch "$BASE/hub-linux-amd64.sha256" "$TMP/sum"; then
	log "下载校验文件失败"
	exit 1
fi

EXPECT=$(cut -d' ' -f1 "$TMP/sum")
ACTUAL=$(sha256sum "$TMP/hub" | cut -d' ' -f1)
if [ "$EXPECT" != "$ACTUAL" ]; then
	log "sha256 不匹配：期望 $EXPECT，实际 $ACTUAL"
	exit 1
fi
chmod +x "$TMP/hub"

[ -f "$BIN_DIR/hub" ] && cp -p "$BIN_DIR/hub" "$BIN_DIR/hub.prev"
mv "$TMP/hub" "$BIN_DIR/hub"
sudo -n systemctl restart ttyfish

for _ in $(seq 1 10); do
	if curl -sf --max-time 3 "$HEALTH" >/dev/null; then
		echo "$TAG" >"$BIN_DIR/.version"
		log "已切换到 $TAG"
		exit 0
	fi
	sleep 1
done

log "健康检查失败，回滚"
if [ -f "$BIN_DIR/hub.prev" ]; then
	mv "$BIN_DIR/hub.prev" "$BIN_DIR/hub"
	sudo -n systemctl restart ttyfish
fi
exit 1
