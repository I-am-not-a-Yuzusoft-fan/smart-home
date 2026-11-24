import type { Capability, ItemType } from '../../api/client.ts'
import type { LiveState } from '../../store/liveState.ts'

/** 只读传感器的过期窗口（毫秒）：超过此时长未更新则标为不可用，绝不展示过期读数。 */
export const STALE_WINDOW_MS = 60_000

const optionLabel = (capability: Capability, value: unknown): string => {
  if (!capability.options) return String(value)
  return capability.options.find((option) => option.value === value)?.label ?? String(value)
}

/** 只有只读能力（传感器）有过期语义；可写设备长时间未操作仍保留当前状态。 */
export function isCapabilityStale(
  capability: Pick<Capability, 'writable'>,
  updatedAt: string | undefined,
  nowMs = Date.now(),
): boolean {
  if (capability.writable) return false
  if (!updatedAt) return true
  const parsed = Date.parse(updatedAt)
  return !Number.isFinite(parsed) || nowMs - parsed > STALE_WINDOW_MS
}

/**
 * 控制面板的显示值合并：推送值覆盖首次拉取的快照，用户尚未下发的本地编辑草稿优先级最高，
 * 避免 WebSocket 连上后用户改不动表单或点下发时发出旧值。
 */
export function mergeControlValues(
  fetched: Record<string, unknown>,
  live: Record<string, unknown>,
  draft: Record<string, unknown>,
): Record<string, unknown> {
  return { ...fetched, ...live, ...draft }
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
  if (!first) {
    const isReadOnlySensor = type.capabilities.length > 0 && type.capabilities.every((c) => !c.writable)
    return isReadOnlySensor ? '不可用' : ''
  }
  return `${first.name} ${String(values[first.key])}${first.unit ?? ''}`
}

/** 从实时 store 里挑出某件物品的全部有效能力值（过期的只读传感器读数不返回）。 */
export function liveValuesFor(
  states: Record<string, LiveState>,
  itemId: string,
  type: ItemType | undefined,
  nowMs = Date.now(),
): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const capability of type?.capabilities ?? []) {
    const state = states[`${itemId}:${capability.key}`]
    if (!state) continue
    if (isCapabilityStale(capability, state.ts, nowMs)) continue
    values[capability.key] = state.value
  }
  return values
}
