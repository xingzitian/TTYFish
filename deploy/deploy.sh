#!/usr/bin/env bash
# 服务器侧的部署脚本：把指定 tag 的 Release 资产换上来。
#
#   /opt/ttyfish/deploy.sh v0.1.6
#
# 由 CI 通过 SSH 调用（也方便手动执行）。三样东西：
#   1. backend  账号 + 房间 + 规则
#   2. 前端    解到 /opt/1panel/www/sites/TTYFish/index（1Panel 的站点目录）
#   3. hub      SSH 网关
#
# 顺序是"后端 → 前端 → 网关"：前端要用后端的新接口，所以排在后端之后；
# 网关重启会踢掉正在玩的人，所以压在最后。
#
# 不在生产机上编译，也不从 CI 传几十 MB 的二进制过来——这个方向跨境只有几十 KB/s，
# 让服务器经本机 mihomo 从 GitHub Release 拉才是秒级。
set -uo pipefail

TAG=${1:-}
if [ -z "$TAG" ]; then
	echo "用法: $0 <tag>（例如 v0.1.6）" >&2
	exit 2
fi

REPO="xingzitian/TTYFish"
BIN_DIR="/opt/ttyfish"
# 前端正本放在 1Panel 的站点目录里：openresty 跑在容器里，只挂载了 /opt/1panel/www
WEB_DIR="/opt/1panel/www/sites/TTYFish/index"
SSH_PORT=12331
WEB_PORT=12332
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

# 前端单独下：没有这个资产就只更新二进制（老版本 Release 没有 web.tar.gz）
HAS_WEB=0
if fetch "$BASE/web.tar.gz" "$TMP/web.tar.gz"; then
	if tar -tzf "$TMP/web.tar.gz" >/dev/null 2>&1; then
		HAS_WEB=1
	else
		log "web.tar.gz 下载下来不是有效压缩包，跳过前端"
	fi
else
	log "这个 Release 没有 web.tar.gz，跳过前端"
fi

# ---- 1. 后端 ----
log "替换后端"
[ -f "$BIN_DIR/backend" ] && cp -p "$BIN_DIR/backend" "$BIN_DIR/backend.prev"
mv "$TMP/backend" "$BIN_DIR/backend"

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
	log "后端没起来，回滚二进制"
	[ -f "$BIN_DIR/backend.prev" ] && mv "$BIN_DIR/backend.prev" "$BIN_DIR/backend"
	sudo -n systemctl restart ttyfish-backend
	exit 1
fi
log "后端健康"

# ---- 2. 前端 ----
if [ "$HAS_WEB" -eq 1 ]; then
	log "替换前端"
	rm -rf "$WEB_DIR.new"
	mkdir -p "$WEB_DIR.new"
	if ! tar -xzf "$TMP/web.tar.gz" -C "$WEB_DIR.new"; then
		log "解包失败，前端保持原样"
		rm -rf "$WEB_DIR.new"
		exit 1
	fi
	# 先解到 .new 再整体换，避免 nginx 读到解了一半的目录
	rm -rf "$WEB_DIR.prev"
	[ -d "$WEB_DIR" ] && mv "$WEB_DIR" "$WEB_DIR.prev"
	mv "$WEB_DIR.new" "$WEB_DIR"
	chmod -R a+rX "$WEB_DIR"

	ok=0
	for _ in $(seq 1 5); do
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 --noproxy '*' "http://127.0.0.1:$WEB_PORT/")
		deep=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 --noproxy '*' "http://127.0.0.1:$WEB_PORT/account")
		if [ "$code" = "200" ] && [ "$deep" = "200" ]; then
			ok=1
			break
		fi
		sleep 1
	done
	if [ "$ok" -ne 1 ]; then
		log "前端起不来（首页 $code / 深链 $deep），回滚"
		rm -rf "$WEB_DIR"
		[ -d "$WEB_DIR.prev" ] && mv "$WEB_DIR.prev" "$WEB_DIR"
		exit 1
	fi
	log "前端健康"
else
	log "跳过前端"
fi

# ---- 3. 网关 ----
log "替换网关"
[ -f "$BIN_DIR/hub" ] && cp -p "$BIN_DIR/hub" "$BIN_DIR/hub.prev"
mv "$TMP/hub" "$BIN_DIR/hub"

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
