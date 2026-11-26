import { useMemo, useState } from 'react'
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

const OPS: ModeCondition['op'][] = ['==', '!=', '>', '>=', '<', '<=']

function genNodeId(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 7)}`
}

function defaultValueForCap(cap: Capability | undefined): unknown {
  if (!cap) return true
  const vals = defaultCapabilityValues([cap])
  return vals[cap.key]
}

type GuardEditorProps = {
  label: string
  guard?: ModeGuard
  devices: Item[]
  typeMap: Map<string, ItemType>
  onChange: (next?: ModeGuard) => void
}

function GuardEditor({ label, guard, devices, typeMap, onChange }: GuardEditorProps) {
  const mode: 'all' | 'any' = guard?.any && guard.any.length > 0 && (!guard.all || guard.all.length === 0) ? 'any' : 'all'
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
    <div
      className="space-y-2 p-2.5"
      style={{
        border: '1px dashed var(--color-border)',
        borderRadius: 'var(--radius-sm)',
        background: 'var(--color-bg)',
      }}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">{label}</span>
        <div className="flex items-center gap-1.5">
          {list.length > 0 ? (
            <select
              value={mode}
              onChange={(e) => setList(e.target.value as 'all' | 'any', list)}
              className="px-1.5 py-0.5 text-xs"
              style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
            >
              <option value="all">满足全部 (all)</option>
              <option value="any">满足任一 (any)</option>
            </select>
          ) : null}
          <button
            type="button"
            onClick={addCondition}
            disabled={devices.length === 0}
            className="px-2 py-0.5 text-xs"
            style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
          >
            + 添加条件
          </button>
        </div>
      </div>

      {list.length === 0 ? (
        <p className="text-[11px]" style={{ color: 'var(--color-muted)' }}>
          未设置条件（默认放行）
        </p>
      ) : (
        <div className="space-y-1.5">
          {list.map((cond, idx) => {
            const dev = devices.find((d) => d.id === cond.itemId)
            const caps = dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []
            const cap = caps.find((c) => c.key === cond.capability)
            const updateAt = (patch: Partial<ModeCondition>) => {
              const next = list.map((c, i) => (i === idx ? { ...c, ...patch } : c))
              setList(mode, next)
            }
            return (
              <div key={idx} className="flex flex-wrap items-center gap-1.5 text-xs">
                <select
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
                  className="px-1.5 py-1 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                >
                  {devices.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>

                <select
                  value={cond.capability}
                  onChange={(e) => {
                    const nextCap = caps.find((c) => c.key === e.target.value)
                    updateAt({ capability: e.target.value, value: defaultValueForCap(nextCap) })
                  }}
                  className="px-1.5 py-1 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                >
                  {caps.map((c) => (
                    <option key={c.key} value={c.key}>
                      {c.name} ({c.key})
                    </option>
                  ))}
                </select>

                <select
                  value={cond.op}
                  onChange={(e) => updateAt({ op: e.target.value as ModeCondition['op'] })}
                  className="px-1.5 py-1 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                >
                  {OPS.map((op) => (
                    <option key={op} value={op}>
                      {op}
                    </option>
                  ))}
                </select>

                {cap?.type === 'bool' ? (
                  <select
                    value={String(cond.value === true)}
                    onChange={(e) => updateAt({ value: e.target.value === 'true' })}
                    className="px-1.5 py-1 text-xs"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                  >
                    <option value="true">是 (true)</option>
                    <option value="false">否 (false)</option>
                  </select>
                ) : cap?.type === 'number' ? (
                  <input
                    type="number"
                    step={cap.step ?? 'any'}
                    value={typeof cond.value === 'number' ? cond.value : 0}
                    onChange={(e) => updateAt({ value: Number(e.target.value) })}
                    className="w-20 px-1.5 py-1 text-xs"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                  />
                ) : cap?.type === 'enum' && cap.options ? (
                  <select
                    value={String(cond.value ?? '')}
                    onChange={(e) => updateAt({ value: e.target.value })}
                    className="px-1.5 py-1 text-xs"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                  >
                    {cap.options.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type="text"
                    value={String(cond.value ?? '')}
                    onChange={(e) => updateAt({ value: e.target.value })}
                    className="w-24 px-1.5 py-1 text-xs"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                  />
                )}

                <button
                  type="button"
                  onClick={() => setList(mode, list.filter((_, i) => i !== idx))}
                  className="px-1.5 py-0.5 text-xs"
                  style={{ color: '#dc2626' }}
                >
                  删
                </button>
              </div>
            )
          })}
        </div>
      )}
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

function StepListEditor({ steps, devices, typeMap, depth = 0, onChange }: StepListEditorProps) {
  const writableDevices = useMemo(
    () =>
      devices.filter((d) =>
        (typeMap.get(d.typeKey)?.capabilities ?? []).some((c) => c.writable),
      ),
    [devices, typeMap],
  )

  const addAction = () => {
    const dev = writableDevices[0]
    const caps = (dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []).filter((c) => c.writable)
    const cap = caps[0]
    onChange([
      ...steps,
      {
        nodeId: genNodeId('a'),
        type: 'action',
        alias: dev && cap ? `${dev.name}·${cap.name}` : '设备动作',
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
        when: dev && cap ? { all: [{ itemId: dev.id, capability: cap.key, op: '==', value: defaultValueForCap(cap) }] } : undefined,
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
    <div className="space-y-2">
      {steps.map((node, idx) => {
        const isDisabled = node.enabled === false
        const dev = node.itemId ? devices.find((d) => d.id === node.itemId) : undefined
        const writableCaps = (dev ? typeMap.get(dev.typeKey)?.capabilities ?? [] : []).filter((c) => c.writable)
        const selectedCap = writableCaps.find((c) => c.key === node.capability)
        const canComfort = node.type === 'action' && dev?.typeKey === 'ac'

        return (
          <div
            key={node.nodeId}
            className="space-y-2 p-2.5"
            style={{
              marginLeft: `${depth * 14}px`,
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: isDisabled ? '#f9fafb' : 'var(--color-surface, #fff)',
              opacity: isDisabled ? 0.72 : 1,
            }}
          >
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] font-mono" style={{ color: 'var(--color-muted)' }}>
                  #{idx + 1} [{node.type}]
                </span>
                <input
                  type="text"
                  value={node.alias ?? ''}
                  placeholder="步骤别名"
                  onChange={(e) => updateAt(idx, { ...node, alias: e.target.value })}
                  className="w-40 px-1.5 py-0.5 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: '4px' }}
                />
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <label className="flex items-center gap-1">
                  <input
                    type="checkbox"
                    checked={node.enabled !== false}
                    onChange={(e) => updateAt(idx, { ...node, enabled: e.target.checked })}
                  />
                  启用
                </label>
                <label className="flex items-center gap-1" title="失败后继续执行后续步骤">
                  <input
                    type="checkbox"
                    checked={node.continueOnError === true}
                    onChange={(e) => updateAt(idx, { ...node, continueOnError: e.target.checked })}
                  />
                  失败继续
                </label>
                <button type="button" onClick={() => moveAt(idx, -1)} disabled={idx === 0} className="px-1 disabled:opacity-40">
                  ↑
                </button>
                <button
                  type="button"
                  onClick={() => moveAt(idx, 1)}
                  disabled={idx === steps.length - 1}
                  className="px-1 disabled:opacity-40"
                >
                  ↓
                </button>
                <button type="button" onClick={() => removeAt(idx)} style={{ color: '#dc2626' }} className="px-1">
                  删
                </button>
              </div>
            </div>

            {node.type === 'action' ? (
              <div className="space-y-2 text-xs">
                <div className="flex flex-wrap items-center gap-2">
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
                    className="px-2 py-1 text-xs"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                  >
                    {writableDevices.map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.name}
                      </option>
                    ))}
                  </select>

                  {canComfort ? (
                    <label className="flex items-center gap-1">
                      <input
                        type="checkbox"
                        checked={node.valueFrom === 'comfort'}
                        onChange={(e) =>
                          updateAt(idx, {
                            ...node,
                            capability: e.target.checked ? 'target_temp' : node.capability || writableCaps[0]?.key,
                            valueFrom: e.target.checked ? 'comfort' : undefined,
                            value: e.target.checked ? undefined : defaultValueForCap(selectedCap ?? writableCaps[0]),
                          })
                        }
                      />
                      动态舒适温控（valueFrom: comfort）
                    </label>
                  ) : null}
                </div>

                {node.valueFrom === 'comfort' ? (
                  <p className="text-[11px]" style={{ color: '#1e40af' }}>
                    执行时由策略层按当前季节、室温与湿度动态计算目标温度并附带调节依据。
                  </p>
                ) : (
                  <div className="space-y-2">
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
                      className="px-2 py-1 text-xs"
                      style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
                    >
                      {writableCaps.map((c) => (
                        <option key={c.key} value={c.key}>
                          {c.name} ({c.key})
                        </option>
                      ))}
                    </select>
                    {selectedCap ? (
                      <div className="max-w-xs">
                        <CapabilityForm
                          capabilities={[selectedCap]}
                          values={{ [selectedCap.key]: node.value }}
                          onChange={(_k, v) => updateAt(idx, { ...node, value: v })}
                          idPrefix={`mode-${node.nodeId}`}
                        />
                      </div>
                    ) : null}
                  </div>
                )}
              </div>
            ) : null}

            {node.type === 'delay' ? (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span>延时（毫秒）：</span>
                <input
                  type="number"
                  min={0}
                  max={300000}
                  step={500}
                  value={node.ms ?? 0}
                  onChange={(e) => updateAt(idx, { ...node, ms: Math.max(0, Number(e.target.value)) })}
                  className="w-24 px-2 py-1 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: '4px' }}
                />
                {[1000, 2000, 5000].map((ms) => (
                  <button
                    key={ms}
                    type="button"
                    onClick={() => updateAt(idx, { ...node, ms })}
                    className="px-1.5 py-0.5 text-[11px]"
                    style={{ border: '1px solid var(--color-border)', borderRadius: '4px' }}
                  >
                    {ms / 1000}s
                  </button>
                ))}
              </div>
            ) : null}

            {node.type === 'choose' ? (
              <div className="space-y-2">
                <GuardEditor
                  label="分支条件 (when)"
                  guard={node.when}
                  devices={devices}
                  typeMap={typeMap}
                  onChange={(nextWhen) => updateAt(idx, { ...node, when: nextWhen })}
                />
                <div className="pl-2" style={{ borderLeft: '2px solid #93c5fd' }}>
                  <p className="mb-1 text-[11px] font-medium" style={{ color: '#1d4ed8' }}>
                    条件成立执行 (then)：
                  </p>
                  <StepListEditor
                    steps={node.then ?? []}
                    devices={devices}
                    typeMap={typeMap}
                    depth={depth + 1}
                    onChange={(nextThen) => updateAt(idx, { ...node, then: nextThen })}
                  />
                </div>
                <div className="pl-2" style={{ borderLeft: '2px solid #fcd34d' }}>
                  <p className="mb-1 text-[11px] font-medium" style={{ color: '#92400e' }}>
                    否则执行 (else)：
                  </p>
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

      <div className="flex flex-wrap gap-1.5 pt-1" style={{ marginLeft: `${depth * 14}px` }}>
        <button
          type="button"
          onClick={addAction}
          disabled={writableDevices.length === 0}
          className="px-2 py-1 text-xs"
          style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
        >
          + 动作 (action)
        </button>
        <button
          type="button"
          onClick={addDelay}
          className="px-2 py-1 text-xs"
          style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
        >
          + 延时 (delay)
        </button>
        {depth < 2 ? (
          <button
            type="button"
            onClick={addChoose}
            disabled={devices.length === 0}
            className="px-2 py-1 text-xs"
            style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
          >
            + 条件分支 (choose)
          </button>
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
    const definition: ModeDefinition = {
      version: 1,
      ...(guard ? { guard } : {}),
      defaults: { continueOnError: defaultContinue },
      steps,
    }
    try {
      const saved = initial
        ? await updateMode(initial.id, { name: name.trim(), runMode, enabled, definition })
        : await createMode({ name: name.trim(), runMode, enabled, definition })
      onSaved(saved)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : '保存场景失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">{initial ? `编辑场景 · ${initial.name}` : '新建自定义场景'}</h3>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-2.5 py-1 text-xs"
            style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
          >
            取消
          </button>
          <button
            type="submit"
            disabled={saving}
            className="px-3 py-1 text-xs font-medium text-white disabled:opacity-50"
            style={{ background: 'var(--color-primary, #4f6bed)', borderRadius: 'var(--radius-sm)' }}
          >
            {saving ? '保存中…' : '保存场景'}
          </button>
        </div>
      </div>

      {error ? (
        <div className="px-3 py-2 text-xs" style={{ background: '#fee2e2', color: '#991b1b', borderRadius: '4px' }}>
          {error}
        </div>
      ) : null}

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <label className="grid gap-1 text-xs">
          <span>场景名称</span>
          <input
            type="text"
            value={name}
            maxLength={64}
            placeholder="例如：影院模式"
            onChange={(e) => setName(e.target.value)}
            className="px-2 py-1 text-xs"
            style={{ border: '1px solid var(--color-border)', borderRadius: '4px' }}
          />
        </label>
        <label className="grid gap-1 text-xs">
          <span>重复触发策略 (runMode)</span>
          <select
            value={runMode}
            onChange={(e) => setRunMode(e.target.value as 'restart' | 'single')}
            className="px-2 py-1 text-xs"
            style={{ border: '1px solid var(--color-border)', borderRadius: '4px', background: '#fff' }}
          >
            <option value="restart">重跑 (restart，默认)</option>
            <option value="single">忽略重复 (single)</option>
          </select>
        </label>
      </div>

      <div className="flex flex-wrap items-center gap-4 text-xs">
        <label className="flex items-center gap-1.5">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          启用该场景
        </label>
        <label className="flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={defaultContinue}
            onChange={(e) => setDefaultContinue(e.target.checked)}
          />
          默认失败后继续执行（defaults.continueOnError）
        </label>
      </div>

      <GuardEditor
        label="模式级顶层守卫 (guard，不满足时不执行并说明原因)"
        guard={guard}
        devices={devices}
        typeMap={typeMap}
        onChange={setGuard}
      />

      <div>
        <h4 className="mb-1.5 text-xs font-medium">步骤序列 (steps)</h4>
        <StepListEditor steps={steps} devices={devices} typeMap={typeMap} onChange={setSteps} />
      </div>
    </form>
  )
}
