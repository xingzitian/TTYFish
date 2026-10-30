import { reactive, readonly } from 'vue'
import { api, setToken } from '../api/client'
import type { Account, Session } from '../api/types'

interface AuthState {
  account: Account | null
  /** 到期时间戳；令牌本身只存在 api/client 的模块变量里 */
  expiresAt: number
}

const state = reactive<AuthState>({
  account: null,
  expiresAt: 0,
})

export function useAuth() {
  function applySession(session: Session): void {
    setToken(session.access_token)
    state.account = session.account
    state.expiresAt = Date.now() + session.expires_in * 1000
  }

  async function loginWithPassword(username: string, password: string): Promise<void> {
    applySession(await api.loginPassword({ username, password }))
  }

  /** SSH 挑战确认后，用轮询拿到的会话登录 */
  function loginWithSession(session: Session): void {
    applySession(session)
  }

  async function refresh(): Promise<void> {
    state.account = await api.me()
  }

  async function logout(): Promise<void> {
    try {
      await api.logout()
    } catch {
      // 令牌可能已经过期；本地状态照样清掉
    }
    setToken(null)
    state.account = null
    state.expiresAt = 0
  }

  function isLoggedIn(): boolean {
    return state.account !== null && Date.now() < state.expiresAt
  }

  return {
    state: readonly(state),
    loginWithPassword,
    loginWithSession,
    refresh,
    logout,
    isLoggedIn,
  }
}
