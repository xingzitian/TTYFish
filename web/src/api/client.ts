import type { Account, ApiErrorBody, ChallengeState, Key, Session, SSHChallenge } from './types'

const BASE = import.meta.env.VITE_API_BASE ?? ''

/** 后端统一错误格式抛出来的异常 */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/**
 * 令牌只放在内存里。
 *
 * 后端明确不读 Cookie，令牌只能由前端保存；放 localStorage 会被任意 XSS 读到，
 * 所以这里只留内存——刷新页面就要重新登录，这是刻意换来的安全性。
 * 以后要"刷新不掉线"，正确做法是后端加一个 HttpOnly 的刷新令牌，而不是往
 * localStorage 里塞东西。
 */
let token: string | null = null

export function setToken(next: string | null): void {
  token = next
}

export function hasToken(): boolean {
  return token !== null
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  let res: Response
  try {
    res = await fetch(BASE + path, { ...init, headers, credentials: 'omit' })
  } catch {
    throw new ApiError(0, 'network_error', '连不上服务器，检查网络或稍后再试')
  }

  if (res.status === 204) {
    return undefined as T
  }

  const text = await res.text()
  let body: unknown = null
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = null
    }
  }

  if (!res.ok) {
    const err = (body as ApiErrorBody | null)?.error
    const code = err?.code ?? 'internal_error'
    // 后端的 message 常带 "invalid_request: " 前缀，界面上只想要人话
    const message = (err?.message ?? `请求失败（HTTP ${res.status}）`).replace(
      new RegExp(`^${code}:\\s*`),
      '',
    )
    throw new ApiError(res.status, code, message)
  }

  return body as T
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export const api = {
  register(input: { username: string; password?: string; public_key?: string }) {
    return post<Account>('/api/v1/accounts', input)
  },

  loginPassword(input: { username: string; password: string }) {
    return post<Session>('/api/v1/auth/password', input)
  },

  logout() {
    return post<void>('/api/v1/auth/logout')
  },

  me() {
    return request<Account>('/api/v1/me')
  },

  changePassword(password: string) {
    return request<void>('/api/v1/me/password', {
      method: 'PUT',
      body: JSON.stringify({ password }),
    })
  },

  listKeys() {
    return request<{ items: Key[] }>('/api/v1/me/keys')
  },

  addKey(publicKey: string) {
    return post<Key>('/api/v1/me/keys', { public_key: publicKey })
  },

  deleteKey(fingerprint: string) {
    return request<void>(`/api/v1/me/keys/${encodeURIComponent(fingerprint)}`, {
      method: 'DELETE',
    })
  },

  startSSHChallenge(username: string) {
    return post<SSHChallenge>('/api/v1/auth/ssh/challenges', { username })
  },

  /** poll_token 必须走 body——后端明确禁止放进 URL */
  pollSSHChallenge(pollToken: string) {
    return post<ChallengeState>('/api/v1/auth/ssh/challenges/poll', {
      poll_token: pollToken,
    })
  },
}
