# 前端（Web 页面）

Vue 3 + TypeScript + Element Plus，独立于后端构建，通过 JSON API 通信。

## 跑起来

```bash
# 1. 后端（账号 API）
cd hub
go run ./cmd/backend -addr 127.0.0.1:8081 -db /tmp/backend.db -cors-origins http://localhost:5173

# 2. 前端
cd web
npm install
npm run dev          # http://localhost:5173
```

开发时 Vite 把 `/api` 代理到 `127.0.0.1:8081`，但浏览器的 Origin 仍会保留，
所以后端也需要配置上面的来源白名单。SSH 挑战登录还需启动使用同一网关凭证的 SSH 网关，完整步骤见 [README](../README.md)。

## 页面

| 路由 | 内容 |
|---|---|
| `/login` | SSH 挑战登录（拿确认码 → 终端执行 → 自动跳转）、密码登录 |
| `/register` | 注册：固定用户名 + 公钥 / 密码（至少一项） |
| `/account` | 账号信息、公钥列表（增删）、密码登录开关、设置/修改密码、退出 |

未登录访问 `/account` 会被路由守卫弹回 `/login`。

## 几个刻意的决定

**令牌只放内存**。后端不读 Cookie，令牌只能前端保存；放 `localStorage` 会被任意
XSS 读到。代价是刷新页面要重新登录。以后要"刷新不掉线"，应该在**后端**加一个
HttpOnly 的刷新令牌，而不是往 `localStorage` 里塞东西。

**`poll_token` 只走请求体**。后端明确禁止把它放进 URL（会进日志、进 Referer），
所以轮询是 `POST /api/v1/auth/ssh/challenges/poll`。

**轮询节奏听后端的**。创建挑战时返回 `poll_interval_ms`（1500ms），前端不能更快，
默认每个 poll_token 每分钟 60 次、每来源合计 1200 次，轮询不消耗密码登录预算。
429 响应提供 Retry-After，跨源请求也可读取该头；应等待指定时间后再尝试，不立即循环重试。

**错误提示按 code 分流**：`account_conflict` → 用户名/公钥被占用；
`last_credential` → 至少保留一把 SSH 公钥或启用密码登录；`rate_limited` → 稍后再试。

## 构建

```bash
cd web
npm run build        # 产物在 web/dist
```

`dist/` 是纯静态文件，交给后端或 nginx 托管即可。

## 已知取舍

- Element Plus 整包引入（约 300 KB gzip）。后续可以改用
  `unplugin-vue-components` 按需引入，能砍掉一多半。
- 没有做 i18n，界面是中文写死的。
