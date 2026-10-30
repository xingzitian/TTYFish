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
  <p>使用注册时确定的用户名登录。支持 SSH 密钥或账号密码。</p>
  <p class="muted">需要在电脑终端中完成一次确认。点击后会显示操作步骤；第一次使用需要先创建 SSH 密钥。</p>
  <form method="post" action="/auth/start">
    <label>用户名 <input name="username" required autocomplete="username"></label>
    <button type="submit">用 SSH 登录</button>
  </form>
  <p class="muted" style="margin-top:16px">
    用户名固定，SSH 连接时也必须填写此用户名。
  </p>
  <form method="post" action="/auth/password">
    <p><label>用户名 <input name="username" required autocomplete="username"></label></p>
    <p><label>密码 <input type="password" name="password" required autocomplete="current-password"></label></p>
    <button type="submit">用密码登录</button>
  </form>
</div>
<div class="card">
  <h2>注册账号</h2>
  <form method="post" action="/register">
    <p><label>固定用户名 <input name="username" required pattern="[a-z][a-z0-9_-]{2,19}" minlength="3" maxlength="20" autocomplete="username"></label></p>
    <p class="muted">3–20 位小写字母、数字、下划线或短横线，以字母开头；注册后用于所有登录。</p>
    <p><label>SSH 公钥（可选）<br><textarea name="public_key" rows="3" style="width:100%" placeholder="ssh-ed25519 AAAA…"></textarea></label></p>
    <p class="muted">粘贴电脑上 .ssh/id_ed25519.pub 的完整内容。没有密钥可运行 ssh-keygen -t ed25519 创建。只提交 .pub 公钥，私钥留在自己的电脑上。</p>
    <p><label>密码（可选，10–72 字节）<input type="password" name="password" minlength="10" maxlength="72" autocomplete="new-password"></label></p>
    <p class="muted">公钥和密码至少填写一项。只填公钥即可使用密钥登录，以后也可以设置密码。</p>
    <button type="submit">注册</button>
  </form>
</div>
{{template "foot" .}}{{end}}

{{define "waiting"}}{{template "head" .}}
<div class="card">
  <p>在你的电脑上确认这次网页登录</p>
  <ol>
    <li>打开终端：Windows 在开始菜单搜索 PowerShell；macOS 打开“终端”；Linux 打开终端应用。</li>
    <li>复制下面的完整命令，粘贴到终端中，按 Enter 执行。</li>
    <li>终端提示确认成功后，回到此页面，网页会自动登录。</li>
  </ol>
  <div class="cmd" id="login-command">ssh {{.SSHHint}} auth {{.Code}}</div>
  <p><button type="button" id="copy-command">复制登录命令</button> <span class="muted" id="copy-status" role="status"></span></p>
  <details>
    <summary>第一次使用，或者连接失败？</summary>
    <p>请使用已经绑定到当前账号的密钥。没有绑定密钥时，可以回首页使用账号密码登录，再到账号页添加公钥。</p>
    <p>第一次连接可能要求确认服务器指纹，请与站点管理员提供的指纹核对后再输入 <code>yes</code>。如果提示输入密钥口令，填写你创建密钥时设置的口令。</p>
    <p>如果提示找不到 <code>ssh</code>，请安装或启用 OpenSSH 客户端。如果提示连接失败，请联系站点管理员确认连接地址和端口。</p>
    <p class="muted">SSH 密钥保存在你的电脑上，用来识别账号。请妥善保管，不要把私钥发给任何人；换电脑时需要迁移密钥或在账号页添加新公钥。</p>
  </details>
  <p class="muted">90 秒内有效，只能用一次。确认后这个页面会自动跳转。</p>
  <form method="post" action="/auth/start"><input type="hidden" name="username" value="{{.Handle}}"><button class="ghost" type="submit">重新获取登录命令</button></form>
</div>
<p class="muted" id="status">等待终端确认…</p>
<script>
(function(){
  document.getElementById('copy-command').addEventListener('click', function(){
    var status = document.getElementById('copy-status');
    if (!navigator.clipboard) { status.textContent = '请选中上方完整命令后手动复制。'; return; }
    navigator.clipboard.writeText(document.getElementById('login-command').textContent)
      .then(function(){ status.textContent = '已复制，请到终端粘贴并按 Enter。'; })
      .catch(function(){ status.textContent = '请选中上方完整命令后手动复制。'; });
  });
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
  <p>在终端执行以下命令进入游戏：</p>
  <div class="cmd">ssh {{.SSHHint}}</div>
  <p class="muted">SSH 会先尝试密钥；未使用匹配密钥时，可以输入你为账号设置的密码。输入密码时终端不会显示字符。</p>
</div>
<div class="card">
  <div class="row">
    <b>公钥</b><span class="spacer"></span>
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
    登录游戏时必须使用当前用户名 @{{.Handle}}；密钥和密码登录会进入同一个游戏账号。
  </p>
  <form method="post" action="/account/key">
    <p><label>添加 SSH 公钥<br><textarea name="public_key" required rows="3" style="width:100%"></textarea></label></p>
    <button type="submit">保存公钥</button>
  </form>
  <form method="post" action="/account/password">
    <p><label>设置或修改密码 <input type="password" name="password" required minlength="10" maxlength="72" autocomplete="new-password"></label></p>
    <button type="submit">保存密码</button>
  </form>
</div>
{{template "foot" .}}{{end}}
`
