import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  Clock,
  Code2,
  GitBranch,
  Plus,
  ShieldCheck,
  Sliders,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  createMode,
  updateMode,
  type Capability,
  type Item,
  type ItemType,
  type Mode,
  type ModeCondition,
  type ModeDefinition,
  type ModeGuard,
  type ModeNode,
} from '../../api/client.ts'
import CapabilityForm, { defaultCapabilityValues } from '../../components/CapabilityForm.tsx'
import { Badge } from '../../components/ui/badge.tsx'
import { Button } from '../../components/ui/button.tsx'
import { Input } from '../../components/ui/input.tsx'
import { Label } from '../../components/ui/label.tsx'
import { Switch } from '../../components/ui/switch.tsx'
import { cn } from '../../lib/utils.ts'

const OP_LABELS: { value: ModeCondition['op']; label: string }[] = [
  { value: '==', label: '等于' },
  { value: '!=', label: '不等于' },
  { value: '>', label: '大于' },
  { value: '>=', label: '大于等于' },
  { value: '<', label: '小于' },
  { value: '<=', label: '小于等于' },
]

function genNodeId(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 7)}`
}

function defaultValueForCap(cap: Capability | undefined): unknown {
  if (!cap) return true
  const vals = defaultCapabilityValues([cap])
  return vals[cap.key]
}

type GuardEditorProps = {
  title: string
  hint?: string
  guard?: ModeGuard
  devices: Item[]
  typeMap: Map<string, ItemType>
  onChange: (next?: ModeGuard) => void
}

function GuardEditor({ title, hint, guard, devices, typeMap, onChange }: GuardEditorProps) {
  const mode: 'all' | 'any' =
    guard?.any && guard.any.length > 0 && (!guard.all || guard.all.length === 0) ? 'any' : 'all'
  const list: ModeCondition[] = (mode === 'all' ? guard?.all : guard?.any) ?? []

  const setList = (nextMode: 'all' | 'any', nextList: ModeCondition[]) => {
    if (nextList.length === 0) {
      onChange(undefined)
      return
    }
    onChange(nextMode === 'all' ? { all: nextList } : { any: nextList })
  }

  const addCondition = () => {
    const dev = devices[0]
    const caps = dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []
    const cap = caps[0]
    const next: ModeCondition = {
      itemId: dev?.id ?? '',
      capability: cap?.key ?? '',
      op: '==',
      value: defaultValueForCap(cap),
    }
    setList(mode, [...list, next])
  }

  return (
    <div className="space-y-2.5 rounded-[var(--radius-md)] border border-dashed border-border-strong bg-bg-subtle/50 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <ShieldCheck className="h-4 w-4 text-accent" />
          <span className="text-xs font-semibold text-text-primary">{title}</span>
        </div>
        <div className="flex items-center gap-1.5">
          {list.length > 1 ? (
            <select
              aria-label="条件组合方式"
              value={mode}
              onChange={(e) => setList(e.target.value as 'all' | 'any', list)}
              className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
            >
              <option value="all">同时满足以下全部</option>
              <option value="any">满足以下任意一条</option>
            </select>
          ) : null}
          <Button type="button" variant="outline" size="xs" onClick={addCondition} disabled={devices.length === 0}>
            <Plus className="h-3 w-3" />
            添加条件
          </Button>
        </div>
      </div>

      {hint && list.length === 0 ? <p className="text-[11px] text-text-muted">{hint}</p> : null}

      {list.length > 0 ? (
        <div className="space-y-2">
          {list.map((cond, idx) => {
            const dev = devices.find((d) => d.id === cond.itemId)
            const caps = dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []
            const cap = caps.find((c) => c.key === cond.capability)
            const updateAt = (patch: Partial<ModeCondition>) => {
              const next = list.map((c, i) => (i === idx ? { ...c, ...patch } : c))
              setList(mode, next)
            }

            return (
              <div
                key={idx}
                className="flex flex-wrap items-center gap-1.5 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface p-2 text-xs"
              >
                <span className="text-[11px] text-text-muted">当</span>
                <select
                  aria-label="条件设备"
                  value={cond.itemId}
                  onChange={(e) => {
                    const nextDev = devices.find((d) => d.id === e.target.value)
                    const nextCaps = nextDev ? typeMap.get(nextDev.typeKey)?.capabilities ?? [] : []
                    const firstCap = nextCaps[0]
                    updateAt({
                      itemId: e.target.value,
                      capability: firstCap?.key ?? '',
                      value: defaultValueForCap(firstCap),
                    })
                  }}
                  className="h-7 max-w-[140px] rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
                >
                  {devices.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.roomName ? `${d.roomName} · ${d.name}` : d.name}
                    </option>
                  ))}
                </select>

                <span className="text-[11px] text-text-muted">的</span>
                <select
                  aria-label="条件属性"
                  value={cond.capability}
                  onChange={(e) => {
                    const nextCap = caps.find((c) => c.key === e.target.value)
                    updateAt({ capability: e.target.value, value: defaultValueForCap(nextCap) })
                  }}
                  className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
                >
                  {caps.map((c) => (
                    <option key={c.key} value={c.key}>
                      {c.name}
                    </option>
                  ))}
                </select>

                <select
                  aria-label="比较方式"
                  value={cond.op}
                  onChange={(e) => updateAt({ op: e.target.value as ModeCondition['op'] })}
                  className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
                >
                  {OP_LABELS.map((op) => (
                    <option key={op.value} value={op.value}>
                      {op.label}
                    </option>
                  ))}
                </select>

                {cap?.type === 'bool' ? (
                  <select
                    aria-label="条件目标值"
                    value={String(cond.value === true)}
                    onChange={(e) => updateAt({ value: e.target.value === 'true' })}
                    className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
                  >
                    <option value="true">是 / 开启</option>
                    <option value="false">否 / 关闭</option>
                  </select>
                ) : cap?.type === 'number' ? (
                  <div className="flex items-center gap-1">
                    <Input
                      type="number"
                      step={cap.step ?? 'any'}
                      value={typeof cond.value === 'number' ? cond.value : 0}
                      onChange={(e) => updateAt({ value: Number(e.target.value) })}
                      className="h-7 w-20 text-xs"
                    />
                    {cap.unit ? <span className="text-[11px] text-text-muted">{cap.unit}</span> : null}
                  </div>
                ) : cap?.type === 'enum' && cap.options ? (
                  <select
                    aria-label="条件枚举值"
                    value={String(cond.value ?? '')}
                    onChange={(e) => updateAt({ value: e.target.value })}
                    className="h-7 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2 text-xs text-text-primary outline-none focus:border-accent"
                  >
                    {cap.options.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Input
                    type="text"
                    value={String(cond.value ?? '')}
                    onChange={(e) => updateAt({ value: e.target.value })}
                    className="h-7 w-24 text-xs"
                  />
                )}

                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="ml-auto h-7 w-7 text-status-danger hover:bg-status-danger-soft"
                  onClick={() => setList(mode, list.filter((_, i) => i !== idx))}
                  aria-label="删除条件"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

type StepListEditorProps = {
  steps: ModeNode[]
  devices: Item[]
  typeMap: Map<string, ItemType>
  depth?: number
  onChange: (next: ModeNode[]) => void
}

const STEP_BADGE: Record<ModeNode['type'], { label: string; icon: typeof Sliders }> = {
  action: { label: '控制设备', icon: Sliders },
  delay: { label: '等待', icon: Clock },
  choose: { label: '条件分支', icon: GitBranch },
}

function StepListEditor({ steps, devices, typeMap, depth = 0, onChange }: StepListEditorProps) {
  const writableDevices = useMemo(
    () =>
      devices.filter((d) =>
        (typeMap.get(d.typeKey)?.capabilities ?? []).some((c) => c.writable),
      ),
    [devices, typeMap],
  )

  const groupedWritableDevices = useMemo(() => {
    const map = new Map<string, Item[]>()
    for (const d of writableDevices) {
      const key = d.roomName || '未分配房间'
      const arr = map.get(key)
      if (arr) arr.push(d)
      else map.set(key, [d])
    }
    return Array.from(map.entries())
  }, [writableDevices])

  const addAction = () => {
    const dev = writableDevices[0]
    const caps = (dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []).filter((c) => c.writable)
    const cap = caps[0]
    onChange([
      ...steps,
      {
        nodeId: genNodeId('a'),
        type: 'action',
        alias: dev && cap ? `${dev.name} · ${cap.name}` : '控制设备',
        enabled: true,
        itemId: dev?.id,
        capability: cap?.key,
        value: defaultValueForCap(cap),
      },
    ])
  }

  const addDelay = () => {
    onChange([
      ...steps,
      {
        nodeId: genNodeId('d'),
        type: 'delay',
        alias: '等待 2 秒',
        enabled: true,
        ms: 2000,
      },
    ])
  }

  const addChoose = () => {
    const dev = devices[0]
    const cap = dev ? (typeMap.get(dev.typeKey)?.capabilities ?? [])[0] : undefined
    onChange([
      ...steps,
      {
        nodeId: genNodeId('c'),
        type: 'choose',
        alias: '条件分支',
        enabled: true,
        when:
          dev && cap
            ? { all: [{ itemId: dev.id, capability: cap.key, op: '==', value: defaultValueForCap(cap) }] }
            : undefined,
        then: [],
        else: [],
      },
    ])
  }

  const updateAt = (idx: number, nextNode: ModeNode) => {
    onChange(steps.map((s, i) => (i === idx ? nextNode : s)))
  }

  const moveAt = (idx: number, delta: -1 | 1) => {
    const target = idx + delta
    if (target < 0 || target >= steps.length) return
    const copy = [...steps]
    ;[copy[idx], copy[target]] = [copy[target], copy[idx]]
    onChange(copy)
  }

  const removeAt = (idx: number) => {
    onChange(steps.filter((_, i) => i !== idx))
  }

  return (
    <div className="space-y-2.5">
      {steps.map((node, idx) => {
        const isDisabled = node.enabled === false
        const dev = node.itemId ? devices.find((d) => d.id === node.itemId) : undefined
        const writableCaps = (dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []).filter((c) => c.writable)
        const selectedCap = writableCaps.find((c) => c.key === node.capability)
        const canComfort = node.type === 'action' && writableCaps.some((c) => c.key === 'target_temp')
        const meta = STEP_BADGE[node.type]
        const StepIcon = meta.icon

        return (
          <div
            key={node.nodeId}
            className={cn(
              'space-y-3 rounded-[var(--radius-md)] border border-border-default bg-bg-surface p-3 shadow-[var(--shadow-sm)] transition-opacity',
              isDisabled && 'bg-bg-subtle/60 opacity-65',
            )}
          >
            {/* 卡片头 */}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex min-w-0 flex-1 items-center gap-2">
                <Badge variant={node.type === 'choose' ? 'warning' : node.type === 'delay' ? 'secondary' : 'accent'}>
                  <StepIcon className="h-3 w-3" />
                  <span>
                    #{idx + 1} {meta.label}
                  </span>
                </Badge>
                <Input
                  type="text"
                  value={node.alias ?? ''}
                  placeholder="给这一步起个名字…"
                  onChange={(e) => updateAt(idx, { ...node, alias: e.target.value })}
                  className="h-7 max-w-[200px] text-xs"
                />
              </div>

              <div className="flex items-center gap-1">
                <label className="mr-1 inline-flex cursor-pointer items-center gap-1 text-[11px] text-text-muted">
                  <Switch
                    checked={node.enabled !== false}
                    onCheckedChange={(checked) => updateAt(idx, { ...node, enabled: checked })}
                  />
                  <span>启用</span>
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => moveAt(idx, -1)}
                  disabled={idx === 0}
                  aria-label="上移"
                >
                  <ArrowUp className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => moveAt(idx, 1)}
                  disabled={idx === steps.length - 1}
                  aria-label="下移"
                >
                  <ArrowDown className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-status-danger hover:bg-status-danger-soft"
                  onClick={() => removeAt(idx)}
                  aria-label="删除步骤"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>

            {/* 动作步骤 */}
            {node.type === 'action' ? (
              <div className="space-y-2.5 text-xs">
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  <div className="space-y-1">
                    <Label>目标设备</Label>
                    <select
                      value={node.itemId ?? ''}
                      onChange={(e) => {
                        const nextDev = writableDevices.find((d) => d.id === e.target.value)
                        const nextCaps = (nextDev ? typeMap.get(nextDev.typeKey)?.capabilities ?? [] : []).filter(
                          (c) => c.writable,
                        )
                        const first = nextCaps[0]
                        updateAt(idx, {
                          ...node,
                          itemId: e.target.value,
                          capability: first?.key,
                          value: defaultValueForCap(first),
                          valueFrom: undefined,
                        })
                      }}
                      className="h-8 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 text-xs text-text-primary outline-none focus:border-accent"
                    >
                      {groupedWritableDevices.map(([roomLabel, roomDevices]) => (
                        <optgroup key={roomLabel} label={roomLabel}>
                          {roomDevices.map((d) => (
                            <option key={d.id} value={d.id}>
                              {d.name}
                            </option>
                          ))}
                        </optgroup>
                      ))}
                    </select>
                  </div>

                  {node.valueFrom !== 'comfort' ? (
                    <div className="space-y-1">
                      <Label>要调整的参数</Label>
                      <select
                        value={node.capability ?? ''}
                        onChange={(e) => {
                          const nextCap = writableCaps.find((c) => c.key === e.target.value)
                          updateAt(idx, {
                            ...node,
                            capability: e.target.value,
                            value: defaultValueForCap(nextCap),
                          })
                        }}
                        className="h-8 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 text-xs text-text-primary outline-none focus:border-accent"
                      >
                        {writableCaps.map((c) => (
                          <option key={c.key} value={c.key}>
                            {c.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  ) : null}
                </div>

                {canComfort ? (
                  <div className="flex items-center justify-between rounded-[var(--radius-sm)] border border-accent-border/70 bg-accent-soft/50 px-3 py-2">
                    <div className="flex items-center gap-2">
                      <Sparkles className="h-3.5 w-3.5 text-accent" />
                      <div>
                        <p className="text-xs font-medium text-text-primary">按季节自动计算舒适温度</p>
                        <p className="text-[11px] text-text-muted">
                          执行时结合当前季节、室内外温湿度自动决策目标温度
                        </p>
                      </div>
                    </div>
                    <Switch
                      checked={node.valueFrom === 'comfort'}
                      onCheckedChange={(checked) =>
                        updateAt(idx, {
                          ...node,
                          capability: checked ? 'target_temp' : node.capability || writableCaps[0]?.key,
                          valueFrom: checked ? 'comfort' : undefined,
                          value: checked ? undefined : defaultValueForCap(selectedCap ?? writableCaps[0]),
                        })
                      }
                    />
                  </div>
                ) : null}

                {node.valueFrom !== 'comfort' && selectedCap ? (
                  <CapabilityForm
                    capabilities={[selectedCap]}
                    values={{ [selectedCap.key]: node.value }}
                    onChange={(_k, v) => updateAt(idx, { ...node, value: v })}
                    idPrefix={`mode-${node.nodeId}`}
                  />
                ) : null}
              </div>
            ) : null}

            {/* 等待步骤（面向用户以秒为单位） */}
            {node.type === 'delay' ? (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="text-text-secondary">等待</span>
                <Input
                  type="number"
                  min={0}
                  max={300}
                  step={0.5}
                  value={Math.round(((node.ms ?? 0) / 1000) * 10) / 10}
                  onChange={(e) => {
                    const sec = Math.max(0, Number(e.target.value))
                    updateAt(idx, {
                      ...node,
                      ms: Math.round(sec * 1000),
                      alias: `等待 ${sec} 秒`,
                    })
                  }}
                  className="h-8 w-20 text-xs"
                />
                <span className="text-text-secondary">秒</span>
                <div className="flex items-center gap-1">
                  {[1, 2, 5, 10].map((sec) => (
                    <Button
                      key={sec}
                      type="button"
                      variant={node.ms === sec * 1000 ? 'soft' : 'outline'}
                      size="xs"
                      onClick={() => updateAt(idx, { ...node, ms: sec * 1000, alias: `等待 ${sec} 秒` })}
                    >
                      {sec} 秒
                    </Button>
                  ))}
                </div>
              </div>
            ) : null}

            {/* 条件分支步骤 */}
            {node.type === 'choose' ? (
              <div className="space-y-3">
                <GuardEditor
                  title="如果满足以下条件"
                  hint="添加一个条件，满足时执行「那么」，否则执行「否则」"
                  guard={node.when}
                  devices={devices}
                  typeMap={typeMap}
                  onChange={(nextWhen) => updateAt(idx, { ...node, when: nextWhen })}
                />
                <div className="space-y-2 border-l-2 border-accent/60 pl-3">
                  <p className="text-[11px] font-semibold text-accent">那么执行：</p>
                  <StepListEditor
                    steps={node.then ?? []}
                    devices={devices}
                    typeMap={typeMap}
                    depth={depth + 1}
                    onChange={(nextThen) => updateAt(idx, { ...node, then: nextThen })}
                  />
                </div>
                <div className="space-y-2 border-l-2 border-status-warning/60 pl-3">
                  <p className="text-[11px] font-semibold text-status-warning">否则执行：</p>
                  <StepListEditor
                    steps={node.else ?? []}
                    devices={devices}
                    typeMap={typeMap}
                    depth={depth + 1}
                    onChange={(nextElse) => updateAt(idx, { ...node, else: nextElse })}
                  />
                </div>
              </div>
            ) : null}
          </div>
        )
      })}

      {/* 添加步骤按钮条 */}
      <div className="flex flex-wrap gap-1.5 pt-1">
        <Button
          type="button"
          variant="soft"
          size="xs"
          onClick={addAction}
          disabled={writableDevices.length === 0}
        >
          <Plus className="h-3.5 w-3.5" />
          控制设备
        </Button>
        <Button type="button" variant="outline" size="xs" onClick={addDelay}>
          <Clock className="h-3.5 w-3.5" />
          等待
        </Button>
        {depth < 2 ? (
          <Button
            type="button"
            variant="outline"
            size="xs"
            onClick={addChoose}
            disabled={devices.length === 0}
          >
            <GitBranch className="h-3.5 w-3.5" />
            条件分支
          </Button>
        ) : null}
      </div>
    </div>
  )
}

type ModeEditorProps = {
  initial?: Mode | null
  devices: Item[]
  typeMap: Map<string, ItemType>
  onSaved: (saved: Mode) => void
  onCancel: () => void
}

export default function ModeEditor({ initial, devices, typeMap, onSaved, onCancel }: ModeEditorProps) {
  const [name, setName] = useState(initial?.name ?? '')
  const [runMode, setRunMode] = useState<'restart' | 'single'>(
    initial?.runMode === 'single' ? 'single' : 'restart',
  )
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)
  const [defaultContinue, setDefaultContinue] = useState(
    initial?.definition.defaults?.continueOnError ?? false,
  )
  const [guard, setGuard] = useState<ModeGuard | undefined>(initial?.definition.guard)
  const [steps, setSteps] = useState<ModeNode[]>(initial?.definition.steps ?? [])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [jsonPreviewOpen, setJsonPreviewOpen] = useState(false)

  const draftDefinition: ModeDefinition = useMemo(
    () => ({
      version: 1,
      ...(guard ? { guard } : {}),
      defaults: { continueOnError: defaultContinue },
      steps,
    }),
    [guard, defaultContinue, steps],
  )

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setError('请填写场景名称')
      return
    }
    if (steps.length === 0) {
      setError('请至少添加一个执行步骤')
      return
    }
    setSaving(true)
    setError(null)
    try {
      const saved = initial
        ? await updateMode(initial.id, { name: name.trim(), runMode, enabled, definition: draftDefinition })
        : await createMode({ name: name.trim(), runMode, enabled, definition: draftDefinition })
      toast.success(initial ? `已保存场景「${saved.name}」` : `已创建场景「${saved.name}」`)
      onSaved(saved)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '保存场景失败'
      setError(msg)
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {error ? (
          <div
            role="alert"
            className="rounded-[var(--radius-sm)] border border-status-danger/25 bg-status-danger-soft px-3 py-2 text-xs text-status-danger"
          >
            {error}
          </div>
        ) : null}

        {/* 基本信息 */}
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="mode-name">场景名称</Label>
            <Input
              id="mode-name"
              type="text"
              value={name}
              maxLength={64}
              placeholder="例如：影院模式、午休模式"
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="flex items-center justify-between rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/40 px-3 py-2">
            <div>
              <p className="text-xs font-medium text-text-primary">启用此场景</p>
              <p className="text-[11px] text-text-muted">关闭后将暂时无法被一键触发</p>
            </div>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>
        </div>

        {/* 前提条件 */}
        <GuardEditor
          title="前提条件（可选）"
          hint="未设置前提条件时，点击即可直接执行；设置后不满足条件会自动拦截并说明原因。"
          guard={guard}
          devices={devices}
          typeMap={typeMap}
          onChange={setGuard}
        />

        {/* 执行步骤 */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h4 className="text-xs font-semibold text-text-secondary">执行步骤（按顺序运行）</h4>
            <Badge variant="secondary">{steps.length} 步</Badge>
          </div>
          {steps.length === 0 ? (
            <div className="rounded-[var(--radius-md)] border border-dashed border-border-strong bg-bg-subtle/30 px-4 py-5 text-center text-xs text-text-muted">
              点击下方按钮添加第一步设备控制、等待或条件分支
            </div>
          ) : null}
          <StepListEditor steps={steps} devices={devices} typeMap={typeMap} onChange={setSteps} />
        </div>

        {/* 渐进式披露：高级运行选项 */}
        <div className="rounded-[var(--radius-md)] border border-border-default bg-bg-subtle/30">
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            className="flex w-full cursor-pointer items-center justify-between px-3 py-2.5 text-xs font-medium text-text-secondary hover:text-text-primary"
          >
            <span>高级运行选项</span>
            {advancedOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
          {advancedOpen ? (
            <div className="space-y-3 border-t border-border-default p-3 text-xs">
              <div className="space-y-1">
                <Label htmlFor="mode-concurrency">重复触发时的行为</Label>
                <select
                  id="mode-concurrency"
                  value={runMode}
                  onChange={(e) => setRunMode(e.target.value as 'restart' | 'single')}
                  className="h-8 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 text-xs text-text-primary outline-none focus:border-accent"
                >
                  <option value="restart">重新开始执行（推荐）</option>
                  <option value="single">正在运行时忽略重复触发</option>
                </select>
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-xs font-medium text-text-primary">遇到离线设备时继续执行后续步骤</p>
                  <p className="text-[11px] text-text-muted">关闭时，任意设备失败将中断整条场景</p>
                </div>
                <Switch checked={defaultContinue} onCheckedChange={setDefaultContinue} />
              </div>
            </div>
          ) : null}
        </div>

        {/* 渐进式披露：只读 JSON 预览 */}
        <div className="rounded-[var(--radius-md)] border border-border-default bg-bg-subtle/30">
          <button
            type="button"
            onClick={() => setJsonPreviewOpen((v) => !v)}
            className="flex w-full cursor-pointer items-center justify-between px-3 py-2 text-xs font-medium text-text-muted hover:text-text-primary"
          >
            <span className="inline-flex items-center gap-1.5">
              <Code2 className="h-3.5 w-3.5" />
              只读结构预览（排错用）
            </span>
            {jsonPreviewOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
          {jsonPreviewOpen ? (
            <pre className="max-h-48 overflow-auto border-t border-border-default bg-bg-canvas p-3 font-mono text-[11px] text-text-secondary">
              {JSON.stringify(draftDefinition, null, 2)}
            </pre>
          ) : null}
        </div>
      </div>

      {/* 吸底保存条 */}
      <footer className="flex items-center justify-end gap-2 border-t border-border-default bg-bg-surface px-5 py-3">
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" variant="default" size="sm" disabled={saving}>
          {saving ? '保存中…' : initial ? '保存修改' : '创建场景'}
        </Button>
      </footer>
    </form>
  )
}
