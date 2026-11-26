import { create } from 'zustand'
import type { Run, RunStatus, RunStepStatus } from '../api/client.ts'

/** 一条设备状态。字段名与 04-详细设计说明书 §3.2 的封套一致。 */
export type LiveState = {
  itemId: string
  capabilityKey: string
  value: unknown
  source: string
  ts: string
}

/** WS 推来的单步执行状态更新。 */
export type LiveRunStep = {
  runId: string
  nodeId: string
  path: string
  alias?: string
  status: RunStepStatus
  error?: string
  note?: string
  runStatus?: RunStatus
  startedAt?: string
  finishedAt?: string
}

type LiveStore = {
  states: Record<string, LiveState>
  runSteps: Record<string, Record<string, LiveRunStep>>
  runStatuses: Record<string, RunStatus>
  latestRunId: string | null
  connected: boolean
  lastSeq: number | null
  applyEnvelope: (envelope: { seq: number; type: string; payload: unknown }) => void
  seedRun: (run: Run) => void
  setConnected: (connected: boolean) => void
  reset: () => void
}

const keyOf = (itemId: string, capabilityKey: string) => `${itemId}:${capabilityKey}`

export const useLiveStore = create<LiveStore>((set, get) => ({
  states: {},
  runSteps: {},
  runStatuses: {},
  latestRunId: null,
  connected: false,
  lastSeq: null,
  setConnected: (connected) => set({ connected }),
  reset: () => set({ states: {}, runSteps: {}, runStatuses: {}, latestRunId: null, lastSeq: null }),
  seedRun: (run) =>
    set((store) => {
      const existing = store.runSteps[run.id] ?? {}
      const merged: Record<string, LiveRunStep> = { ...existing }
      const runTerminal = run.status !== 'running'
      for (const st of run.steps) {
        const cur = merged[st.nodeId]
        const stepTerminal = st.status === 'ok' || st.status === 'failed' || st.status === 'skipped'
        // 服务端已进入终态或单步已进入终态时，以服务端权威记录为准，覆盖可能陈旧的 running 状态。
        if (!cur || cur.status === 'pending' || stepTerminal || runTerminal) {
          merged[st.nodeId] = { ...st, runStatus: run.status }
        }
      }
      return {
        latestRunId: run.id,
        runStatuses: { ...store.runStatuses, [run.id]: run.status },
        runSteps: { ...store.runSteps, [run.id]: merged },
      }
    }),
  applyEnvelope: (envelope) => {
    const { lastSeq } = get()
    if (envelope.type === 'snapshot') {
      const payload = envelope.payload as { states?: LiveState[] }
      const states = payload.states ?? []
      set({
        lastSeq: envelope.seq,
        states: Object.fromEntries(states.map((s) => [keyOf(s.itemId, s.capabilityKey), s])),
      })
      return
    }
    if (envelope.type !== 'state' && envelope.type !== 'run_step') return
    // 跳号：漏了消息，丢弃全部本地缓存，交给调用方重连拿新快照与回拉执行详情（§6.3 第 3 条）。
    if (lastSeq !== null && envelope.seq > lastSeq + 1) {
      set({ states: {}, runSteps: {}, runStatuses: {}, latestRunId: null, lastSeq: null })
      return
    }
    // 过期或重复的增量：直接丢弃，绝不回退 lastSeq 也不覆盖更新的值。
    if (lastSeq !== null && envelope.seq <= lastSeq) {
      return
    }
    if (envelope.type === 'state') {
      const state = envelope.payload as LiveState
      set((store) => ({
        lastSeq: envelope.seq,
        states: { ...store.states, [keyOf(state.itemId, state.capabilityKey)]: state },
      }))
      return
    }
    const step = envelope.payload as LiveRunStep
    set((store) => ({
      lastSeq: envelope.seq,
      latestRunId: step.runId,
      runStatuses: step.runStatus
        ? { ...store.runStatuses, [step.runId]: step.runStatus }
        : store.runStatuses,
      runSteps: {
        ...store.runSteps,
        [step.runId]: {
          ...(store.runSteps[step.runId] ?? {}),
          [step.nodeId]: step,
        },
      },
    }))
  },
}))

/** 取某个设备某项能力的最新值；没有就走调用方的兜底。 */
export function useLiveValue(itemId: string | null | undefined, capabilityKey: string): unknown {
  return useLiveStore((store) => (itemId ? store.states[keyOf(itemId, capabilityKey)]?.value : undefined))
}

/**
 * 连上 /ws/state：先收全量快照，之后只收增量。
 * 断线 1.5s 后自动重连；重连时 store 清掉 lastSeq，服务端会先发一份新快照。
 */
export function connectLiveState(onGap: () => void, onSynced?: () => void) {
  let socket: WebSocket | null = null
  let timer: number | null = null
  let stopped = false

  const connect = () => {
    if (stopped) return
    useLiveStore.setState({ connected: false })
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    socket = new WebSocket(`${protocol}//${window.location.host}/ws/state`)
    socket.onopen = () => useLiveStore.setState({ connected: true, lastSeq: null })
    socket.onmessage = (event) => {
      let envelope: { seq: number; type: string; payload: unknown }
      try {
        envelope = JSON.parse(event.data) as { seq: number; type: string; payload: unknown }
      } catch {
        return
      }
      const { lastSeq } = useLiveStore.getState()
      if ((envelope.type === 'state' || envelope.type === 'run_step') && lastSeq !== null && envelope.seq > lastSeq + 1) {
        useLiveStore.getState().reset()
        onGap()
        socket?.close()
        return
      }
      useLiveStore.getState().applyEnvelope(envelope)
      if (envelope.type === 'snapshot') onSynced?.()
    }
    socket.onclose = () => {
      useLiveStore.setState({ connected: false })
      if (!stopped) timer = window.setTimeout(connect, 1500)
    }
  }

  connect()

  return () => {
    stopped = true
    if (timer) window.clearTimeout(timer)
    socket?.close()
    useLiveStore.setState({ connected: false })
  }
}
