import { Activity, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { listItemHistory, type Capability, type StateHistory } from '../../api/client.ts'
import { cn } from '../../lib/utils.ts'

type RangeKey = '1h' | '24h' | '7d'

const RANGE_HOURS: Record<RangeKey, number> = {
  '1h': 1,
  '24h': 24,
  '7d': 24 * 7,
}

type Props = {
  itemId: string
  capabilities: Capability[]
}

/**
 * F4 轻量响应式 SVG 历史趋势图（数值折线 / 布尔阶梯线），零重型依赖，自动吃 CSS Variables。
 */
export default function HistoryChart({ itemId, capabilities }: Props) {
  const chartableCaps = useMemo(
    () => capabilities.filter((c) => c.type === 'number' || c.type === 'bool'),
    [capabilities],
  )
  const [selectedKey, setSelectedKey] = useState<string>(() => chartableCaps[0]?.key ?? '')
  const [range, setRange] = useState<RangeKey>('24h')
  const [rows, setRows] = useState<StateHistory[]>([])
  const [loading, setLoading] = useState(false)
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)

  useEffect(() => {
    if (!chartableCaps.some((c) => c.key === selectedKey)) {
      setSelectedKey(chartableCaps[0]?.key ?? '')
    }
  }, [chartableCaps, selectedKey])

  useEffect(() => {
    if (!selectedKey) return
    let cancelled = false
    setLoading(true)
    const from = new Date(Date.now() - RANGE_HOURS[range] * 3600 * 1000).toISOString()
    listItemHistory(itemId, { key: selectedKey, from, limit: 120 })
      .then((data) => {
        if (!cancelled) setRows([...data].reverse())
      })
      .catch(() => {
        if (!cancelled) setRows([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [itemId, selectedKey, range])

  const activeCap = chartableCaps.find((c) => c.key === selectedKey)
  if (chartableCaps.length === 0) return null

  const points = rows
    .map((r) => {
      const y =
        typeof r.value === 'boolean'
          ? r.value
            ? 1
            : 0
          : typeof r.value === 'number'
            ? r.value
            : Number(r.value)
      return {
        t: new Date(r.recordedAt).getTime(),
        label: new Date(r.recordedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
        y,
        raw: r.value,
      }
    })
    .filter((p) => Number.isFinite(p.t) && Number.isFinite(p.y))

  const W = 260
  const H = 104
  const padL = 28
  const padR = 8
  const padT = 10
  const padB = 18
  const innerW = W - padL - padR
  const innerH = H - padT - padB

  const isBool = activeCap?.type === 'bool'
  const minY = isBool ? 0 : Math.min(...(points.length ? points.map((p) => p.y) : [0]), activeCap?.min ?? Infinity)
  const maxY = isBool ? 1 : Math.max(...(points.length ? points.map((p) => p.y) : [1]), activeCap?.max ?? -Infinity)
  const safeMinY = Number.isFinite(minY) ? minY : 0
  const safeMaxY = Number.isFinite(maxY) && maxY > safeMinY ? maxY : safeMinY + 1

  const minT = points.length > 0 ? points[0].t : 0
  const maxT = points.length > 1 ? points[points.length - 1].t : minT + 1

  const coords = points.map((p, idx) => {
    const x =
      points.length === 1
        ? padL + innerW / 2
        : padL + ((p.t - minT) / Math.max(1, maxT - minT)) * innerW
    const y = padT + innerH - ((p.y - safeMinY) / (safeMaxY - safeMinY)) * innerH
    return { ...p, x, y, idx }
  })

  const pathD = coords.reduce((acc, pt, i) => {
    if (i === 0) return `M ${pt.x.toFixed(1)} ${pt.y.toFixed(1)}`
    if (isBool) {
      const prev = coords[i - 1]
      return `${acc} L ${pt.x.toFixed(1)} ${prev.y.toFixed(1)} L ${pt.x.toFixed(1)} ${pt.y.toFixed(1)}`
    }
    return `${acc} L ${pt.x.toFixed(1)} ${pt.y.toFixed(1)}`
  }, '')

  const hovered = hoverIdx !== null ? coords[hoverIdx] : coords[coords.length - 1]

  return (
    <section className="space-y-2 rounded-[var(--radius-md)] border border-border-default bg-bg-subtle/40 p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-text-secondary">
          <Activity className="h-3.5 w-3.5 text-accent" />
          历史趋势
        </span>
        <div className="inline-flex rounded-[var(--radius-sm)] bg-bg-subtle p-0.5 text-[10px]">
          {(['1h', '24h', '7d'] as const).map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRange(r)}
              className={cn(
                'cursor-pointer rounded px-1.5 py-0.5 font-medium transition-colors',
                range === r ? 'bg-bg-surface text-accent shadow-xs' : 'text-text-muted hover:text-text-primary',
              )}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      {chartableCaps.length > 1 ? (
        <div className="flex flex-wrap gap-1">
          {chartableCaps.map((c) => (
            <button
              key={c.key}
              type="button"
              onClick={() => setSelectedKey(c.key)}
              className={cn(
                'cursor-pointer rounded-full border px-2 py-0.5 text-[10px] font-medium transition-colors',
                c.key === selectedKey
                  ? 'border-accent bg-accent-soft text-accent'
                  : 'border-border-default bg-bg-surface text-text-muted hover:text-text-primary',
              )}
            >
              {c.name}
            </button>
          ))}
        </div>
      ) : null}

      {loading ? (
        <div className="flex h-[104px] items-center justify-center text-xs text-text-muted">
          <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          加载历史曲线…
        </div>
      ) : points.length === 0 ? (
        <div className="flex h-[84px] items-center justify-center rounded-[var(--radius-sm)] bg-bg-surface/60 text-[11px] text-text-muted">
          该时间范围暂无历史记录
        </div>
      ) : (
        <div className="space-y-1">
          <svg
            viewBox={`0 0 ${W} ${H}`}
            className="h-[104px] w-full overflow-visible rounded-[var(--radius-sm)] bg-bg-surface"
            onMouseLeave={() => setHoverIdx(null)}
          >
            {/* 横向参考线 */}
            {[0, 0.5, 1].map((ratio) => {
              const y = padT + innerH * (1 - ratio)
              const val = safeMinY + (safeMaxY - safeMinY) * ratio
              return (
                <g key={ratio}>
                  <line
                    x1={padL}
                    x2={W - padR}
                    y1={y}
                    y2={y}
                    stroke="var(--border-default)"
                    strokeDasharray="2 2"
                    strokeWidth={0.8}
                  />
                  <text
                    x={padL - 4}
                    y={y + 3}
                    textAnchor="end"
                    fontSize={8}
                    fill="var(--text-muted)"
                  >
                    {isBool ? (ratio === 1 ? '开' : ratio === 0 ? '关' : '') : Math.round(val * 10) / 10}
                  </text>
                </g>
              )
            })}

            <path d={pathD} fill="none" stroke="var(--accent)" strokeWidth={1.8} strokeLinejoin="round" />

            {coords.map((pt) => (
              <circle
                key={pt.idx}
                cx={pt.x}
                cy={pt.y}
                r={hoverIdx === pt.idx ? 3.5 : 2}
                fill="var(--accent)"
                className="cursor-pointer"
                onMouseEnter={() => setHoverIdx(pt.idx)}
              />
            ))}
          </svg>

          {hovered ? (
            <div className="flex items-center justify-between text-[11px] text-text-muted">
              <span>{hovered.label}</span>
              <span className="font-mono font-medium text-text-primary">
                {isBool
                  ? hovered.y === 1
                    ? '开启'
                    : '关闭'
                  : `${hovered.y}${activeCap?.unit ? ` ${activeCap.unit}` : ''}`}
              </span>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}
