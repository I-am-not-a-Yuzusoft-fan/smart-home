import { useCallback, useEffect, useRef, useState } from 'react'
import { Circle, Layer, Line, Stage, Text } from 'react-konva'
import type Konva from 'konva'
import {
  ApiError,
  createWall,
  deleteWall,
  getPlan,
  logout,
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

const GRID = 20
/** 1 厘米 = 1 像素。户型按厘米存，画布也按厘米画，中间不做单位换算。 */
const PX_PER_CM = 1
/** 端点吸附半径（像素）。12 px 约等于"手指能对准"的宽度。 */
const SNAP_PX = 12
/** 短于这个长度的拖拽当成误触，不建墙。 */
const MIN_DRAG_PX = 8

type Draft = { ax: number; ay: number; bx: number; by: number }

/**
 * 平面图主页（首屏）。
 *
 * T2：拖出墙、端点吸附、长度显示、刷新后仍在、可删除。
 * 坐标一律按厘米与后端对话；屏幕上 1 cm 画 1 px。
 */
export default function PlanPage({ user, onSignedOut }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 800, height: 600 })
  const [plan, setPlan] = useState<Plan>({ corners: [], walls: [], rooms: [], openWalls: 0 })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showTypes, setShowTypes] = useState(false)
  // 只接受"最后一次请求"的响应：连拖两面墙时，先发的请求可能后到，
  // 若整份覆盖就会把后画的墙抹掉（刷新才回来，用户以为丢了）。
  const writeSeq = useRef(0)

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

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      setSize({ width: Math.max(width, 320), height: Math.max(height, 240) })
    })
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  // 删墙也支持键盘：选中后按 Delete / Backspace
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== 'Delete' && e.key !== 'Backspace') return
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA')) return
      if (selectedId) void removeWall(selectedId)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // removeWall 每次渲染都是新函数，但内部只用 setState 与 props，闭包安全
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId])

  // 只认"被墙引用"的端点：删墙后留下的孤立端点既不该画出来，也不该被吸附——
  // 否则用户会吸到一个看不见的点上，还以为是 bug。
  const liveCorners = plan.corners.filter((c) =>
    plan.walls.some((w) => w.aId === c.id || w.bId === c.id),
  )

  /** 把屏幕坐标吸附到最近的端点；没有就四舍五入到 1 cm。 */
  function snap(x: number, y: number): { x: number; y: number } {
    let best = null as { x: number; y: number } | null
    let bestDist = SNAP_PX
    for (const c of liveCorners) {
      const d = Math.hypot(c.x - x, c.y - y)
      if (d <= bestDist) {
        bestDist = d
        best = { x: c.x, y: c.y }
      }
    }
    if (best) return best
    return { x: Math.round(x), y: Math.round(y) }
  }

  function pointer(): { x: number; y: number } | null {
    const stage = stageRef.current
    const p = stage?.getPointerPosition()
    if (!p) return null
    return { x: p.x / PX_PER_CM, y: p.y / PX_PER_CM }
  }

  const stageRef = useRef<Konva.Stage>(null)

  function onMouseDown() {
    const p = pointer()
    if (!p) return
    const s = snap(p.x, p.y)
    setDraft({ ax: s.x, ay: s.y, bx: s.x, by: s.y })
    setSelectedId(null)
  }

  function onMouseMove() {
    if (!draft) return
    const p = pointer()
    if (!p) return
    const s = snap(p.x, p.y)
    setDraft((d) => (d ? { ...d, bx: s.x, by: s.y } : d))
  }

  async function onMouseUp() {
    const d = draft
    setDraft(null)
    if (!d) return
    // 终点要用松手那一刻的指针，不能用最后一次 mousemove 的位置：
    // 浏览器可能在松手前不再发 mousemove，那样墙会短一截、四个角对不上。
    const p = pointer()
    const end = p ? snap(p.x, p.y) : { x: d.bx, y: d.by }
    const wall = { ax: d.ax, ay: d.ay, bx: end.x, by: end.y }
    if (Math.hypot(wall.bx - wall.ax, wall.by - wall.ay) < MIN_DRAG_PX / PX_PER_CM) return
    await submitWall(wall)
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

  const gridLines = []
  for (let x = 0; x <= size.width; x += GRID) {
    gridLines.push(<Line key={`v${x}`} points={[x, 0, x, size.height]} stroke="#e8e8f2" strokeWidth={1} />)
  }
  for (let y = 0; y <= size.height; y += GRID) {
    gridLines.push(<Line key={`h${y}`} points={[0, y, size.width, y]} stroke="#e8e8f2" strokeWidth={1} />)
  }

  const selected = plan.walls.find((w) => w.id === selectedId) ?? null

  // 有墙但推不出房间时的提示文案：空户型和"画了墙但没围上"要说不同的话，
  // 否则用户画了三面墙回到刷新，会以为房间功能坏了。
  const hint =
    plan.walls.length === 0
      ? null
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
            {loading ? '读取中…' : `按住并拖动可画墙（${plan.walls.length} 面）；点墙选中后按 Delete 删除`}
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
          onMouseDown={onMouseDown}
          onMouseMove={onMouseMove}
          onMouseUp={() => void onMouseUp()}
          onMouseLeave={() => setDraft(null)}
          style={{ cursor: 'crosshair' }}
        >
          <Layer>{gridLines}</Layer>

          {/* 房间画在墙下面：半透明填充，墙压在上面边界才清楚 */}
          <Layer listening={false}>
            {plan.rooms.map((room) => (
              <RoomShape key={room.hash} room={room} />
            ))}
          </Layer>

          <Layer>
            {plan.walls.map((w) => (
              <WallShape
                key={w.id}
                wall={w}
                selected={w.id === selectedId}
                onSelect={() => setSelectedId(w.id)}
              />
            ))}
            {draft && (
              <Line
                points={[
                  draft.ax * PX_PER_CM,
                  draft.ay * PX_PER_CM,
                  draft.bx * PX_PER_CM,
                  draft.by * PX_PER_CM,
                ]}
                stroke="#4f6bed"
                strokeWidth={10}
                opacity={0.4}
                lineCap="round"
              />
            )}
          </Layer>

          <Layer listening={false}>
            {liveCorners.map((c) => (
              <Circle key={c.id} x={c.x * PX_PER_CM} y={c.y * PX_PER_CM} radius={3} fill="#8b8b9e" />
            ))}
            {draft && (
              <Text
                x={((draft.ax + draft.bx) / 2) * PX_PER_CM + 8}
                y={((draft.ay + draft.by) / 2) * PX_PER_CM - 8}
                text={formatCm(Math.hypot(draft.bx - draft.ax, draft.by - draft.ay))}
                fontSize={12}
                fill="#4f6bed"
              />
            )}
            {!loading && hint && (
              <Text x={32} y={32} text={hint} fontSize={14} fill="#b06a00" />
            )}
            {!loading && plan.walls.length === 0 && (
              <Text
                x={32}
                y={32}
                text="空地。在画布上按住并拖动，画下第一面墙；端点会自动吸附到附近的墙端。"
                fontSize={14}
                fill="#6b6b80"
              />
            )}
          </Layer>
        </Stage>

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

function WallShape({ wall, selected, onSelect }: { wall: Wall; selected: boolean; onSelect: () => void }) {
  const midX = ((wall.ax + wall.bx) / 2) * PX_PER_CM
  const midY = ((wall.ay + wall.by) / 2) * PX_PER_CM
  // 标签挪到墙的法线方向一点，避免压在墙线上
  const dx = (wall.bx - wall.ax) * PX_PER_CM
  const dy = (wall.by - wall.ay) * PX_PER_CM
  const len = Math.hypot(dx, dy) || 1
  const nx = (-dy / len) * 14
  const ny = (dx / len) * 14

  return (
    <>
      <Line
        points={[wall.ax * PX_PER_CM, wall.ay * PX_PER_CM, wall.bx * PX_PER_CM, wall.by * PX_PER_CM]}
        stroke={selected ? '#4f6bed' : '#3a3a4d'}
        strokeWidth={Math.max(wall.thickness, 4) * PX_PER_CM}
        lineCap="round"
        onClick={(e) => {
          e.cancelBubble = true
          onSelect()
        }}
        onTap={(e) => {
          e.cancelBubble = true
          onSelect()
        }}
      />
      <Text x={midX + nx} y={midY + ny - 6} text={formatCm(len)} fontSize={12} fill="#5a5a70" />
    </>
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

/**
 * 一个房间：半透明多边形 + 面积标签。
 *
 * 面积与顶点都来自后端，这里只画——前端若自己也推导一遍，两套实现迟早分叉，
 * 就会出现「画布上是一间、列表里是两间」。
 */
function RoomShape({ room }: { room: Room }) {
  const points = room.ring.flatMap((p) => [p.x * PX_PER_CM, p.y * PX_PER_CM])
  const cx = (room.ring.reduce((sum, p) => sum + p.x, 0) / room.ring.length) * PX_PER_CM
  const cy = (room.ring.reduce((sum, p) => sum + p.y, 0) / room.ring.length) * PX_PER_CM
  const label = room.name ? `${room.name}　${room.areaM2.toFixed(1)} ㎡` : `${room.areaM2.toFixed(1)} ㎡`

  return (
    <>
      <Line points={points} closed fill="rgba(79,107,237,0.10)" stroke="rgba(79,107,237,0.35)" strokeWidth={1} />
      <Text x={cx} y={cy} text={label} fontSize={13} fill="#3a4a8f" offsetX={18} />
    </>
  )
}
