import { PencilRuler, Search, Sparkles } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { sendItemCommand, type Item, type ItemType, type Room } from '../../api/client.ts'
import { liveValuesFor, stateSummary } from '../../features/control/live.ts'
import { useNow } from '../../features/control/useNow.ts'
import { ItemIcon } from '../../lib/icons.tsx'
import { cn } from '../../lib/utils.ts'
import { useLiveStore } from '../../store/liveState.ts'
import { Badge } from '../ui/badge.tsx'
import { Button } from '../ui/button.tsx'
import { Input } from '../ui/input.tsx'
import { Skeleton } from '../ui/skeleton.tsx'
import { Switch } from '../ui/switch.tsx'

type Props = {
  items: Item[]
  types: ItemType[]
  rooms: Room[]
  loading?: boolean
  isAdmin: boolean
  selectedItemId: string | null
  onSelectItem: (id: string) => void
  onSwitchToEdit?: () => void
}

type FilterMode = 'all' | 'controllable' | 'sensor'

/**
 * 「运行控制」模式下的左侧栏：按房间分组的设备与传感器速览，支持搜索与行内一键开关。
 */
export default function RoomDeviceList({
  items,
  types,
  rooms,
  loading = false,
  isAdmin,
  selectedItemId,
  onSelectItem,
  onSwitchToEdit,
}: Props) {
  const [q, setQ] = useState('')
  const [filterMode, setFilterMode] = useState<FilterMode>('all')
  const [togglingId, setTogglingId] = useState<string | null>(null)
  const liveStates = useLiveStore((s) => s.states)
  const nowMs = useNow()

  const typeByKey = useMemo(() => new Map(types.map((t) => [t.key, t])), [types])
  const roomNameByHash = useMemo(() => {
    const map = new Map<string, string>()
    for (const r of rooms) {
      if (r.name) map.set(r.hash, r.name)
    }
    return map
  }, [rooms])

  const devices = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return items.filter((item) => {
      if (item.kind !== 'device') return false
      const typ = typeByKey.get(item.typeKey)
      const writable = Boolean(typ?.capabilities.some((c) => c.writable))
      if (filterMode === 'controllable' && !writable) return false
      if (filterMode === 'sensor' && writable) return false
      if (needle) {
        const roomLabel = (item.roomName || (item.roomHash ? roomNameByHash.get(item.roomHash) : '') || '').toLowerCase()
        if (!item.name.toLowerCase().includes(needle) && !roomLabel.includes(needle)) return false
      }
      return true
    })
  }, [items, typeByKey, filterMode, q, roomNameByHash])

  const groups = useMemo(() => {
    const map = new Map<string, { label: string; items: Item[] }>()
    for (const dev of devices) {
      const key = dev.roomHash || '__unassigned__'
      const label = dev.roomName || (dev.roomHash ? roomNameByHash.get(dev.roomHash) : undefined) || '未分配房间'
      const entry = map.get(key)
      if (entry) entry.items.push(dev)
      else map.set(key, { label, items: [dev] })
    }
    return Array.from(map.entries()).sort(([a], [b]) => {
      if (a === '__unassigned__') return 1
      if (b === '__unassigned__') return -1
      return 0
    })
  }, [devices, roomNameByHash])

  async function quickToggle(item: Item, capKey: string, nextValue: boolean) {
    setTogglingId(item.id)
    try {
      await sendItemCommand(item.id, {
        capabilityKey: capKey,
        value: nextValue,
        requestId: crypto.randomUUID(),
      })
      toast.success(`${item.name} 已${nextValue ? '开启' : '关闭'}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : `控制 ${item.name} 失败`)
    } finally {
      setTogglingId(null)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="space-y-2.5 border-b border-border-default p-3">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-semibold tracking-wide text-text-secondary">房间与设备速览</h2>
          <Badge variant="secondary">{devices.length} 台</Badge>
        </div>

        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="搜索设备或房间…"
            className="h-8 pl-8 text-xs"
          />
        </div>

        <div className="grid grid-cols-3 gap-1 rounded-[var(--radius-sm)] bg-bg-subtle p-0.5 text-[11px]">
          {(
            [
              ['all', '全部'],
              ['controllable', '受控'],
              ['sensor', '传感'],
            ] as const
          ).map(([val, label]) => (
            <button
              key={val}
              type="button"
              onClick={() => setFilterMode(val)}
              className={cn(
                'cursor-pointer rounded-[calc(var(--radius-sm)-2px)] py-1 font-medium transition-colors',
                filterMode === val
                  ? 'bg-bg-surface text-text-primary shadow-[var(--shadow-sm)]'
                  : 'text-text-muted hover:text-text-primary',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {loading ? (
          <div className="space-y-2.5">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : groups.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-[var(--radius-md)] border border-dashed border-border-strong bg-bg-subtle/40 px-4 py-8 text-center">
            <Sparkles className="mb-2 h-6 w-6 text-text-muted" />
            <p className="text-xs font-medium text-text-primary">
              {items.some((i) => i.kind === 'device') ? '没有匹配的设备' : '还没有布放智能设备'}
            </p>
            <p className="mt-1 text-[11px] text-text-muted">
              {items.some((i) => i.kind === 'device')
                ? '试试清空搜索关键字或切换筛选分类'
                : '切换到户型编辑模式，从素材库拖入灯光、空调或传感器'}
            </p>
            {isAdmin && onSwitchToEdit && !items.some((i) => i.kind === 'device') ? (
              <Button variant="soft" size="xs" className="mt-3" onClick={onSwitchToEdit}>
                <PencilRuler className="h-3 w-3" />
                去户型编辑放置
              </Button>
            ) : null}
          </div>
        ) : (
          <div className="space-y-4">
            {groups.map(([key, group]) => (
              <section key={key} className="space-y-1.5">
                <div className="flex items-center justify-between px-1">
                  <span className="text-[11px] font-semibold text-text-muted">{group.label}</span>
                  <span className="text-[10px] text-text-muted">{group.items.length}</span>
                </div>
                <ul className="space-y-1">
                  {group.items.map((dev) => {
                    const typ = typeByKey.get(dev.typeKey)
                    const vals = liveValuesFor(liveStates, dev.id, typ, nowMs)
                    const summary = stateSummary(typ, vals)
                    const powerCap = typ?.capabilities.find(
                      (c) => c.writable && c.type === 'bool' && (c.key === 'power' || c.key === 'on'),
                    )
                    const isOn = powerCap ? vals[powerCap.key] === true : false
                    const selected = dev.id === selectedItemId

                    return (
                      <li key={dev.id}>
                        <div
                          role="button"
                          tabIndex={0}
                          onClick={() => onSelectItem(dev.id)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault()
                              onSelectItem(dev.id)
                            }
                          }}
                          className={cn(
                            'flex cursor-pointer items-center justify-between gap-2 rounded-[var(--radius-sm)] border px-2.5 py-2 transition-colors',
                            selected
                              ? 'border-accent bg-accent-soft/70'
                              : 'border-border-default bg-bg-surface hover:border-border-strong hover:bg-bg-subtle/60',
                          )}
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            <span
                              className={cn(
                                'flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)]',
                                isOn
                                  ? 'bg-status-warning-soft text-status-warning'
                                  : 'bg-bg-subtle text-text-muted',
                              )}
                            >
                              <ItemIcon
                                iconName={typ?.icons}
                                typeKey={dev.typeKey}
                                kind={dev.kind}
                                className="h-3.5 w-3.5"
                              />
                            </span>
                            <div className="min-w-0">
                              <p className="truncate text-xs font-medium text-text-primary">{dev.name}</p>
                              <p className="truncate text-[11px] text-text-muted">
                                {summary || typ?.name || dev.typeKey}
                              </p>
                            </div>
                          </div>

                          {powerCap ? (
                            <div
                              onClick={(e) => e.stopPropagation()}
                              onKeyDown={(e) => e.stopPropagation()}
                            >
                              <Switch
                                aria-label={`切换 ${dev.name}`}
                                checked={isOn}
                                disabled={togglingId === dev.id}
                                onCheckedChange={(checked) => void quickToggle(dev, powerCap.key, checked)}
                              />
                            </div>
                          ) : null}
                        </div>
                      </li>
                    )
                  })}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
