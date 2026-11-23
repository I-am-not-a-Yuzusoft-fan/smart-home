import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type DragEvent as ReactDragEvent } from 'react'
import { Circle, Group, Image as KonvaImage, Layer, Line, Path, Rect, Stage, Text } from 'react-konva'
import type Konva from 'konva'
import {
  ApiError,
  createItem,
  createOpening,
  clearRoomName,
  clearUnderlay,
  createWall,
  deleteItem,
  deleteOpening,
  deleteWall,
  getPlan,
  listItems,
  listItemTypes,
  logout,
  updateItem,
  updateRoomName,
  updateUnderlay,
  uploadUnderlay,
  underlayImageUrl,
  updateWall,
  type Corner,
  type Item,
  type ItemType,
  type Opening,
  type Plan,
  type Room,
  type RoomKind,
  type SessionUser,
  type Underlay,
  type UnderlayPatch,
  type Wall,
} from '../api/client.ts'
import ItemInspector from '../components/ItemInspector.tsx'
import ItemSidebar, { type ItemFilters } from '../components/ItemSidebar.tsx'
import { UnderlayPanel } from '../components/UnderlayPanel.tsx'
import TypeManager from '../components/TypeManager.tsx'
import { tidy, wallAngle, wallLength, withAngle, withEndpoint, withLength } from '../features/plan/edit.ts'
import {
  arcCircle,
  arcLength,
  arcPointAt,
  arcPoints,
  clampWallOffset,
  distanceToPolyline,
  distanceToSegment,
  nearestWallAnchor,
  pointInPolygon,
  type Pt,
  type Seg,
} from '../features/plan/geometry.ts'
import { DEFAULT_RULES, snap as snapTo, type Guide as SnapGuide, type SnapRule } from '../features/plan/snap.ts'
import {
  axisSegments,
  collinearRuns,
  outerRunsInBounds,
  polygonCentroid,
  type AxisSegment,
} from '../features/plan/dimensions.ts'
import {
  calibrate,
  imageCenter,
  imageRect,
  imageToPlan,
  planToImage,
  type UnderlayTransform,
} from '../features/plan/underlay.ts'
import { replacePlan, type WallSnapshot } from '../api/client.ts'
import { sampleAsFile, syntheticCalibrations, type UnderlaySample } from '../features/plan/samples.ts'

type Props = {
  user: SessionUser
  onSignedOut: () => void
}

/** 网格步长（厘米）。缩得太小时会翻倍，避免一屏几千条线。 */
const GRID_CM = 20
/** 1 厘米 = 1 世界单位。户型按厘米存，画布也按厘米画，中间不做单位换算。 */
const PX_PER_CM = 1
/** 端点/墙线吸附半径（屏幕像素）。换算成世界单位要除以缩放。 */
const SNAP_PX = 12
/** 短于这个长度的拖拽当成点击，不建墙（屏幕像素）。 */
/** 元数据 → 纯函数用的变换参数：字段名对得上，只是把接口类型收窄成数学类型。 */
function transformOf(u: Underlay): UnderlayTransform {
  return {
    widthPx: u.widthPx,
    heightPx: u.heightPx,
    cmPerPx: u.cmPerPx,
    offsetXCm: u.offsetXCm,
    offsetYCm: u.offsetYCm,
    rotationDeg: u.rotationDeg,
  }
}

const MIN_DRAG_PX = 8
// 下限要低于 25%：annotations 占的是一圈固定的屏幕像素（约 110px），
// 窄窗口里 25% 会把户型撑到边缘，最外一排尺寸被画布裁掉。
const ZOOM_MIN = 0.18
const ZOOM_MAX = 4

type Viewport = { scale: number; x: number; y: number }

/** 墙属性面板的草稿值；一律用字符串，避免"输入 4." 时被数字回写打断。 */
type WallForm = {
  length: string
  angle: string
  thickness: string
  arc: string
  ax: string
  ay: string
  bx: string
  by: string
}

function newWallForm(w: Wall): WallForm {
  return {
    arc: String(w.arcExtent ?? 0),
    length: (wallLength(w) / 100).toFixed(2),
    angle: wallAngle(w).toFixed(1),
    thickness: String(tidy(w.thickness)),
    ax: String(tidy(w.ax)),
    ay: String(tidy(w.ay)),
    bx: String(tidy(w.bx)),
    by: String(tidy(w.by)),
  }
}

type Draft = {
  /** 起点（世界坐标，已吸附） */
  ax: number
  ay: number
  /** 当前点（世界坐标，已吸附） */
  bx: number
  by: number
  /** 拉弧模式下正在调的角度（度），松手落库前一直为空 */
  arcExtent?: number
}

type Edit = { wallId: string; end: 'a' | 'b'; ax: number; ay: number; bx: number; by: number }

type Guide = SnapGuide<Wall>

type SnapResult = { x: number; y: number; guide: Guide }

/**
 * 平面图主页（首屏）。
 *
 * T2：拖出墙、端点吸附、长度显示、刷新后仍在、可删除。
 * T21：视口（平移/缩放/回到全览）、选中（悬停高亮、标签可点）、
 *      吸附正交（端点、墙线投影、0°/90°）、Esc/右键取消。
 *
 * 坐标一律按厘米与后端对话；屏幕上 1 cm 画 1 px，缩放交给 Stage。
 */
export default function PlanPage({ user, onSignedOut }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const stageRef = useRef<Konva.Stage>(null)
  // 撤销/重做存的是「整份墙快照」而不是操作序列：墙的 id 由服务端生成，
  // 删了再建会换 id，门窗的 wall_id 与前置的选中态都会断，所以只能整份写回。
  const [history, setHistory] = useState<{ past: WallSnapshot[][]; future: WallSnapshot[][] }>({
    past: [],
    future: [],
  })
  const [showUnderlay, setShowUnderlay] = useState(false)
  // 画布左上角那行操作提示：一开始给新用户看，真开始缩放/平移之后就淡出——
  // 它没有背景，高倍时底下的墙和标注正好从字后面穿过去，看着像字糊在一起。
  // 提示只在操作时淡一点，停手 3 秒再亮回来：彻底藏起来用户就再也找不到了。
  const [hintDimmed, setHintDimmed] = useState(false)
  const hintTimer = useRef<number | null>(null)
  const dimHint = () => {
    setHintDimmed(true)
    if (hintTimer.current !== null) window.clearTimeout(hintTimer.current)
    hintTimer.current = window.setTimeout(() => setHintDimmed(false), 3000)
  }
  const showHint = () => {
    if (hintTimer.current !== null) window.clearTimeout(hintTimer.current)
    hintTimer.current = null
    setHintDimmed(false)
  }
  const [underlayBusy, setUnderlayBusy] = useState(false)
  const [underlayImage, setUnderlayImage] = useState<HTMLImageElement | null>(null)
  // 两点定标：进入后画布点击只标点，不画墙；点满两个再填真实长度。
  const [calibrating, setCalibrating] = useState(false)
  const [calibPoints, setCalibPoints] = useState<Pt[]>([])
  const [size, setSize] = useState({ width: 800, height: 600 })
  // 画布真实尺寸要等 ResizeObserver 报一次；在那之前 fit 会按 800×600 算，
  // 结果内容偏在左上角（第一次打开时尤其明显）。
  const [sizeReady, setSizeReady] = useState(false)
  const [plan, setPlan] = useState<Plan>({
    corners: [],
    walls: [],
    rooms: [],
    openings: [],
    openWalls: 0,
    underlay: null,
    staleRooms: [],
    version: 0,
  })
  // 回调里要读「最新」的户型：闭包捕获的 plan 可能落后一次渲染。
  const planRef = useRef<Plan>(plan)
  planRef.current = plan

  // 底图的「图片像素 ↔ 世界厘米」变换：只从元数据算，不从画布读。
  const underlayTransform = useMemo<UnderlayTransform | null>(
    () => (plan.underlay ? transformOf(plan.underlay) : null),
    [plan.underlay],
  )

  // 图片按 sha 拉一次：换图必然换 sha，所以不必担心拿到旧缓存。
  useEffect(() => {
    const sha = plan.underlay?.sha256
    if (!sha) {
      setUnderlayImage(null)
      return
    }
    let cancelled = false
    const img = new window.Image()
    img.onload = () => {
      if (!cancelled) setUnderlayImage(img)
    }
    img.onerror = () => {
      if (!cancelled) setUnderlayImage(null)
    }
    img.src = underlayImageUrl(sha)
    return () => {
      cancelled = true
    }
  }, [plan.underlay?.sha256])

  /** 底图相关的异步操作都有同一套收尾：置忙、统一报错、复位忙。 */
  const withUnderlayBusy = async (fallback: string, run: () => Promise<void>) => {
    setUnderlayBusy(true)
    try {
      await run()
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : fallback)
    } finally {
      setUnderlayBusy(false)
    }
  }

  const saveUnderlay = (file: File) =>
    withUnderlayBusy('上传底图失败', async () => {
      setPlan(await uploadUnderlay(file))
      // 换图是一次硬操作：撤销栈里的快照只含墙，回退过去会让人误以为能"撤销换图"。
      setHistory({ past: [], future: [] })
    })

  const dropUnderlay = () =>
    withUnderlayBusy('删除底图失败', async () => {
      await clearUnderlay()
      setPlan(await getPlan())
      setHistory({ past: [], future: [] })
    })

  /** 示例图：上传后若图自带定标点，顺手把比例与应用内真实尺寸对上。 */
  const loadSample = (sample: UnderlaySample) =>
    withUnderlayBusy('示例图加载失败', async () => {
      const next = await uploadUnderlay(await sampleAsFile(sample))
      const calibration = (await syntheticCalibrations()).get(sample.path)
      if (!calibration || !next.underlay) {
        setPlan(next)
        return
      }
      const t = transformOf(next.underlay)
      // 钉住第一个标定点，只改比例与平移——和手动定标走同一个函数。
      const anchor = imageToPlan(calibration.p1, t)
      setPlan(
        await updateUnderlay(
          calibrate(calibration.p1, calibration.p2, anchor, calibration.lengthCm, t),
        ),
      )
    })

  const patchUnderlay = (patch: UnderlayPatch) =>
    withUnderlayBusy('修改底图失败', async () => {
      setPlan(await updateUnderlay(patch))
    })

  const startCalibration = () => {
    if (!plan.underlay) return
    setCalibrating(true)
    setCalibPoints([])
    setError(null)
  }

  const cancelCalibration = () => {
    setCalibrating(false)
    setCalibPoints([])
  }

  const applyCalibration = (lengthCm: number) => {
    if (!underlayTransform || calibPoints.length !== 2) return
    return withUnderlayBusy('定标失败', async () => {
      const [p1, p2] = calibPoints
      // 用户点的是世界坐标；定标要在图片像素里量距离，所以反解回去。
      const u1 = planToImage(p1, underlayTransform)
      const u2 = planToImage(p2, underlayTransform)
      setPlan(await updateUnderlay(calibrate(u1, u2, p1, lengthCm, underlayTransform)))
      setCalibPoints([])
      setCalibrating(false)
    })
  }

  // 两个点在图上的当前距离：给用户一个「现在是多少」的参照，默认值也用它。
  const calibMeasureCm =
    calibPoints.length === 2
      ? Math.round(Math.hypot(calibPoints[1].x - calibPoints[0].x, calibPoints[1].y - calibPoints[0].y))
      : null
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  // 拉弧模式：Ctrl/Alt 松手后不马上建墙，先跟着鼠标定弧度，再点一下落库。
  const [bend, setBend] = useState<Draft | null>(null)
  // macOS 的 Ctrl+点击是系统右键，拉弧只能用 Option；Windows/Linux 上是 Ctrl。
  const arcModifier = /mac|iphone|ipad/i.test(
    (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ??
      navigator.platform,
  )
    ? '⌥'
    : 'Ctrl'
  const [guide, setGuide] = useState<Guide>({ kind: 'none' })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  // 物品与墙各自有选中态，但同一时刻只允许一个：点墙清物品，点物品清墙。
  const [items, setItems] = useState<Item[]>([])
  const [types, setTypes] = useState<ItemType[]>([])
  const [selectedItemId, setSelectedItemId] = useState<string | null>(null)
  // 房间按哈希选中：几何由后端推导，前端只认这个键。
  const [selectedRoomHash, setSelectedRoomHash] = useState<string | null>(null)
  const [filters, setFilters] = useState<ItemFilters>({ room: '', kind: '', q: '' })
  const [showTypes, setShowTypes] = useState(false)
  const [showDims, setShowDims] = useState(true)
  // 正在拖的端点：整段坐标都存下来，渲染预览时直接用，不必回头找 wall。
  const [editing, setEditing] = useState<Edit | null>(null)
  const [hoverId, setHoverId] = useState<string | null>(null)
  const [viewport, setViewport] = useState<Viewport>({ scale: 1, x: 0, y: 0 })
  const [spaceDown, setSpaceDown] = useState(false)
  // 网格吸附默认关：户型以真实尺寸为准，网格是辅助对齐，不该在你没要求时替你改数字。
  const [gridSnap, setGridSnap] = useState(false)
  const [wallForm, setWallForm] = useState<WallForm | null>(null)
  const [workSeg, setWorkSeg] = useState<Seg | null>(null)
  const lengthRef = useRef<HTMLInputElement>(null)
  // 刚画完一面墙时把焦点放到长度输入上，这样"画完就能输数字"不用先点一下。
  const focusLengthRef = useRef(false)
  // 只接受"最后一次请求"的响应：连拖两面墙时，先发的请求可能后到，
  // 若整份覆盖就会把后画的墙抹掉（刷新才回来，用户以为丢了）。
  const writeSeq = useRef(0)
  // 按下时落点与命中的墙：松手时若几乎没动，就当成"点选"，而不是画一面极短的墙。
  const pressRef = useRef<{ x: number; y: number; wallId: string | null; roomHash: string | null } | null>(
    null,
  )
  // 按下时就把「要不要拉弧」记下来：先松 ⌥ 再松鼠标的人很多，
  // 只认 mouseup 那一刻的 altKey 会静默退化成画直墙。
  const arcKeyRef = useRef(false)
  // 按住修饰键时给一句提示，否则「拖出弧」这个手势没人猜得到。
  const [arcKeyHeld, setArcKeyHeld] = useState(false)
  // 画弧模式：不用按修饰键，先点这个按钮再拖，拖完松手就在等定弧度。
  const [arcMode, setArcMode] = useState(false)
  const panRef = useRef<{ sx: number; sy: number; vx: number; vy: number } | null>(null)
  const fittedRef = useRef(false)

  useEffect(() => {
    const onDown = (e: KeyboardEvent) => {
      if (e.key === 'Alt' || e.key === 'Control') setArcKeyHeld(true)
    }
    const onUp = (e: KeyboardEvent) => {
      if (e.key === 'Alt' || e.key === 'Control') setArcKeyHeld(false)
    }
    const onBlur = () => setArcKeyHeld(false)
    window.addEventListener('keydown', onDown)
    window.addEventListener('keyup', onUp)
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('keydown', onDown)
      window.removeEventListener('keyup', onUp)
      window.removeEventListener('blur', onBlur)
    }
  }, [])

  const load = useCallback(async () => {
    try {
      const [nextPlan, nextItems, nextTypes] = await Promise.all([getPlan(), listItems(), listItemTypes()])
      setPlan(nextPlan)
      setItems(nextItems)
      setTypes(nextTypes)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取户型失败')
    } finally {
      setLoading(false)
    }
  }, [])

  const typeByKey = useMemo(() => new Map(types.map((typ) => [typ.key, typ])), [types])
  const selectedItem = items.find((item) => item.id === selectedItemId) ?? null
  const selectedItemType = selectedItem ? typeByKey.get(selectedItem.typeKey) : undefined
  const selectedRoom = plan.rooms.find((room) => room.hash === selectedRoomHash) ?? null

  // 户型一变（撤销、别人改墙），选中的房间可能已经不在了；留着面板会指向幽灵。
  useEffect(() => {
    if (selectedRoomHash && !plan.rooms.some((room) => room.hash === selectedRoomHash)) {
      setSelectedRoomHash(null)
    }
  }, [plan.rooms, selectedRoomHash])

  async function saveRoomName(hash: string, name: string, kind: RoomKind) {
    try {
      setPlan(await updateRoomName(hash, { name, kind }))
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存房间名失败')
    }
  }

  async function forgetRoomName(hash: string) {
    try {
      await clearRoomName(hash)
      setPlan(await getPlan())
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '清除房间名失败')
    }
  }

  async function forgetAllStaleRooms() {
    try {
      for (const stale of plan.staleRooms) {
        await clearRoomName(stale.hash)
      }
      setPlan(await getPlan())
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '清理失配房间名失败')
    }
  }

  // 房间命中：后端已经把多边形算好了，这里只做点面判断。
  function roomAt(x: number, y: number): string | null {
    for (const room of plan.rooms) {
      if (pointInPolygon({ x, y }, room.ring)) return room.hash
    }
    return null
  }

  useEffect(() => {
    void load()
  }, [load])


  // 首帧先量一次：ResizeObserver 的首次回调在某些环境（无头浏览器、隐藏标签页）
  // 要等到真正绘制才来，而 fit 必须拿到真实尺寸，否则内容会按 800×600 摆偏。
  useLayoutEffect(() => {
    const host = hostRef.current
    if (!host) return
    const r = host.getBoundingClientRect()
    if (r.width > 0 && r.height > 0) {
      setSize({ width: Math.max(r.width, 320), height: Math.max(r.height, 240) })
      setSizeReady(true)
    }
  }, [])

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      setSize({ width: Math.max(width, 320), height: Math.max(height, 240) })
      setSizeReady(true)
    })
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  /** 让所有墙正好落进视口，四周留 60 px。空户型就回到 1:1 原点。 */
  const snapshotOf = useCallback(
    (p: Plan): WallSnapshot[] =>
      p.walls.map((w) => ({ id: w.id, ax: w.ax, ay: w.ay, bx: w.bx, by: w.by, thickness: w.thickness })),
    [],
  )

  /** 记一次「改动前」的快照；新改动会清空重做栈（分叉的历史没有意义）。 */
  const remember = useCallback(
    (before: WallSnapshot[]) =>
      setHistory((h) => ({ past: [...h.past, before].slice(-50), future: [] })),
    [],
  )

  /**
   * 把某一份快照写回服务端。
   *
   * 409 表示别处已经改过——这时用旧快照覆盖会静默回滚别人的修改，
   * 所以重新取全量并清空历史，把冲突摆到用户面前而不是替它做决定。
   */
  const applySnapshot = useCallback(
    async (target: WallSnapshot[]) => {
      setSaving(true)
      try {
        const next = await replacePlan({ version: planRef.current.version, walls: target })
        setPlan(next)
        setSelectedId(null)
        setError(null)
      } catch (e) {
        if (e instanceof ApiError && e.status === 409) {
          setHistory({ past: [], future: [] })
          try {
            setPlan(await getPlan())
          } catch {
            /* 取不到就保持现状，下一轮再说 */
          }
          setError('户型已被别处修改，已刷新到最新状态')
        } else {
          setError(e instanceof Error ? e.message : '操作失败')
        }
      } finally {
        setSaving(false)
      }
    },
    [],
  )

  const undo = useCallback(async () => {
    const past = history.past
    if (past.length === 0) return
    const current = snapshotOf(planRef.current)
    const target = past[past.length - 1]
    setHistory({ past: past.slice(0, -1), future: [current, ...history.future].slice(0, 50) })
    await applySnapshot(target)
  }, [history.past, history.future, snapshotOf, applySnapshot])

  const redo = useCallback(async () => {
    const future = history.future
    if (future.length === 0) return
    const current = snapshotOf(planRef.current)
    const target = future[0]
    setHistory({ past: [...history.past, current].slice(-50), future: future.slice(1) })
    await applySnapshot(target)
  }, [history.past, history.future, snapshotOf, applySnapshot])

  const fitToContent = useCallback(() => {
    showHint()
    if (plan.corners.length === 0) {
      setViewport({ scale: 1, x: 0, y: 0 })
      return
    }
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    for (const c of plan.corners) {
      minX = Math.min(minX, c.x)
      minY = Math.min(minY, c.y)
      maxX = Math.max(maxX, c.x)
      maxY = Math.max(maxY, c.y)
    }
    // 三级标注画在户型外侧（墙厚/2 + 102px 起），全览必须留出这一圈，
    // 否则最外一排总尺寸会被裁在视口外。132 是「102 + 数字高度 + 余量」。
    const pad = 120
    const bw = Math.max(maxX - minX, 1)
    const bh = Math.max(maxY - minY, 1)
    const scale = clamp(Math.min((size.width - pad * 2) / bw, (size.height - pad * 2) / bh), ZOOM_MIN, ZOOM_MAX)
    setViewport({
      scale,
      x: (size.width - bw * scale) / 2 - minX * scale,
      y: (size.height - bh * scale) / 2 - minY * scale,
    })
  }, [plan.corners, size.height, size.width])

  // 首次读到有内容的户型时自动全览一次；之后用户自己调，不再插手。
  useEffect(() => {
    if (loading || !sizeReady || fittedRef.current || plan.corners.length === 0) return
    fittedRef.current = true
    fitToContent()
  }, [fitToContent, loading, plan.corners.length, sizeReady])

  // 删墙也支持键盘：选中后按 Delete / Backspace；Esc 取消正在画的墙。
  useEffect(() => {
    function isTyping(target: EventTarget | null) {
      const el = target as HTMLElement | null
      return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
    }
    function onKey(e: KeyboardEvent) {
      if (e.code === 'Space' && !isTyping(e.target)) {
        e.preventDefault()
        setSpaceDown(true)
      }
      if (e.key === 'Escape' && isTyping(e.target)) {
        ;(e.target as HTMLElement).blur()
        setDraft(null)
        setEditing(null)
        setGuide({ kind: 'none' })
        return
      }
      if (isTyping(e.target)) return
      // Ctrl/Cmd+Z 撤销，再加 Shift 重做；Ctrl+Y 也认，习惯 Windows 的人会按。
      const mod = e.metaKey || e.ctrlKey
      if (mod && (e.key === 'z' || e.key === 'Z')) {
        e.preventDefault()
        if (e.shiftKey) void redo()
        else void undo()
        return
      }
      if (mod && (e.key === 'y' || e.key === 'Y')) {
        e.preventDefault()
        void redo()
        return
      }
      if (e.key === 'Escape') {
        setCalibrating(false)
        setCalibPoints([])
        setDraft(null)
        setBend(null)
        setEditing(null)
        setGuide({ kind: 'none' })
        return
      }
      if (e.key !== 'Delete' && e.key !== 'Backspace') return
      if (selectedItemId) {
        void removeItem(selectedItemId)
        return
      }
      if (selectedId) void removeWall(selectedId)
    }
    function onKeyUp(e: KeyboardEvent) {
      if (e.code === 'Space') setSpaceDown(false)
    }
    function onBlur() {
      setSpaceDown(false)
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('keyup', onKeyUp)
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('keyup', onKeyUp)
      window.removeEventListener('blur', onBlur)
    }
    // removeWall/undo/redo 每次渲染都是新函数，但内部只用 setState 与 props，闭包安全
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, selectedItemId, history.past.length, history.future.length])

  // 只认"被墙引用"的端点：删墙后留下的孤立端点既不该画出来，也不该被吸附——
  // 否则用户会吸到一个看不见的点上，还以为是 bug。
  // 墙的四个角点：两端各按「斜接」裁一刀。
  //
  // 以前的画法是四边形沿轴线外延半个墙厚——90° 恰好拼成方角，斜角就会探出方头，
  // 放大看像墙头被啃了一口。现在把每面墙的端面直接取成**斜接线**：
  // 两面墙在转角处共用同一条 `M外—M内` 线段，任意夹角都严丝合缝，
  // 直角时它就是那条 45° 斜接线，和制图习惯一致。
  const { wallQuads } = useMemo(() => {
    const byCorner = new Map<string, typeof plan.walls>()
    for (const wall of plan.walls) {
      for (const id of [wall.aId, wall.bId]) {
        const list = byCorner.get(id)
        if (list) list.push(wall)
        else byCorner.set(id, [wall])
      }
    }
    const unit = (dx: number, dy: number): Pt | null => {
      const len = Math.hypot(dx, dy)
      return len < 1e-6 ? null : { x: dx / len, y: dy / len }
    }
    /**
     * 从 p 出发、沿这面墙指向另一端的方向。
     *
     * 弧墙必须给出**端点切线**而不是弦方向：转角处的斜接是按两墙方向求平分线的，
     * 一边用切线、一边用弦，算出来的端面就对不上，放大看是一道楔形缺口。
     */
    const outOf = (wall: Wall, p: Pt): Pt | null => {
      const da = Math.hypot(wall.ax - p.x, wall.ay - p.y)
      const db = Math.hypot(wall.bx - p.x, wall.by - p.y)
      if (Math.min(da, db) > 1) return null
      if (Math.abs(wall.arcExtent) >= 0.5) {
        const pts = arcPoints({ x: wall.ax, y: wall.ay }, { x: wall.bx, y: wall.by }, wall.arcExtent)
        const n = pts.length
        if (n >= 3) {
          return da <= db
            ? unit(pts[1].x - pts[0].x, pts[1].y - pts[0].y)
            : unit(pts[n - 2].x - pts[n - 1].x, pts[n - 2].y - pts[n - 1].y)
        }
      }
      return da <= db ? unit(wall.bx - wall.ax, wall.by - wall.ay) : unit(wall.ax - wall.bx, wall.ay - wall.by)
    }
    /** 两墙夹角的一半正弦；自由端 / T 型路口返回 null。 */
    const sinHalfAt = (wall: Wall, p: Pt, corner: typeof plan.walls): number | null => {
      if (corner.length !== 2) return null
      const a = outOf(wall, p)
      const other = corner[0] === wall ? corner[1] : corner[0]
      const b = outOf(other, p)
      if (!a || !b) return null
      const cos = Math.min(1, Math.max(-1, a.x * b.x + a.y * b.y))
      return Math.sin(Math.acos(cos) / 2)
    }
    // 法线必须始终按 a→b 方向算。以前用的是「从 p 指向另一端」的方向，
    // 到 b 端就反了过来，端面两点的正负号跟着翻转，四边形被拧成蝴蝶结，
    // 画出来就是转角处飞出去的尖刺。
    const wallNormal = (wall: Wall): Pt => {
      const dir = unit(wall.bx - wall.ax, wall.by - wall.ay)
      return dir ? { x: -dir.y, y: dir.x } : { x: 1, y: 0 }
    }
    const endFace = (
      wall: Wall,
      p: Pt,
      corner: typeof plan.walls,
      normal: Pt,
      dir: Pt | null,
    ): [Pt, Pt] => {
      const half = Math.max(wall.thickness, 4) / 2
      const flat = (): [Pt, Pt] => [
        { x: p.x + normal.x * half, y: p.y + normal.y * half },
        { x: p.x - normal.x * half, y: p.y - normal.y * half },
      ]
      const sinHalf = sinHalfAt(wall, p, corner)
      if (!dir || sinHalf === null) return flat()
      // 所有转角统一走斜接，只用「斜接上限」防尖刺：
      // 上限内是标准斜接（90° 就是方角），超了就把斜接点截短，
      // 锐角于是变成一个短短的尖，不会甩出长针。两面墙共用同一个 reach，
      // 端面始终重合，所以不会再出现「一个圆角一个尖角」这种混合接头。
      const other = corner[0] === wall ? corner[1] : corner[0]
      const otherDir = outOf(other, p)
      if (!otherDir) return flat()
      const sum = { x: dir.x + otherDir.x, y: dir.y + otherDir.y }
      const sumLen = Math.hypot(sum.x, sum.y)
      if (sumLen < 1e-6) return flat()
      const shared = Math.max(half, Math.max(other.thickness, 4) / 2)
      // 锐角时 shared/sinHalf 会甩出很长的针，夹在 1.6 倍半厚以内；sinHalf 太小时退平头。
      if (sinHalf < 0.3) return flat()
      const reach = Math.min(shared / sinHalf, 1.6 * shared)
      const ux = sum.x / sumLen
      const uy = sum.y / sumLen
      const a = { x: p.x - ux * reach, y: p.y - uy * reach }
      const b = { x: p.x + ux * reach, y: p.y + uy * reach }
      const da = a.x * normal.x + a.y * normal.y
      const db = b.x * normal.x + b.y * normal.y
      return da >= db ? [a, b] : [b, a]
    }
    const quads = new Map<string, Pt[]>()
    for (const wall of plan.walls) {
      const a = { x: wall.ax, y: wall.ay }
      const b = { x: wall.bx, y: wall.by }
      if (Math.abs(wall.arcExtent) >= 0.5) {
        // 弧墙：沿弧取偏移点拼成一条带，两端仍用斜接端面（按端点切线算，
        // 不能用弦方向，否则 90° 以上的弧端面会明显偏）。
        const pts = arcPoints(a, b, wall.arcExtent)
        const half = Math.max(wall.thickness, 4) / 2
        const n = pts.length
        const tangentAt = (i: number): Pt | null => {
          const prev = pts[Math.max(0, i - 1)]
          const next = pts[Math.min(n - 1, i + 1)]
          return unit(next.x - prev.x, next.y - prev.y)
        }
        const normalAt = (i: number): Pt => {
          const t = tangentAt(i) ?? { x: 1, y: 0 }
          return { x: -t.y, y: t.x }
        }
        const plus: Pt[] = []
        const minus: Pt[] = []
        for (let i = 1; i < n - 1; i += 1) {
          const nm = normalAt(i)
          plus.push({ x: pts[i].x + nm.x * half, y: pts[i].y + nm.y * half })
          minus.push({ x: pts[i].x - nm.x * half, y: pts[i].y - nm.y * half })
        }
        const dirA = unit(pts[1].x - pts[0].x, pts[1].y - pts[0].y)
        const dirB = unit(pts[n - 2].x - pts[n - 1].x, pts[n - 2].y - pts[n - 1].y)
        const [aPlus, aMinus] = endFace(wall, a, byCorner.get(wall.aId) ?? [], normalAt(0), dirA)
        const [bPlus, bMinus] = endFace(wall, b, byCorner.get(wall.bId) ?? [], normalAt(n - 1), dirB)
        quads.set(wall.id, [aPlus, ...plus, bPlus, bMinus, ...minus.reverse(), aMinus])
        continue
      }
      const normal = wallNormal(wall)
      const dir = outOf(wall, a)
      const [aPlus, aMinus] = endFace(wall, a, byCorner.get(wall.aId) ?? [], normal, dir)
      const [bPlus, bMinus] = endFace(wall, b, byCorner.get(wall.bId) ?? [], normal, outOf(wall, b))
      quads.set(wall.id, [aPlus, aMinus, bMinus, bPlus])
    }
    // 斜接是端点自带的，不再额外补圆点——以前锐角补圆、直角斜接，
    // 同一个三角形上就会出现「一个圆角一个尖角」。
    return { wallQuads: quads }
  }, [plan.walls])

  const liveCorners = useMemo(
    () => plan.corners.filter((c) => plan.walls.some((w) => w.aId === c.id || w.bId === c.id)),
    [plan.corners, plan.walls],
  )

  /** 屏幕坐标 → 世界坐标（厘米）。 */
  function toWorld(p: { x: number; y: number }): { x: number; y: number } {
    return { x: (p.x - viewport.x) / viewport.scale, y: (p.y - viewport.y) / viewport.scale }
  }

  /** 平面图的几何中心，用来判断墙的哪一侧算"外侧"。 */
  /** 两级标注的第一级：共线且相接的墙合成一条链，出总长。 */
  const runs = useMemo(() => collinearRuns(plan.walls), [plan.walls])
  /** 只有一面墙的链：它已经开始画外侧尺寸线，墙上的分段标签就重复了，让位。 */
  const singleRunWallIds = useMemo(
    () => new Set(runs.filter((run) => run.wallIds.length === 1).flatMap((run) => run.wallIds)),
    [runs],
  )

  /** 只有外轮廓上的链才画尺寸线；内墙的长度由墙上的标签负责。 */
  const outerRuns = useMemo(() => {
    if (plan.corners.length === 0) return []
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
    for (const c of plan.corners) {
      if (c.x < minX) minX = c.x
      if (c.x > maxX) maxX = c.x
      if (c.y < minY) minY = c.y
      if (c.y > maxY) maxY = c.y
    }
    return outerRunsInBounds(runs, { minX, minY, maxX, maxY })
  }, [runs, plan.corners])

  /**
   * 尺寸只标在**下方与左方**两侧。
   *
   * GB/T 50001—2017 §11.2 要求尺寸线尽量注写在图形轮廓线以外、同一方向的尺寸集中排列；
   * 制图惯例是一个方向只标一侧——开间标在下方、进深标在左方，四边都标属于重复标注。
   * 这里按惯例取「下 + 左」，同时把四边都有的绿色预览、悬停标签留给交互用。
   */
  const dimRuns = useMemo(() => {
    if (outerRuns.length === 0) return []
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
    for (const c of plan.corners) {
      if (c.x < minX) minX = c.x
      if (c.x > maxX) maxX = c.x
      if (c.y < minY) minY = c.y
      if (c.y > maxY) maxY = c.y
    }
    const near = (a: number, b: number) => Math.abs(a - b) < 1
    return outerRuns.filter((run) =>
      run.horizontal
        ? near(run.a.y, maxY) && near(run.b.y, maxY)
        : near(run.a.x, minX) && near(run.b.x, minX),
    )
  }, [outerRuns, plan.corners])

  /** 每个房间的开间×进深：后端的栋深多边形是绕中线走的，这里只报边长，不改几何。 */

  const center = (() => {
    if (plan.corners.length === 0) return { x: 0, y: 0 }
    let sx = 0
    let sy = 0
    for (const c of plan.corners) {
      sx += c.x
      sy += c.y
    }
    return { x: sx / plan.corners.length, y: sy / plan.corners.length }
  })()

  /**
   * 指针是否按在了「选中墙」的端点手柄上。
   *
   * 和 wallAt 一样走几何判定：端点手柄比墙线更"精确"，所以先判它，
   * 否则用户想拖端点时会变成又画出一条新墙。
   */
  function handleAt(
    x: number,
    y: number,
    tolPx = 10,
  ): { wallId: string; end: 'a' | 'b'; ax: number; ay: number; bx: number; by: number } | null {
    const wall = editing
      ? { id: editing.wallId, ax: editing.ax, ay: editing.ay, bx: editing.bx, by: editing.by }
      : selectedId
        ? plan.walls.find((w) => w.id === selectedId)
        : undefined
    if (!wall) return null
    const tol = tolPx / viewport.scale
    const near = (px: number, py: number) => Math.hypot(px - x, py - y) <= tol
    if (near(wall.ax, wall.ay)) {
      return { wallId: wall.id, end: 'a', ax: wall.ax, ay: wall.ay, bx: wall.bx, by: wall.by }
    }
    if (near(wall.bx, wall.by)) {
      return { wallId: wall.id, end: 'b', ax: wall.ax, ay: wall.ay, bx: wall.bx, by: wall.by }
    }
    return null
  }

  /**
   * 指针落在哪面墙上；没落在任何墙上返回 null。
   *
   * 自己算点到线段的距离，不依赖 Konva 的 hit graph：在自定义 Stage 变换下
   * 那一层的 hit canvas 实测是空的，而且命中区只该跟几何和缩放有关。
   */
  function wallAt(x: number, y: number, tolPx = 10): string | null {
    const tol = tolPx / viewport.scale
    let best: string | null = null
    let bestDist = tol
    for (const w of plan.walls) {
      const d =
        Math.abs(w.arcExtent) >= 0.5
          ? distanceToPolyline(
              x,
              y,
              arcPoints({ x: w.ax, y: w.ay }, { x: w.bx, y: w.by }, w.arcExtent),
            )
          : distanceToSegment(x, y, w)
      if (d <= bestDist) {
        bestDist = d
        best = w.id
      }
    }
    return best
  }

  function pointer(): { x: number; y: number } | null {
    const p = stageRef.current?.getPointerPosition()
    return p ? toWorld(p) : null
  }

  /**
   * 吸附：端点 → （正交 ∧ 墙线）交点 → 墙线投影 → 正交 → 1 cm 取整。
   *
   * 正交与墙线同时开时不能只做投影：贴着别人墙中段画时，投影会给出一个
   * 斜点，正交就"消失"了。正确的是求「过锚点的水平/竖直线」与目标墙的交点，
   * 既落在墙上、又保持 0°/90°。交点够不着（平行或落在段外）才退回自由拉。
   */
  const snapRules = useMemo<SnapRule[]>(
    () => DEFAULT_RULES.map((rule) => (rule.kind === 'grid' ? { ...rule, enabled: gridSnap } : rule)),
    [gridSnap],
  )

  /**
   * 吸附落点。规则本体在 `features/plan/snap.ts`：纯函数、可单测。
   * 这里只把当前的端点、墙与缩放喂进去，并把「没命中」时补一次 1 厘米取整。
   */
  function snap(
    x: number,
    y: number,
    opts: { anchor?: { x: number; y: number }; orthogonal?: boolean; ignoreWallId?: string } = {},
  ): SnapResult {
    const out = snapTo<Wall>({
      x,
      y,
      scale: viewport.scale,
      corners: liveCorners,
      walls: plan.walls,
      anchor: opts.anchor ?? null,
      orthogonal: opts.orthogonal ?? false,
      gridCm: GRID_CM,
      rules: snapRules,
      ignoreWallId: opts.ignoreWallId,
    })
    const round = out.kind === 'none'
    return {
      x: round ? Math.round(out.x) : out.x,
      y: round ? Math.round(out.y) : out.y,
      guide: out.guide,
    }
  }

  function onMouseDown(e: Konva.KonvaEventObject<MouseEvent>) {
    arcKeyRef.current = e.evt.altKey || e.evt.ctrlKey
    if (bend) {
      const b = bend
      const extent = Math.round(b.arcExtent ?? 0)
      if (e.evt.button === 0) {
        // 松手后没动鼠标就点了一下：这时弧度还是 0，落定等于画一条直墙，
        // 十有八九还会和已有的墙撞上（409）。直接说清楚，别让人以为"点了没反应"。
        if (Math.abs(extent) < 1) {
          setError('弧度还是 0°：先移动鼠标把弧拉出来，再单击落定')
          return
        }
        setBend(null)
        void submitWall({ ...b, arcExtent: extent })
        return
      }
      setBend(null)
      return
    }
    const stage = stageRef.current
    if (!stage) return
    const raw = stage.getPointerPosition()
    if (!raw) return
    const p = toWorld(raw)

    // 中键或按住空格：拖画布，不画墙。
    if (e.evt.button === 1 || spaceDown) {
      panRef.current = { sx: raw.x, sy: raw.y, vx: viewport.x, vy: viewport.y }
      dimHint()
      return
    }
    if (e.evt.button === 2) {
      // 右键取消进行中的画线或拖动。
      setDraft(null)
      setEditing(null)
      setGuide({ kind: 'none' })
      return
    }

    // 定标模式下点击只落两个点，不画墙、不选中，第三个点重新开始。
    if (calibrating) {
      setCalibPoints((cur) => (cur.length >= 2 ? [p] : [...cur, p]))
      return
    }

    // 先看有没有按在选中墙的端点上——那是拖动改墙，不是画新墙。
    const grabbed = handleAt(p.x, p.y)
    if (grabbed) {
      pressRef.current = { x: p.x, y: p.y, wallId: null, roomHash: null }
      setEditing({ wallId: grabbed.wallId, end: grabbed.end, ax: grabbed.ax, ay: grabbed.ay, bx: grabbed.bx, by: grabbed.by })
      return
    }

    const wallId = wallAt(p.x, p.y)
    // 墙上的点击优先给墙；只有落在空地上才去找房间——否则永远选不中墙。
    const roomHash = wallId ? null : roomAt(p.x, p.y)
    pressRef.current = { x: p.x, y: p.y, wallId, roomHash }
    // 按在空白或墙上都算「离开物品」，否则点完物品再画墙，Delete 会删错东西。
    setSelectedItemId(null)

    const s = snap(p.x, p.y)
    setDraft({ ax: s.x, ay: s.y, bx: s.x, by: s.y })
    setGuide(s.guide)
  }

  function onMouseMove(e: Konva.KonvaEventObject<MouseEvent>) {
    const stage = stageRef.current
    if (!stage) return
    const raw = stage.getPointerPosition()
    if (!raw) return

    if (panRef.current) {
      const start = panRef.current
      setViewport((vp) => ({ ...vp, x: start.vx + (raw.x - start.sx), y: start.vy + (raw.y - start.sy) }))
      return
    }

    const p = toWorld(raw)
    if (editing) {
      const anchor = editing.end === 'a' ? { x: editing.bx, y: editing.by } : { x: editing.ax, y: editing.ay }
      // 拖动端点默认自由旋转，只在线段接近水平/垂直时轻轻吸一下（8px）。
      // `Shift` 在画墙和拖端点里都表示「不要吸附」——同一个修饰键必须是一个意思，
      // 否则用户按 Shift 想画斜的，却被锁成正交。
      const free = e.evt.shiftKey
      let sn = snap(p.x, p.y, {
        ignoreWallId: editing.wallId,
      })
      if (!free) {
        const tol = 8 / viewport.scale
        if (Math.abs(sn.y - anchor.y) <= tol) {
          sn = { ...sn, y: anchor.y }
        } else if (Math.abs(sn.x - anchor.x) <= tol) {
          sn = { ...sn, x: anchor.x }
        }
      }
      setEditing((cur) =>
        cur
          ? cur.end === 'a'
            ? { ...cur, ax: sn.x, ay: sn.y }
            : { ...cur, bx: sn.x, by: sn.y }
          : cur,
      )
      setGuide(sn.guide)
      return
    }
    if (bend) {
      const dx = bend.bx - bend.ax
      const dy = bend.by - bend.ay
      const chord = Math.hypot(dx, dy) || 1
      // 指针到弦的有向距离（沿左法线为正），矢高 → 圆心角：s = (L/2)·tan(θ/4)
      const sag = (-dy * (p.x - bend.ax) + dx * (p.y - bend.ay)) / chord
      const theta = (4 * Math.atan((2 * Math.abs(sag)) / chord) * 180) / Math.PI
      // 屏幕 y 向下为正，而正弧度向上凸出，所以向下拖取负号，手势才跟手。
      // 接近半圆时吸附到正 180°：s = (L/2)·tan(θ/4)，θ 由矢高反解，
      // 手抖一点就到不了 180，直接给个吸附更顺手。
      const raw = sag >= 0 ? -theta : theta
      const snapped = Math.abs(raw) >= 178 ? Math.sign(raw) * 180 : raw
      setBend({ ...bend, arcExtent: clamp(snapped, -180, 180) })
      return
    }
    if (!draft) {
      setHoverId(wallAt(p.x, p.y))
      return
    }
    const s = snap(p.x, p.y, {
      anchor: { x: draft.ax, y: draft.ay },
      // 画弧时弦要能斜着，预览就必须跟落点用同一套吸附，否则松手会突然跳一下。
      orthogonal: !e.evt.shiftKey && !arcMode && !e.evt.altKey && !e.evt.ctrlKey,
    })
    setDraft((d) => (d ? { ...d, bx: s.x, by: s.y } : d))
    setGuide(s.guide)
  }

  async function onMouseUp(e: Konva.KonvaEventObject<MouseEvent>) {
    const stage = stageRef.current
    if (panRef.current) {
      panRef.current = null
      return
    }
    if (!stage) return
    const raw = stage.getPointerPosition()
    const press = pressRef.current
    pressRef.current = null

    if (editing) {
      const cur = editing
      setEditing(null)
      setGuide({ kind: 'none' })
      if (Math.hypot(cur.bx - cur.ax, cur.by - cur.ay) < 1) return
      await submitEdit(cur)
      return
    }

    const d = draft
    setDraft(null)
    setGuide({ kind: 'none' })
    if (!raw || !press) return

    const p = toWorld(raw)
    const moved = Math.hypot(p.x - press.x, p.y - press.y) * viewport.scale

    // 几乎没动 = 点击：点中墙就选中，点空白就取消选中。
    if (moved < MIN_DRAG_PX) {
      setSelectedId(press.wallId)
      // 墙优先：有墙就选墙，没有再看落点有没有踩中房间。
      setSelectedRoomHash(press.wallId ? null : press.roomHash)
      return
    }
    // 开始画墙就离开房间面板，否则属性面板会一直压着画布右边。
    setSelectedRoomHash(null)
    if (!d) return

    // 终点要用松手那一刻的指针，不能用最后一次 mousemove 的位置：
    // 浏览器可能在松手前不再发 mousemove，那样墙会短一截、四个角对不上。
    // 必须带上 anchor，否则松手这一下会退化成「只吸附墙线、不做正交」，
    // 预览是直的、落库却是斜的。
    const arcWanted = arcMode || e.evt.altKey || e.evt.ctrlKey || arcKeyRef.current
    const end = snap(p.x, p.y, {
      anchor: { x: d.ax, y: d.ay },
      // 弦不强制正交：斜着的弦才画得出常见的弧（想正交按住 Shift）。
      orthogonal: !e.evt.shiftKey && !arcWanted,
    })
    const wall = { ax: d.ax, ay: d.ay, bx: end.x, by: end.y }
    if (Math.hypot(wall.bx - wall.ax, wall.by - wall.ay) < MIN_DRAG_PX / viewport.scale) return
    const wantsArc = arcWanted
    arcKeyRef.current = false
    if (wantsArc) {
      // 拉弧模式：墙先不落库，指针定弧度，再点一下才建。
      setBend({ ...wall, arcExtent: 0 })
      return
    }
    await submitWall(wall)
  }

  function onWheel(e: Konva.KonvaEventObject<WheelEvent>) {
    e.evt.preventDefault()
    const stage = stageRef.current
    const raw = stage?.getPointerPosition()
    if (!raw) return
    // 以指针为锚点缩放：缩放前后，指针下面的那个点不动。
    const before = toWorld(raw)
    const factor = e.evt.deltaY < 0 ? 1.1 : 1 / 1.1
    dimHint()
    const scale = clamp(viewport.scale * factor, ZOOM_MIN, ZOOM_MAX)
    setViewport({ scale, x: raw.x - before.x * scale, y: raw.y - before.y * scale })
  }

  async function submitWall(d: Draft) {
    const seq = ++writeSeq.current
    setSaving(true)
    try {
      const before = new Set(plan.walls.map((w) => w.id))
      const snapshot = snapshotOf(plan)
      const next = await createWall({
        ax: d.ax,
        ay: d.ay,
        bx: d.bx,
        by: d.by,
        ...(d.arcExtent ? { arcExtent: d.arcExtent } : {}),
      })
      if (seq === writeSeq.current) {
        remember(snapshot)
        setPlan(next)
        setError(null)
        const created = next.walls.find((w) => !before.has(w.id))
        if (created) {
          focusLengthRef.current = true
          setSelectedId(created.id)
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function submitEdit(cur: Edit) {
    const seq = ++writeSeq.current
    setSaving(true)
    try {
      const snapshot = snapshotOf(plan)
      const next = await updateWall(cur.wallId, { ax: cur.ax, ay: cur.ay, bx: cur.bx, by: cur.by })
      if (seq === writeSeq.current) {
        remember(snapshot)
        setPlan(next)
        setSelectedId(cur.wallId)
        setError(null)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  /** 改一个字段：先更新草稿字符串，再用纯函数换算，最后回填没在编辑的派生字段。 */
  function updateWallField(field: keyof WallForm, raw: string) {
    setWallForm((f) => (f ? { ...f, [field]: raw } : f))
    const n = Number.parseFloat(raw)
    if (!Number.isFinite(n) || !workSeg) return
    // 厚度与弧度不影响端点几何，只留在表单里等「应用」
    if (field === 'arc' || field === 'thickness') return

    let next = workSeg
    if (field === 'length') next = withLength(workSeg, n * 100)
    else if (field === 'angle') next = withAngle(workSeg, n)
    else if (field === 'ax') next = withEndpoint(workSeg, 'a', n, workSeg.ay)
    else if (field === 'ay') next = withEndpoint(workSeg, 'a', workSeg.ax, n)
    else if (field === 'bx') next = withEndpoint(workSeg, 'b', n, workSeg.by)
    else if (field === 'by') next = withEndpoint(workSeg, 'b', workSeg.bx, n)

    setWorkSeg(next)
    const movedPoint = field === 'ax' || field === 'ay' || field === 'bx' || field === 'by'
    setWallForm((f) =>
      f
        ? {
            ...f,
            ...(field === 'length' ? {} : { length: (wallLength(next) / 100).toFixed(2) }),
            ...(field === 'angle' ? {} : { angle: wallAngle(next).toFixed(1) }),
            ...(movedPoint
              ? {}
              : { ax: String(tidy(next.ax)), ay: String(tidy(next.ay)), bx: String(tidy(next.bx)), by: String(tidy(next.by)) }),
          }
        : f,
    )
  }

  /** 把面板上的数字写回库里。校验与拖动走同一条 PATCH，规则只有一份。 */
  async function applyWallForm() {
    if (!selected || !workSeg || !wallForm) return
    if (wallLength(workSeg) < 1) {
      setError('墙长不能小于 1 厘米')
      return
    }
    const thickness = clamp(
      Number.parseFloat(wallForm.thickness) || selected.thickness,
      1,
      100,
    )
    const arcExtent = Math.min(180, Math.max(-180, Number.parseFloat(wallForm.arc) || 0))
    const seq = ++writeSeq.current
    setSaving(true)
    try {
      const next = await updateWall(selected.id, {
        ax: tidy(workSeg.ax),
        ay: tidy(workSeg.ay),
        bx: tidy(workSeg.bx),
        by: tidy(workSeg.by),
        thickness,
        arcExtent,
      })
      if (seq === writeSeq.current) {
        setPlan(next)
        setError(null)
        const w = next.walls.find((x) => x.id === selected.id)
        if (w) {
          setWallForm(newWallForm(w))
          setWorkSeg({ ax: w.ax, ay: w.ay, bx: w.bx, by: w.by })
        }
      }
    } catch (e) {
      const err = e as { status?: number; code?: string; message?: string }
      if (err.status === 409) setError('这个位置已经有一面同样的墙了')
      else if (err.status === 404) setError('这面墙已经被删除了')
      else setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function removeWall(id: string) {
    // 开口与挂墙物品是级联删除的（issue #8 二选一里选了级联）：
    // 删之前把数量说清楚，别让用户以为是只删一面墙。
    const openingCount = plan.openings.filter((o) => o.wallId === id).length
    const itemCount = items.filter((it) => it.wallId === id).length
    if (openingCount + itemCount > 0) {
      const parts = [
        openingCount > 0 ? `${openingCount} 个门窗` : '',
        itemCount > 0 ? `${itemCount} 件挂墙物品` : '',
      ].filter(Boolean)
      if (!window.confirm(`这面墙上还有 ${parts.join('、')}，删墙会把它们一起删掉。继续？`)) return
    }
    const seq = ++writeSeq.current
    const snapshot = snapshotOf(plan)
    setSaving(true)
    try {
      await deleteWall(id)
    } catch (e) {
      // 404 说明库里本来就没有（双击、连按 Delete、别人先删了）：
      // 当作成功继续本地清理，否则会出现"库里没了、画布还在"。
      if (!(e instanceof ApiError && e.status === 404)) {
        setError(e instanceof Error ? e.message : '删除失败')
        setSaving(false)
        return
      }
    }
    // 房间是后端每次读时重算的，本地只删一面墙的话 rooms/openWalls 会停在旧值：
    // 删掉矩形的一条边，房间填充还在、提示也不出现。所以删完重新取一次整份户型。
    if (seq === writeSeq.current) {
      try {
        const next = await getPlan()
        if (seq === writeSeq.current) {
          remember(snapshot)
          setPlan(next)
          setSelectedId(null)
          setError(null)
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : '刷新户型失败')
      }
    }
    setSaving(false)
  }

  async function removeItem(id: string) {
    setSaving(true)
    try {
      await deleteItem(id)
      setItems((current) => current.filter((item) => item.id !== id))
      if (selectedItemId === id) setSelectedItemId(null)
      setError(null)
    } catch (e) {
      // 已经没了（别的标签页删的、双击 Delete）不该报错，本地跟着清掉即可。
      if (!(e instanceof ApiError && e.status === 404)) {
        setError(e instanceof Error ? e.message : '删除物品失败')
      } else {
        setItems((current) => current.filter((item) => item.id !== id))
        if (selectedItemId === id) setSelectedItemId(null)
      }
    } finally {
      setSaving(false)
    }
  }

  /** 在当前选中的墙上开一个门/窗；位置默认居中。 */
  async function addOpening(kind: 'door' | 'window') {
    if (!selected) return
    const length = wallLength(selected)
    const width = kind === 'door' ? 90 : 150
    const offset = Math.max(0, Math.round(length / 2 - width / 2))
    setSaving(true)
    try {
      setPlan(await createOpening({ wallId: selected.id, kind, offset, width }))
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '开洞失败')
    } finally {
      setSaving(false)
    }
  }

  async function removeOpening(id: string) {
    setSaving(true)
    try {
      await deleteOpening(id)
      setPlan(await getPlan())
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除开口失败')
    } finally {
      setSaving(false)
    }
  }

  async function updateItemFields(
    id: string,
    patch: {
      name?: string
      roomHash?: string
      x?: number
      y?: number
      rotation?: number
      wallId?: string
      wallOffset?: number
    },
  ) {
    const saved = await updateItem(id, patch)
    setItems((current) => current.map((item) => (item.id === saved.id ? saved : item)))
    setError(null)
  }

  async function onItemDragEnd(item: Item, e: Konva.KonvaEventObject<Event>) {
    const node = e.target
    // Stage 已经应用了视口变换，子节点的 x/y 就是世界坐标（厘米），不用再换算。
    const x = Math.round(node.x())
    const y = Math.round(node.y())
    // 挂墙物品不报坐标：只报「挂到哪面墙、离起点多远」，位置与朝向交给后端按墙算。
    // 松手在房间中央（离任何墙都超过 40px）就退回原位——窗帘不能浮在空中。
    // 判据用类型的 mount，而不是 item.wallId：旧数据里可能有一件「类型要挂墙、
    // 但 wall_id 是空」的窗帘，按 wallId 判断会把它当成自由物品放到地上。
    const mustBeOnWall = Boolean(item.wallId) || typeByKey.get(item.typeKey)?.mount === 'wall'
    if (mustBeOnWall) {
      const anchor = nearestWallAnchor(planRef.current.walls, x, y, 40 / viewport.scale)
      if (!anchor) {
        node.position({ x: item.x, y: item.y })
        setError('挂墙物品要贴着墙放：拖到某面墙的 40 像素以内')
        return
      }
      const width = typeByKey.get(item.typeKey)?.defaultWidth ?? 60
      const offset = clampWallOffset(anchor.offset, width, anchor.length)
      setSaving(true)
      try {
        const saved = await updateItem(item.id, { wallId: anchor.wallId, wallOffset: offset })
        setItems((current) => current.map((row) => (row.id === item.id ? saved : row)))
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : '保存物品位置失败')
        void load()
      } finally {
        setSaving(false)
      }
      return
    }
    setItems((current) => current.map((row) => (row.id === item.id ? { ...row, x, y } : row)))
    try {
      await updateItemFields(item.id, { x, y })
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存物品位置失败')
      void load()
    }
  }

  async function onDropType(e: ReactDragEvent<HTMLElement>) {
    e.preventDefault()
    const typeKey = e.dataTransfer.getData('application/x-smart-home-type')
    if (!typeKey) return
    const typ = typeByKey.get(typeKey)
    if (!typ) return
    const host = hostRef.current
    if (!host) return
    const rect = host.getBoundingClientRect()
    // 落点是屏幕坐标，必须先过视口变换，否则缩放/平移之后物品会落在别处。
    const world = toWorld({ x: e.clientX - rect.left, y: e.clientY - rect.top })
    const x = Math.max(0, Math.min(5000, Math.round(world.x)))
    const y = Math.max(0, Math.min(5000, Math.round(world.y)))
    // 挂墙物品：落点必须在墙边 40 像素内，否则拒绝——窗帘凭空立在地板中间没有意义。
    if (typ.mount === 'wall') {
      const anchor = nearestWallAnchor(planRef.current.walls, world.x, world.y, 40 / viewport.scale)
      if (!anchor) {
        setError(`「${typ.name}」要挂在墙上：把落点放到某面墙的 40 像素以内`)
        return
      }
      const offset = clampWallOffset(anchor.offset, typ.defaultWidth, anchor.length)
      try {
        const created = await createItem({
          typeKey,
          name: typ.name,
          x: anchor.x,
          y: anchor.y,
          rotation: anchor.rotation,
          wallId: anchor.wallId,
          wallOffset: offset,
        })
        setItems((current) => [...current, created])
        setSelectedItemId(created.id)
        setSelectedId(null)
        setSelectedRoomHash(null)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : '布放失败')
      }
      return
    }
    try {
      const created = await createItem({ typeKey, name: typ.name, x, y, rotation: 0 })
      setItems((current) => [...current, created])
      setSelectedItemId(created.id)
      setSelectedId(null)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '布放失败')
    }
  }

  async function onLogout() {
    try {
      await logout()
      onSignedOut()
    } catch {
      setError('退出失败，请稍后重试')
    }
  }

  // 网格按视口算：只画看得见的那一段，缩小时把步长翻倍，避免几千条线。
  const gridLines = useMemo(() => {
    const step = viewport.scale < 0.6 ? GRID_CM * 5 : GRID_CM
    const x0 = -viewport.x / viewport.scale
    const y0 = -viewport.y / viewport.scale
    const x1 = x0 + size.width / viewport.scale
    const y1 = y0 + size.height / viewport.scale
    const lines = []
    const startX = Math.floor(x0 / step) * step
    const startY = Math.floor(y0 / step) * step
    for (let x = startX; x <= x1; x += step) {
      lines.push(
        <Line key={`v${x}`} points={[x, y0, x, y1]} stroke="#e8e8f2" strokeWidth={1 / viewport.scale} />,
      )
    }
    for (let y = startY; y <= y1; y += step) {
      lines.push(
        <Line key={`h${y}`} points={[x0, y, x1, y]} stroke="#e8e8f2" strokeWidth={1 / viewport.scale} />,
      )
    }
    return lines
  }, [size.height, size.width, viewport])

  // 拖动中的那面墙按编辑后的坐标画，其余原样——这份派生数据同时喂给渲染与手柄。
  const renderWalls = editing
    ? plan.walls.map((w) =>
        w.id === editing.wallId ? { ...w, ax: editing.ax, ay: editing.ay, bx: editing.bx, by: editing.by } : w,
      )
    : plan.walls

  const selected = plan.walls.find((w) => w.id === selectedId) ?? null

  useEffect(() => {
    if (!selected) {
      setWallForm(null)
      setWorkSeg(null)
      return
    }
    setWallForm(newWallForm(selected))
    setWorkSeg({ ax: selected.ax, ay: selected.ay, bx: selected.bx, by: selected.by })
    if (focusLengthRef.current) {
      focusLengthRef.current = false
      requestAnimationFrame(() => {
        lengthRef.current?.focus()
        lengthRef.current?.select()
      })
    }
    // 只在"选中了哪面墙"变化时重置；拖动同一面墙不该把用户正在输入的数字冲掉。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId])

  const handles = editing
    ? [
        { x: editing.ax, y: editing.ay },
        { x: editing.bx, y: editing.by },
      ]
    : selected
      ? [
          { x: selected.ax, y: selected.ay },
          { x: selected.bx, y: selected.by },
        ]
      : []
  const labelSize = 12 / viewport.scale
  const wallOpenings = selected ? plan.openings.filter((o) => o.wallId === selected.id) : []
  const wallsById = useMemo(() => new Map(plan.walls.map((w) => [w.id, w])), [plan.walls])

  // 有墙但推不出房间时的提示文案：空户型和"画了墙但没围上"要说不同的话，
  // 否则用户画了三面墙回到刷新，会以为房间功能坏了。
  const hint =
    plan.walls.length === 0
      ? '空地。按住并拖动画下第一面墙；滚轮缩放，空格或中键拖动画布。'
      : plan.rooms.length === 0
        ? `还没围成闭合区域：${plan.openWalls} 面墙没有形成闭环`
        : plan.openWalls > 0
          ? `已识别 ${plan.rooms.length} 个房间；另有 ${plan.openWalls} 面墙没围上`
          : null

  /** 面板里的一个数字输入。 */
  const wallField = (label: string, value: string, onChange: (v: string) => void) => (
    <label className="flex flex-col gap-0.5">
      <span style={{ color: 'var(--color-muted)' }}>{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        inputMode="decimal"
        className="w-full rounded px-1 py-0.5 text-xs"
        style={{ border: '1px solid var(--color-border)', background: 'var(--color-bg)' }}
      />
    </label>
  )

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex min-w-0 items-center justify-between gap-4 px-4 py-3"
        style={{ background: 'var(--color-surface)', borderBottom: '1px solid var(--color-border)' }}
      >
        <div className="flex min-w-0 items-baseline gap-3">
          <h1 className="shrink-0 whitespace-nowrap text-base font-semibold">平面图</h1>
          <span className="whitespace-nowrap text-xs" style={{ color: 'var(--color-muted)' }}>
            {loading ? '读取中…' : `${plan.walls.length} 面墙 · ${items.length} 件物品`}
          </span>
          {selected && (
            <button
              onClick={() => void removeWall(selected.id)}
              disabled={saving}
              className="whitespace-nowrap px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-danger)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--color-danger)',
                background: 'transparent',
              }}
            >
              删除这面墙（{formatLength(selected)}）
            </button>
          )}
        </div>
        <div className="flex items-center gap-3 text-sm">
          <button
            onClick={() => setGridSnap((v) => !v)}
            aria-pressed={gridSnap}
            title={`吸附到 ${GRID_CM} 厘米网格`}
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: `1px solid ${gridSnap ? 'var(--color-primary)' : 'var(--color-border)'}`,
              borderRadius: 'var(--radius-sm)',
              background: gridSnap ? 'var(--color-primary-weak)' : 'var(--color-bg)',
              color: gridSnap ? 'var(--color-primary)' : 'inherit',
            }}
          >
            网格 {GRID_CM}cm
          </button>
          <button
            onClick={() => setShowDims((v) => !v)}
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: `1px solid ${showDims ? 'var(--color-primary)' : 'var(--color-border)'}`,
              borderRadius: 'var(--radius-sm)',
              background: showDims ? 'var(--color-primary-weak)' : 'var(--color-bg)',
              color: showDims ? 'var(--color-primary)' : 'inherit',
            }}
          >
            尺寸标注
          </button>
          <button
            onClick={() => setArcMode((v) => !v)}
            disabled={saving}
            title="画弧模式：拖出弦后松手，移动鼠标定弧度，单击落定"
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: `1px solid ${arcMode ? 'var(--color-primary)' : 'var(--color-border)'}`,
              borderRadius: 'var(--radius-sm)',
              background: arcMode ? 'var(--color-primary-weak)' : 'var(--color-bg)',
              color: arcMode ? 'var(--color-primary)' : 'inherit',
            }}
          >
            画弧
          </button>
          <button
            onClick={() => {
              const stage = stageRef.current
              if (!stage) return
              const url = stage.toDataURL({ pixelRatio: 2 })
              const a = document.createElement('a')
              a.href = url
              a.download = `户型-${new Date().toISOString().slice(0, 10)}.png`
              a.click()
            }}
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          >
            导出 PNG
          </button>
          <button
            onClick={() => void undo()}
            disabled={history.past.length === 0 || saving}
            title="撤销（Ctrl/Cmd+Z）"
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
              opacity: history.past.length === 0 || saving ? 0.45 : 1,
            }}
          >
            撤销
          </button>
          <button
            onClick={() => void redo()}
            disabled={history.future.length === 0 || saving}
            title="重做（Ctrl/Cmd+Shift+Z）"
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
              opacity: history.future.length === 0 || saving ? 0.45 : 1,
            }}
          >
            重做
          </button>
          <button
            onClick={fitToContent}
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          >
            回到全览
          </button>
          <span className="text-xs" style={{ color: 'var(--color-muted)' }}>
            {Math.round(viewport.scale * 100)}%
          </span>
          <button
            onClick={() => setShowUnderlay((v) => !v)}
            aria-pressed={showUnderlay}
            title="上传一张户型图垫在画布下面照着描"
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: `1px solid ${showUnderlay ? 'var(--color-primary)' : 'var(--color-border)'}`,
              borderRadius: 'var(--radius-sm)',
              background: showUnderlay ? 'var(--color-primary-weak)' : 'var(--color-bg)',
              color: showUnderlay ? 'var(--color-primary)' : 'inherit',
            }}
          >
            底图
          </button>
          {user.role === 'admin' ? (
            <button
              onClick={() => setShowTypes(true)}
              className="whitespace-nowrap px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: 'var(--color-bg)',
              }}
            >
              类型管理
            </button>
          ) : null}
          <span className="whitespace-nowrap">
            {user.displayName}
            <span className="ml-2 text-xs" style={{ color: 'var(--color-muted)' }}>
              {user.role === 'admin' ? '管理员' : '家庭成员'}
            </span>
          </span>
          <button
            onClick={onLogout}
            className="whitespace-nowrap px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          >
            退出
          </button>
        </div>
      </header>

      {error && (
        <p className="px-4 py-2 text-sm" style={{ color: 'var(--color-danger)' }} role="alert">
          {error}
        </p>
      )}

      {plan.staleRooms.length > 0 && (
        <div
          className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-xs"
          style={{ background: 'rgba(245, 158, 11, 0.14)', color: '#7c4a03' }}
          role="status"
        >
          <span>
            户型改过之后，这些房间名已经对不上原来的房间：
            {plan.staleRooms.map((stale) => `「${stale.name}」`).join('、')}
          </span>
          <button
            className="rounded px-2 py-0.5 font-medium"
            style={{ border: '1px solid rgba(124, 74, 3, 0.35)' }}
            onClick={() => void forgetAllStaleRooms()}
          >
            全部清理
          </button>
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <ItemSidebar
          types={types}
          items={items}
          rooms={plan.rooms}
          filters={filters}
          selectedItemId={selectedItemId}
          onFiltersChange={(patch) => setFilters((current) => ({ ...current, ...patch }))}
          onSelectItem={(id) => {
            setSelectedItemId(id)
            setSelectedId(null)
            setSelectedRoomHash(null)
          }}
        />
        <main
          ref={hostRef}
          className="relative min-w-0 flex-1 overflow-hidden"
          onDragOver={(e) => {
            e.preventDefault()
            e.dataTransfer.dropEffect = 'copy'
          }}
          onDrop={(e) => void onDropType(e)}
        >
        {selectedRoom && (
          <RoomPanel
            room={selectedRoom}
            onClose={() => setSelectedRoomHash(null)}
            onSave={(name, kind) => void saveRoomName(selectedRoom.hash, name, kind)}
            onClear={() => void forgetRoomName(selectedRoom.hash)}
          />
        )}
        {selected && wallForm && (
          <div
            className="absolute right-3 top-3 z-10 w-56 rounded p-3 text-xs"
            style={{
              background: 'var(--color-surface)',
              border: '1px solid var(--color-border)',
              boxShadow: '0 6px 20px rgba(0, 0, 0, 0.08)',
            }}
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="mb-2 flex items-center justify-between">
              <span className="font-semibold">墙属性</span>
              <button onClick={() => setSelectedId(null)} style={{ color: 'var(--color-muted)' }}>
                关闭
              </button>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <label className="flex flex-col gap-0.5">
                <span style={{ color: 'var(--color-muted)' }}>长度（米）</span>
                <input
                  ref={lengthRef}
                  value={wallForm.length}
                  onChange={(e) => updateWallField('length', e.target.value)}
                  inputMode="decimal"
                  className="w-full rounded px-1 py-0.5 text-xs"
                  style={{ border: '1px solid var(--color-border)', background: 'var(--color-bg)' }}
                />
              </label>
              {wallField('角度（°）', wallForm.angle, (v) => updateWallField('angle', v))}
              {wallField('厚度（厘米）', wallForm.thickness, (v) => updateWallField('thickness', v))}
              {wallField('弧度（度）', wallForm.arc, (v) => updateWallField('arc', v))}
              {wallField('起点 X', wallForm.ax, (v) => updateWallField('ax', v))}
              {wallField('起点 Y', wallForm.ay, (v) => updateWallField('ay', v))}
              {wallField('终点 X', wallForm.bx, (v) => updateWallField('bx', v))}
              {wallField('终点 Y', wallForm.by, (v) => updateWallField('by', v))}
            </div>
            <p className="mt-2 leading-4" style={{ color: 'var(--color-muted)' }}>
              改长度保持方向，改角度保持长度；坐标单位厘米。按 Esc 关闭画布操作。
            </p>
            <button
              onClick={() => void applyWallForm()}
              disabled={saving}
              className="mt-2 w-full rounded py-1 text-xs"
              style={{
                background: 'var(--color-primary)',
                color: '#fff',
                opacity: saving ? 0.6 : 1,
              }}
            >
              {saving ? '保存中…' : '应用'}
            </button>

            <div className="mt-3 pt-2" style={{ borderTop: '1px solid var(--color-border)' }}>
              <div className="mb-1 flex items-center justify-between">
                <span className="font-semibold">门窗</span>
                <span className="flex gap-1">
                  <button
                    onClick={() => void addOpening('door')}
                    disabled={saving}
                    className="rounded px-1.5 py-0.5"
                    style={{ border: '1px solid var(--color-border)' }}
                  >
                    + 门
                  </button>
                  <button
                    onClick={() => void addOpening('window')}
                    disabled={saving}
                    className="rounded px-1.5 py-0.5"
                    style={{ border: '1px solid var(--color-border)' }}
                  >
                    + 窗
                  </button>
                </span>
              </div>
              {wallOpenings.length === 0 ? (
                <p style={{ color: 'var(--color-muted)' }}>这面墙还没有开口。门宽 90、窗宽 150 厘米。</p>
              ) : (
                <ul className="flex flex-col gap-0.5">
                  {wallOpenings.map((opening) => (
                    <li key={opening.id} className="flex items-center justify-between gap-2">
                      <span>
                        {opening.kind === 'door' ? '门' : '窗'} · 偏移 {Math.round(opening.offset)} · 宽{' '}
                        {Math.round(opening.width)}
                      </span>
                      <button
                        onClick={() => void removeOpening(opening.id)}
                        disabled={saving}
                        style={{ color: 'var(--color-danger)' }}
                      >
                        删
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        )}
        {/* 提示放左上角：下方与左方现在是尺寸标注的地盘，压在数字上会糊成一团 */}
        <div
          className="pointer-events-none absolute left-3 top-2 rounded px-2 py-1 text-xs transition-opacity duration-300"
          style={{
            color: 'var(--color-muted)',
            background: 'color-mix(in srgb, var(--color-bg) 82%, transparent)',
            opacity: hintDimmed ? 0.35 : 1,
          }}
        >
          {arcModifier} 拖出弧 · 拖拽画墙 · 点墙选中后 Delete 删除 · 空格/中键平移 · 滚轮缩放 · Shift 自由角度 · 拖端点改墙
        </div>
        {showUnderlay ? (
          <UnderlayPanel
            underlay={plan.underlay}
            isAdmin={user.role === 'admin'}
            busy={underlayBusy}
            onUpload={(file) => void saveUnderlay(file)}
            onClear={() => void dropUnderlay()}
            onClose={() => setShowUnderlay(false)}
            calibrating={calibrating}
            calibCount={calibPoints.length}
            calibMeasureCm={calibMeasureCm}
            onStartCalibration={startCalibration}
            onCancelCalibration={cancelCalibration}
            onCalibrate={(lengthCm) => void applyCalibration(lengthCm)}
            onPatch={(patch) => void patchUnderlay(patch)}
            onSample={(sample) => void loadSample(sample)}
          />
        ) : null}
        <Stage
          ref={stageRef}
          width={size.width}
          height={size.height}
          x={viewport.x}
          y={viewport.y}
          scaleX={viewport.scale}
          scaleY={viewport.scale}
          onMouseDown={onMouseDown}
          onMouseMove={onMouseMove}
          onMouseUp={(e) => void onMouseUp(e)}
          onWheel={onWheel}
          onContextMenu={(e) => {
            e.evt.preventDefault()
            setDraft(null)
            setGuide({ kind: 'none' })
          }}
          style={{ cursor: panRef.current ? 'grabbing' : spaceDown ? 'grab' : 'crosshair' }}
        >
          <Layer>{gridLines}</Layer>

          {/* 房间画在墙下面：半透明填充，墙压在上面边界才清楚 */}
          <Layer listening={false}>
            {plan.rooms.map((room) => (
              <RoomShape
                key={room.hash}
                room={room}
                selected={room.hash === selectedRoomHash}
              />
            ))}
          </Layer>

          {/* 房间填充在底图下面、底图在墙下面：底图不压色，墙压在最上面才看得清边界 */}
          {underlayTransform && underlayImage && plan.underlay?.visible ? (
            <Layer listening={false}>
              <KonvaImage
                image={underlayImage}
                x={imageCenter(underlayTransform).x}
                y={imageCenter(underlayTransform).y}
                offsetX={imageRect(underlayTransform).w / 2}
                offsetY={imageRect(underlayTransform).h / 2}
                width={imageRect(underlayTransform).w}
                height={imageRect(underlayTransform).h}
                rotation={plan.underlay.rotationDeg}
                opacity={plan.underlay.opacity}
              />
            </Layer>
          ) : null}

          {showDims ? (
            <Layer>
              {dimRuns.map((run) => (
                <RunDimension
                  key={`dim-${run.wallIds.join('-')}`}
                  run={run}
                  center={center}
                  scale={viewport.scale}
                  thickness={Math.max(...run.wallIds.map((id) => wallsById.get(id)?.thickness ?? 10))}
                  onPick={
                    run.wallIds.length === 1
                      ? () => {
                          setSelectedId(run.wallIds[0])
                          setSelectedItemId(null)
                          setSelectedRoomHash(null)
                        }
                      : undefined
                  }
                />
              ))}
              {/* 第二排：轴线尺寸。它读的是开间/进深，所以内隔墙也参与。 */}
              {dimRuns.map((run, i) => {
                const min = run.horizontal
                  ? Math.min(run.a.x, run.b.x)
                  : Math.min(run.a.y, run.b.y)
                const max = run.horizontal
                  ? Math.max(run.a.x, run.b.x)
                  : Math.max(run.a.y, run.b.y)
                const segments = axisSegments(
                  plan.walls.map((w) => ({ id: w.id, ax: w.ax, ay: w.ay, bx: w.bx, by: w.by })),
                  run.horizontal,
                  min,
                  max,
                )
                if (segments.length < 2) return null
                return (
                  <AxisChain
                    key={`axis-${i}`}
                    run={run}
                    center={center}
                    scale={viewport.scale}
                    thickness={Math.max(
                      ...run.wallIds.map((id) => wallsById.get(id)?.thickness ?? 10),
                    )}
                    segments={segments}
                  />
                )
              })}
              {/* 弧墙单独标弧长：尺寸线是与弧同心的圆弧，不走直线尺寸链。 */}
              {plan.walls
                .filter((w) => Math.abs(w.arcExtent) >= 0.5)
                .map((w) => (
                  <ArcDim key={`arcdim-${w.id}`} wall={w} scale={viewport.scale} />
                ))}
            </Layer>
          ) : null}

          <Layer>
            {renderWalls.map((w) => (
              <WallLine
                key={`line-${w.id}`}
                wall={w}
                quad={wallQuads.get(w.id)}
                selected={w.id === selectedId}
                hovered={w.id === hoverId}
                scale={viewport.scale}
              />
            ))}
            {renderWalls
              .filter((w) => !showDims || w.id === selectedId || w.id === hoverId)
              .map((w) => (
              <WallLabel
                key={`label-${w.id}`}
                wall={w}
                selected={w.id === selectedId}
                hovered={w.id === hoverId}
                scale={viewport.scale}
                labelSize={labelSize}
                centerX={center.x}
                centerY={center.y}
                onPick={(id) => {
                  setSelectedId(id)
                  setSelectedItemId(null)
                  setSelectedRoomHash(null)
                }}
                onHover={setHoverId}
              />
            ))}
            {handles.map((h, i) => (
              <Circle
                key={`handle-${i}`}
                x={h.x}
                y={h.y}
                radius={6 / viewport.scale}
                fill="#ffffff"
                stroke="#4f6bed"
                strokeWidth={2 / viewport.scale}
              />
            ))}
            {bend && (
              <>
                {/* 拉弧预览：跟手改弧度，点一下才落库 */}
                <Line
                  points={arcPoints(
                    { x: bend.ax, y: bend.ay },
                    { x: bend.bx, y: bend.by },
                    bend.arcExtent ?? 0,
                  ).flatMap((p) => [p.x, p.y])}
                  stroke="#4f6bed"
                  strokeWidth={Math.max(10, 4)}
                  lineCap="butt"
                  lineJoin="round"
                  opacity={0.5}
                  listening={false}
                />
                <Text
                  x={(bend.ax + bend.bx) / 2 + 8 / viewport.scale}
                  y={(bend.ay + bend.by) / 2 - 16 / viewport.scale}
                  text={
                    Math.abs(bend.arcExtent ?? 0) < 1
                      ? '移动鼠标定弧度 · 单击落定（Esc 取消）'
                      : `弧度 ${Math.round(bend.arcExtent ?? 0)}° · 单击落定（Esc 取消）`
                  }
                  fontSize={11 / viewport.scale}
                  fill="#3a3a4d"
                  listening={false}
                />
              </>
            )}

            {draft && (
              <>
                {/* 吸附参考线：贴到哪面墙上就把它整条描出来 */}
                {guide.kind === 'segment' && (
                  <Line
                    points={[
                      guide.wall.ax,
                      guide.wall.ay,
                      guide.wall.bx,
                      guide.wall.by,
                    ]}
                    stroke="#4f6bed"
                    strokeWidth={3 / viewport.scale}
                    dash={[6 / viewport.scale, 4 / viewport.scale]}
                    opacity={0.5}
                    listening={false}
                  />
                )}
                <Line
                  points={[draft.ax, draft.ay, draft.bx, draft.by]}
                  stroke="#4f6bed"
                  strokeWidth={arcMode ? 6 : 10}
                  opacity={arcMode ? 0.55 : 0.4}
                  // 画弧模式下这条只是"弦"，画成虚线，免得看起来像在画直墙。
                  dash={arcMode ? [10 / viewport.scale, 6 / viewport.scale] : undefined}
                  lineCap="round"
                  listening={false}
                />
                <Text
                  x={(draft.ax + draft.bx) / 2 + 8 / viewport.scale}
                  y={(draft.ay + draft.by) / 2 - 8 / viewport.scale}
                  text={
                    arcMode
                      ? `${formatCm(Math.hypot(draft.bx - draft.ax, draft.by - draft.ay))} · 松手后移动鼠标定弧度`
                      : formatCm(Math.hypot(draft.bx - draft.ax, draft.by - draft.ay))
                  }
                  fontSize={labelSize}
                  fill="#4f6bed"
                  listening={false}
                />
              </>
            )}
          </Layer>

          {/* 门窗画在墙上方：白色块盖住墙线，看起来才像"洞"。
              位置由「墙 + 沿墙偏移」算出来，和物品的挂墙逻辑共用 clampWallOffset。 */}
          <Layer listening={!bend}>
            {plan.openings.map((opening) => {
              const wall = wallsById.get(opening.wallId)
              if (!wall) return null
              return (
                <OpeningShape
                  key={opening.id}
                  opening={opening}
                  wall={wall}
                  scale={viewport.scale}
                  swing={swingSide(opening, wall, (x, y) => roomAt(x, y) !== null)}
                  onPick={() => {
                    setSelectedId(wall.id)
                    setSelectedItemId(null)
                    setSelectedRoomHash(null)
                  }}
                />
              )
            })}
          </Layer>

          {/* 物品层在墙之上：家具要压在墙线上，但顺序反过来就点不到被盖住的墙了。
              选中用几何命中（wallAt/handleAt），不依赖 Konva 的 hit graph。 */}
          <Layer listening={!bend}>
            {items.map((item) => (
              <ItemShape
                key={item.id}
                item={item}
                type={typeByKey.get(item.typeKey)}
                selected={item.id === selectedItemId}
                scale={viewport.scale}
                onSelect={() => {
                  setSelectedItemId(item.id)
                  setSelectedId(null)
                  setSelectedRoomHash(null)
                }}
                onDragEnd={(e) => void onItemDragEnd(item, e)}
              />
            ))}
          </Layer>

          {/* 房间名/面积/开间进深压在最上面：它们是读图的入口，不能被家具标签盖住 */}
          <Layer listening={false}>
            {plan.rooms.map((room) => (
              <RoomLabel
                key={`room-label-${room.hash}`}
                room={room}
                scale={viewport.scale}
                selected={room.hash === selectedRoomHash}
              />
            ))}
          </Layer>

          <Layer listening={false}>
            {/* 端点小圆点只在「正在画墙」或「悬停/选中某面墙」时出现：
                墙现在是实心的，常显的圆点会变成一个个关节疙瘩，糊住转角。 */}
            {liveCorners
              .filter((c) => {
                if (draft) return true
                const w = wallsById.get(selectedId ?? hoverId ?? '')
                return w ? w.aId === c.id || w.bId === c.id : false
              })
              .map((c) => (
                <Circle key={c.id} x={c.x} y={c.y} radius={3 / viewport.scale} fill="#8b8b9e" />
              ))}
            {guide.kind === 'point' && (
              <Circle
                x={guide.x}
                y={guide.y}
                radius={6 / viewport.scale}
                stroke="#4f6bed"
                strokeWidth={2 / viewport.scale}
              />
            )}
          </Layer>
        </Stage>

        {!loading && (arcMode || arcKeyHeld || bend) && (
          <div
            className="pointer-events-none absolute left-1/2 top-24 -translate-x-1/2 rounded px-3 py-1.5 text-xs"
            style={{ color: '#1f4fa8', background: 'rgba(255,255,255,0.92)', border: '1px solid #cfd8ea', borderRadius: 8 }}
          >
            {bend
              ? '移动鼠标定弧度 → 单击落定（Esc 取消）'
              : arcMode
                ? '画弧模式：拖出一段弦 → 松手 → 移动鼠标定弧度 → 单击落定（再点「画弧」退出）'
                : `${arcModifier} 已按住：拖出一段弦 → 松手 → 移动鼠标定弧度 → 单击落定`}
          </div>
        )}

        {!loading && hint && (
          <div
            className="pointer-events-none absolute left-4 top-11 px-3 py-1 text-xs"
            style={{ color: '#b06a00', background: 'rgba(255,255,255,0.82)', borderRadius: 6 }}
          >
            {hint}
          </div>
        )}

        {saving && (
          <div
            className="absolute right-4 bottom-4 px-3 py-1 text-xs"
            style={{ background: 'var(--color-surface)', border: '1px solid var(--color-border)', borderRadius: 6 }}
          >
            保存中…
          </div>
        )}
        </main>
        <ItemInspector
          item={selectedItem}
          type={selectedItemType}
          rooms={plan.rooms}
          onUpdate={updateItemFields}
          onDelete={removeItem}
        />
      </div>

      {showTypes ? <TypeManager onClose={() => setShowTypes(false)} /> : null}
    </div>
  )
}

type WallShapeProps = {
  wall: Wall
  selected: boolean
  hovered: boolean
  scale: number
}

function WallLine({ wall, quad, selected, hovered, scale }: WallShapeProps & { quad?: Pt[] }) {
  const fill = selected ? '#4f6bed' : hovered ? '#55557a' : '#3a3a4d'
  // 没有四角数据（例如刚画完还没重算）时退回中心线描边，至少不会消失。
  const points = quad
    ? quad.flatMap((p) => [p.x, p.y])
    : [wall.ax, wall.ay, wall.bx, wall.by]
  return (
    <Line
      points={points}
      closed={Boolean(quad)}
      // 同色描边 1px：相邻两面墙共用一条斜接线，两块多边形各自抗锯齿会在公共边上
      // 混进一点背景色，放大后能看到一条发丝缝；描一圈同色就把它盖住了。
      stroke={quad ? (selected ? '#ffffff' : fill) : fill}
      strokeWidth={quad ? (selected ? 1.5 / scale : 2 / scale) : Math.max(wall.thickness, 4)}
      // 同色描边是为了盖住两块多边形公共边的发丝缝；但默认的 miter join 会在
      // 方角外侧顶出一个小尖，放大看就是「角上多出来一个小点」。圆角连接不会外凸。
      lineJoin="round"
      lineCap="round"
      fill={quad ? fill : undefined}
      listening={false}
    />
  )
}

type WallLabelProps = WallShapeProps & {
  labelSize: number
  centerX: number
  centerY: number
  onPick: (id: string) => void
  onHover: (id: string | null) => void
}

function WallLabel({
  wall,
  selected,
  hovered,
  scale,
  labelSize,
  centerX,
  centerY,
  onPick,
  onHover,
}: WallLabelProps) {
  const midX = (wall.ax + wall.bx) / 2
  const midY = (wall.ay + wall.by) / 2
  const dx = wall.bx - wall.ax
  const dy = wall.by - wall.ay
  const len = Math.hypot(dx, dy) || 1
  // 标签放到墙体朝外的一侧，避免压在房间填充上、也避免被画布边缘裁掉。
  //
  // 间距不是拍出来的：文字框紧贴墙面 6px，再沿法线让开自己在法线方向上的半个尺寸——
  // 横墙的法线是竖直的，让开半高；竖墙的法线是水平的，而数字是横排的，得让开半个字宽。
  // 所以两种朝向看起来一样「贴」，只是让开的量不同。
  const vertical = Math.abs(dy) > Math.abs(dx)
  const text = formatMm(len)
  // 外法线的单位向量，从户型中心指向墙外侧
  let nx = -dy / len
  let ny = dx / len
  if ((midX - centerX) * nx + (midY - centerY) * ny < 0) {
    nx = -nx
    ny = -ny
  }
  const tint = selected ? '#4f6bed' : hovered ? '#3f3f57' : '#5a5a70'
  // 文字宽度得等 Konva 排完版才量得准：先用 0 占位（只差半帧），
  // 量到真实宽度后再把标签推到「墙的外表面 + 6px」的位置。
  // 从外表面而不是墙中线量：墙厚随缩放一起变粗，按中线算放大后数字会被墙盖住。
  const labelRef = useRef<Konva.Text | null>(null)
  useLayoutEffect(() => {
    const node = labelRef.current
    if (!node) return
    const w = node.getTextWidth()
    // 法线方向上的半尺寸：竖墙的法线是水平的，避开半个字宽；横墙避开半个字高
    const halfExtent = vertical ? w / 2 : node.fontSize() / 2
    const dist = (wall.thickness ?? 10) / 2 + halfExtent + 6 / scale
    node.position({ x: midX + nx * dist, y: midY + ny * dist })
    node.offsetX(w / 2)
  })
  return (
    <Text
      ref={labelRef}
      x={midX}
      y={midY}
      text={text}
      fontSize={labelSize}
      fill={tint}
      // 描一圈背景色，标签压在墙上时也能读清。白边按屏幕像素给，且别太肥：
      // 3px 的白边在 11px 的字上会把字缝糊住（放大后尤其明显）。
      stroke="#fafafd"
      strokeWidth={Math.min(2.4, Math.max(1.2, labelSize * scale * 0.18)) / scale}
      fillAfterStrokeEnabled
      offsetY={labelSize / 2}
      // 标签离墙 17–52 px，超出几何命中区，只能自己接事件；
      // cancelBubble 阻止冒泡到 Stage，否则 Stage 的 onMouseDown 会立刻把选中清掉。
      onMouseDown={(e) => {
        e.cancelBubble = true
        onPick(wall.id)
      }}
      onMouseEnter={() => onHover(wall.id)}
      onMouseLeave={() => onHover(null)}
    />
  )
}

/**
 * 图上的尺寸数字：毫米、不注单位（GB/T 50001—2017 §11.2.1）。
 *
 * 面板、按钮这些「操作界面」里仍用米/厘米并带单位——那是给人输入的数据，
 * 不是图纸上的标注，两者不同口径是有意的。
 */
function formatMm(cm: number): string {
  return String(Math.round(cm * 10))
}

/** 长度显示：小于 1 m 用厘米，否则用米（两位小数），都带单位。 */
function formatCm(cm: number): string {
  if (cm < 100) return `${Math.round(cm)} cm`
  return `${(cm / 100).toFixed(2)} m`
}

function formatLength(w: Wall): string {
  return formatCm(Math.hypot(w.bx - w.ax, w.by - w.ay))
}

function clamp(v: number, min: number, max: number): number {
  return Math.min(Math.max(v, min), max)
}

/**
 * 「过 anchor 的水平/竖直线」与这面墙的交点；平行或交点落在段外返回 null。
 *
 * 正交约束与墙线吸附同时开时用它：既吸到墙上，又保持 0°/90°。
 */

/** 点到线段的最短距离（世界单位）。 */

/** 点在线段上的垂足；垂足落在线段外就返回 null。 */

/**
 * 一个房间：半透明多边形 + 面积标签。
 *
 * 面积与顶点都来自后端，这里只画——前端若自己也推导一遍，两套实现迟早分叉，
 * 就会出现「画布上是一间、列表里是两间」。
 */
function RoomShape({ room, selected }: { room: Room; selected: boolean }) {
  const points = room.ring.flatMap((p) => [p.x, p.y])
  return (
    <Line
      points={points}
      closed
      fill={selected ? 'rgba(79,107,237,0.22)' : 'rgba(79,107,237,0.10)'}
      stroke={selected ? '#4f6bed' : 'rgba(79,107,237,0.35)'}
      strokeWidth={selected ? 2 : 1}
    />
  )
}

/**
 * 房间标签：名字 + 面积 + 开间×进深。
 *
 * 单独画在最上层、垫一块半透明白板——否则"客厅 23.1 ㎡"正好被沙发、茶几
 * 的标签压住，压在房间里最该读的位置。
 */
/** 从给的点出发，保证落在多边形内部：不在就朝各顶点方向逐步试，全失败退回包围盒中心。 */
function insideAnchor(ring: Pt[], from: Pt): Pt {
  if (pointInPolygon(from, ring)) return from
  for (const step of [0.5, 0.35, 0.2]) {
    for (const v of ring) {
      const p = { x: from.x + (v.x - from.x) * step, y: from.y + (v.y - from.y) * step }
      if (pointInPolygon(p, ring)) return p
    }
  }
  return from
}

function RoomLabel({
  room,
  scale,
  selected,
}: {
  room: Room
  scale: number
  selected: boolean
}) {
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const p of room.ring) {
    if (p.x < minX) minX = p.x
    if (p.x > maxX) maxX = p.x
    if (p.y < minY) minY = p.y
    if (p.y > maxY) maxY = p.y
  }
  const wPx = (maxX - minX) * scale
  const hPx = (maxY - minY) * scale
  // 房间在屏幕上小于这个尺寸时，标签会把整个户型盖住——那还不如不画，
  // 缩放进来自然就有了。
  if (Math.min(wPx, hPx) < 40) return null
  // 字号以**世界单位**为主（34cm，跟打印图纸一个量级），再夹到 7–15px 的屏幕区间：
  // 太小读不了，太大又会溢出到隔壁房间。
  const size = Math.min(Math.max(34, 7 / scale), 15 / scale)
  // 房间轮廓是**墙中线**：只按字号留 pad，文字会压在墙上（20cm 外墙放大后很明显）。
  // 再让开半墙厚 + 一点余量，标签就落在房间内侧了。
  const pad = size * 1.1 + 12
  // 三角形/L 形的包围盒左上角常常落在房间外面（在凹口或房间之外），
  // 直接摆上去文字会浮在空白处。此时退回形心，形心也不在里面就沿对角线找第一个落点。
  const corner = { x: minX + pad, y: minY + pad }
  const anchor = pointInPolygon(corner, room.ring) ? corner : insideAnchor(room.ring, polygonCentroid(room.ring))
  // 房间标签只写名字 + 面积。宽×深一律不标：矩形房间的外接矩形恰好等于房间本身，
  // 异形的却会写出一个图上看不出来的矩形；房间尺寸本来就该由墙上的尺寸链回答。
  const lines: string[] = [room.name ? room.name : `${room.areaM2.toFixed(1)} ㎡`]
  if (room.name && wPx >= 96) lines.push(`${room.areaM2.toFixed(1)} ㎡`)
  return (
    <Text
      x={anchor.x}
      y={anchor.y}
      text={lines.join('\n')}
      fontSize={size}
      lineHeight={1.25}
      fill={selected ? '#3b5bdb' : 'rgba(58,74,143,0.9)'}
      stroke="#ffffff"
      // 描边必须是**屏幕像素**而不是世界单位：字号随缩放缩小，而世界的 3cm 描边会跟着放大，
      // 到 300% 以上白边能有几十像素，字就被自己的描边糊成一坨了。
      strokeWidth={Math.min(2.4, Math.max(1.2, size * scale * 0.18)) / scale}
      fillAfterStrokeEnabled
      listening={false}
    />
  )
}

function AxisChain({
  run,
  center,
  scale,
  thickness,
  segments,
}: {
  run: { a: Pt; b: Pt; horizontal: boolean }
  center: Pt
  scale: number
  thickness: number
  segments: AxisSegment[]
}) {
  const midX = (run.a.x + run.b.x) / 2
  const midY = (run.a.y + run.b.y) / 2
  const outward = run.horizontal
    ? midY < center.y
      ? -1
      : 1
    : midX < center.x
      ? -1
      : 1
  // 比总尺寸链往里一档，两条线才不会叠在一起。
  const offset = thickness / 2 + (run.horizontal ? 40 : 56) / scale
  const x1 = run.a.x + (run.horizontal ? 0 : outward * offset)
  const y1 = run.a.y + (run.horizontal ? outward * offset : 0)
  const x2 = run.b.x + (run.horizontal ? 0 : outward * offset)
  const y2 = run.b.y + (run.horizontal ? outward * offset : 0)
  const ticks = [segments[0].a, ...segments.map((s) => s.b)]
  return (
    <>
      <Line
        points={[x1, y1, x2, y2]}
        stroke="rgba(90, 100, 120, 0.5)"
        strokeWidth={1 / scale}
        listening={false}
      />
      {ticks.map((t, i) => {
        const tx = run.horizontal ? t : x1
        const ty = run.horizontal ? y1 : t
        return (
          <Line
            key={`axis-tick-${i}`}
            points={[tx - 3 / scale, ty + 3 / scale, tx + 3 / scale, ty - 3 / scale]}
            stroke="rgba(90, 100, 120, 0.75)"
            strokeWidth={2 / scale}
            listening={false}
          />
        )
      })}
      {segments.map((seg, i) => (
        <AxisLabel
          key={`axis-label-${i}`}
          x={run.horizontal ? (seg.a + seg.b) / 2 : x1}
          y={run.horizontal ? y1 : (seg.a + seg.b) / 2}
          horizontal={run.horizontal}
          scale={scale}
          text={formatMm(seg.length)}
        />
      ))}
    </>
  )
}

/** 轴线链上的一个数字。排版规则与总尺寸相同：横排在线之上，竖排逆时针 90° 在线之左。 */
function AxisLabel({
  x,
  y,
  horizontal,
  scale,
  text,
}: {
  x: number
  y: number
  horizontal: boolean
  scale: number
  text: string
}) {
  const ref = useRef<Konva.Text | null>(null)
  useLayoutEffect(() => {
    const node = ref.current
    if (!node) return
    const w = node.getTextWidth()
    const h = node.fontSize()
    if (horizontal) {
      node.offsetX(w / 2)
      node.offsetY(h)
      node.position({ x, y: y - 4 / scale })
    } else {
      node.offsetX(w / 2)
      node.offsetY(h / 2)
      node.position({ x: x - 5 / scale - h / 2, y })
    }
  })
  return (
    <Text
      ref={ref}
      x={x}
      y={y}
      rotation={horizontal ? 0 : -90}
      text={text}
      fontSize={11 / scale}
      fill="#5a6478"
      stroke="#fafafd"
      strokeWidth={Math.min(2.4, Math.max(1.2, 11 * 0.18)) / scale}
      fillAfterStrokeEnabled
      listening={false}
    />
  )
}

type ArcDimProps = {
  wall: Wall
  scale: number
}

/**
 * 弧长标注（GB/T 50001—2017 §11.2.5）：尺寸线用与所注圆弧同心的圆弧表示，
 * 尺寸界线沿半径方向（垂直于弧），数字上方加弧长符号，这里用「⌒」前缀代替。
 */
function ArcDim({ wall, scale }: ArcDimProps) {
  const ref = useRef<Konva.Text>(null)
  const a = { x: wall.ax, y: wall.ay }
  const b = { x: wall.bx, y: wall.by }
  const circle = arcCircle(a, b, wall.arcExtent)
  const text = formatMm(arcLength(a, b, wall.arcExtent))
  // 字宽实测：字体里没有 U+2312，弧长符号改成自己画一道小弧。
  const [labelW, setLabelW] = useState(0)
  useLayoutEffect(() => {
    const node = ref.current
    if (!node) return
    const w = node.getTextWidth()
    if (Math.abs(w - labelW) > 0.01) setLabelW(w)
  })
  if (!circle) return null
  const pad = 22 / scale
  const half = Math.max(wall.thickness, 4) / 2
  const rLine = circle.r + half + pad
  const steps = Math.max(6, Math.ceil(Math.abs(circle.delta) / ((6 * Math.PI) / 180)))
  const pts: number[] = []
  for (let i = 0; i <= steps; i += 1) {
    const p = arcPointAt({ cx: circle.cx, cy: circle.cy, r: rLine }, circle.start + (circle.delta * i) / steps)
    pts.push(p.x, p.y)
  }
  const ends = [circle.start, circle.start + circle.delta].map((ang) => {
    const inner = arcPointAt({ cx: circle.cx, cy: circle.cy, r: circle.r + half }, ang)
    const outer = arcPointAt({ cx: circle.cx, cy: circle.cy, r: rLine + 4 / scale }, ang)
    return [inner, outer] as const
  })
  const midAng = circle.start + circle.delta / 2
  const label = arcPointAt({ cx: circle.cx, cy: circle.cy, r: rLine + 12 / scale }, midAng)
  // 字头朝上：把角度归到 (-90°, 90°]，否则弧在下半圈时数字会倒着写。
  let labelDeg = (midAng * 180) / Math.PI + 90
  if (labelDeg > 90) labelDeg -= 180
  if (labelDeg <= -90) labelDeg += 180
  return (
    <>
      <Line points={pts} stroke="#7a7a8c" strokeWidth={1 / scale} dash={[6 / scale, 4 / scale]} listening={false} />
      {ends.map(([p1, p2], i) => (
        <Line
          key={`w-${i}`}
          points={[p1.x, p1.y, p2.x, p2.y]}
          stroke="#b0b0bd"
          strokeWidth={0.6 / scale}
          listening={false}
        />
      ))}
      <Group x={label.x} y={label.y} rotation={labelDeg} listening={false}>
        <Text ref={ref} x={-labelW / 2} y={0} text={text} fontSize={11 / scale} fill="#4a4a5a" />
        {/* GB/T 50001 的弧长符号：数字上方一道小弧 */}
        <Line
          x={-labelW / 2}
          y={-7 / scale}
          points={[0, (3 / scale), labelW / 2, 0, labelW, 3 / scale]}
          tension={0.4}
          stroke="#4a4a5a"
          strokeWidth={0.8 / scale}
        />
      </Group>
    </>
  )
}

function RunDimension({
  run,
  center,
  scale,
  thickness,
  onPick,
}: {
  run: { a: { x: number; y: number }; b: { x: number; y: number }; total: number; horizontal: boolean }
  center: { x: number; y: number }
  scale: number
  /** 这条链上最厚的一面墙：尺寸线要从墙的外表面往外量，否则放大后会被墙压住 */
  thickness: number
  /** 只有一面墙的链：数字就是这面墙的长度，把它做成点选墙的入口。 */
  onPick?: () => void
}) {
  const midX = (run.a.x + run.b.x) / 2
  const midY = (run.a.y + run.b.y) / 2
  const outward = run.horizontal
    ? midY < center.y
      ? -1
      : 1
    : midX < center.x
      ? -1
      : 1
  // 分段标签贴着墙（17 / 52 px），一级标注必须再往外一档，否则两个数字叠在一起。
  // 单墙链没有内圈的分段数字，就贴着墙标——否则一面 4 米的墙会白留出两道空隙。
  // 三排标注由内到外：墙上分段（默认隐藏）→ 轴线（第二排）→ 总尺寸（最外）
  const offset = thickness / 2 + (run.horizontal ? 64 : 82) / scale

  const x1 = run.a.x + (run.horizontal ? 0 : outward * offset)
  const y1 = run.a.y + (run.horizontal ? outward * offset : 0)
  const x2 = run.b.x + (run.horizontal ? 0 : outward * offset)
  const y2 = run.b.y + (run.horizontal ? outward * offset : 0)

  const text = formatMm(run.total)
  const fontPx = 11 / scale
  const lineMidX = (x1 + x2) / 2
  const lineMidY = (y1 + y2) / 2

  // 尺寸数字的位置和朝向按《房屋建筑制图统一标准》GB/T 50001—2017 §11.2 来：
  //  - 水平尺寸线：数字在尺寸线的**上方中部**，字头朝上、横排；
  //  - 竖直尺寸线：数字在尺寸线的**左侧中部**，字头朝左、由下往上读，所以要逆时针转 90°。
  // 宽度得等 Konva 排完版才量得准，先在 layout effect 里量再定位。
  const textRef = useRef<Konva.Text | null>(null)
  useLayoutEffect(() => {
    const node = textRef.current
    if (!node) return
    const w = node.getTextWidth()
    const h = node.fontSize()
    if (run.horizontal) {
      node.offsetX(w / 2)
      node.offsetY(h)
      node.position({ x: lineMidX, y: lineMidY - 4 / scale })
    } else {
      node.offsetX(w / 2)
      node.offsetY(h / 2)
      node.position({ x: lineMidX - 5 / scale - h / 2, y: lineMidY })
    }
  })

  // 尺寸界线：从墙外表面留 4px 起，拉到尺寸线外 4px——图纸上不能只有一根孤零零的尺寸线。
  const face = thickness / 2 + 4 / scale
  const tail = 4 / scale
  const witness = run.horizontal
    ? [
        [run.a.x, run.a.y + outward * face, run.a.x, y1 + outward * tail],
        [run.b.x, run.b.y + outward * face, run.b.x, y2 + outward * tail],
      ]
    : [
        [run.a.x + outward * face, run.a.y, x1 + outward * tail, run.a.y],
        [run.b.x + outward * face, run.b.y, x2 + outward * tail, run.b.y],
      ]

  return (
    <>
      {witness.map((points, i) => (
        <Line
          key={`witness-${i}`}
          points={points}
          stroke="rgba(90, 100, 120, 0.45)"
          strokeWidth={1 / scale}
          listening={false}
        />
      ))}
      <Line
        points={[x1, y1, x2, y2]}
        stroke="rgba(90, 100, 120, 0.55)"
        strokeWidth={1 / scale}
        dash={[6 / scale, 4 / scale]}
      />
      {/* 起止符号：45° 中粗斜短线（GB/T 50001—2017 §11.3），比垂直小钩更像建筑图纸 */}
      {[
        [x1, y1],
        [x2, y2],
      ].map(([x, y], i) => (
        <Line
          key={`tick-${i}`}
          points={[x - 3 / scale, y + 3 / scale, x + 3 / scale, y - 3 / scale]}
          stroke="rgba(90, 100, 120, 0.8)"
          strokeWidth={2 / scale}
          listening={false}
        />
      ))}
      <Text
        ref={textRef}
        x={lineMidX}
        y={lineMidY}
        rotation={run.horizontal ? 0 : -90}
        text={text}
        fontSize={fontPx}
        fill="#5a6478"
        stroke="#fafafd"
        strokeWidth={Math.min(2.4, Math.max(1.2, fontPx * scale * 0.18)) / scale}
        fillAfterStrokeEnabled
        listening={Boolean(onPick)}
        onMouseDown={
          onPick
            ? (e) => {
                // 数字在墙外 17–52 px，超出几何命中区，得自己接事件
                e.cancelBubble = true
                onPick()
              }
            : undefined
        }
      />
    </>
  )
}

/**
 * 平面图上的家具/设备。
 *
 * `item.x` / `item.y` 是物品**中心**（厘米）。Group 的原点放在中心，再用 offset
 * 把绘制原点挪回左上角——这样 `rotation` 绕自身中心转，而不是绕左上角把沙发甩出房间。
 * 标签放在中心、水平居中，旋转后也不会和图形分家。
 */
/**
 * 门/窗：画成盖在墙线上的白色块。开着窗就是「洞」，关着窗是玻璃——
 * 这里只表达几何，不表达开关状态（那是 capability）。
 */
/** 门往哪边开：优先开向房间里侧（探测点落在某个房间内），两侧都不是房间就默认 +y。 */
function swingSide(
  opening: Opening,
  wall: Wall,
  inside: (x: number, y: number) => boolean,
): 1 | -1 {
  const length = Math.hypot(wall.bx - wall.ax, wall.by - wall.ay) || 1
  const ux = (wall.bx - wall.ax) / length
  const uy = (wall.by - wall.ay) / length
  const midX = wall.ax + ux * (opening.offset + opening.width / 2)
  const midY = wall.ay + uy * (opening.offset + opening.width / 2)
  const probe = (wall.thickness || 10) / 2 + 25
  // 门的局部 +y 在世界坐标里是墙法线 n = (-uy, ux)
  if (inside(midX - uy * probe, midY + ux * probe)) return 1
  if (inside(midX + uy * probe, midY - ux * probe)) return -1
  return 1
}

function OpeningShape({
  opening,
  wall,
  scale,
  swing,
  onPick,
}: {
  opening: Opening
  wall: Wall
  scale: number
  swing: 1 | -1
  onPick: () => void
}) {
  const length = Math.hypot(wall.bx - wall.ax, wall.by - wall.ay)
  const offset = clampWallOffset(opening.offset, opening.width, length)
  const ux = length === 0 ? 1 : (wall.bx - wall.ax) / length
  const uy = length === 0 ? 0 : (wall.by - wall.ay) / length
  const rotation = (Math.atan2(wall.by - wall.ay, wall.bx - wall.ax) * 180) / Math.PI
  const thickness = wall.thickness || 10
  return (
    <Group
      x={wall.ax + ux * offset}
      y={wall.ay + uy * offset}
      rotation={rotation}
      onMouseDown={(e) => {
        e.cancelBubble = true
        onPick()
      }}
      onClick={(e) => {
        e.cancelBubble = true
        onPick()
      }}
    >
      <Rect
        x={0}
        y={-thickness / 2}
        width={opening.width}
        height={thickness + 2}
        fill="#ffffff"
        stroke="#94a3b8"
        strokeWidth={1 / scale}
      />
      {opening.kind === 'window' ? (
        <Line
          points={[0, 0, opening.width, 0]}
          stroke="#4f6bed"
          strokeWidth={1.5 / scale}
          listening={false}
        />
      ) : (
        <>
          {/* 门扇：从合页（门洞起点）沿墙法线立起来，90° 全开。
              门弧以合页为圆心、门宽为半径，从门洞另一侧扫到门扇端头——和制图规范一致。 */}
          <Line
            points={[0, 0, 0, swing * opening.width]}
            stroke="#475569"
            strokeWidth={2 / scale}
            lineCap="round"
            listening={false}
          />
          <Path
            data={`M ${opening.width} 0 A ${opening.width} ${opening.width} 0 0 ${
              swing > 0 ? 1 : 0
            } 0 ${swing * opening.width}`}
            stroke="#94a3b8"
            strokeWidth={1 / scale}
            listening={false}
          />
        </>
      )}
    </Group>
  )
}

function ItemShape({
  item,
  type,
  selected,
  scale,
  onSelect,
  onDragEnd,
}: {
  item: Item
  type?: ItemType
  selected: boolean
  scale: number
  onSelect: () => void
  onDragEnd: (e: Konva.KonvaEventObject<Event>) => void
}) {
  const width = type?.defaultWidth ?? 60
  const depth = type?.defaultDepth ?? 60
  // 标签是 Group 的兄弟节点（文字不能跟着物品转，否则 180° 的窗帘会倒过来），
  // 所以拖动时它不会自己走：用同一个 Konva 节点的位置手推一下。
  const labelRef = useRef<Konva.Text | null>(null)
  // 家具名只在悬停/选中时出现：常显的话，一屋子标签会把房间名和尺寸压得没法读。
  const [hovered, setHovered] = useState(false)
  function syncLabel(node: Konva.Node) {
    const p = node.position()
    labelRef.current?.position({ x: p.x - width / 2, y: p.y - 7 / scale })
  }
  return (
    <>
      <Group
        x={item.x}
        y={item.y}
        offsetX={width / 2}
        offsetY={depth / 2}
        rotation={item.rotation}
        draggable
        onMouseDown={(e) => {
          // 不让 Stage 把它当成"开始画墙"
          e.cancelBubble = true
        }}
        onClick={(e) => {
          e.cancelBubble = true
          onSelect()
        }}
        onTap={(e) => {
          e.cancelBubble = true
          onSelect()
        }}
        onDragStart={onSelect}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onDragMove={(e) => syncLabel(e.target)}
        onDragEnd={(e) => {
          onDragEnd(e)
          // 被拒绝的挂墙物品会在这里把节点挪回原位，标签也要跟着回。
          syncLabel(e.target)
        }}
      >
        <Rect
          width={width}
          height={depth}
          fill={item.kind === 'device' ? '#dbe6ff' : '#e8e1d4'}
          stroke={selected ? '#4f6bed' : '#8a8a9e'}
          strokeWidth={(selected ? 2 : 1) / scale}
          cornerRadius={2 / scale}
        />
      </Group>
      {selected || hovered ? (
        <Text
          ref={labelRef}
          x={item.x - width / 2}
          y={item.y - 7 / scale}
          width={width}
          align="center"
          text={item.name}
          fontSize={11 / scale}
          fill="#3a3a4d"
          wrap="none"
          listening={false}
        />
      ) : null}
    </>
  )
}

/** 房间用途的中文名。顺序与后端 `room_name.kind` 的检查约束一致。 */
const ROOM_KINDS: { value: RoomKind; label: string }[] = [
  { value: 'living', label: '客厅' },
  { value: 'bedroom', label: '卧室' },
  { value: 'kitchen', label: '厨房' },
  { value: 'bathroom', label: '卫生间' },
  { value: 'study', label: '书房' },
  { value: 'hallway', label: '走廊' },
]

/**
 * 房间属性面板：只编辑名字与用途。
 *
 * 几何不可编辑是刻意的——房间由墙推导，改形状请去改墙；
 * 面板里显示哈希前缀，是为了让「名字失配」这件事看得见（哈希变了名字就挂不上了）。
 */
function RoomPanel({
  room,
  onClose,
  onSave,
  onClear,
}: {
  room: Room
  onClose: () => void
  onSave: (name: string, kind: RoomKind) => void
  onClear: () => void
}) {
  const [name, setName] = useState(room.name ?? '')
  const [kind, setKind] = useState<RoomKind>((room.kind as RoomKind | undefined) ?? 'living')

  // 选中另一间房时表单要跟着换，否则会把 A 的名字存到 B 上。
  useEffect(() => {
    setName(room.name ?? '')
    setKind((room.kind as RoomKind | undefined) ?? 'living')
  }, [room.hash, room.name, room.kind])

  const trimmed = name.trim()
  const fieldStyle = { border: '1px solid var(--color-border)', background: 'var(--color-bg)' }

  return (
    <div
      className="absolute right-3 top-3 z-10 w-56 rounded p-3 text-xs"
      style={{
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        boxShadow: '0 6px 20px rgba(0, 0, 0, 0.08)',
      }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      <div className="mb-2 flex items-center justify-between">
        <span className="font-semibold">房间属性</span>
        <button onClick={onClose} style={{ color: 'var(--color-muted)' }}>
          关闭
        </button>
      </div>
      <p className="mb-2" style={{ color: 'var(--color-muted)' }}>
        {room.areaM2.toFixed(1)} ㎡ · 哈希 {room.hash.slice(0, 8)}
      </p>
      <label className="mb-2 flex flex-col gap-0.5">
        <span style={{ color: 'var(--color-muted)' }}>名字</span>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="如：主卧"
          maxLength={24}
          className="w-full rounded px-1 py-0.5 text-xs"
          style={fieldStyle}
        />
      </label>
      <label className="mb-2 flex flex-col gap-0.5">
        <span style={{ color: 'var(--color-muted)' }}>用途</span>
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as RoomKind)}
          className="w-full rounded px-1 py-0.5 text-xs"
          style={fieldStyle}
        >
          {ROOM_KINDS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
      <div className="flex gap-2">
        <button
          onClick={() => onSave(trimmed, kind)}
          disabled={!trimmed}
          className="rounded px-2 py-1"
          style={{
            background: 'var(--color-accent, #4f6bed)',
            color: '#fff',
            opacity: trimmed ? 1 : 0.45,
          }}
        >
          保存
        </button>
        <button
          onClick={onClear}
          className="rounded px-2 py-1"
          style={{ border: '1px solid var(--color-border)' }}
        >
          清除名字
        </button>
      </div>
    </div>
  )
}
