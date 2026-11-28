import { AlertTriangle, ChevronDown, ChevronRight, MousePointerClick, Send, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { listItemStates, sendItemCommand, type Item, type ItemType, type Room } from '../api/client.ts'
import { isCapabilityStale, liveValuesFor, mergeControlValues } from '../features/control/live.ts'
import { useNow } from '../features/control/useNow.ts'
import { ItemIcon } from '../lib/icons.tsx'
import { useLiveStore } from '../store/liveState.ts'
import CapabilityForm, { type CapabilityValues } from './CapabilityForm.tsx'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from './ui/alert-dialog.tsx'
import { Badge } from './ui/badge.tsx'
import { Button } from './ui/button.tsx'
import { Input } from './ui/input.tsx'
import { Label } from './ui/label.tsx'
import type { WorkspaceMode } from './workspace/AppHeader.tsx'
import HistoryChart from './workspace/HistoryChart.tsx'

type Props = {
  item: Item | null
  type?: ItemType
  rooms: Room[]
  mode?: WorkspaceMode
  isAdmin?: boolean
  onUpdate: (
    id: string,
    patch: {
      name?: string
      roomHash?: string
      x?: number
      y?: number
      rotation?: number
      wallId?: string
      wallOffset?: number
    },
  ) => Promise<void>
  onDelete: (id: string) => Promise<void>
}

/** 选中物品后的详情与控制面板（嵌入右侧栏「设备详情」Tab）。 */
export default function ItemInspector({
  item,
  type,
  rooms,
  mode = 'control',
  isAdmin = true,
  onUpdate,
  onDelete,
}: Props) {
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
  const [propsOpen, setPropsOpen] = useState(mode === 'edit')
  const nowMs = useNow()

  const liveStates = useLiveStore((store) => store.states)
  const liveConnected = useLiveStore((store) => store.connected)

  useEffect(() => {
    setPropsOpen(mode === 'edit' || item?.kind === 'furniture')
  }, [mode, item?.kind])

  const liveValues = useMemo(
    () => (item ? liveValuesFor(liveStates, item.id, type, nowMs) : {}),
    [liveStates, item, type, nowMs],
  )
  const mergedValues = useMemo<CapabilityValues>(
    () => mergeControlValues(capabilityValues, liveValues, draftValues),
    [capabilityValues, liveValues, draftValues],
  )
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
      <div className="flex h-full flex-col items-center justify-center p-6 text-center">
        <div className="mb-3 flex h-11 w-11 items-center justify-center rounded-full bg-bg-subtle text-text-muted">
          <MousePointerClick className="h-5 w-5" />
        </div>
        <p className="text-sm font-medium text-text-primary">未选中设备或物品</p>
        <p className="mt-1 text-xs leading-relaxed text-text-muted">
          点击户型画布或左侧列表中的任意设备，即可在这里实时控制参数或调整位置。
        </p>
      </div>
    )
  }

  const namedRooms = rooms.filter((room) => room.name)
  const writableCaps = (type?.capabilities ?? []).filter((c) => c.writable)
  const hasStale = Boolean(
    type?.capabilities.some((capability) => isCapabilityStale(capability, updatedAtOf(capability.key), nowMs)),
  )
  const latestUpdatedAt = useMemo(() => {
    if (!item || !type) return null
    let maxTs = 0
    for (const c of type.capabilities) {
      const ts = updatedAtOf(c.key)
      if (!ts) continue
      const ms = new Date(ts).getTime()
      if (Number.isFinite(ms) && ms > maxTs) maxTs = ms
    }
    return maxTs > 0 ? new Date(maxTs).toLocaleTimeString('zh-CN') : null
  }, [item, type, liveStates, stateUpdatedAt])

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
      if (item.wallId) {
        const nOffset = Number(wallOffset)
        if (!Number.isFinite(nOffset) || nOffset < 0) {
          setError('沿墙偏移必须是不小于 0 的数字')
          return
        }
        await onUpdate(item.id, {
          name: name.trim(),
          roomHash,
          wallId: item.wallId,
          wallOffset: nOffset,
        })
      } else {
        await onUpdate(item.id, {
          name: name.trim(),
          roomHash,
          x: nx,
          y: ny,
          rotation: nr,
        })
      }
      toast.success(`已保存「${name.trim()}」属性`)
    } catch (e) {
      const msg = e instanceof Error ? e.message : '保存失败'
      setError(msg)
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  async function dispatchCapability(capabilityKey: string, capabilityName: string, nextValue?: unknown) {
    if (!item) return
    const val = nextValue !== undefined ? nextValue : mergedValues[capabilityKey]
    if (val === undefined) return
    setControlState('sending')
    setError(null)
    try {
      await sendItemCommand(item.id, {
        capabilityKey,
        value: val,
        requestId: crypto.randomUUID(),
      })
      setDraftValues((current) => {
        const next = { ...current }
        delete next[capabilityKey]
        return next
      })
      const states = await listItemStates(item.id)
      setCapabilityValues(Object.fromEntries(states.map((state) => [state.capabilityKey, state.value])))
      setStateUpdatedAt(Object.fromEntries(states.map((state) => [state.capabilityKey, state.updatedAt])))
      setControlState('ok')
      toast.success(`${item.name} · ${capabilityName} 已更新`)
    } catch (e) {
      const msg = e instanceof Error ? e.message : '设备命令执行失败'
      setError(msg)
      setControlState('error')
      toast.error(msg)
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-3.5">
      {/* 头部摘要 */}
      <div className="flex items-start gap-2.5">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-accent-soft text-accent">
          <ItemIcon iconName={type?.icons} typeKey={item.typeKey} kind={item.kind} className="h-4.5 w-4.5" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-text-primary">{item.name}</h2>
          <div className="mt-1 flex flex-wrap items-center gap-1">
            <Badge variant="secondary">{type?.name ?? item.typeKey}</Badge>
            {item.roomName ? <Badge variant="outline">{item.roomName}</Badge> : null}
            {item.kind === 'device' ? (
              <Badge variant={liveConnected ? 'success' : 'warning'}>
                {liveConnected ? '实时' : '重连中'}
              </Badge>
            ) : null}
          </div>
        </div>
      </div>

      {error ? (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-[var(--radius-sm)] border border-status-danger/25 bg-status-danger-soft px-3 py-2 text-xs text-status-danger"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{error}</span>
        </div>
      ) : null}

      {/* 设备控制区（运行模式下放在最顶部） */}
      {item.kind === 'device' && type ? (
        <section className="space-y-2.5">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-xs font-semibold text-text-secondary">实时控制与状态</h3>
            <div className="flex items-center gap-1.5">
              {latestUpdatedAt ? (
                <span className="font-mono text-[10px] text-text-muted">更新于 {latestUpdatedAt}</span>
              ) : null}
              {Object.keys(draftValues).length > 0 ? (
                <Badge variant="warning">未下发</Badge>
              ) : null}
            </div>
          </div>

          {hasStale ? (
            <div
              role="status"
              className="flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-status-warning/30 bg-status-warning-soft px-2.5 py-1.5 text-[11px] text-status-warning"
            >
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              传感器读数超过更新时间窗口，当前不可用
            </div>
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
            onChange={(key, value) => {
              setDraftValues((current) => ({ ...current, [key]: value }))
              // 布尔开关直接即时下发，无需二次点击“下发”按钮
              const cap = type.capabilities.find((c) => c.key === key)
              if (cap?.type === 'bool' && cap.writable) {
                void dispatchCapability(key, cap.name, value)
              }
            }}
            disabled={controlState === 'sending'}
          />

          {writableCaps.filter((c) => c.type !== 'bool').length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {writableCaps
                .filter((c) => c.type !== 'bool')
                .map((capability) => (
                  <Button
                    key={capability.key}
                    variant={Object.prototype.hasOwnProperty.call(draftValues, capability.key) ? 'default' : 'soft'}
                    size="xs"
                    disabled={controlState === 'sending' || mergedValues[capability.key] === undefined}
                    onClick={() => void dispatchCapability(capability.key, capability.name)}
                  >
                    <Send className="h-3 w-3" />
                    应用{capability.name}
                  </Button>
                ))}
            </div>
          ) : null}

          <HistoryChart itemId={item.id} capabilities={type.capabilities} />
        </section>
      ) : null}

      {/* 几何与基础属性（编辑模式默认展开，运行模式默认折叠；仅管理员可改） */}
      {isAdmin ? (
        <section className="rounded-[var(--radius-md)] border border-border-default bg-bg-subtle/40">
          <button
            type="button"
            onClick={() => setPropsOpen((v) => !v)}
            className="flex w-full cursor-pointer items-center justify-between px-3 py-2 text-xs font-medium text-text-secondary hover:text-text-primary"
          >
            <span>位置与基础属性</span>
            {propsOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>

          {propsOpen ? (
            <div className="space-y-2.5 border-t border-border-default p-3">
              <div className="space-y-1">
                <Label htmlFor="insp-name">名称</Label>
                <Input
                  id="insp-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="h-8 text-xs"
                />
              </div>

              <div className="space-y-1">
                <Label htmlFor="insp-room">所属房间</Label>
                <select
                  id="insp-room"
                  value={roomHash}
                  onChange={(e) => setRoomHash(e.target.value)}
                  className="h-8 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 text-xs text-text-primary outline-none focus:border-accent"
                >
                  <option value="">未分配房间</option>
                  {namedRooms.map((room) => (
                    <option key={room.hash} value={room.hash}>
                      {room.name}
                    </option>
                  ))}
                </select>
              </div>

              {!item.wallId && type?.mount === 'wall' ? (
                <p className="rounded-[var(--radius-sm)] bg-status-warning-soft px-2.5 py-1.5 text-[11px] text-status-warning">
                  这件「{type.name}」尚未挂墙：拖到墙边（40px 内）会自动贴合。
                </p>
              ) : item.wallId ? (
                <div className="space-y-1">
                  <Label htmlFor="insp-wall-offset">沿墙偏移（cm）</Label>
                  <Input
                    id="insp-wall-offset"
                    type="number"
                    min={0}
                    value={wallOffset}
                    onChange={(e) => setWallOffset(e.target.value)}
                    className="h-8 text-xs"
                  />
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-2">
                    <div className="space-y-1">
                      <Label htmlFor="insp-x">X（cm）</Label>
                      <Input
                        id="insp-x"
                        type="number"
                        value={x}
                        onChange={(e) => setX(e.target.value)}
                        className="h-8 text-xs"
                      />
                    </div>
                    <div className="space-y-1">
                      <Label htmlFor="insp-y">Y（cm）</Label>
                      <Input
                        id="insp-y"
                        type="number"
                        value={y}
                        onChange={(e) => setY(e.target.value)}
                        className="h-8 text-xs"
                      />
                    </div>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="insp-rot">旋转角度</Label>
                    <div className="flex items-center gap-1.5">
                      <Input
                        id="insp-rot"
                        type="number"
                        step={90}
                        value={rotation}
                        onChange={(e) => setRotation(e.target.value)}
                        className="h-8 w-20 text-xs"
                      />
                      <div className="flex flex-1 gap-1">
                        {[0, 90, 180, 270].map((deg) => (
                          <button
                            key={deg}
                            type="button"
                            onClick={() => setRotation(String(deg))}
                            className="flex-1 cursor-pointer rounded-[var(--radius-sm)] border border-border-default bg-bg-surface py-1 text-[11px] text-text-secondary hover:bg-bg-subtle"
                          >
                            {deg}°
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                </>
              )}

              <div className="flex items-center justify-between gap-2 pt-1">
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="ghost" size="xs" disabled={saving} className="text-status-danger hover:bg-status-danger-soft">
                      <Trash2 className="h-3.5 w-3.5" />
                      删除
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>删除「{item.name}」？</AlertDialogTitle>
                      <AlertDialogDescription>
                        删除后该物品将从户型图中移除；若场景模式引用了此设备，后续运行该步骤会自动跳过。
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>取消</AlertDialogCancel>
                      <AlertDialogAction onClick={() => void onDelete(item.id)}>
                        确认删除
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>

                <Button variant="default" size="xs" disabled={saving} onClick={() => void save()}>
                  {saving ? '保存中…' : '保存属性'}
                </Button>
              </div>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  )
}
