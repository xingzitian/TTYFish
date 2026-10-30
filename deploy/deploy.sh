#!/usr/bin/env bash
# 服务器侧的部署脚本：把指定 tag 的 Release 资产换上来。
#
#   /opt/ttyfish/deploy.sh v0.1.5
#
# 由 CI 通过 SSH 调用（也方便手动执行）。两件事：
#   1. 下载并替换两个二进制：backend（账号+房间）和 hub（SSH 网关）
#   2. 重启两个服务，逐个健康检查，失败自动回滚
#
# 不在生产机上编译，也不从 CI 传几十 MB 的二进制过来——这个方向跨境只有几十 KB/s，
# 让服务器经本机 mihomo 从 GitHub Release 拉才是秒级。
set -uo pipefail

TAG=${1:-}
if [ -z "$TAG" ]; then
	echo "用法: $0 <tag>（例如 v0.1.5）" >&2
	exit 2
fi

REPO="xingzitian/TTYFish"
BIN_DIR="/opt/ttyfish"
SSH_PORT=12331
BACKEND_HEALTH="http://127.0.0.1:8082/healthz"
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

# 发版是自己挑的时间窗口，有人在玩也照推——只留一行日志方便事后对账
ACTIVE=$(ss -Htn state established "( sport = :$SSH_PORT )" 2>/dev/null | wc -l)
if [ "$ACTIVE" -gt 0 ]; then
	log "注意：$SSH_PORT 上还有 $ACTIVE 个连接，仍按计划强推（会踢掉这些人）"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

BASE="https://github.com/$REPO/releases/download/$TAG"
export https_proxy="$PROXY" http_proxy="$PROXY"
# 下载走代理，但本地健康检查绝对不能走——否则 curl 会把 127.0.0.1 也塞给 mihomo
export no_proxy="127.0.0.1,localhost" NO_PROXY="127.0.0.1,localhost"

log "下载 $TAG"
for name in backend hub; do
	if ! fetch "$BASE/$name-linux-amd64" "$TMP/$name"; then
		log "下载 $name 失败（Release 还没建好？）"
		exit 1
	fi
	if ! fetch "$BASE/$name-linux-amd64.sha256" "$TMP/$name.sum"; then
		log "下载 $name 的校验文件失败"
		exit 1
	fi
	expect=$(cut -d' ' -f1 "$TMP/$name.sum")
	actual=$(sha256sum "$TMP/$name" | cut -d' ' -f1)
	if [ "$expect" != "$actual" ]; then
		log "$name sha256 不匹配：期望 $expect，实际 $actual"
		exit 1
	fi
	chmod +x "$TMP/$name"
done

# 两个二进制一起换好，再按"先后端、后网关"的顺序重启
for name in backend hub; do
	[ -f "$BIN_DIR/$name" ] && cp -p "$BIN_DIR/$name" "$BIN_DIR/$name.prev"
	mv "$TMP/$name" "$BIN_DIR/$name"
done

log "重启后端"
sudo -n systemctl restart ttyfish-backend
ok=0
for _ in $(seq 1 10); do
	if curl -sf --max-time 3 --noproxy '*' "$BACKEND_HEALTH" >/dev/null; then
		ok=1
		break
	fi
	sleep 1
done
if [ "$ok" -ne 1 ]; then
	log "后端没起来，回滚两个二进制"
	for name in backend hub; do
		[ -f "$BIN_DIR/$name.prev" ] && mv "$BIN_DIR/$name.prev" "$BIN_DIR/$name"
	done
	sudo -n systemctl restart ttyfish-backend
	sudo -n systemctl restart ttyfish
	exit 1
fi
log "后端健康"

log "重启网关"
sudo -n systemctl restart ttyfish

ok=0
for _ in $(seq 1 10); do
	if systemctl is-active --quiet ttyfish; then
		ok=1
		break
	fi
	sleep 1
done
if [ "$ok" -ne 1 ]; then
	log "网关没起来，回滚"
	[ -f "$BIN_DIR/hub.prev" ] && mv "$BIN_DIR/hub.prev" "$BIN_DIR/hub"
	sudo -n systemctl restart ttyfish
	exit 1
fi

echo "$TAG" >"$BIN_DIR/.version"
log "已切换到 $TAG"
