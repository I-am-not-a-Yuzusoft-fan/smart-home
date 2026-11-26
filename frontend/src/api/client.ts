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

async function requestWithResponse<T>(
  path: string,
  init?: RequestInit,
): Promise<{ body: T; res: Response }> {
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
    return { body: undefined as T, res }
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
  return { body: body as T, res }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  return (await requestWithResponse<T>(path, init)).body
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
  /** 弧度（度），0 是直墙；半径与圆心由弦长和弧角反解，不落库。 */
  arcExtent: number
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

/**
 * 门窗：挂在墙上、沿墙定位。高度与离地的默认值由后端按类型给
 * （门 210/0，窗 120/90），几何的唯一来源仍是墙的端点。
 */
export type OpeningKind = 'door' | 'window'

export type Opening = {
  id: string
  wallId: string
  kind: OpeningKind
  /** 距墙 A 端点的偏移（厘米） */
  offset: number
  width: number
  height: number
  /** 离地高度（厘米）；门为 0 */
  sill: number
}

export type OpeningInput = {
  wallId: string
  kind: OpeningKind
  offset: number
  width: number
  height?: number
  sill?: number
}

export type OpeningPatch = {
  offset?: number
  width?: number
  height?: number
  sill?: number
}

/** 房间用途。与后端的 `room_name.kind` 检查约束一致。 */
export type RoomKind =
  | 'living'
  | 'bedroom'
  | 'kitchen'
  | 'bathroom'
  | 'study'
  | 'hallway'
  | 'balcony'

/** 失配的房间名：墙改过之后哈希对不上任何现存房间，名字悬空在这里。 */
export type StaleRoom = { hash: string; name: string; kind?: RoomKind }

export type Plan = {
  corners: Corner[]
  walls: Wall[]
  rooms: Room[]
  /** 门窗：位置由「墙 + 沿墙偏移」决定，不随物品一起移动 */
  openings: Opening[]
  /** 没参与任何闭合区域的墙数：大于 0 说明还有墙没围上 */
  openWalls: number
  /** 失配的房间名：前端据此提示「原来起的名字已失效」并允许清理 */
  staleRooms: StaleRoom[]
  /** 垫在画布下面的底图元数据；没有底图时是 null */
  underlay: Underlay | null
  /** 平面图版本号：撤销/重做写回时带上它做乐观并发 */
  version: number
}

/** 底图元数据。图片字节走 `/api/plan/underlay/image`，不在这份 JSON 里。 */
export type Underlay = {
  contentType: 'image/png' | 'image/jpeg' | 'image/webp'
  widthPx: number
  heightPx: number
  sha256: string
  /** 一个图片像素等于多少世界厘米 */
  cmPerPx: number
  offsetXCm: number
  offsetYCm: number
  rotationDeg: 0 | 90 | 180 | 270
  opacity: number
  visible: boolean
  updatedAt: string
}

/** 底图元数据补丁：只传要改的字段。 */
export type UnderlayPatch = {
  cmPerPx?: number
  offsetXCm?: number
  offsetYCm?: number
  rotationDeg?: 0 | 90 | 180 | 270
  opacity?: number
  visible?: boolean
}

/** 整份替换时的墙：带 id 表示「这面墙原来就在」，撤销删除靠它保住 id。 */
export type WallSnapshot = {
  id?: string
  ax: number
  ay: number
  bx: number
  by: number
  thickness?: number
  arcExtent?: number
}

/** 读当前户型。 */
export const getPlan = () => request<Plan>('/api/plan')

/** 画一面墙；返回保存后的整个户型。 */
export const createWall = async (input: {
  ax: number
  ay: number
  bx: number
  by: number
  thickness?: number
  arcExtent?: number
}): Promise<{ plan: Plan; existing: boolean }> => {
  const { body, res } = await requestWithResponse<Plan>('/api/walls', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  // 幂等创建：同端点、同弧度的墙本来就存在时，服务端不改数据、只带这个头回来。
  return { plan: body, existing: res.headers.get('X-Wall-Existing') === '1' }
}

/** 改一面墙的端点或厚度（拖端点微调长度与角度走这条）。 */
export const updateWall = (
  id: string,
  // PATCH 是部分更新：只传要改的字段，服务端会保留没传的（弧、厚度、端点）。
  input: { ax?: number; ay?: number; bx?: number; by?: number; thickness?: number; arcExtent?: number },
) => request<Plan>(`/api/walls/${id}`, { method: 'PATCH', body: JSON.stringify(input) })

/** 删一面墙。墙上的门窗与挂墙物品会被级联删除。 */
export const deleteWall = (id: string) => request<void>(`/api/walls/${id}`, { method: 'DELETE' })

/** 在墙上开一个门/窗；返回保存后的整个户型。 */
export const createOpening = (input: OpeningInput) =>
  request<Plan>('/api/openings', { method: 'POST', body: JSON.stringify(input) })

/** 改开口的偏移/宽高/离地。 */
export const updateOpening = (id: string, input: OpeningPatch) =>
  request<Plan>(`/api/openings/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })

/** 删开口（幂等：不存在也算成功）。 */
export const deleteOpening = (id: string) =>
  request<void>(`/api/openings/${encodeURIComponent(id)}`, { method: 'DELETE' })

export type ItemKind = 'furniture' | 'device'
export type ItemStatus = 'normal' | 'idle' | 'maintenance'

/** 家具或设备实例；kind/roomName 由后端 join 出来。 */
export type Item = {
  id: string
  typeKey: string
  kind: ItemKind
  name: string
  roomHash?: string
  roomName?: string
  x: number
  y: number
  rotation: number
  status?: ItemStatus
  attrs?: Record<string, unknown>
  /** 挂墙物品：位置由 (wallId, wallOffset) 决定，x/y/rotation 是服务端算出来的派生值 */
  wallId?: string
  wallOffset?: number
}

export type ItemFilter = {
  room?: string
  kind?: ItemKind
  q?: string
}

export type ItemInput = {
  typeKey: string
  name: string
  roomHash?: string
  x: number
  y: number
  rotation?: number
  status?: ItemStatus
  attrs?: Record<string, unknown>
  /** 挂墙物品：给了 wallId 就由服务端按墙重算 x/y/rotation */
  wallId?: string
  wallOffset?: number
}

export type ItemUpdate = {
  name?: string
  roomHash?: string
  x?: number
  y?: number
  rotation?: number
  status?: ItemStatus
  attrs?: Record<string, unknown>
  /** 换一面墙 / 沿墙挪：空字符串表示脱离墙面，回到自由摆放 */
  wallId?: string
  wallOffset?: number
}

/** 物品列表；筛选条件为空时返回全部。 */
export const listItems = (filter: ItemFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.room) params.set('room', filter.room)
  if (filter.kind) params.set('kind', filter.kind)
  if (filter.q) params.set('q', filter.q)
  const query = params.toString()
  return request<Item[]>(`/api/items${query ? `?${query}` : ''}`)
}

/** 新建物品；拖入平面图时使用。 */
export const createItem = (input: ItemInput) =>
  request<Item>('/api/items', { method: 'POST', body: JSON.stringify(input) })

/** 修改物品字段；只传需要改的字段。 */
export const updateItem = (id: string, input: ItemUpdate) =>
  request<Item>(`/api/items/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })

/** 删除物品。 */
export const deleteItem = (id: string) =>
  request<void>(`/api/items/${encodeURIComponent(id)}`, { method: 'DELETE' })

/**
 * 整份替换户型（撤销/重做）。
 *
 * 版本号对不上会拿到 409 —— 那是「别处已经改过」，调用方应重新取全量，
 * 而不是把自己的旧快照覆盖上去。
 */
export const replacePlan = (input: { version: number; walls: WallSnapshot[] }) =>
  request<Plan>('/api/plan', {
    method: 'PUT',
    body: JSON.stringify(input),
  })

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

/** 挂在哪：落地、贴墙、吸顶。挂墙物品必须带 wallId + wallOffset。 */
export type Mount = 'floor' | 'wall' | 'ceiling'

export type ItemType = {
  key: string
  kind: 'furniture' | 'device'
  name: string
  icons?: string
  mount: Mount
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
  mount?: Mount
  defaultWidth?: number
  defaultDepth?: number
  capabilities: Capability[]
  driver?: string
}

export type ItemTypeUpdate = {
  name?: string
  icons?: string
  mount?: Mount
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

/** T8：设备能力命令请求。requestId 用 UUID 作幂等键。 */
export type CommandInput = {
  capabilityKey: string
  value: unknown
  requestId: string
}

export type CommandResult = {
  requestId: string
  status: 'ok'
  value: unknown
}

export type CommandLog = {
  id: number
  requestId: string
  itemId?: string
  capabilityKey?: string
  target?: unknown
  status: 'ok' | 'failed' | 'timeout' | 'rejected'
  error?: string
  source: 'manual' | 'automation' | 'device' | 'timeout'
  createdAt: string
  completedAt?: string
  durationMs?: number
}

export const sendItemCommand = (itemId: string, input: CommandInput) =>
  request<CommandResult>(`/api/items/${encodeURIComponent(itemId)}/commands`, {
    method: 'POST',
    body: JSON.stringify(input),
  })

export const listCommandLogs = (limit = 50) =>
  request<CommandLog[]>(`/api/command-logs?limit=${encodeURIComponent(limit)}`)

export type CapabilityState = {
  itemId: string
  capabilityKey: string
  value: unknown
  source: 'manual' | 'automation' | 'device' | 'timeout'
  updatedAt: string
}

export const listItemStates = (itemId: string) =>
  request<CapabilityState[]>(`/api/items/${encodeURIComponent(itemId)}/states`)

export type SimTickResult = { minutes: number; simulatedAt: string }

export const advanceSimulation = (minutes: number) =>
  request<SimTickResult>('/api/sim/tick', {
    method: 'POST',
    body: JSON.stringify({ minutes }),
  })

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

/** 给房间起名或改用途。哈希必须是当前户型里存在的房间。 */
export const updateRoomName = (hash: string, input: { name: string; kind?: RoomKind }) =>
  request<Plan>(`/api/rooms/${encodeURIComponent(hash)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })

/** 清掉房间名。幂等：失配的旧名字也用它清理。 */
export const clearRoomName = (hash: string) =>
  request<void>(`/api/rooms/${encodeURIComponent(hash)}`, { method: 'DELETE' })

/** 底图的图片地址。带上 sha 做缓存键：换图就换 URL，旧缓存自然失效。 */
export const underlayImageUrl = (sha: string) => `/api/plan/underlay/image?sha=${encodeURIComponent(sha)}`

/** 上传底图：请求体是图片原始字节，所以不能走 `request()`（它固定发 JSON）。 */
export async function uploadUnderlay(file: File): Promise<Plan> {
  const res = await fetch('/api/plan/underlay', {
    method: 'PUT',
    credentials: 'same-origin',
    headers: {
      'X-Requested-With': 'fetch',
      'Content-Type': 'application/octet-stream',
    },
    body: file,
  })
  const text = await res.text()
  const body: unknown = text ? JSON.parse(text) : null
  if (!res.ok) {
    const err = body as ApiErrorBody | null
    throw new ApiError(
      res.status,
      err?.error?.code ?? 'INTERNAL',
      err?.error?.message ?? `上传失败（HTTP ${res.status}）`,
    )
  }
  return body as Plan
}

/** 改底图的定标与显示参数（仅管理员）。 */
export const updateUnderlay = (patch: UnderlayPatch) =>
  request<Plan>('/api/plan/underlay', { method: 'PATCH', body: JSON.stringify(patch) })

/** 删掉底图（仅管理员）；幂等。 */
export const clearUnderlay = () => request<void>('/api/plan/underlay', { method: 'DELETE' })

/** 场景条件与步骤树类型，与 openapi.yaml 对齐。 */
export type ModeCondition = {
  itemId: string
  capability: string
  op: '==' | '!=' | '>' | '>=' | '<' | '<='
  value: unknown
}

export type ModeGuard = {
  all?: ModeCondition[]
  any?: ModeCondition[]
}

export type ModeNode = {
  nodeId: string
  type: 'action' | 'delay' | 'choose'
  alias?: string
  enabled?: boolean
  continueOnError?: boolean
  itemId?: string
  capability?: string
  value?: unknown
  valueFrom?: 'comfort'
  ms?: number
  when?: ModeGuard
  then?: ModeNode[]
  else?: ModeNode[]
}

export type ModeDefinition = {
  version: number
  guard?: ModeGuard
  defaults?: { continueOnError?: boolean }
  steps: ModeNode[]
}

export type Mode = {
  id: string
  name: string
  definition: ModeDefinition
  runMode: 'single' | 'restart' | 'queued' | 'parallel'
  enabled: boolean
  createdBy?: string
  createdAt: string
  updatedAt: string
}

export type RunStepStatus = 'pending' | 'running' | 'ok' | 'failed' | 'skipped'

export type RunStep = {
  runId: string
  nodeId: string
  path: string
  alias?: string
  status: RunStepStatus
  error?: string
  startedAt?: string
  finishedAt?: string
}

export type RunStatus = 'running' | 'ok' | 'failed' | 'cancelled'

export type Run = {
  id: string
  modeId?: string
  status: RunStatus
  source: 'manual' | 'automation' | 'device' | 'timeout'
  startedAt: string
  finishedAt?: string
  steps: RunStep[]
}

/** 列出所有场景模式。 */
export const listModes = () => request<Mode[]>('/api/modes')

/** 获取单个场景模式详情。 */
export const getMode = (id: string) => request<Mode>(`/api/modes/${encodeURIComponent(id)}`)

/** 触发执行场景模式，返回 202 与初始 Run。 */
export const runMode = (id: string, requestId?: string) =>
  request<Run>(`/api/modes/${encodeURIComponent(id)}/run`, {
    method: 'POST',
    body: JSON.stringify(requestId ? { requestId } : {}),
  })

/** 查询一次执行的最新状态与步骤时间线（用于刷新/重连恢复）。 */
export const getRun = (id: string) => request<Run>(`/api/runs/${encodeURIComponent(id)}`)

/** 取消正在运行的场景执行。 */
export const cancelRun = (id: string) =>
  request<Run>(`/api/runs/${encodeURIComponent(id)}/cancel`, { method: 'POST' })
