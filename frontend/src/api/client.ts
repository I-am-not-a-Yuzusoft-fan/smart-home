// 与后端契约对齐的最小客户端。
// 只用浏览器原生 fetch：Cookie 由浏览器自动携带（同源代理），不碰 localStorage。

/** 会话用户，字段与 openapi.yaml 的 SessionUser 一致。 */
export type SessionUser = {
  id: string
  username: string
  displayName: string
  role: 'admin' | 'member'
}

/** 后端统一错误封套。 */
export type ApiErrorBody = { error: { code: string; message: string } }

/** 带错误码的请求失败，页面据此决定显示什么。 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  const body: unknown = text ? JSON.parse(text) : null
  if (!res.ok) {
    const err = body as ApiErrorBody | null
    throw new ApiError(
      res.status,
      err?.error?.code ?? 'INTERNAL',
      err?.error?.message ?? `请求失败（HTTP ${res.status}）`,
    )
  }
  return body as T
}

/** 取当前会话用户；未登录时抛 401 的 ApiError。 */
export const getMe = () => request<SessionUser>('/api/auth/me')

/** 用账号口令换会话 Cookie。 */
export const login = (username: string, password: string) =>
  request<SessionUser>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })

/** 清除会话。 */
export const logout = () => request<void>('/api/auth/logout', { method: 'POST' })
