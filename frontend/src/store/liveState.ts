import { create } from 'zustand'

export type LiveState = {
  itemId: string
  capabilityKey: string
  value: unknown
  source: string
  updatedAt: string
}

type LiveStore = {
  states: Record<string, LiveState>
  connected: boolean
  lastSeq: number | null
  applyEnvelope: (envelope: { seq: number; type: string; payload: unknown }) => void
  setConnected: (connected: boolean) => void
  setState: (state: LiveState) => void
  getState: (itemId: string, capabilityKey: string) => unknown
}

const keyOf = (itemId: string, capabilityKey: string) => `${itemId}:${capabilityKey}`

export const useLiveStore = create<LiveStore>((set, get) => ({
  states: {},
  connected: false,
  lastSeq: null,
  setConnected: (connected) => set({ connected }),
  setState: (state) => set((store) => ({
    states: { ...store.states, [keyOf(state.itemId, state.capabilityKey)]: state },
  })),
  getState: (itemId, capabilityKey) => get().states[keyOf(itemId, capabilityKey)]?.value,
  applyEnvelope: (envelope) => {
    const lastSeq = get().lastSeq
    if (lastSeq !== null && envelope.seq > lastSeq + 1) {
      set({ states: {}, lastSeq: null })
      return
    }
    set({ lastSeq: envelope.seq })
    if (envelope.type === 'snapshot') {
      const payload = envelope.payload as LiveState[]
      set({
        states: Object.fromEntries(payload.map((state) => [
          keyOf(state.itemId, state.capabilityKey),
          state,
        ])),
      })
    }
    if (envelope.type === 'state') {
      const state = envelope.payload as LiveState
      get().setState(state)
    }
  },
}))

export function connectLiveState(onGap: () => void) {
  let socket: WebSocket
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
      const lastSeq = useLiveStore.getState().lastSeq
      if (lastSeq !== null && envelope.seq > lastSeq + 1) {
        useLiveStore.setState({ states: {}, lastSeq: null })
        onGap()
        socket.close()
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
    if (timer !== null) window.clearTimeout(timer)
    socket?.close()
  }
}
