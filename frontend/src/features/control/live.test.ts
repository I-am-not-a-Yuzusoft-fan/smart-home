import { describe, expect, it } from 'vitest'
import type { ItemType } from '../../api/client.ts'
import type { LiveState } from '../../store/liveState.ts'
import { liveValuesFor, mergeControlValues, stateSummary } from './live.ts'

const sensorType: ItemType = {
  key: 'sensor_th',
  kind: 'device',
  name: '温湿度传感器',
  icons: 'sensor',
  defaultWidth: 8,
  defaultDepth: 8,
  mount: 'floor',
  capabilitiesVersion: 1,
  capabilities: [
    { key: 'temperature', name: '温度', type: 'number', unit: '℃', writable: false, history: true },
    { key: 'humidity', name: '湿度', type: 'number', unit: '%RH', writable: false, history: true },
  ],
}

const lightType: ItemType = {
  key: 'ceiling_light',
  kind: 'device',
  name: '吸顶灯',
  icons: 'light',
  defaultWidth: 40,
  defaultDepth: 40,
  mount: 'ceiling',
  capabilitiesVersion: 1,
  capabilities: [
    { key: 'on', name: '开关', type: 'bool', writable: true, history: true },
    { key: 'brightness', name: '亮度', type: 'number', unit: '%', min: 0, max: 100, writable: true, history: true },
  ],
}

describe('实时状态格式化与过期一致性（F4 三处一致）', () => {
  it('只读传感器超过 60 秒未更新时，liveValuesFor 不返回过期读数，摘要显示不可用', () => {
    const now = Date.parse('2026-11-24T12:00:00Z')
    const staleTs = new Date(now - 61_000).toISOString()
    const states: Record<string, LiveState> = {
      'sensor-1:temperature': {
        itemId: 'sensor-1',
        capabilityKey: 'temperature',
        value: 25.5,
        source: 'device',
        ts: staleTs,
      },
    }
    const values = liveValuesFor(states, 'sensor-1', sensorType, now)
    expect(values.temperature).toBeUndefined()
    expect(stateSummary(sensorType, values)).toBe('不可用')
  })

  it('可写设备即使超过 60 秒未操作仍正常显示最新状态', () => {
    const now = Date.parse('2026-11-24T12:00:00Z')
    const oldTs = new Date(now - 3600_000).toISOString()
    const states: Record<string, LiveState> = {
      'light-1:on': {
        itemId: 'light-1',
        capabilityKey: 'on',
        value: true,
        source: 'manual',
        ts: oldTs,
      },
    }
    const values = liveValuesFor(states, 'light-1', lightType, now)
    expect(values.on).toBe(true)
    expect(stateSummary(lightType, values)).toBe('开')
  })

  it('控制面板本地未提交的草稿值不被 WS 推送的旧 live 值覆写', () => {
    const merged = mergeControlValues(
      { on: false, brightness: 40 },
      { on: true, brightness: 80 },
      { brightness: 50 },
    )
    expect(merged.on).toBe(true)
    expect(merged.brightness).toBe(50)
  })
})
