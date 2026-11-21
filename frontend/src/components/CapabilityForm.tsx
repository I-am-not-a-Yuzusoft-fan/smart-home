import type { Capability } from '../api/client.ts'

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
      <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
        这个类型没有可配置的能力项（家具或纯占位设备）。
      </p>
    )
  }

  return (
    <div className="grid gap-3">
      {capabilities.map((capability) => {
        const id = `${idPrefix}-${capability.key}`
        const readOnly = disabled || !capability.writable
        const value = values[capability.key]
        return (
          <div key={capability.key} className="grid gap-1">
            <label htmlFor={id} className="flex items-baseline justify-between text-xs">
              <span>
                {capability.name}
                {capability.unit ? (
                  <span className="ml-1" style={{ color: 'var(--color-muted)' }}>
                    ({capability.unit})
                  </span>
                ) : null}
              </span>
              {!capability.writable ? (
                <span style={{ color: 'var(--color-muted)' }}>只读</span>
              ) : null}
            </label>

            {capability.type === 'bool' ? (
              <label className="flex items-center gap-2 text-sm">
                <input
                  id={id}
                  type="checkbox"
                  checked={value === true}
                  disabled={readOnly}
                  onChange={(e) => onChange(capability.key, e.target.checked)}
                />
                <span style={{ color: 'var(--color-muted)' }}>{value === true ? '开' : '关'}</span>
              </label>
            ) : null}

            {capability.type === 'number' ? (
              <input
                id={id}
                type="number"
                className="w-full px-2 py-1 text-sm"
                style={inputStyle}
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

            {capability.type === 'enum' ? (
              <select
                id={id}
                className="w-full px-2 py-1 text-sm"
                style={inputStyle}
                value={typeof value === 'string' ? value : ''}
                disabled={readOnly}
                onChange={(e) => onChange(capability.key, e.target.value)}
              >
                {(capability.options ?? []).map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            ) : null}

            {capability.type === 'string' ? (
              <input
                id={id}
                type="text"
                className="w-full px-2 py-1 text-sm"
                style={inputStyle}
                value={typeof value === 'string' ? value : ''}
                disabled={readOnly}
                onChange={(e) => onChange(capability.key, e.target.value)}
              />
            ) : null}

            {capability.type === 'number' && (capability.min !== undefined || capability.max !== undefined) ? (
              <span className="text-[11px]" style={{ color: 'var(--color-muted)' }}>
                范围 {capability.min ?? '−∞'} – {capability.max ?? '+∞'}
              </span>
            ) : null}
          </div>
        )
      })}
    </div>
  )
}

const inputStyle = {
  border: '1px solid var(--color-border)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--color-bg)',
  color: 'inherit',
} as const
