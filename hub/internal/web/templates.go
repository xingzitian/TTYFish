package web

// 页面模板都内联，不引入静态资源依赖——一个二进制就能跑。
const templates = `
{{define "head"}}<!doctype html>
<html lang="zh-CN"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · CLI_GAME</title>
<style>
  :root { color-scheme: dark; }
  body { margin: 0; font: 15px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
         background: #0d1117; color: #c9d1d9; }
  main { max-width: 640px; margin: 8vh auto; padding: 0 20px; }
  h1 { font-size: 22px; margin: 0 0 4px; color: #f0f6fc; }
  .sub { color: #8b949e; margin: 0 0 28px; }
  .card { border: 1px solid #30363d; border-radius: 8px; padding: 20px; background: #161b22; margin-bottom: 16px; }
  code, .code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .code { display: block; font-size: 24px; letter-spacing: 2px; color: #f0f6fc; padding: 12px 0; text-align: center; }
  .cmd { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 12px 14px;
         color: #7ee787; overflow-x: auto; white-space: pre; }
  button { background: #238636; color: #fff; border: 1px solid #2ea043; border-radius: 6px;
           padding: 9px 18px; font-size: 15px; cursor: pointer; }
  button:hover { background: #2ea043; }
  button.ghost { background: transparent; border-color: #30363d; color: #c9d1d9; }
  table { width: 100%; border-collapse: collapse; font-size: 14px; }
  th, td { text-align: left; padding: 8px 6px; border-bottom: 1px solid #21262d; }
  th { color: #8b949e; font-weight: 500; }
  .muted { color: #8b949e; font-size: 13px; }
  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .spacer { flex: 1; }
</style>
</head><body><main>
<h1>{{.Title}}</h1>
{{if .Handle}}<p class="sub">@{{.Handle}}</p>{{end}}
{{end}}

{{define "foot"}}</main></body></html>{{end}}

{{define "login"}}{{template "head" .}}
<div class="card">
  <p>这个平台没有密码。登录方式是用你的 <b>SSH 私钥</b>签一次名。</p>
  <form method="post" action="/auth/start">
    <button type="submit">用 SSH 登录</button>
  </form>
  <p class="muted" style="margin-top:16px">
    第一次登录会自动创建账号；你的公钥就是账号，用户名只是昵称。
  </p>
</div>
{{template "foot" .}}{{end}}

{{define "waiting"}}{{template "head" .}}
<div class="card">
  <p>在终端里执行这条命令：</p>
  <div class="cmd">ssh {{.SSHHint}} auth {{.Code}}</div>
  <p class="muted">90 秒内有效，只能用一次。确认后这个页面会自动跳转。</p>
</div>
<p class="muted" id="status">等待终端确认…</p>
<script>
(function(){
  var poll = {{.Poll}};
  function tick(){
    fetch('/auth/poll?p=' + encodeURIComponent(poll), {cache:'no-store'})
      .then(function(r){ return r.json(); })
      .then(function(d){
        if (d.state === 'ok' && d.next) { location.href = d.next; return; }
        if (d.state === 'expired') {
          document.getElementById('status').textContent = '这个码过期了，回上一页重新来一次。';
          return;
        }
        setTimeout(tick, 1500);
      })
      .catch(function(){ setTimeout(tick, 2500); });
  }
  setTimeout(tick, 1200);
})();
</script>
{{template "foot" .}}{{end}}

{{define "account"}}{{template "head" .}}
<div class="card">
  <div class="row">
    <b>公钥</b><span class="spacer"></span>
    <form method="post" action="/auth/link"><button class="ghost" type="submit">添加公钥</button></form>
    <form method="post" action="/logout"><button class="ghost" type="submit">退出</button></form>
  </div>
  <table>
    <tr><th>指纹</th><th>添加时间</th><th>最后使用</th></tr>
    {{range .Keys}}
    <tr><td class="code">{{.Short}}</td><td class="muted">{{.Added}}</td><td class="muted">{{.Used}}</td></tr>
    {{else}}
    <tr><td colspan="3" class="muted">还没有公钥</td></tr>
    {{end}}
  </table>
  <p class="muted" style="margin-top:14px">
    添加公钥：点上面的按钮，然后用<b>新的一把</b>密钥在终端确认。别删掉最后一把。
  </p>
</div>
{{template "foot" .}}{{end}}
`
