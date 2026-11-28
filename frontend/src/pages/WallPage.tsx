import { CloudSun, Home, LogIn, RefreshCw, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { getWallSnapshot, type Item, type WallScreenSnapshot } from '../api/client.ts'
import { Badge } from '../components/ui/badge.tsx'
import { Button } from '../components/ui/button.tsx'
import { liveValuesFor, stateSummary } from '../features/control/live.ts'
import { ItemIcon } from '../lib/icons.tsx'

const SEASON_TEXT: Record<string, string> = {
  spring: '春季',
  summer: '夏季',
  autumn: '秋季',
  winter: '冬季',
}

/**
 * FR-4.7 免登录只读挂墙屏（/wall）：
 * 仅调用公开只读接口 GET /api/wall/snapshot，每 3 秒静默刷新，无任何控制与编辑入口。
 */
export default function WallPage() {
  const [snap, setSnap] = useState<WallScreenSnapshot | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [now, setNow] = useState(() => new Date())

  const refresh = useCallback(async () => {
    try {
      const data = await getWallSnapshot()
      setSnap(data)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : '无法读取家庭实况快照')
    }
  }, [])

  useEffect(() => {
    void refresh()
    const poll = window.setInterval(() => void refresh(), 3000)
    const clock = window.setInterval(() => setNow(new Date()), 1000)
    return () => {
      window.clearInterval(poll)
      window.clearInterval(clock)
    }
  }, [refresh])

  const typeMap = useMemo(() => new Map((snap?.types ?? []).map((t) => [t.key, t])), [snap?.types])
  const liveStateMap = useMemo(() => {
    const map: Parameters<typeof liveValuesFor>[0] = {}
    for (const st of snap?.states ?? []) {
      map[`${st.itemId}:${st.capabilityKey}`] = {
        itemId: st.itemId,
        capabilityKey: st.capabilityKey,
        value: st.value,
        source: st.source,
        ts: st.updatedAt,
      }
    }
    return map
  }, [snap?.states])

  const nowMs = now.getTime()
  const deviceView = useCallback(
    (dev: Item) => {
      const typ = typeMap.get(dev.typeKey)
      const vals = liveValuesFor(liveStateMap, dev.id, typ, nowMs)
      const isOn = vals.on === true || vals.power === true
      const summary = stateSummary(typ, vals) || '待机'
      return { typ, isOn, summary }
    },
    [typeMap, liveStateMap, nowMs],
  )

  const roomGroups = useMemo(() => {
    const map = new Map<string, { name: string; devices: Item[] }>()
    for (const r of snap?.plan.rooms ?? []) {
      if (r.name) map.set(r.hash, { name: r.name, devices: [] })
    }
    for (const it of snap?.items ?? []) {
      if (it.kind !== 'device') continue
      const key = it.roomHash || '__other__'
      const label = it.roomName || map.get(key)?.name || '其他区域'
      const entry = map.get(key)
      if (entry) entry.devices.push(it)
      else map.set(key, { name: label, devices: [it] })
    }
    return Array.from(map.entries()).filter(([, g]) => g.devices.length > 0)
  }, [snap])

  // 计算 2D 户型 SVG viewBox
  const bounds = useMemo(() => {
    const pts: { x: number; y: number }[] = []
    for (const w of snap?.plan.walls ?? []) {
      pts.push({ x: w.ax, y: w.ay }, { x: w.bx, y: w.by })
    }
    for (const it of snap?.items ?? []) {
      pts.push({ x: it.x, y: it.y })
    }
    if (pts.length === 0) return { minX: 0, minY: 0, width: 500, height: 400 }
    const xs = pts.map((p) => p.x)
    const ys = pts.map((p) => p.y)
    const pad = 45
    const minX = Math.min(...xs) - pad
    const minY = Math.min(...ys) - pad
    const width = Math.max(120, Math.max(...xs) - Math.min(...xs) + pad * 2)
    const height = Math.max(120, Math.max(...ys) - Math.min(...ys) + pad * 2)
    return { minX, minY, width, height }
  }, [snap])

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-bg-app text-text-primary">
      <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b border-border-default bg-bg-surface px-5">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-[var(--radius-md)] bg-accent-soft text-accent">
            <Home className="h-4.5 w-4.5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-semibold">家庭实况挂墙屏</h1>
              <Badge variant="accent">
                <ShieldCheck className="h-3 w-3" />
                免登录只读
              </Badge>
            </div>
            <p className="text-[11px] text-text-muted">每 3 秒自动同步 · 仅展示状态不可操控</p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          {snap?.simEnv ? (
            <Badge variant="secondary" className="gap-1 px-2.5 py-1 text-xs">
              <CloudSun className="h-3.5 w-3.5 text-accent" />
              {SEASON_TEXT[snap.simEnv.effectiveSeason] ?? snap.simEnv.effectiveSeason}
              {snap.simEnv.outdoorTemp !== undefined ? ` · 室外 ${snap.simEnv.outdoorTemp}℃` : ''}
            </Badge>
          ) : null}
          <span className="font-mono text-sm font-semibold tabular-nums text-text-primary">
            {now.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
          </span>
          <Button variant="outline" size="sm" onClick={() => { window.location.href = '/' }}>
            <LogIn className="h-3.5 w-3.5" />
            登录控制
          </Button>
        </div>
      </header>

      {error ? (
        <div className="border-b border-status-danger/25 bg-status-danger-soft px-5 py-2 text-xs text-status-danger">
          {error}
        </div>
      ) : null}

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-12">
        {/* 左侧：只读 2D 户型实时投影 */}
        <main className="relative flex items-center justify-center overflow-hidden bg-bg-canvas p-6 lg:col-span-8">
          <svg
            viewBox={`${bounds.minX} ${bounds.minY} ${bounds.width} ${bounds.height}`}
            className="h-full max-h-full w-full max-w-full"
          >
            {(snap?.plan.rooms ?? []).map((room) => (
              <polygon
                key={room.hash}
                points={room.ring.map((p) => `${p.x},${p.y}`).join(' ')}
                fill="var(--canvas-room-fill)"
                stroke="var(--canvas-room-stroke)"
                strokeWidth={1.5}
              />
            ))}
            {(snap?.plan.walls ?? []).map((w) => (
              <line
                key={w.id}
                x1={w.ax}
                y1={w.ay}
                x2={w.bx}
                y2={w.by}
                stroke="var(--canvas-wall)"
                strokeWidth={Math.max(4, w.thickness * 0.6)}
                strokeLinecap="round"
              />
            ))}
            {(snap?.items ?? []).map((it) => {
              const { typ, isOn } = deviceView(it)
              const w = typ?.defaultWidth ?? 40
              const d = typ?.defaultDepth ?? 40
              return (
                <g key={it.id} transform={`translate(${it.x}, ${it.y})`}>
                  <rect
                    x={-w / 2}
                    y={-d / 2}
                    width={w}
                    height={d}
                    rx={4}
                    fill={it.kind === 'device' ? (isOn ? 'var(--canvas-device-on-glow)' : 'var(--accent-soft)') : 'var(--canvas-furniture-fill)'}
                    stroke={isOn ? 'var(--canvas-device-on)' : 'var(--canvas-device-off)'}
                    strokeWidth={isOn ? 2.5 : 1.2}
                  />
                  <text
                    x={0}
                    y={d / 2 + 12}
                    textAnchor="middle"
                    fontSize={11}
                    fill="var(--canvas-label)"
                  >
                    {it.name}
                  </text>
                </g>
              )
            })}
          </svg>
          <span className="surface-glass absolute bottom-3 left-4 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[11px] text-text-muted">
            <RefreshCw className="h-3 w-3" />
            只读实况视图
          </span>
        </main>

        {/* 右侧：各房间设备与传感状态只读卡片 */}
        <aside className="min-h-0 overflow-y-auto border-t border-border-default bg-bg-surface p-4 lg:col-span-4 lg:border-l lg:border-t-0">
          <h2 className="mb-3 text-xs font-semibold tracking-wide text-text-secondary">
            房间设备实时状态（{snap?.items.filter((i) => i.kind === 'device').length ?? 0} 台）
          </h2>
          <div className="space-y-4">
            {roomGroups.map(([key, group]) => (
              <section key={key} className="space-y-2">
                <h3 className="text-xs font-semibold text-text-muted">{group.name}</h3>
                <div className="grid gap-1.5">
                  {group.devices.map((dev) => {
                    const { typ, isOn, summary } = deviceView(dev)
                    return (
                      <div
                        key={dev.id}
                        className="flex items-center justify-between gap-2 rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/40 px-3 py-2"
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)] bg-bg-surface text-accent">
                            <ItemIcon iconName={typ?.icons} typeKey={dev.typeKey} kind={dev.kind} className="h-3.5 w-3.5" />
                          </span>
                          <span className="truncate text-xs font-medium text-text-primary">{dev.name}</span>
                        </div>
                        <Badge variant={isOn ? 'success' : 'secondary'}>
                          {summary}
                        </Badge>
                      </div>
                    )
                  })}
                </div>
              </section>
            ))}
          </div>
        </aside>
      </div>
    </div>
  )
}
