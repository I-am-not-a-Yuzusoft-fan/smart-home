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
  beforeEach(() =>
    useLiveStore.setState({
      states: {},
      runSteps: {},
      runStatuses: {},
      latestRunId: null,
      lastSeq: null,
      connected: false,
    }),
  )

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

  it('交错收到 run_step 与 state 时保持序号连续并更新步骤状态', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    useLiveStore.getState().applyEnvelope({
      seq: 8,
      type: 'run_step',
      payload: {
        runId: 'run-1',
        nodeId: 'l1',
        path: '000',
        alias: '关客厅主灯',
        status: 'running',
        runStatus: 'running',
      },
    })
    useLiveStore.getState().applyEnvelope({
      seq: 9,
      type: 'state',
      payload: { itemId: 'item-a', capabilityKey: 'on', value: false, source: 'manual', ts: '2026-11-23T20:01:00+08:00' },
    })
    useLiveStore.getState().applyEnvelope({
      seq: 10,
      type: 'run_step',
      payload: {
        runId: 'run-1',
        nodeId: 'l1',
        path: '000',
        alias: '关客厅主灯',
        status: 'ok',
        runStatus: 'ok',
      },
    })
    const state = useLiveStore.getState()
    expect(state.lastSeq).toBe(10)
    expect(state.latestRunId).toBe('run-1')
    expect(state.runStatuses['run-1']).toBe('ok')
    expect(state.runSteps['run-1'].l1.status).toBe('ok')
    expect(state.states['item-a:on'].value).toBe(false)
  })

  it('seedRun 在服务端返回取消/终态时覆盖本地陈旧 running 状态，且跳号时清空 runSteps 缓存', () => {
    useLiveStore.getState().applyEnvelope(snapshot)
    useLiveStore.getState().applyEnvelope({
      seq: 8,
      type: 'run_step',
      payload: { runId: 'run-2', nodeId: 'l3', path: '002', status: 'running', runStatus: 'running' },
    })
    useLiveStore.getState().seedRun({
      id: 'run-2',
      status: 'cancelled',
      source: 'manual',
      startedAt: '2026-11-26T10:00:00Z',
      steps: [{ runId: 'run-2', nodeId: 'l3', path: '002', status: 'skipped', error: '已取消' }],
    })
    expect(useLiveStore.getState().runStatuses['run-2']).toBe('cancelled')
    expect(useLiveStore.getState().runSteps['run-2'].l3.status).toBe('skipped')

    // 序号跳号时连同 runSteps / runStatuses / latestRunId 一并清空
    useLiveStore.getState().applyEnvelope({
      seq: 12,
      type: 'run_step',
      payload: { runId: 'run-2', nodeId: 'l4', path: '003', status: 'skipped' },
    })
    const afterGap = useLiveStore.getState()
    expect(afterGap.lastSeq).toBeNull()
    expect(afterGap.runSteps).toEqual({})
    expect(afterGap.runStatuses).toEqual({})
    expect(afterGap.latestRunId).toBeNull()
  })
})
