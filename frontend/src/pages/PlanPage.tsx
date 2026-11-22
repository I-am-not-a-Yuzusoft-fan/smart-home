import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Circle, Layer, Line, Stage, Text } from 'react-konva'
import type Konva from 'konva'
import {
  ApiError,
  createWall,
  deleteWall,
  getPlan,
  logout,
  updateWall,
  type Corner,
  type Plan,
  type Room,
  type SessionUser,
  type Wall,
} from '../api/client.ts'
import TypeManager from '../components/TypeManager.tsx'
import { tidy, wallAngle, wallLength, withAngle, withEndpoint, withLength } from '../features/plan/edit.ts'
import { distanceToSegment, type Seg } from '../features/plan/geometry.ts'
import { DEFAULT_RULES, snap as snapTo, type Guide as SnapGuide, type SnapRule } from '../features/plan/snap.ts'
import { collinearRuns } from '../features/plan/dimensions.ts'
import { replacePlan, type WallSnapshot } from '../api/client.ts'

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
const MIN_DRAG_PX = 8
const ZOOM_MIN = 0.25
const ZOOM_MAX = 4

type Viewport = { scale: number; x: number; y: number }

/** 墙属性面板的草稿值；一律用字符串，避免"输入 4." 时被数字回写打断。 */
type WallForm = {
  length: string
  angle: string
  thickness: string
  ax: string
  ay: string
  bx: string
  by: string
}

function newWallForm(w: Wall): WallForm {
  return {
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
  const [size, setSize] = useState({ width: 800, height: 600 })
  // 画布真实尺寸要等 ResizeObserver 报一次；在那之前 fit 会按 800×600 算，
  // 结果内容偏在左上角（第一次打开时尤其明显）。
  const [sizeReady, setSizeReady] = useState(false)
  const [plan, setPlan] = useState<Plan>({ corners: [], walls: [], rooms: [], openWalls: 0, version: 0 })
  // 回调里要读「最新」的户型：闭包捕获的 plan 可能落后一次渲染。
  const planRef = useRef<Plan>(plan)
  planRef.current = plan
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [guide, setGuide] = useState<Guide>({ kind: 'none' })
  const [selectedId, setSelectedId] = useState<string | null>(null)
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
  const pressRef = useRef<{ x: number; y: number; wallId: string | null } | null>(null)
  const panRef = useRef<{ sx: number; sy: number; vx: number; vy: number } | null>(null)
  const fittedRef = useRef(false)

  const load = useCallback(async () => {
    try {
      setPlan(await getPlan())
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取户型失败')
    } finally {
      setLoading(false)
    }
  }, [])

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
    // 四级标注画在户型外侧，全览必须留出这一圈，否则尺寸线被裁在视口外
    const pad = 96
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
        setDraft(null)
        setEditing(null)
        setGuide({ kind: 'none' })
        return
      }
      if (e.key !== 'Delete' && e.key !== 'Backspace') return
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
  }, [selectedId, history.past.length, history.future.length])

  // 只认"被墙引用"的端点：删墙后留下的孤立端点既不该画出来，也不该被吸附——
  // 否则用户会吸到一个看不见的点上，还以为是 bug。
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
      const d = distanceToSegment(x, y, w)
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
    const stage = stageRef.current
    if (!stage) return
    const raw = stage.getPointerPosition()
    if (!raw) return
    const p = toWorld(raw)

    // 中键或按住空格：拖画布，不画墙。
    if (e.evt.button === 1 || spaceDown) {
      panRef.current = { sx: raw.x, sy: raw.y, vx: viewport.x, vy: viewport.y }
      return
    }
    if (e.evt.button === 2) {
      // 右键取消进行中的画线或拖动。
      setDraft(null)
      setEditing(null)
      setGuide({ kind: 'none' })
      return
    }

    // 先看有没有按在选中墙的端点上——那是拖动改墙，不是画新墙。
    const grabbed = handleAt(p.x, p.y)
    if (grabbed) {
      pressRef.current = { x: p.x, y: p.y, wallId: null }
      setEditing({ wallId: grabbed.wallId, end: grabbed.end, ax: grabbed.ax, ay: grabbed.ay, bx: grabbed.bx, by: grabbed.by })
      return
    }

    const wallId = wallAt(p.x, p.y)
    pressRef.current = { x: p.x, y: p.y, wallId }

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
      // 拖动端点和画新墙不一样：画墙时正交是默认，拖端点时用户多半是想改角度。
      // 所以默认自由旋转，只在线段接近水平/垂直时轻轻吸一下；按住 Shift 才强制正交。
      const strict = e.evt.shiftKey
      let sn = snap(p.x, p.y, {
        anchor: strict ? anchor : undefined,
        orthogonal: strict,
        ignoreWallId: editing.wallId,
      })
      if (!strict) {
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
    if (!draft) {
      setHoverId(wallAt(p.x, p.y))
      return
    }
    const s = snap(p.x, p.y, {
      anchor: { x: draft.ax, y: draft.ay },
      orthogonal: !e.evt.shiftKey,
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
      return
    }
    if (!d) return

    // 终点要用松手那一刻的指针，不能用最后一次 mousemove 的位置：
    // 浏览器可能在松手前不再发 mousemove，那样墙会短一截、四个角对不上。
    // 必须带上 anchor，否则松手这一下会退化成「只吸附墙线、不做正交」，
    // 预览是直的、落库却是斜的。
    const end = snap(p.x, p.y, {
      anchor: { x: d.ax, y: d.ay },
      orthogonal: !e.evt.shiftKey,
    })
    const wall = { ax: d.ax, ay: d.ay, bx: end.x, by: end.y }
    if (Math.hypot(wall.bx - wall.ax, wall.by - wall.ay) < MIN_DRAG_PX / viewport.scale) return
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
    const scale = clamp(viewport.scale * factor, ZOOM_MIN, ZOOM_MAX)
    setViewport({ scale, x: raw.x - before.x * scale, y: raw.y - before.y * scale })
  }

  async function submitWall(d: Draft) {
    const seq = ++writeSeq.current
    setSaving(true)
    try {
      const before = new Set(plan.walls.map((w) => w.id))
      const snapshot = snapshotOf(plan)
      const next = await createWall({ ax: d.ax, ay: d.ay, bx: d.bx, by: d.by })
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
    const seq = ++writeSeq.current
    setSaving(true)
    try {
      const next = await updateWall(selected.id, {
        ax: tidy(workSeg.ax),
        ay: tidy(workSeg.ay),
        bx: tidy(workSeg.bx),
        by: tidy(workSeg.by),
        thickness,
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
          <span className="truncate text-xs" style={{ color: 'var(--color-muted)' }}>
            {loading
              ? '读取中…'
              : `按住拖动画墙（${plan.walls.length} 面）· 点墙选中后按 Delete 删除 · 空格/中键平移 · 滚轮缩放 · Shift 自由角度`}
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
          <span>
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

      <main ref={hostRef} className="relative flex-1 overflow-hidden">
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
          </div>
        )}
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
              <RoomShape key={room.hash} room={room} labelSize={13 / viewport.scale} />
            ))}
          </Layer>

          {showDims ? (
            <Layer listening={false}>
              {runs.filter((run) => run.wallIds.length > 1).map((run) => (
                <RunDimension
                  key={`dim-${run.wallIds.join('-')}`}
                  run={run}
                  center={center}
                  scale={viewport.scale}
                />
              ))}
            </Layer>
          ) : null}

          <Layer>
            {renderWalls.map((w) => (
              <WallLine
                key={`line-${w.id}`}
                wall={w}
                selected={w.id === selectedId}
                hovered={w.id === hoverId}
                scale={viewport.scale}
              />
            ))}
            {renderWalls.map((w) => (
              <WallLabel
                key={`label-${w.id}`}
                wall={w}
                selected={w.id === selectedId}
                hovered={w.id === hoverId}
                scale={viewport.scale}
                labelSize={labelSize}
                centerX={center.x}
                centerY={center.y}
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
                  strokeWidth={10}
                  opacity={0.4}
                  lineCap="round"
                  listening={false}
                />
                <Text
                  x={(draft.ax + draft.bx) / 2 + 8 / viewport.scale}
                  y={(draft.ay + draft.by) / 2 - 8 / viewport.scale}
                  text={formatCm(Math.hypot(draft.bx - draft.ax, draft.by - draft.ay))}
                  fontSize={labelSize}
                  fill="#4f6bed"
                  listening={false}
                />
              </>
            )}
          </Layer>

          <Layer listening={false}>
            {liveCorners.map((c) => (
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

        {!loading && hint && (
          <div
            className="pointer-events-none absolute left-4 top-4 px-3 py-1 text-xs"
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

function WallLine({ wall, selected, hovered, scale }: WallShapeProps) {
  const stroke = selected ? '#4f6bed' : hovered ? '#6b6b8f' : '#3a3a4d'
  return (
    <Line
      points={[wall.ax, wall.ay, wall.bx, wall.by]}
      stroke={stroke}
      strokeWidth={Math.max(wall.thickness, 4) + (selected || hovered ? 2 / scale : 0)}
      lineCap="round"
    />
  )
}

type WallLabelProps = WallShapeProps & { labelSize: number; centerX: number; centerY: number }

function WallLabel({ wall, selected, hovered, scale, labelSize, centerX, centerY }: WallLabelProps) {
  const midX = (wall.ax + wall.bx) / 2
  const midY = (wall.ay + wall.by) / 2
  const dx = wall.bx - wall.ax
  const dy = wall.by - wall.ay
  const len = Math.hypot(dx, dy) || 1
  // 标签放到墙体朝外的一侧，避免压在房间填充上、也避免被画布边缘裁掉。
  // 竖向墙的标签是横排的，要让开半个字宽，否则字会压在墙上。
  const vertical = Math.abs(dy) > Math.abs(dx)
  const off = (vertical ? 52 : 17) / scale
  let nx = (-dy / len) * off
  let ny = (dx / len) * off
  if ((midX - centerX) * nx + (midY - centerY) * ny < 0) {
    nx = -nx
    ny = -ny
  }
  const tint = selected ? '#4f6bed' : hovered ? '#3f3f57' : '#5a5a70'
  return (
    <Text
      x={midX + nx}
      y={midY + ny}
      text={formatCm(len)}
      fontSize={labelSize}
      fill={tint}
      // 描一圈背景色，标签压在墙上时也能读清
      stroke="#fafafd"
      strokeWidth={3 / scale}
      fillAfterStrokeEnabled
      offsetX={-4 / scale}
      offsetY={labelSize / 2}
    />
  )
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
function RoomShape({ room, labelSize }: { room: Room; labelSize: number }) {
  const points = room.ring.flatMap((p) => [p.x, p.y])
  const cx = room.ring.reduce((sum, p) => sum + p.x, 0) / room.ring.length
  const cy = room.ring.reduce((sum, p) => sum + p.y, 0) / room.ring.length
  const label = room.name ? `${room.name}　${room.areaM2.toFixed(1)} ㎡` : `${room.areaM2.toFixed(1)} ㎡`

  return (
    <>
      <Line points={points} closed fill="rgba(79,107,237,0.10)" stroke="rgba(79,107,237,0.35)" strokeWidth={1} />
      <Text x={cx} y={cy} text={label} fontSize={labelSize} fill="#3a4a8f" offsetX={labelSize * 1.4} />
    </>
  )
}

/**
 * 一级标注：一条标注链的总长。
 *
 * 画在户型外侧，所以要先判断链在中心的哪一侧——把尺寸线放到外侧，
 * 否则和室内的家具、设备图标挤在一起。
 */
function RunDimension({
  run,
  center,
  scale,
}: {
  run: { a: { x: number; y: number }; b: { x: number; y: number }; total: number; horizontal: boolean }
  center: { x: number; y: number }
  scale: number
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
  // 分段标签贴着墙（17 / 52 px），一级标注必须再往外一档，否则两个数字叠在一起
  const offset = run.horizontal ? 52 / scale : 76 / scale

  const x1 = run.a.x + (run.horizontal ? 0 : outward * offset)
  const y1 = run.a.y + (run.horizontal ? outward * offset : 0)
  const x2 = run.b.x + (run.horizontal ? 0 : outward * offset)
  const y2 = run.b.y + (run.horizontal ? outward * offset : 0)

  const text = run.total >= 100 ? `${(run.total / 100).toFixed(2)} m` : `${Math.round(run.total)} cm`

  return (
    <>
      <Line
        points={[x1, y1, x2, y2]}
        stroke="rgba(90, 100, 120, 0.55)"
        strokeWidth={1 / scale}
        dash={[6 / scale, 4 / scale]}
      />
      {/* 两端的短竖线，读起来才像尺寸线而不是一根多余的墙 */}
      {run.horizontal ? (
        <>
          <Line points={[x1, y1 - 4 / scale, x1, y1 + 4 / scale]} stroke="rgba(90, 100, 120, 0.55)" strokeWidth={1 / scale} />
          <Line points={[x2, y2 - 4 / scale, x2, y2 + 4 / scale]} stroke="rgba(90, 100, 120, 0.55)" strokeWidth={1 / scale} />
        </>
      ) : (
        <>
          <Line points={[x1 - 4 / scale, y1, x1 + 4 / scale, y1]} stroke="rgba(90, 100, 120, 0.55)" strokeWidth={1 / scale} />
          <Line points={[x2 - 4 / scale, y2, x2 + 4 / scale, y2]} stroke="rgba(90, 100, 120, 0.55)" strokeWidth={1 / scale} />
        </>
      )}
      <Text
        x={(x1 + x2) / 2}
        y={(y1 + y2) / 2}
        text={text}
        fontSize={11 / scale}
        offsetX={(text.length * 3.2) / scale}
        offsetY={-14 / scale}
        fill="#5a6478"
        stroke="#fafafd"
        strokeWidth={3 / scale}
        fillAfterStrokeEnabled
      />
    </>
  )
}
