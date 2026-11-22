import { useEffect, useState } from 'react'
import type { Item, ItemType, Room } from '../api/client.ts'

type Props = {
  item: Item | null
  type?: ItemType
  rooms: Room[]
  onUpdate: (id: string, patch: {
    name?: string
    roomHash?: string
    x?: number
    y?: number
    rotation?: number
    wallId?: string
    wallOffset?: number
  }) => Promise<void>
  onDelete: (id: string) => Promise<void>
}

/** 选中物品后的属性面板：名称、房间、坐标、旋转、删除。 */
export default function ItemInspector({ item, type, rooms, onUpdate, onDelete }: Props) {
  const [name, setName] = useState('')
  const [roomHash, setRoomHash] = useState('')
  const [x, setX] = useState('0')
  const [y, setY] = useState('0')
  const [rotation, setRotation] = useState('0')
  const [wallOffset, setWallOffset] = useState('0')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!item) return
    setName(item.name)
    setRoomHash(item.roomHash ?? '')
    setX(String(item.x))
    setY(String(item.y))
    setRotation(String(item.rotation))
    setWallOffset(String(item.wallOffset ?? 0))
    setError(null)
  }, [item?.id, item?.name, item?.roomHash, item?.x, item?.y, item?.rotation, item?.wallOffset])

  if (!item) {
    return (
      <aside className="w-64 min-w-64 p-3 text-xs" style={{ borderLeft: '1px solid var(--color-border)', color: 'var(--color-muted)' }}>
        选中画布或列表里的物品后，这里可以改名称、所属房间和坐标。
      </aside>
    )
  }

  const namedRooms = rooms.filter((room) => room.name)

  async function save() {
    if (!item) return
    const nx = Number(x)
    const ny = Number(y)
    const nr = Number(rotation)
    if (!Number.isFinite(nx) || !Number.isFinite(ny) || !Number.isFinite(nr)) {
      setError('坐标与旋转必须是数字')
      return
    }
    if (name.trim() === '') {
      setError('名称不能为空')
      return
    }
    setSaving(true)
    setError(null)
    try {
      // 挂墙物品只报沿墙偏移：x/y/rotation 是墙的函数，直接写会被后端拒掉（400）。
      if (item.wallId) {
        const nOffset = Number(wallOffset)
        if (!Number.isFinite(nOffset) || nOffset < 0) {
          setError('沿墙偏移必须是不小于 0 的数字')
          setSaving(false)
          return
        }
        await onUpdate(item.id, {
          name: name.trim(),
          roomHash,
          wallId: item.wallId,
          wallOffset: nOffset,
        })
        return
      }
      await onUpdate(item.id, {
        name: name.trim(),
        roomHash,
        x: nx,
        y: ny,
        rotation: nr,
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <aside className="flex w-64 min-w-64 flex-col gap-3 overflow-auto p-3" style={{ borderLeft: '1px solid var(--color-border)' }}>
      <div>
        <h2 className="text-sm font-semibold">{item.name}</h2>
        <p className="text-[11px]" style={{ color: 'var(--color-muted)' }}>
          {item.kind === 'device' ? '设备' : '家具'} · {type?.name ?? item.typeKey}
          {type ? ` · ${type.defaultWidth}×${type.defaultDepth}cm` : ''}
          {item.wallId ? ' · 挂墙' : type?.mount === 'ceiling' ? ' · 吸顶' : ''}
        </p>
      </div>

      {error ? (
        <p className="text-xs" style={{ color: 'var(--color-danger)' }} role="alert">{error}</p>
      ) : null}

      <label className="grid gap-1 text-[11px]">
        名称
        <input value={name} onChange={(e) => setName(e.target.value)} style={inputStyle} className="px-2 py-1 text-sm" />
      </label>
      <label className="grid gap-1 text-[11px]">
        所属房间
        <select value={roomHash} onChange={(e) => setRoomHash(e.target.value)} style={inputStyle} className="px-2 py-1 text-sm">
          <option value="">未分配房间</option>
          {namedRooms.map((room) => (
            <option key={room.hash} value={room.hash}>{room.name}</option>
          ))}
        </select>
      </label>
      {item.wallId ? (
        <>
          <label className="grid gap-1 text-[11px]">
            沿墙偏移（cm）
            <input
              type="number"
              min={0}
              value={wallOffset}
              onChange={(e) => setWallOffset(e.target.value)}
              style={inputStyle}
              className="px-2 py-1 text-sm"
            />
          </label>
          <p className="text-[11px]" style={{ color: 'var(--color-muted)' }}>
            {type?.mount === 'wall'
              ? '挂墙物品的位置与朝向由墙决定：挪这面墙，它会跟着走；拖到另一面墙边就能换墙。'
              : '这面墙的位置与朝向决定它的摆放：挪墙它会跟着走。把它拖离墙面就能自由摆放。'}
          </p>
        </>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-2">
            <label className="grid gap-1 text-[11px]">
              x（cm）
              <input type="number" value={x} onChange={(e) => setX(e.target.value)} style={inputStyle} className="px-2 py-1 text-sm" />
            </label>
            <label className="grid gap-1 text-[11px]">
              y（cm）
              <input type="number" value={y} onChange={(e) => setY(e.target.value)} style={inputStyle} className="px-2 py-1 text-sm" />
            </label>
          </div>
          <label className="grid gap-1 text-[11px]">
            旋转（度）
            <input type="number" step={90} value={rotation} onChange={(e) => setRotation(e.target.value)} style={inputStyle} className="px-2 py-1 text-sm" />
          </label>
          <div className="flex flex-wrap gap-1">
            {[0, 90, 180, 270].map((deg) => (
              <button key={deg} onClick={() => setRotation(String(deg))} className="px-2 py-1 text-xs" style={buttonStyle}>
                {deg}°
              </button>
            ))}
          </div>
        </>
      )}

      <div className="mt-auto flex items-center justify-between gap-2">
        <button
          onClick={() => void onDelete(item.id)}
          disabled={saving}
          className="px-2 py-1 text-xs"
          style={{ ...buttonStyle, color: 'var(--color-danger)' }}
        >
          删除物品
        </button>
        <button onClick={() => void save()} disabled={saving} className="px-2 py-1 text-xs" style={buttonStyle}>
          {saving ? '保存中…' : '保存'}
        </button>
      </div>
    </aside>
  )
}

const buttonStyle = {
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--color-bg)',
} as const

const inputStyle = {
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--color-bg)',
  color: 'inherit',
} as const
