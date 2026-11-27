import type { Capability } from '../api/client.ts'
import { cn } from '../lib/utils.ts'
import { Badge } from './ui/badge.tsx'
import { Input } from './ui/input.tsx'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select.tsx'
import { Slider } from './ui/slider.tsx'
import { Switch } from './ui/switch.tsx'

export type CapabilityValues = Record<string, unknown>

/**
 * 按能力元数据生成默认值。
 *
 * 这是 T7 新建设备时初始化 capability_state 的前端参考实现：
 * 有 default 用 default，没有就按类型取零值/第一个选项。
 */
export function defaultCapabilityValues(capabilities: Capability[]): CapabilityValues {
  const values: CapabilityValues = {}
  for (const capability of capabilities) {
    if (capability.default !== undefined) {
      values[capability.key] = capability.default
      continue
    }
    switch (capability.type) {
      case 'bool':
        values[capability.key] = false
        break
      case 'number': {
        const lower = capability.min ?? 0
        values[capability.key] =
          capability.max !== undefined ? Math.min(lower, capability.max) : lower
        break
      }
      case 'enum':
        values[capability.key] = capability.options?.[0]?.value ?? ''
        break
      case 'string':
        values[capability.key] = ''
        break
    }
  }
  return values
}

type Props = {
  capabilities: Capability[]
  values: CapabilityValues
  onChange: (key: string, value: unknown) => void
  disabled?: boolean
  idPrefix?: string
}

/**
 * 能力元数据驱动的动态表单。
 *
 * 这里不出现任何具体设备类型或能力键——新增一个能力项只改数据，不改这个文件，
 * 这就是 FR-2.4「新增类型不需改前端代码」的落点。
 */
export default function CapabilityForm({
  capabilities,
  values,
  onChange,
  disabled = false,
  idPrefix = 'cap',
}: Props) {
  if (capabilities.length === 0) {
    return (
      <p className="text-xs text-text-muted">
        这个类型没有可配置的能力项（家具或纯占位设备）。
      </p>
    )
  }

  return (
    <div className="space-y-3">
      {capabilities.map((capability) => {
        const id = `${idPrefix}-${capability.key}`
        const readOnly = disabled || !capability.writable
        const value = values[capability.key]

        // 只读传感器项：紧凑卡片读数展示
        if (!capability.writable) {
          const display =
            value === undefined || value === null
              ? '—'
              : capability.type === 'bool'
                ? value === true
                  ? '是'
                  : '否'
                : capability.type === 'enum'
                  ? capability.options?.find((o) => o.value === value)?.label ?? String(value)
                  : `${String(value)}${capability.unit ? ` ${capability.unit}` : ''}`

          return (
            <div
              key={capability.key}
              className="flex items-center justify-between rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/60 px-3 py-2"
            >
              <span className="text-xs text-text-secondary">{capability.name}</span>
              <span className="font-mono text-xs font-semibold text-text-primary">{display}</span>
            </div>
          )
        }

        const hasRange =
          capability.type === 'number' &&
          capability.min !== undefined &&
          capability.max !== undefined &&
          capability.max > capability.min

        const numVal =
          typeof value === 'number' && Number.isFinite(value)
            ? value
            : capability.min ?? 0

        const options = capability.options ?? []

        return (
          <div
            key={capability.key}
            className="space-y-1.5 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface p-2.5"
          >
            <div className="flex items-center justify-between gap-2">
              <label htmlFor={id} className="text-xs font-medium text-text-primary">
                {capability.name}
                {capability.unit ? (
                  <span className="ml-1 font-normal text-text-muted">({capability.unit})</span>
                ) : null}
              </label>

              {capability.type === 'bool' ? (
                <div className="flex items-center gap-2">
                  <Badge variant={value === true ? 'success' : 'secondary'}>
                    {value === true ? '开启' : '关闭'}
                  </Badge>
                  <Switch
                    id={id}
                    checked={value === true}
                    disabled={readOnly}
                    onCheckedChange={(checked) => onChange(capability.key, checked)}
                  />
                </div>
              ) : capability.type === 'number' ? (
                <span className="font-mono text-xs font-semibold text-accent">
                  {typeof value === 'number' ? value : '—'}
                  {capability.unit ? ` ${capability.unit}` : ''}
                </span>
              ) : null}
            </div>

            {capability.type === 'number' && hasRange ? (
              <div className="space-y-1 pt-0.5">
                <Slider
                  id={id}
                  min={capability.min}
                  max={capability.max}
                  step={capability.step ?? 1}
                  disabled={readOnly}
                  value={[Math.min(Math.max(numVal, capability.min!), capability.max!)]}
                  onValueChange={([v]) => onChange(capability.key, v)}
                />
                <div className="flex items-center justify-between text-[10px] text-text-muted">
                  <span>
                    {capability.min}
                    {capability.unit ?? ''}
                  </span>
                  <span>
                    {capability.max}
                    {capability.unit ?? ''}
                  </span>
                </div>
              </div>
            ) : capability.type === 'number' ? (
              <Input
                id={id}
                type="number"
                className="h-8 text-xs"
                value={typeof value === 'number' ? value : ''}
                min={capability.min}
                max={capability.max}
                step={capability.step ?? 'any'}
                disabled={readOnly}
                onChange={(e) => {
                  const next = e.target.value === '' ? undefined : Number(e.target.value)
                  onChange(capability.key, next)
                }}
              />
            ) : null}

            {capability.type === 'enum' && options.length > 0 && options.length <= 4 ? (
              <div className="grid gap-1 pt-0.5" style={{ gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` }}>
                {options.map((option) => {
                  const active = value === option.value
                  return (
                    <button
                      key={option.value}
                      type="button"
                      disabled={readOnly}
                      onClick={() => onChange(capability.key, option.value)}
                      className={cn(
                        'cursor-pointer rounded-[var(--radius-sm)] border px-2 py-1 text-xs font-medium transition-colors',
                        active
                          ? 'border-accent bg-accent-soft text-accent'
                          : 'border-border-default bg-bg-subtle/50 text-text-secondary hover:bg-bg-subtle',
                        readOnly && 'pointer-events-none opacity-50',
                      )}
                    >
                      {option.label}
                    </button>
                  )
                })}
              </div>
            ) : capability.type === 'enum' ? (
              <Select
                value={typeof value === 'string' ? value : ''}
                disabled={readOnly}
                onValueChange={(v) => onChange(capability.key, v)}
              >
                <SelectTrigger id={id} className="h-8 text-xs">
                  <SelectValue placeholder="请选择…" />
                </SelectTrigger>
                <SelectContent>
                  {options.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}

            {capability.type === 'string' ? (
              <Input
                id={id}
                type="text"
                className="h-8 text-xs"
                value={typeof value === 'string' ? value : ''}
                disabled={readOnly}
                onChange={(e) => onChange(capability.key, e.target.value)}
              />
            ) : null}
          </div>
        )
      })}
    </div>
  )
}
