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

type Draft = {
  /** 起点（世界坐标，已吸附） */
  ax: number
  ay: number
  /** 当前点（世界坐标，已吸附） */
  bx: number
  by: number
}

type Edit = { wallId: string; end: 'a' | 'b'; ax: number; ay: number; bx: number; by: number }

type Guide =
  | { kind: 'none' }
  | { kind: 'point'; x: number; y: number }
  | { kind: 'segment'; wall: Wall }

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
  const [size, setSize] = useState({ width: 800, height: 600 })
  // 画布真实尺寸要等 ResizeObserver 报一次；在那之前 fit 会按 800×600 算，
  // 结果内容偏在左上角（第一次打开时尤其明显）。
  const [sizeReady, setSizeReady] = useState(false)
  const [plan, setPlan] = useState<Plan>({ corners: [], walls: [], rooms: [], openWalls: 0 })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [guide, setGuide] = useState<Guide>({ kind: 'none' })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showTypes, setShowTypes] = useState(false)
  // 正在拖的端点：整段坐标都存下来，渲染预览时直接用，不必回头找 wall。
  const [editing, setEditing] = useState<Edit | null>(null)
  const [hoverId, setHoverId] = useState<string | null>(null)
  const [viewport, setViewport] = useState<Viewport>({ scale: 1, x: 0, y: 0 })
  const [spaceDown, setSpaceDown] = useState(false)
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
    const pad = 60
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
      if (isTyping(e.target)) return
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
    // removeWall 每次渲染都是新函数，但内部只用 setState 与 props，闭包安全
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId])

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
  function snap(
    x: number,
    y: number,
    opts: { anchor?: { x: number; y: number }; orthogonal?: boolean; ignoreWallId?: string } = {},
  ): SnapResult {
    const radius = SNAP_PX / viewport.scale
    let best: { x: number; y: number } | null = null
    let bestDist = radius
    for (const c of liveCorners) {
      const d = Math.hypot(c.x - x, c.y - y)
      if (d <= bestDist) {
        bestDist = d
        best = { x: c.x, y: c.y }
      }
    }
    if (best) return { x: best.x, y: best.y, guide: { kind: 'point', x: best.x, y: best.y } }

    const anchor = opts.anchor
    const axis: 'h' | 'v' | null =
      anchor && opts.orthogonal
        ? Math.abs(x - anchor.x) >= Math.abs(y - anchor.y)
          ? 'h'
          : 'v'
        : null

    let hitWall: Wall | null = null
    let hitX = 0
    let hitY = 0
    let wallDist = radius
    for (const w of plan.walls) {
      if (w.id === opts.ignoreWallId) continue
      if (axis && anchor) {
        const cross = axisCross(anchor, axis, w)
        if (!cross) continue
        const d = axis === 'h' ? Math.abs(cross.x - x) : Math.abs(cross.y - y)
        if (d < wallDist) {
          wallDist = d
          hitWall = w
          hitX = cross.x
          hitY = cross.y
        }
        continue
      }
      const foot = projectOnSegment(x, y, w)
      if (!foot) continue
      const d = Math.hypot(foot.x - x, foot.y - y)
      if (d < wallDist) {
        wallDist = d
        hitWall = w
        hitX = foot.x
        hitY = foot.y
      }
    }
    if (hitWall) {
      return { x: hitX, y: hitY, guide: { kind: 'segment', wall: hitWall } }
    }

    if (anchor && axis === 'h') return { x: Math.round(x), y: anchor.y, guide: { kind: 'none' } }
    if (anchor && axis === 'v') return { x: anchor.x, y: Math.round(y), guide: { kind: 'none' } }

    return { x: Math.round(x), y: Math.round(y), guide: { kind: 'none' } }
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
      const next = await createWall({ ax: d.ax, ay: d.ay, bx: d.bx, by: d.by })
      if (seq === writeSeq.current) {
        setPlan(next)
        setError(null)
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
      const next = await updateWall(cur.wallId, { ax: cur.ax, ay: cur.ay, bx: cur.bx, by: cur.by })
      if (seq === writeSeq.current) {
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

  async function removeWall(id: string) {
    const seq = ++writeSeq.current
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

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex items-center justify-between px-4 py-3"
        style={{ background: 'var(--color-surface)', borderBottom: '1px solid var(--color-border)' }}
      >
        <div className="flex items-baseline gap-3">
          <h1 className="text-base font-semibold">平面图</h1>
          <span className="text-xs" style={{ color: 'var(--color-muted)' }}>
            {loading
              ? '读取中…'
              : `按住拖动画墙（${plan.walls.length} 面）· 点墙选中后按 Delete 删除 · 空格/中键平移 · 滚轮缩放 · Shift 自由角度`}
          </span>
          {selected && (
            <button
              onClick={() => void removeWall(selected.id)}
              disabled={saving}
              className="px-2 py-1 text-xs"
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
            onClick={fitToContent}
            className="px-2 py-1 text-xs"
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
              className="px-2 py-1 text-xs"
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
            className="px-2 py-1 text-xs"
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
function axisCross(
  anchor: { x: number; y: number },
  axis: 'h' | 'v',
  w: Wall,
): { x: number; y: number } | null {
  if (axis === 'h') {
    const dy = w.by - w.ay
    if (Math.abs(dy) < 1e-6) return null
    const t = (anchor.y - w.ay) / dy
    if (t < -1e-6 || t > 1 + 1e-6) return null
    return { x: w.ax + t * (w.bx - w.ax), y: anchor.y }
  }
  const dx = w.bx - w.ax
  if (Math.abs(dx) < 1e-6) return null
  const t = (anchor.x - w.ax) / dx
  if (t < -1e-6 || t > 1 + 1e-6) return null
  return { x: anchor.x, y: w.ay + t * (w.by - w.ay) }
}

/** 点到线段的最短距离（世界单位）。 */
function distanceToSegment(x: number, y: number, w: Wall): number {
  const dx = w.bx - w.ax
  const dy = w.by - w.ay
  const len2 = dx * dx + dy * dy
  if (len2 === 0) return Math.hypot(x - w.ax, y - w.ay)
  let t = ((x - w.ax) * dx + (y - w.ay) * dy) / len2
  t = Math.min(Math.max(t, 0), 1)
  return Math.hypot(x - (w.ax + t * dx), y - (w.ay + t * dy))
}

/** 点在线段上的垂足；垂足落在线段外就返回 null。 */
function projectOnSegment(
  x: number,
  y: number,
  w: Wall,
): { x: number; y: number } | null {
  const dx = w.bx - w.ax
  const dy = w.by - w.ay
  const len2 = dx * dx + dy * dy
  if (len2 === 0) return null
  const t = ((x - w.ax) * dx + (y - w.ay) * dy) / len2
  if (t < 0 || t > 1) return null
  return { x: w.ax + t * dx, y: w.ay + t * dy }
}

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
