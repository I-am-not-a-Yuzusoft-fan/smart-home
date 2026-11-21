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
  // 写请求必须带这个头：后端拿它区分"同源页面发的"和"别的站点发起的"。
  // 跨站表单带不了自定义头，所以它挡得住 CSRF；同源 fetch 能原样带上。
  const method = (init?.method ?? 'GET').toUpperCase()
  const unsafe = method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS'
  const res = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      ...(unsafe ? { 'X-Requested-With': 'fetch' } : {}),
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

/** 户型里的一个端点（单位厘米，原点左上角）。 */
export type Corner = { id: string; x: number; y: number }

/** 一面墙，两端坐标已由服务端 join 好。 */
export type Wall = {
  id: string
  aId: string
  bId: string
  ax: number
  ay: number
  bx: number
  by: number
  thickness: number
}

/** 整个户型。端点单列是因为两面墙共用一个端点。 */
export type PlanPoint = { x: number; y: number }

/**
 * 房间。几何**不存库**：后端每次从墙的闭环推导出来，哈希是多边形的身份，
 * 房间的名字挂在这个哈希上。所以墙一改，旧的命名自然失配——不会把
 * 「客厅」这个名字挂到别处的房间上。
 */
export type Room = {
  hash: string
  ring: PlanPoint[]
  areaM2: number
  name?: string
  kind?: string
}

export type Plan = {
  corners: Corner[]
  walls: Wall[]
  rooms: Room[]
  /** 没参与任何闭合区域的墙数：大于 0 说明还有墙没围上 */
  openWalls: number
}

/** 读当前户型。 */
export const getPlan = () => request<Plan>('/api/plan')

/** 画一面墙；返回保存后的整个户型。 */
export const createWall = (input: { ax: number; ay: number; bx: number; by: number; thickness?: number }) =>
  request<Plan>('/api/walls', { method: 'POST', body: JSON.stringify(input) })

/** 删一面墙。 */
export const deleteWall = (id: string) => request<void>(`/api/walls/${id}`, { method: 'DELETE' })

/** 枚举能力的一个选项：value 入库，label 给人看。 */
export type CapabilityOption = { value: string; label: string }

export type CapabilityType = 'bool' | 'number' | 'enum' | 'string'

/** 类型级能力定义；它没有值，值在 capability_state。 */
export type Capability = {
  key: string
  name: string
  type: CapabilityType
  unit?: string
  min?: number
  max?: number
  step?: number
  options?: CapabilityOption[]
  writable: boolean
  history: boolean
  default?: unknown
}

export type ItemType = {
  key: string
  kind: 'furniture' | 'device'
  name: string
  icons?: string
  defaultWidth: number
  defaultDepth: number
  capabilities: Capability[]
  capabilitiesVersion: number
  driver?: string
}

export type ItemTypeInput = {
  key: string
  kind: 'furniture' | 'device'
  name: string
  icons?: string
  defaultWidth?: number
  defaultDepth?: number
  capabilities: Capability[]
  driver?: string
}

export type ItemTypeUpdate = {
  name?: string
  icons?: string
  defaultWidth?: number
  defaultDepth?: number
  capabilities?: Capability[]
  /** 乐观锁：修改 capabilities 时必填（否则 400），过期返回 409 */
  capabilitiesVersion?: number
  /** 省略表示不变；空字符串表示清除 */
  driver?: string
}

/** 全部类型模板。成员也能读。 */
export const listItemTypes = () => request<ItemType[]>('/api/item-types')

/** 新建类型（仅管理员）。 */
export const createItemType = (input: ItemTypeInput) =>
  request<ItemType>('/api/item-types', { method: 'POST', body: JSON.stringify(input) })

/** 修改类型（仅管理员）。key 不可改。 */
export const updateItemType = (key: string, input: ItemTypeUpdate) =>
  request<ItemType>(`/api/item-types/${encodeURIComponent(key)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })

/** 删除类型（仅管理员）；仍被物品引用时返回 409。 */
export const deleteItemType = (key: string) =>
  request<void>(`/api/item-types/${encodeURIComponent(key)}`, { method: 'DELETE' })
