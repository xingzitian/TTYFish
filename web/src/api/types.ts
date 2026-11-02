// 这些类型和后端 hub/internal/api/openapi.json 一一对应。
// 改了接口先改这里，TypeScript 会把用错的地方指出来。

export interface Account {
  id: string
  username: string
  display_name: string
  created_at: string
}

export interface Key {
  /** 玩家自己起的名字，可以是空字符串 */
  name: string
  fingerprint: string
  public_key: string
  created_at: string
  /** 从没用过时是 null，不是空字符串 */
  last_used_at: string | null
}

export interface Session {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  account: Account
}

/** 一条登录会话。current 表示就是你现在正用着的这条。 */
export interface SessionInfo {
  id: string
  /** password / ssh_key 之类，用来在界面上说明这条登录是怎么来的 */
  auth_method: string
  credential_fingerprint: string | null
  created_at: string
  expires_at: string
  last_seen_at: string
  source_ip: string
  user_agent: string
  current: boolean
}

export interface SSHChallenge {
  username: string
  /** 给终端敲的一次性确认码 */
  code: string
  /** 浏览器轮询凭证，绝不能出现在 URL 或终端命令里 */
  poll_token: string
  expires_in: number
  poll_interval_ms: number
}

/** 挑战轮询的三种结果 */
export type ChallengeState =
  | { state: 'pending' }
  | { state: 'expired' }
  | { state: 'ok'; session: Session }

export interface ApiErrorBody {
  error: { code: string; message: string }
}
