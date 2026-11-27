import { GripVertical, Search } from 'lucide-react'
import { useMemo } from 'react'
import type { Item, ItemKind, ItemType, Room } from '../api/client.ts'
import { liveValuesFor, stateSummary } from '../features/control/live.ts'
import { useNow } from '../features/control/useNow.ts'
import { ItemIcon } from '../lib/icons.tsx'
import { cn } from '../lib/utils.ts'
import { useLiveStore } from '../store/liveState.ts'
import { Badge } from './ui/badge.tsx'
import { Input } from './ui/input.tsx'

export type ItemFilters = {
  room: string
  kind: '' | ItemKind
  q: string
}

type Props = {
  types: ItemType[]
  items: Item[]
  rooms: Room[]
  filters: ItemFilters
  selectedItemId: string | null
  onFiltersChange: (patch: Partial<ItemFilters>) => void
  onSelectItem: (id: string) => void
}

/**
 * 「户型编辑」模式下的左侧素材库 + 已放物品筛选。
 * 家具库负责 HTML5 拖拽源，真正的落点在 PlanPage 的画布上。
 */
export default function ItemSidebar({
  types,
  items,
  rooms,
  filters,
  selectedItemId,
  onFiltersChange,
  onSelectItem,
}: Props) {
  const namedRooms = useMemo(() => rooms.filter((room) => room.name), [rooms])
  const liveStates = useLiveStore((store) => store.states)
  const nowMs = useNow()
  const typeByKey = useMemo(() => new Map(types.map((typ) => [typ.key, typ])), [types])
  const summaryOf = (item: Item) => {
    const type = typeByKey.get(item.typeKey)
    return item.kind === 'device' ? stateSummary(type, liveValuesFor(liveStates, item.id, type, nowMs)) : ''
  }
  const visibleItems = useMemo(() => {
    const q = filters.q.trim().toLowerCase()
    return items.filter((item) => {
      if (filters.room && item.roomHash !== filters.room) return false
      if (filters.kind && item.kind !== filters.kind) return false
      if (q && !item.name.toLowerCase().includes(q)) return false
      return true
    })
  }, [filters, items])

  return (
    <div className="flex h-full flex-col overflow-y-auto p-3">
      <section className="space-y-2">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-semibold tracking-wide text-text-secondary">素材库（拖入画布）</h2>
          <Badge variant="secondary">{types.length}</Badge>
        </div>
        <div className="grid gap-1.5">
          {types.map((typ) => (
            <button
              key={typ.key}
              type="button"
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData('application/x-smart-home-type', typ.key)
                e.dataTransfer.effectAllowed = 'copy'
              }}
              className="group flex cursor-grab items-center gap-2 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 py-2 text-left transition-colors hover:border-accent-border hover:bg-accent-soft/40 active:cursor-grabbing"
              title={`拖拽放置「${typ.name}」`}
            >
              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)] bg-bg-subtle text-text-secondary group-hover:bg-accent-soft group-hover:text-accent">
                <ItemIcon iconName={typ.icons} typeKey={typ.key} kind={typ.kind} className="h-3.5 w-3.5" />
              </span>
              <div className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium text-text-primary">{typ.name}</span>
                <span className="block truncate text-[10px] text-text-muted">
                  {typ.kind === 'device' ? '设备' : '家具'} · {typ.defaultWidth}×{typ.defaultDepth}cm
                  {typ.mount === 'wall' ? ' · 挂墙' : typ.mount === 'ceiling' ? ' · 吸顶' : ''}
                </span>
              </div>
              <GripVertical className="h-3.5 w-3.5 shrink-0 text-text-muted/50" />
            </button>
          ))}
        </div>
      </section>

      <section className="mt-4 space-y-2 border-t border-border-default pt-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-semibold tracking-wide text-text-secondary">
            已放置物品
          </h2>
          <span className="text-[11px] text-text-muted">
            {visibleItems.length}/{items.length}
          </span>
        </div>

        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" />
          <Input
            value={filters.q}
            onChange={(e) => onFiltersChange({ q: e.target.value })}
            placeholder="按名称筛选…"
            className="h-8 pl-8 text-xs"
          />
        </div>

        <div className="grid grid-cols-2 gap-1.5">
          <select
            aria-label="按房间筛选"
            value={filters.room}
            onChange={(e) => onFiltersChange({ room: e.target.value })}
            className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-[11px] text-text-primary outline-none focus:border-accent"
          >
            <option value="">全部房间</option>
            {namedRooms.map((room) => (
              <option key={room.hash} value={room.hash}>
                {room.name}
              </option>
            ))}
          </select>
          <select
            aria-label="按类别筛选"
            value={filters.kind}
            onChange={(e) => onFiltersChange({ kind: e.target.value as ItemFilters['kind'] })}
            className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-[11px] text-text-primary outline-none focus:border-accent"
          >
            <option value="">全部类别</option>
            <option value="device">设备</option>
            <option value="furniture">家具</option>
          </select>
        </div>

        <div className="grid gap-1 pt-1">
          {visibleItems.map((item) => {
            const typ = typeByKey.get(item.typeKey)
            const selected = item.id === selectedItemId
            const summary = summaryOf(item)
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => onSelectItem(item.id)}
                className={cn(
                  'flex cursor-pointer items-center gap-2 rounded-[var(--radius-sm)] border px-2.5 py-1.5 text-left transition-colors',
                  selected
                    ? 'border-accent bg-accent-soft/70'
                    : 'border-border-default bg-bg-surface hover:bg-bg-subtle/60',
                )}
              >
                <ItemIcon
                  iconName={typ?.icons}
                  typeKey={item.typeKey}
                  kind={item.kind}
                  className="h-3.5 w-3.5 shrink-0 text-text-muted"
                />
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-xs font-medium text-text-primary">{item.name}</span>
                  <span className="flex items-center gap-1 truncate text-[10px] text-text-muted">
                    {item.roomName || '未分配房间'}
                    {summary ? <span className="text-accent">· {summary}</span> : null}
                  </span>
                </div>
              </button>
            )
          })}
          {visibleItems.length === 0 ? (
            <p className="py-4 text-center text-xs text-text-muted">没有匹配的物品</p>
          ) : null}
        </div>
      </section>
    </div>
  )
}
