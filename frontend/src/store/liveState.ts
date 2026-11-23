import { create } from 'zustand'

/** 一条设备状态。字段名与 04-详细设计说明书 §3.2 的封套一致。 */
export type LiveState = {
  itemId: string
  capabilityKey: string
  value: unknown
  source: string
  ts: string
}

type LiveStore = {
  states: Record<string, LiveState>
  connected: boolean
  lastSeq: number | null
  applyEnvelope: (envelope: { seq: number; type: string; payload: unknown }) => void
  setConnected: (connected: boolean) => void
  reset: () => void
}

const keyOf = (itemId: string, capabilityKey: string) => `${itemId}:${capabilityKey}`

export const useLiveStore = create<LiveStore>((set, get) => ({
  states: {},
  connected: false,
  lastSeq: null,
  setConnected: (connected) => set({ connected }),
  reset: () => set({ states: {}, lastSeq: null }),
  applyEnvelope: (envelope) => {
    const { lastSeq } = get()
    // 跳号：漏了消息，丢弃本地缓存，交给调用方重连拿新快照（§6.3 第 3 条）。
    if (lastSeq !== null && envelope.seq > lastSeq + 1) {
      set({ states: {}, lastSeq: null })
      return
    }
    set({ lastSeq: envelope.seq })
    if (envelope.type === 'snapshot') {
      const payload = envelope.payload as { states?: LiveState[] }
      const states = payload.states ?? []
      set({ states: Object.fromEntries(states.map((s) => [keyOf(s.itemId, s.capabilityKey), s])) })
      return
    }
    if (envelope.type === 'state') {
      const state = envelope.payload as LiveState
      set((store) => ({ states: { ...store.states, [keyOf(state.itemId, state.capabilityKey)]: state } }))
    }
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
export function connectLiveState(onGap: () => void) {
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
      const envelope = JSON.parse(event.data) as { seq: number; type: string; payload: unknown }
      const { lastSeq } = useLiveStore.getState()
      if (lastSeq !== null && envelope.seq > lastSeq + 1) {
        useLiveStore.getState().reset()
        onGap()
        socket?.close()
        return
      }
      useLiveStore.getState().applyEnvelope(envelope)
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
