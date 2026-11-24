import { beforeEach, describe, expect, it } from 'vitest'
import { useLiveStore } from './liveState.ts'

const snapshot = {
  seq: 7,
  type: 'snapshot',
  payload: {
    states: [
      { itemId: 'item-a', capabilityKey: 'on', value: true, source: 'manual', ts: '2026-11-23T20:00:00+08:00' },
      { itemId: 'item-a', capabilityKey: 'brightness', value: 80, source: 'manual', ts: '2026-11-23T20:00:00+08:00' },
    ],
  },
}

describe('实时状态 store', () => {
  beforeEach(() => useLiveStore.setState({ states: {}, lastSeq: null, connected: false }))

  it('快照按 itemId:capabilityKey 建索引', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    const { states, lastSeq } = useLiveStore.getState()
    expect(lastSeq).toBe(7)
    expect(states['item-a:on'].value).toBe(true)
    expect(states['item-a:brightness'].value).toBe(80)
  })

  it('增量只覆盖对应的一项', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    useLiveStore.getState().applyEnvelope({
      seq: 8,
      type: 'state',
      payload: { itemId: 'item-a', capabilityKey: 'on', value: false, source: 'manual', ts: '2026-11-23T20:01:00+08:00' },
    })
    const { states, lastSeq } = useLiveStore.getState()
    expect(lastSeq).toBe(8)
    expect(states['item-a:on'].value).toBe(false)
    expect(states['item-a:brightness'].value).toBe(80)
  })

  it('序号跳号时丢弃缓存，等待重新取快照', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    useLiveStore.getState().applyEnvelope({
      seq: 10,
      type: 'state',
      payload: { itemId: 'item-a', capabilityKey: 'on', value: false, source: 'manual', ts: '2026-11-23T20:01:00+08:00' },
    })
    const { states, lastSeq } = useLiveStore.getState()
    expect(lastSeq).toBeNull()
    expect(states).toEqual({})
  })

  it('忽略序号小于等于 lastSeq 的过期或重复增量，不覆盖新状态也不回退序号', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    useLiveStore.getState().applyEnvelope({
      seq: 8,
      type: 'state',
      payload: { itemId: 'item-a', capabilityKey: 'on', value: false, source: 'manual', ts: '2026-11-23T20:01:00+08:00' },
    })
    useLiveStore.getState().applyEnvelope({
      seq: 7,
      type: 'state',
      payload: { itemId: 'item-a', capabilityKey: 'on', value: true, source: 'manual', ts: '2026-11-23T20:00:00+08:00' },
    })
    const { states, lastSeq } = useLiveStore.getState()
    expect(lastSeq).toBe(8)
    expect(states['item-a:on'].value).toBe(false)
  })
})
