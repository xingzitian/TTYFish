// 这些类型和后端 hub/internal/api/openapi.json 一一对应。
// 改了接口先改这里，TypeScript 会把用错的地方指出来。

export interface Account {
  id: string
  username: string
  display_name: string
  created_at: string
}

export interface Key {
  fingerprint: string
  public_key: string
  created_at: string
}

export interface Session {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  account: Account
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
