import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  ApiError,
  createItemType,
  deleteItemType,
  listItemTypes,
  updateItemType,
  type Capability,
  type CapabilityOption,
  type CapabilityType,
  type ItemType,
} from '../api/client.ts'
import CapabilityForm, {
  defaultCapabilityValues,
  type CapabilityValues,
} from './CapabilityForm.tsx'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from './ui/alert-dialog.tsx'

type DraftCapability = Capability & {
  /** 编辑既有类型时保存的原始定义；新建的能力没有它。 */
  original?: Capability
}

type Draft = {
  key: string
  kind: 'furniture' | 'device'
  name: string
  icons: string
  defaultWidth: string
  defaultDepth: string
  driver: string
  capabilities: DraftCapability[]
}

function emptyDraft(): Draft {
  return {
    key: '',
    kind: 'device',
    name: '',
    icons: '',
    defaultWidth: '60',
    defaultDepth: '60',
    driver: 'simulator',
    capabilities: [],
  }
}

function draftFromType(typ: ItemType): Draft {
  return {
    key: typ.key,
    kind: typ.kind,
    name: typ.name,
    icons: typ.icons ?? '',
    defaultWidth: String(typ.defaultWidth),
    defaultDepth: String(typ.defaultDepth),
    driver: typ.driver ?? '',
    capabilities: typ.capabilities.map((capability) => ({ ...capability, original: capability })),
  }
}

function newCapability(): DraftCapability {
  return {
    key: '',
    name: '',
    type: 'bool',
    writable: true,
    history: true,
    default: false,
  }
}

function emptyValueFor(type: CapabilityType): unknown {
  switch (type) {
    case 'bool':
      return false
    case 'number':
      return 0
    case 'enum':
      return ''
    case 'string':
      return ''
  }
}

function cleanCapability(capability: DraftCapability): Capability {
  const out: Capability = {
    key: capability.key.trim(),
    name: capability.name.trim(),
    type: capability.type,
    writable: capability.writable,
    history: capability.history,
  }
  if (capability.unit?.trim()) out.unit = capability.unit.trim()
  // 枚举的“（无）默认值”在 UI 里是空字符串；不要把它当成一个非法选项提交。
  if (capability.default !== undefined && !(capability.type === 'enum' && capability.default === '')) {
    out.default = capability.default
  }
  if (capability.type === 'number') {
    if (Number.isFinite(capability.min)) out.min = capability.min
    if (Number.isFinite(capability.max)) out.max = capability.max
    if (Number.isFinite(capability.step)) out.step = capability.step
  }
  if (capability.type === 'enum') {
    out.options = (capability.options ?? [])
      .map((option) => ({ value: option.value.trim(), label: option.label.trim() }))
      .filter((option) => option.value !== '' || option.label !== '')
  }
  return out
}

type Props = { onClose: () => void }

/**
 * 管理员类型工作台：内置类型清单 + 新建/编辑/删除 + 能力项编辑 + 动态表单预览。
 *
 * 它是 T7 家具库的数据来源；T6 不在这里做布放。
 */
export default function TypeManager({ onClose }: Props) {
  const [types, setTypes] = useState<ItemType[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft>(emptyDraft())
  const [creating, setCreating] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [previewOverrides, setPreviewOverrides] = useState<CapabilityValues>({})
  const [pendingConfirm, setPendingConfirm] = useState<{
    title: string
    description: string
    onConfirm: () => void
  } | null>(null)

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !pendingConfirm) onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose, pendingConfirm])

  async function refresh(selectKey?: string) {
    setLoading(true)
    try {
      const next = await listItemTypes()
      setTypes(next)
      setError(null)
      if (selectKey) {
        const found = next.find((typ) => typ.key === selectKey)
        if (found) {
          setSelectedKey(found.key)
          setDraft(draftFromType(found))
          setCreating(false)
          setPreviewOverrides({})
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取类型列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  function selectType(typ: ItemType) {
    setSelectedKey(typ.key)
    setDraft(draftFromType(typ))
    setCreating(false)
    setPreviewOverrides({})
    setError(null)
  }

  function startCreate() {
    setSelectedKey(null)
    setDraft(emptyDraft())
    setCreating(true)
    setPreviewOverrides({})
    setError(null)
  }

  function updateDraft(patch: Partial<Draft>) {
    setDraft((current) => ({ ...current, ...patch }))
  }

  function updateCapability(index: number, patch: Partial<Capability>) {
    setDraft((current) => ({
      ...current,
      capabilities: current.capabilities.map((capability, i) =>
        i === index ? { ...capability, ...patch } : capability,
      ),
    }))
  }

  function changeCapabilityType(index: number, type: CapabilityType) {
    const capability = draft.capabilities[index]
    updateCapability(index, {
      type,
      min: undefined,
      max: undefined,
      step: undefined,
      options: type === 'enum' ? [{ value: '', label: '' }] : undefined,
      default: type === 'enum' ? undefined : emptyValueFor(type),
    })
    if (capability.key) setPreviewOverrides((current) => ({ ...current, [capability.key]: emptyValueFor(type) }))
  }

  function removeCapability(index: number) {
    const original = draft.capabilities[index]?.original
    if (original) {
      setPendingConfirm({
        title: `删除能力项「${original.name}」？`,
        description: '历史值会保留在数据库中，但不再显示于设备操控面板。',
        onConfirm: () => {
          setDraft((current) => ({
            ...current,
            capabilities: current.capabilities.filter((_, i) => i !== index),
          }))
        },
      })
      return
    }
    setDraft((current) => ({
      ...current,
      capabilities: current.capabilities.filter((_, i) => i !== index),
    }))
  }

  function addCapability() {
    setDraft((current) => ({ ...current, capabilities: [...current.capabilities, newCapability()] }))
  }

  function updateOption(index: number, optionIndex: number, patch: Partial<CapabilityOption>) {
    const capability = draft.capabilities[index]
    const options = [...(capability.options ?? [])]
    options[optionIndex] = { ...options[optionIndex], ...patch }
    updateCapability(index, { options })
  }

  function addOption(index: number) {
    const capability = draft.capabilities[index]
    updateCapability(index, { options: [...(capability.options ?? []), { value: '', label: '' }] })
  }

  function removeOption(index: number, optionIndex: number) {
    const capability = draft.capabilities[index]
    updateCapability(index, {
      options: (capability.options ?? []).filter((_, i) => i !== optionIndex),
    })
  }

  const defaultPreview = useMemo(() => defaultCapabilityValues(draft.capabilities), [draft.capabilities])
  const previewValues = useMemo(() => {
    const merged: CapabilityValues = { ...defaultPreview }
    for (const capability of draft.capabilities) {
      if (Object.prototype.hasOwnProperty.call(previewOverrides, capability.key)) {
        merged[capability.key] = previewOverrides[capability.key]
      }
    }
    return merged
  }, [defaultPreview, draft.capabilities, previewOverrides])

  function buildInput() {
    const width = Number(draft.defaultWidth)
    const depth = Number(draft.defaultDepth)
    if (!Number.isFinite(width) || !Number.isFinite(depth)) {
      throw new Error('默认尺寸必须是数字')
    }
    return {
      key: draft.key.trim(),
      kind: draft.kind,
      name: draft.name.trim(),
      icons: draft.icons.trim() || undefined,
      defaultWidth: width,
      defaultDepth: depth,
      driver: draft.kind === 'device' ? draft.driver.trim() || undefined : undefined,
      capabilities: draft.capabilities.map(cleanCapability),
    }
  }

  async function save() {
    setSaving(true)
    setError(null)
    try {
      const input = buildInput()
      if (creating) {
        await createItemType(input)
      } else if (selectedKey) {
        await updateItemType(selectedKey, {
          name: input.name,
          // 空字符串表示清除图标；省略会被 PATCH 当成“保持不变”。
          icons: draft.icons.trim(),
          defaultWidth: input.defaultWidth,
          defaultDepth: input.defaultDepth,
          driver: draft.driver.trim(),
          capabilities: input.capabilities,
          capabilitiesVersion: types.find((typ) => typ.key === selectedKey)?.capabilitiesVersion,
        })
      }
      await refresh(creating ? input.key : selectedKey ?? undefined)
      toast.success(creating ? `已创建类型「${input.name}」` : `已保存类型「${input.name}」`)
    } catch (e) {
      setError(e instanceof ApiError || e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  function remove() {
    if (!selectedKey) return
    const keyToDelete = selectedKey
    setPendingConfirm({
      title: `删除类型「${draft.name || keyToDelete}」？`,
      description: '若该类型仍被户型中的物品引用，后端会拒绝本次删除。',
      onConfirm: () => {
        void (async () => {
          setSaving(true)
          setError(null)
          try {
            await deleteItemType(keyToDelete)
            toast.success(`已删除类型「${draft.name || keyToDelete}」`)
            startCreate()
            await refresh()
          } catch (e) {
            setError(e instanceof ApiError || e instanceof Error ? e.message : '删除失败')
          } finally {
            setSaving(false)
          }
        })()
      },
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(20,20,32,0.45)' }}>
      <section
        className="flex h-[86vh] w-full max-w-6xl flex-col overflow-hidden"
        style={{ background: 'var(--color-surface)', border: '1px solid var(--color-border)', borderRadius: 'var(--radius-md)' }}
      >
        <header className="flex items-center justify-between px-4 py-3" style={{ borderBottom: '1px solid var(--color-border)' }}>
          <div>
            <h2 className="text-base font-semibold">类型管理</h2>
            <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
              能力元数据同时驱动表单、校验、模拟器与图标；新增能力项不用改前端代码。
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={startCreate} disabled={saving} className="px-3 py-1 text-xs" style={buttonStyle}>
              新建类型
            </button>
            <button onClick={onClose} className="px-3 py-1 text-xs" style={buttonStyle}>
              关闭
            </button>
          </div>
        </header>

        {error ? (
          <p className="px-4 py-2 text-sm" style={{ color: 'var(--color-danger)' }} role="alert">
            {error}
          </p>
        ) : null}

        <div className="grid min-h-0 flex-1 grid-cols-[240px_minmax(0,1fr)]">
          <aside className="min-h-0 overflow-auto p-2" style={{ borderRight: '1px solid var(--color-border)' }}>
            {loading ? (
              <p className="p-2 text-xs" style={{ color: 'var(--color-muted)' }}>读取中…</p>
            ) : (
              <ul className="grid gap-1">
                {types.map((typ) => (
                  <li key={typ.key}>
                    <button
                      onClick={() => selectType(typ)}
                      disabled={saving}
                      className="w-full px-2 py-1 text-left text-xs"
                      style={{
                        ...buttonStyle,
                        background: typ.key === selectedKey ? 'var(--color-bg)' : 'transparent',
                        borderColor: typ.key === selectedKey ? 'var(--color-accent, #4f6bed)' : 'transparent',
                      }}
                    >
                      <span className="block text-sm">{typ.name}</span>
                      <span style={{ color: 'var(--color-muted)' }}>
                        {typ.kind === 'device' ? '设备' : '家具'} · {typ.key} · v{typ.capabilitiesVersion}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </aside>

          <div className="min-h-0 min-w-0 overflow-auto p-4">
            <fieldset
              disabled={saving}
              className="grid min-w-0 gap-4 lg:grid-cols-2"
              style={{ border: 0, margin: 0, padding: 0 }}
            >
              <section className="grid min-w-0 gap-3">
                <h3 className="text-sm font-semibold">{creating ? '新建类型' : `编辑 ${draft.key}`}</h3>
                <div className="grid grid-cols-2 gap-3">
                  <label className="grid gap-1 text-xs">
                    类型 key
                    <input
                      value={draft.key}
                      disabled={!creating}
                      onChange={(e) => updateDraft({ key: e.target.value })}
                      placeholder="例如 smart_rack"
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    />
                  </label>
                  <label className="grid gap-1 text-xs">
                    种类
                    <select
                      value={draft.kind}
                      disabled={!creating}
                      onChange={(e) => updateDraft({ kind: e.target.value as Draft['kind'] })}
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    >
                      <option value="device">设备（有控制能力）</option>
                      <option value="furniture">家具（零能力）</option>
                    </select>
                  </label>
                  <label className="grid gap-1 text-xs">
                    名称
                    <input
                      value={draft.name}
                      onChange={(e) => updateDraft({ name: e.target.value })}
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    />
                  </label>
                  <label className="grid gap-1 text-xs">
                    图标名
                    <input
                      value={draft.icons}
                      onChange={(e) => updateDraft({ icons: e.target.value })}
                      placeholder="Lucide 图标名"
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    />
                  </label>
                  <label className="grid gap-1 text-xs">
                    默认宽（cm）
                    <input
                      type="number"
                      min={1}
                      max={5000}
                      value={draft.defaultWidth}
                      onChange={(e) => updateDraft({ defaultWidth: e.target.value })}
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    />
                  </label>
                  <label className="grid gap-1 text-xs">
                    默认深（cm）
                    <input
                      type="number"
                      min={1}
                      max={5000}
                      value={draft.defaultDepth}
                      onChange={(e) => updateDraft({ defaultDepth: e.target.value })}
                      style={inputStyle}
                      className="px-2 py-1 text-sm"
                    />
                  </label>
                  {draft.kind === 'device' ? (
                    <label className="grid gap-1 text-xs">
                      驱动
                      <input
                        value={draft.driver}
                        onChange={(e) => updateDraft({ driver: e.target.value })}
                        placeholder="simulator"
                        style={inputStyle}
                        className="px-2 py-1 text-sm"
                      />
                    </label>
                  ) : null}
                </div>

                <div className="flex items-center justify-between">
                  <h4 className="text-sm font-semibold">能力项</h4>
                  <button onClick={addCapability} className="px-2 py-1 text-xs" style={buttonStyle}>
                    加能力项
                  </button>
                </div>

                {draft.capabilities.length === 0 ? (
                  <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                    家具可以没有能力项；设备至少应有一个可写能力。
                  </p>
                ) : null}

                {draft.capabilities.map((capability, index) => (
                  <CapabilityEditor
                    key={index}
                    capability={capability}
                    original={capability.original}
                    onChange={(patch) => updateCapability(index, patch)}
                    onChangeType={(type) => changeCapabilityType(index, type)}
                    onRemove={() => removeCapability(index)}
                    onAddOption={() => addOption(index)}
                    onChangeOption={(optionIndex, patch) => updateOption(index, optionIndex, patch)}
                    onRemoveOption={(optionIndex) => removeOption(index, optionIndex)}
                  />
                ))}
              </section>

              <section className="grid min-w-0 content-start gap-3">
                <h3 className="text-sm font-semibold">表单预览</h3>
                <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                  下面这块由 capability 元数据生成；改能力项不会改代码。
                </p>
                <div
                  className="grid min-w-0 gap-3 p-3"
                  style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
                >
                  <CapabilityForm
                    capabilities={draft.capabilities}
                    values={previewValues}
                    onChange={(key, value) => setPreviewOverrides((current) => ({ ...current, [key]: value }))}
                    idPrefix="preview"
                  />
                </div>
              </section>
            </fieldset>
          </div>
        </div>

        <footer className="flex items-center justify-end gap-2 px-4 py-3" style={{ borderTop: '1px solid var(--color-border)' }}>
          {!creating && selectedKey ? (
            <button onClick={() => remove()} disabled={saving} className="px-3 py-1 text-xs" style={{ ...buttonStyle, color: 'var(--color-danger)' }}>
              删除类型
            </button>
          ) : null}
          <button onClick={() => void save()} disabled={saving} className="px-3 py-1 text-xs" style={buttonStyle}>
            {saving ? '保存中…' : creating ? '创建类型' : '保存修改'}
          </button>
        </footer>
      </section>

      <AlertDialog open={Boolean(pendingConfirm)} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pendingConfirm?.title}</AlertDialogTitle>
            <AlertDialogDescription>{pendingConfirm?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                pendingConfirm?.onConfirm()
                setPendingConfirm(null)
              }}
            >
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

type CapabilityEditorProps = {
  capability: Capability
  /** 已保存的能力项；key、type 与枚举 value 不允许改，避免状态/历史里的旧值失去解释。 */
  original?: Capability
  onChange: (patch: Partial<Capability>) => void
  onChangeType: (type: CapabilityType) => void
  onRemove: () => void
  onAddOption: () => void
  onChangeOption: (optionIndex: number, patch: Partial<CapabilityOption>) => void
  onRemoveOption: (optionIndex: number) => void
}

function CapabilityEditor({
  capability,
  original,
  onChange,
  onChangeType,
  onRemove,
  onAddOption,
  onChangeOption,
  onRemoveOption,
}: CapabilityEditorProps) {
  const keyLocked = original !== undefined
  const typeLocked = original !== undefined
  const originalOptionCount = original?.options?.length ?? 0
  return (
    <div className="grid min-w-0 gap-2 p-2" style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}>
      <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_110px_auto] gap-2">
        <input
          value={capability.key}
          disabled={keyLocked}
          title={keyLocked ? '已发布的能力 key 不能改名或删除' : undefined}
          onChange={(e) => onChange({ key: e.target.value })}
          placeholder="key（snake_case）"
          style={inputStyle}
          className="px-2 py-1 text-xs"
        />
        <input
          value={capability.name}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="显示名"
          style={inputStyle}
          className="px-2 py-1 text-xs"
        />
        <select
          value={capability.type}
          disabled={typeLocked}
          title={typeLocked ? '已保存能力项的类型不能修改；请新增能力项' : undefined}
          onChange={(e) => onChangeType(e.target.value as CapabilityType)}
          style={inputStyle}
          className="px-2 py-1 text-xs"
        >
          <option value="bool">bool</option>
          <option value="number">number</option>
          <option value="enum">enum</option>
          <option value="string">string</option>
        </select>
        <button
          onClick={onRemove}
          className="px-2 py-1 text-xs"
          style={{ ...buttonStyle, color: 'var(--color-danger)' }}
        >
          删
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3 text-xs">
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={capability.writable} onChange={(e) => onChange({ writable: e.target.checked })} />
          可写
        </label>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={capability.history} onChange={(e) => onChange({ history: e.target.checked })} />
          记历史
        </label>
        <label className="flex items-center gap-1">
          单位
          <input
            value={capability.unit ?? ''}
            onChange={(e) => onChange({ unit: e.target.value || undefined })}
            style={inputStyle}
            className="w-16 px-1 py-0.5 text-xs"
          />
        </label>
        {capability.type === 'number' ? (
          <>
            <label className="flex items-center gap-1">
              min
              <input
                type="number"
                value={capability.min ?? ''}
                onChange={(e) => onChange({ min: e.target.value === '' ? undefined : Number(e.target.value) })}
                style={inputStyle}
                className="w-16 px-1 py-0.5 text-xs"
              />
            </label>
            <label className="flex items-center gap-1">
              max
              <input
                type="number"
                value={capability.max ?? ''}
                onChange={(e) => onChange({ max: e.target.value === '' ? undefined : Number(e.target.value) })}
                style={inputStyle}
                className="w-16 px-1 py-0.5 text-xs"
              />
            </label>
            <label className="flex items-center gap-1">
              step
              <input
                type="number"
                value={capability.step ?? ''}
                onChange={(e) => onChange({ step: e.target.value === '' ? undefined : Number(e.target.value) })}
                style={inputStyle}
                className="w-16 px-1 py-0.5 text-xs"
              />
            </label>
          </>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span style={{ color: 'var(--color-muted)' }}>默认值</span>
        {capability.type === 'bool' ? (
          <input
            type="checkbox"
            checked={capability.default === true}
            onChange={(e) => onChange({ default: e.target.checked })}
          />
        ) : null}
        {capability.type === 'number' ? (
          <input
            type="number"
            value={typeof capability.default === 'number' ? capability.default : ''}
            onChange={(e) => onChange({ default: e.target.value === '' ? undefined : Number(e.target.value) })}
            style={inputStyle}
            className="w-24 px-1 py-0.5 text-xs"
          />
        ) : null}
        {capability.type === 'enum' ? (
          <select
            value={typeof capability.default === 'string' ? capability.default : ''}
            onChange={(e) => onChange({ default: e.target.value === '' ? undefined : e.target.value })}
            style={inputStyle}
            className="px-1 py-0.5 text-xs"
          >
            <option value="">（无）</option>
            {(capability.options ?? []).map((option, index) => (
              <option key={`${option.value}-${index}`} value={option.value}>
                {option.label || option.value}
              </option>
            ))}
          </select>
        ) : null}
        {capability.type === 'string' ? (
          <input
            value={typeof capability.default === 'string' ? capability.default : ''}
            onChange={(e) => onChange({ default: e.target.value })}
            style={inputStyle}
            className="w-40 px-1 py-0.5 text-xs"
          />
        ) : null}
      </div>

      {capability.type === 'enum' ? (
        <div className="grid gap-1 text-xs">
          <div className="flex items-center justify-between">
            <span style={{ color: 'var(--color-muted)' }}>枚举选项（value 入库 / label 显示）</span>
            <button onClick={onAddOption} className="px-1 py-0.5 text-xs" style={buttonStyle}>
              加选项
            </button>
          </div>
          {(capability.options ?? []).map((option, index) => (
            <div key={index} className="grid grid-cols-[1fr_1fr_auto] gap-1">
              <input
                value={option.value}
                disabled={index < originalOptionCount}
                title={index < originalOptionCount ? '已发布的枚举 value 不能删除或改名；可以改 label' : undefined}
                onChange={(e) => onChangeOption(index, { value: e.target.value })}
                placeholder="value"
                style={inputStyle}
                className="px-1 py-0.5 text-xs"
              />
              <input
                value={option.label}
                onChange={(e) => onChangeOption(index, { label: e.target.value })}
                placeholder="label"
                style={inputStyle}
                className="px-1 py-0.5 text-xs"
              />
              <button
                onClick={() => onRemoveOption(index)}
                disabled={index < originalOptionCount}
                title={index < originalOptionCount ? '已发布的枚举 value 不能删除' : undefined}
                className="px-1 py-0.5 text-xs"
                style={{ ...buttonStyle, color: 'var(--color-danger)', opacity: index < originalOptionCount ? 0.45 : 1 }}
              >
                删
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
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
