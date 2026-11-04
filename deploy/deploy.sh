#!/usr/bin/env bash
# 服务器侧的部署脚本：安装 GitHub Actions CI 已构建并传来的产物。
#
#   /opt/ttyfish/deploy.sh deploy-<commit> <sha256> <r2-account-id> <bucket> -
#
# 由 CI 通过 SSH 调用（也方便手动执行）。三样东西：
#   1. backend  账号 + 房间 + 规则
#   2. 前端    解到 /opt/1panel/www/sites/TTYFish/index（1Panel 的站点目录）
#   3. hub      SSH 网关
#
# 顺序是"后端 → 前端 → 网关"：前端要用后端的新接口，所以排在后端之后；
# 网关重启会踢掉正在玩的人，所以压在最后。
#
# 不在生产机上编译或下载 Release；部署只安装同一 CI run 生成并校验的产物。
set -uo pipefail

TAG=${1:-}
EXPECTED_SHA256=${2:-}
R2_ACCOUNT_ID=${3:-}
R2_BUCKET=${4:-}
PACKAGE_SOURCE=${5:-}
if [ -z "$TAG" ] || [[ ! "$EXPECTED_SHA256" =~ ^[a-f0-9]{64}$ ]] ||
	[[ ! "$R2_ACCOUNT_ID" =~ ^[a-f0-9]{32}$ ]] || [ -z "$R2_BUCKET" ] ||
	[ "$PACKAGE_SOURCE" != "-" ]; then
	echo "用法: $0 <version> <sha256> <r2-account-id> <bucket> -" >&2
	exit 2
fi

BIN_DIR="/opt/ttyfish"
# 前端正本放在 1Panel 的站点目录里：openresty 跑在容器里，只挂载了 /opt/1panel/www
WEB_DIR="/opt/1panel/www/sites/TTYFish/index"
SSH_PORT=12331
WEB_PORT=12332
BACKEND_HEALTH="http://127.0.0.1:8082/healthz"

log() { echo "[$(date '+%F %T')] $*"; }

# 发版是自己挑的时间窗口，有人在玩也照推——只留一行日志方便事后对账
ACTIVE=$(ss -Htn state established "( sport = :$SSH_PORT )" 2>/dev/null | wc -l)
if [ "$ACTIVE" -gt 0 ]; then
	log "注意：$SSH_PORT 上还有 $ACTIVE 个连接，仍按计划强推（会踢掉这些人）"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
PACKAGE="$TMP/ci-artifact.tar.gz"
R2_URL_PREFIX="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com/"
R2_VIRTUAL_HOST_PREFIX="https://${R2_BUCKET}.${R2_ACCOUNT_ID}.r2.cloudflarestorage.com/"

if ! IFS= read -r PACKAGE_URL; then
	log "没有收到 R2 下载链接"
	exit 1
fi
case "$PACKAGE_URL" in
	"$R2_URL_PREFIX"*|"$R2_VIRTUAL_HOST_PREFIX"*) ;;
	*)
		log "R2 下载链接的主机与配置不符"
		exit 1
		;;
esac
log "从 Cloudflare R2 下载 CI 产物 $TAG"
if ! printf 'url = "%s"\n' "$PACKAGE_URL" |
	curl --config - --fail --silent --show-error --retry 2 --retry-delay 2 \
		--max-time 300 --proto '=https' --output "$PACKAGE"; then
	log "CI 产物下载失败"
	exit 1
fi
unset PACKAGE_URL

log "校验并解包 CI 产物 $TAG"
if ! printf '%s  %s\n' "$EXPECTED_SHA256" "$PACKAGE" | sha256sum --check --status; then
	log "CI 产物 SHA256 校验失败"
	exit 1
fi
if tar -tzf "$PACKAGE" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
	log "CI 产物包含非法路径"
	exit 1
fi
if ! tar -xzf "$PACKAGE" -C "$TMP"; then
	log "CI 产物解包失败"
	exit 1
fi
for name in hub backend; do
	if [ ! -f "$TMP/$name-linux-amd64" ] || [ ! -f "$TMP/$name-linux-amd64.sha256" ]; then
		log "CI 产物缺少 $name 或其校验文件"
		exit 1
	fi
done
if [ ! -f "$TMP/web.tar.gz" ] || [ ! -f "$TMP/web.tar.gz.sha256" ] ||
	! (cd "$TMP" && sha256sum --check web.tar.gz.sha256) ||
	! tar -tzf "$TMP/web.tar.gz" >/dev/null 2>&1; then
	log "CI 产物缺少有效的前端包"
	exit 1
fi
if ! (cd "$TMP" && sha256sum --check hub-linux-amd64.sha256 backend-linux-amd64.sha256); then
	log "CI 二进制校验失败"
	exit 1
fi
mv "$TMP/backend-linux-amd64" "$TMP/backend"
mv "$TMP/hub-linux-amd64" "$TMP/hub"
chmod +x "$TMP/backend" "$TMP/hub"
HAS_WEB=1

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
