import { useEffect, useMemo, useState } from 'react'
import { listItemStates, sendItemCommand, type Item, type ItemType, type Room } from '../api/client.ts'
import { isCapabilityStale, liveValuesFor, mergeControlValues } from '../features/control/live.ts'
import { useLiveStore } from '../store/liveState.ts'
import CapabilityForm, { type CapabilityValues } from './CapabilityForm.tsx'

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
  const [capabilityValues, setCapabilityValues] = useState<CapabilityValues>({})
  const [draftValues, setDraftValues] = useState<CapabilityValues>({})
  const [controlState, setControlState] = useState<'idle' | 'sending' | 'ok' | 'error'>('idle')
  const [stateUpdatedAt, setStateUpdatedAt] = useState<Record<string, string>>({})
  const [nowMs, setNowMs] = useState(() => Date.now())
  const liveStates = useLiveStore((store) => store.states)
  const liveConnected = useLiveStore((store) => store.connected)

  // 只读传感器每 5 秒刷新一次当前时间，避免停报超过 60 秒后页面不重渲染而一直显示旧值。
  const hasReadOnly = Boolean(type?.capabilities.some((capability) => !capability.writable))
  useEffect(() => {
    if (!hasReadOnly) return
    setNowMs(Date.now())
    const timer = window.setInterval(() => setNowMs(Date.now()), 5000)
    return () => window.clearInterval(timer)
  }, [hasReadOnly, item?.id])

  // 推送来的值覆盖一次性拉取的结果，用户尚未下发的本地编辑草稿优先级最高。
  const liveValues = useMemo(
    () => (item ? liveValuesFor(liveStates, item.id, type, nowMs) : {}),
    [liveStates, item, type, nowMs],
  )
  const mergedValues = useMemo<CapabilityValues>(
    () => mergeControlValues(capabilityValues, liveValues, draftValues),
    [capabilityValues, liveValues, draftValues],
  )
  // 陈旧判定优先用推送带来的时间，没收到推送时退回一次性拉取的时间。
  const updatedAtOf = (key: string) => liveStates[`${item?.id}:${key}`]?.ts ?? stateUpdatedAt[key]

  useEffect(() => {
    if (!item) return
    setName(item.name)
    setRoomHash(item.roomHash ?? '')
    setX(String(item.x))
    setY(String(item.y))
    setRotation(String(item.rotation))
    setWallOffset(String(item.wallOffset ?? 0))
    setError(null)
    setControlState('idle')
    setDraftValues({})
    if (item.kind === 'device') {
      void listItemStates(item.id)
        .then((states) => {
          setCapabilityValues(Object.fromEntries(states.map((state) => [state.capabilityKey, state.value])))
          setStateUpdatedAt(Object.fromEntries(states.map((state) => [state.capabilityKey, state.updatedAt])))
        })
        .catch((e) => setError(e instanceof Error ? e.message : '读取设备状态失败'))
    } else {
      setCapabilityValues({})
    }
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
      {!item.wallId && type?.mount === 'wall' ? (
        <p className="text-[11px]" style={{ color: '#b45309' }}>
          这件「{type.name}」还没挂到墙上：把它拖到某面墙边（40 像素内）就会贴上去，
          位置和朝向之后都跟着墙走。
        </p>
      ) : item.wallId ? (
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

      {item.kind === 'device' && type ? (
        <section className="grid gap-2 border-t pt-3" style={{ borderColor: 'var(--color-border)' }}>
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold">设备控制</h3>
            <span className="text-[11px]" style={{ color: liveConnected ? 'var(--color-success, #15803d)' : 'var(--color-muted)' }}>
              {liveConnected ? '实时' : '重连中'}
            </span>
          </div>
          {type.capabilities.some((capability) => isCapabilityStale(capability, updatedAtOf(capability.key), nowMs)) ? (
            <p className="text-xs" style={{ color: 'var(--color-danger)' }} role="status">传感器读数不可用：超过更新时间窗口</p>
          ) : null}
          <CapabilityForm
            capabilities={type.capabilities}
            values={Object.fromEntries(
              type.capabilities.map((capability) => {
                const stale = isCapabilityStale(capability, updatedAtOf(capability.key), nowMs)
                return [capability.key, stale ? undefined : mergedValues[capability.key]]
              }),
            )}
            idPrefix={`control-${item.id}`}
            onChange={(key, value) => setDraftValues((current) => ({ ...current, [key]: value }))}
            disabled={controlState === 'sending'}
          />
          {controlState === 'ok' ? (
            <p className="text-xs" style={{ color: 'var(--color-success, #15803d)' }} role="status">下发成功，状态已落库</p>
          ) : null}
          {controlState === 'error' ? (
            <p className="text-xs" style={{ color: 'var(--color-danger)' }} role="alert">{error}</p>
          ) : null}
          <div className="flex flex-wrap gap-1">
            {type.capabilities.filter((capability) => capability.writable).map((capability) => (
              <button
                key={capability.key}
                disabled={controlState === 'sending' || mergedValues[capability.key] === undefined}
                onClick={async () => {
                  setControlState('sending')
                  setError(null)
                  try {
                    const requestId = crypto.randomUUID()
                    await sendItemCommand(item.id, {
                      capabilityKey: capability.key,
                      value: mergedValues[capability.key],
                      requestId,
                    })
                    setDraftValues((current) => {
                      const next = { ...current }
                      delete next[capability.key]
                      return next
                    })
                    const states = await listItemStates(item.id)
                    setCapabilityValues(Object.fromEntries(states.map((state) => [state.capabilityKey, state.value])))
                    setStateUpdatedAt(Object.fromEntries(states.map((state) => [state.capabilityKey, state.updatedAt])))
                    setControlState('ok')
                  } catch (e) {
                    setError(e instanceof Error ? e.message : '设备命令执行失败')
                    setControlState('error')
                  }
                }}
                className="px-2 py-1 text-xs"
                style={buttonStyle}
              >
                下发{capability.name}
              </button>
            ))}
          </div>
        </section>
      ) : null}

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
