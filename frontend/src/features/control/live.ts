import type { Capability, ItemType } from '../../api/client.ts'
import type { LiveState } from '../../store/liveState.ts'

const optionLabel = (capability: Capability, value: unknown): string => {
  if (!capability.options) return String(value)
  return capability.options.find((option) => option.value === value)?.label ?? String(value)
}

/**
 * 设备当前状态的一句话摘要，画布标签、物品列表、属性面板三处共用同一个函数，
 * 保证「三处显示同一个值」不是三份各写各的格式化逻辑。
 */
export function stateSummary(type: ItemType | undefined, values: Record<string, unknown>): string {
  if (!type) return ''
  const on = type.capabilities.find((capability) => capability.key === 'on')
  if (on && typeof values.on === 'boolean') return values.on ? '开' : '关'
  const mode = type.capabilities.find((capability) => capability.key === 'mode')
  if (mode && values.mode !== undefined) return optionLabel(mode, values.mode)
  const position = type.capabilities.find((capability) => capability.key === 'position')
  if (position && typeof values.position === 'number') return `${values.position}%`
  const temperature = type.capabilities.find((capability) => capability.key === 'temperature')
  if (temperature && typeof values.temperature === 'number') return `${values.temperature}${temperature.unit ?? ''}`
  const occupied = type.capabilities.find((capability) => capability.key === 'occupied')
  if (occupied && typeof values.occupied === 'boolean') return values.occupied ? '有人' : '无人'
  const first = type.capabilities.find((capability) => values[capability.key] !== undefined)
  if (!first) return ''
  return `${first.name} ${String(values[first.key])}${first.unit ?? ''}`
}

/** 从实时 store 里挑出某件物品的全部能力值。 */
export function liveValuesFor(
  states: Record<string, LiveState>,
  itemId: string,
  type: ItemType | undefined,
): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const capability of type?.capabilities ?? []) {
    const state = states[`${itemId}:${capability.key}`]
    if (state) values[capability.key] = state.value
  }
  return values
}
