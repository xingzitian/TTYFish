#!/usr/bin/env bash
# 账号链路的端到端验收：注册 → 密码登录 → 公钥增删 → SSH 挑战登录 → 跨源拒绝。
#
# 只起 cmd/backend，不依赖 SSH 网关：网关那一段由本机手工 e2e 验证，
# CI 里跑 API 这条主线更快也更稳。
#
# 写法约定：先把结果算进变量，再用简单的 [ ] 断言——避免在 check 里塞嵌套引号。
set -uo pipefail

cd "$(dirname "$0")/.."
BIN=/tmp/backend-smoke
DIR=/tmp/backend-smoke.d
PORT=${PORT:-8092}
GATEWAY_TOKEN="smoke-gateway-token-0123456789abcdef"
ORIGIN="http://localhost:5173"
fail=0

check() {
	if eval "$2"; then
		echo "  ok   $1"
	else
		echo "  FAIL $1"
		fail=1
	fi
}

status_of() { curl -s -o /dev/null -w "%{http_code}" "$@"; }
field() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1]))' "$1"; }
urlq() { python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=""))' "$1"; }
mkjson() { python3 -c "$1"; }

rm -rf "$DIR"
mkdir -p "$DIR"
go build -o "$BIN" ./cmd/backend || exit 1
ssh-keygen -t ed25519 -f "$DIR/key1" -N "" -q
ssh-keygen -t ed25519 -f "$DIR/key2" -N "" -q

TTYFISH_GATEWAY_TOKEN="$GATEWAY_TOKEN" "$BIN" \
	-addr "127.0.0.1:$PORT" -db "$DIR/backend.db" \
	-cors-origins "$ORIGIN" >"$DIR/serve.log" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT

for _ in $(seq 1 50); do
	curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
	sleep 0.1
done

API="http://127.0.0.1:$PORT/api/v1"
PUB1=$(tr -d '\n' <"$DIR/key1.pub")
REG=$(mkjson "import json;print(json.dumps({'username':'smoke','password':'smoke-password-123','public_key':'''$PUB1'''}))")
LOGIN='{"username":"smoke","password":"smoke-password-123"}'

echo "== 场景一：注册与密码登录 =="
HEALTH=$(curl -sf "http://127.0.0.1:$PORT/healthz")
check "健康检查" '[ -n "$HEALTH" ]'

CODE=$(status_of -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" -d "$REG" "$API/accounts")
check "注册成功" '[ "$CODE" = "201" ]'

TOKEN=$(curl -s -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" -d "$LOGIN" "$API/auth/password" | field 'd["access_token"]')
check "密码登录拿到令牌" '[ -n "$TOKEN" ]'

ME=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/me" | field 'd["username"]')
check "查自己" '[ "$ME" = "smoke" ]'

echo "== 场景二：公钥增删 =="
PUB2=$(tr -d '\n' <"$DIR/key2.pub")
KEY2=$(mkjson "import json;print(json.dumps({'public_key':'''$PUB2'''}))")
CODE=$(status_of -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d "$KEY2" "$API/me/keys")
check "添加第二把公钥" '[ "$CODE" = "201" ]'

N=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/me/keys" | field 'len(d["items"])')
check "现在有两把" '[ "$N" = "2" ]'

FP2=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/me/keys" | field 'd["items"][1]["fingerprint"]')
CODE=$(status_of -X DELETE -H "Authorization: Bearer $TOKEN" "$API/me/keys/$(urlq "$FP2")")
check "删除一把" '[ "$CODE" = "204" ]'

N=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/me/keys" | field 'len(d["items"])')
check "删完还剩一把" '[ "$N" = "1" ]'

echo "== 场景三：SSH 挑战登录（浏览器发起 → 网关确认 → 浏览器领取）=="
CH=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"smoke"}' "$API/auth/ssh/challenges")
CH_CODE=$(echo "$CH" | field 'd["code"]')
POLL=$(echo "$CH" | field 'd["poll_token"]')
check "创建挑战拿到码" '[ -n "$CH_CODE" ] && [ -n "$POLL" ]'

POLL_BODY=$(mkjson "import json;print(json.dumps({'poll_token':'''$POLL'''}))")
STATE=$(curl -s -X POST -H "Content-Type: application/json" -d "$POLL_BODY" "$API/auth/ssh/challenges/poll" | field 'd["state"]')
check "刚开始是 pending" '[ "$STATE" = "pending" ]'

CONFIRM=$(mkjson "import json;print(json.dumps({'username':'smoke','public_key':'''$PUB1''','code':'''$CH_CODE'''}))")
CODE=$(status_of -X POST -H "Authorization: Bearer $GATEWAY_TOKEN" -H "Content-Type: application/json" -d "$CONFIRM" "$API/internal/auth/ssh/challenges/confirm")
check "网关确认挑战" '[ "$CODE" = "204" ]'

SESSION=$(curl -s -X POST -H "Content-Type: application/json" -d "$POLL_BODY" "$API/auth/ssh/challenges/poll" | field 'd.get("session",{}).get("access_token","")')
check "确认后领取会话" '[ -n "$SESSION" ]'

echo "== 场景四：边界 =="
CODE=$(status_of -X POST -H "Origin: http://evil.test" -H "Content-Type: application/json" -d "$LOGIN" "$API/auth/password")
check "跨源请求被拒" '[ "$CODE" = "403" ]'

CODE=$(status_of "$API/me")
check "没有令牌访问 me 被拒" '[ "$CODE" = "401" ]'

CODE=$(status_of -X POST -H "Content-Type: application/json" -d '{"username":"smoke","password":"wrong-password-000"}' "$API/auth/password")
check "错误密码被拒" '[ "$CODE" = "401" ]'

if [ $fail -ne 0 ]; then
	echo "--- 服务端日志 ---"
	tail -30 "$DIR/serve.log"
	exit 1
fi
echo "全部通过"
