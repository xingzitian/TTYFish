#!/usr/bin/env bash
# 验收：网页注册固定用户名 → 终端确认 → 浏览器拿到会话 → 添加公钥。
#
# 这是"没有密码"那条链路的端到端测试：任何一环断了它都会失败。
set -uo pipefail

cd "$(dirname "$0")/.."
BIN=/tmp/hub-smoke-bin
DIR=/tmp/hub-login-smoke
SSH_PORT=${SSH_PORT:-2291}
HTTP_PORT=${HTTP_PORT:-8091}
fail=0

check() {
	if eval "$2"; then
		echo "  ok   $1"
	else
		echo "  FAIL $1"
		fail=1
	fi
}

rm -rf "$DIR"
mkdir -p "$DIR"
go build -o "$BIN" ./cmd/hub || exit 1
ssh-keygen -t ed25519 -f "$DIR/key1" -N "" -q
ssh-keygen -t ed25519 -f "$DIR/key2" -N "" -q

"$BIN" -addr "127.0.0.1:$SSH_PORT" -http "127.0.0.1:$HTTP_PORT" \
	-db "$DIR/hub.db" -host-key "$DIR/host" \
	-ssh-hint "-p $SSH_PORT play@127.0.0.1" >"$DIR/serve.log" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT

for _ in $(seq 1 50); do
	curl -sf "http://127.0.0.1:$HTTP_PORT/healthz" >/dev/null 2>&1 && break
	sleep 0.1
done

SSH_OPTS=(-p "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
	-o BatchMode=yes -o IdentitiesOnly=yes)

# 从"等待确认"页面里抠出短码和轮询 token
extract() {
	sed -n 's/.*auth \([A-Z0-9-]*\)<.*/\1/p' <<<"$1"
}
extract_poll() {
	sed -n 's/.*var poll = "\([a-f0-9]*\)".*/\1/p' <<<"$1"
}

echo "== 场景一：先注册，再用固定用户名登录 =="
check "首页能打开" 'curl -sf "http://127.0.0.1:$HTTP_PORT/" | grep -q "用 SSH 登录"'
check "网页注册成功" '[ "$(curl -s -o /dev/null -w "%{http_code}" --data-urlencode "username=play" --data-urlencode "public_key@$DIR/key1.pub" "http://127.0.0.1:$HTTP_PORT/register")" = "303" ]'

PAGE=$(curl -s --data-urlencode "username=play" "http://127.0.0.1:$HTTP_PORT/auth/start")
CODE=$(extract "$PAGE")
POLL=$(extract_poll "$PAGE")
check "网页给出了短码" '[ -n "$CODE" ] && [ -n "$POLL" ]'

timeout 15 ssh "${SSH_OPTS[@]}" -i "$DIR/key1" "play@127.0.0.1" "auth $CODE" >"$DIR/ssh1.txt" 2>&1
check "终端确认成功" 'grep -q "登录成功" "$DIR/ssh1.txt"'

curl -s -c "$DIR/jar" "http://127.0.0.1:$HTTP_PORT/auth/poll?p=$POLL" >"$DIR/poll1.json"
check "浏览器拿到会话" 'grep -q "\"ok\"" "$DIR/poll1.json"'

curl -s -b "$DIR/jar" "http://127.0.0.1:$HTTP_PORT/account" >"$DIR/account1.html"
check "账号页显示 handle" 'grep -q "@play" "$DIR/account1.html"'
check "账号页显示公钥" 'grep -q "SHA256:" "$DIR/account1.html"'

echo "== 场景二：换一把密钥登录，还是同一个人（多公钥）=="
check "账号页添加第二把公钥" '[ "$(curl -s -b "$DIR/jar" -o /dev/null -w "%{http_code}" --data-urlencode "public_key@$DIR/key2.pub" "http://127.0.0.1:$HTTP_PORT/account/key")" = "303" ]'
PAGE=$(curl -s --data-urlencode "username=play" "http://127.0.0.1:$HTTP_PORT/auth/start")
CODE=$(extract "$PAGE")
POLL=$(extract_poll "$PAGE")
check "网页给出了登录码" '[ -n "$CODE" ]'

timeout 15 ssh "${SSH_OPTS[@]}" -i "$DIR/key2" "play@127.0.0.1" "auth $CODE" >"$DIR/ssh2.txt" 2>&1
check "第二把钥匙确认成功" 'grep -q "登录成功" "$DIR/ssh2.txt"'

curl -s -b "$DIR/jar" -c "$DIR/jar" "http://127.0.0.1:$HTTP_PORT/auth/poll?p=$POLL" >/dev/null
curl -s -b "$DIR/jar" "http://127.0.0.1:$HTTP_PORT/account" >"$DIR/account2.html"
check "账号页现在有两把钥匙" '[ "$(grep -c "SHA256:" "$DIR/account2.html")" -ge 2 ]'

echo "== 场景三：密码可选，用户名必须匹配 =="
check "错误用户名被拒绝" '! timeout 15 ssh "${SSH_OPTS[@]}" -i "$DIR/key1" "wrong@127.0.0.1" true >/dev/null 2>&1'
check "设置账号密码" '[ "$(curl -s -b "$DIR/jar" -o /dev/null -w "%{http_code}" --data-urlencode "password=smoke-password-123" "http://127.0.0.1:$HTTP_PORT/account/password")" = "303" ]'
check "网页密码登录成功" '[ "$(curl -s -o /dev/null -w "%{http_code}" --data-urlencode "username=play" --data-urlencode "password=smoke-password-123" "http://127.0.0.1:$HTTP_PORT/auth/password")" = "303" ]'
check "跨站 POST 被拒" '[ "$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Origin: http://evil.test" "http://127.0.0.1:$HTTP_PORT/auth/start")" = "403" ]'

if [ $fail -ne 0 ]; then
	echo "--- 服务端日志 ---"
	tail -30 "$DIR/serve.log"
	exit 1
fi
echo "全部通过"
