import { useMemo } from 'react'
import type { Item, ItemKind, ItemType, Room } from '../api/client.ts'

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
 * 左侧家具库 + 筛选 + 物品列表。
 * 家具库只负责 HTML5 拖拽源；真正的落点在 PlanPage 的画布上。
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
    <aside className="flex w-60 min-w-60 flex-col gap-3 overflow-auto p-3" style={{ borderRight: '1px solid var(--color-border)' }}>
      <section>
        <h2 className="mb-2 text-sm font-semibold">家具库</h2>
        <p className="mb-2 text-[11px]" style={{ color: 'var(--color-muted)' }}>
          拖到画布上布放；类型由元数据驱动。
        </p>
        <div className="grid gap-1">
          {types.map((typ) => (
            <button
              key={typ.key}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData('application/x-smart-home-type', typ.key)
                e.dataTransfer.effectAllowed = 'copy'
              }}
              className="cursor-grab px-2 py-1 text-left text-xs active:cursor-grabbing"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: 'var(--color-bg)',
              }}
              title={`${typ.name}（${typ.key}）`}
            >
              <span className="block text-sm">{typ.name}</span>
              <span style={{ color: 'var(--color-muted)' }}>
                {typ.kind === 'device' ? '设备' : '家具'} · {typ.defaultWidth}×{typ.defaultDepth}cm
              </span>
            </button>
          ))}
        </div>
      </section>

      <section className="grid gap-2">
        <h2 className="text-sm font-semibold">筛选</h2>
        <label className="grid gap-1 text-[11px]">
          房间
          <select
            value={filters.room}
            onChange={(e) => onFiltersChange({ room: e.target.value })}
            style={inputStyle}
            className="px-2 py-1 text-xs"
          >
            <option value="">全部房间</option>
            {namedRooms.map((room) => (
              <option key={room.hash} value={room.hash}>
                {room.name}
              </option>
            ))}
          </select>
        </label>
        <label className="grid gap-1 text-[11px]">
          类别
          <select
            value={filters.kind}
            onChange={(e) => onFiltersChange({ kind: e.target.value as ItemFilters['kind'] })}
            style={inputStyle}
            className="px-2 py-1 text-xs"
          >
            <option value="">全部类别</option>
            <option value="furniture">家具</option>
            <option value="device">设备</option>
          </select>
        </label>
        <label className="grid gap-1 text-[11px]">
          关键字
          <input
            value={filters.q}
            onChange={(e) => onFiltersChange({ q: e.target.value })}
            placeholder="按名称搜索"
            style={inputStyle}
            className="px-2 py-1 text-xs"
          />
        </label>
      </section>

      <section className="min-h-0">
        <h2 className="mb-2 text-sm font-semibold">
          物品 <span style={{ color: 'var(--color-muted)' }}>({visibleItems.length}/{items.length})</span>
        </h2>
        <div className="grid gap-1">
          {visibleItems.map((item) => (
            <button
              key={item.id}
              onClick={() => onSelectItem(item.id)}
              className="px-2 py-1 text-left text-xs"
              style={{
                border: '1px solid',
                borderColor: item.id === selectedItemId ? 'var(--color-accent, #4f6bed)' : 'var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: item.id === selectedItemId ? 'var(--color-bg)' : 'transparent',
              }}
            >
              <span className="block text-sm">{item.name}</span>
              <span style={{ color: 'var(--color-muted)' }}>
                {item.kind === 'device' ? '设备' : '家具'}
                {item.roomName ? ` · ${item.roomName}` : ' · 未分配房间'}
              </span>
            </button>
          ))}
          {visibleItems.length === 0 ? (
            <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
              没有匹配的物品。
            </p>
          ) : null}
        </div>
      </section>
    </aside>
  )
}

const inputStyle = {
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--color-bg)',
  color: 'inherit',
} as const
