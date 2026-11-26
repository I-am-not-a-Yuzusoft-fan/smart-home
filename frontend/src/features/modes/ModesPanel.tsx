import { useEffect, useMemo, useState } from 'react'
import {
  cancelRun,
  getRun,
  getSimEnv,
  listModes,
  runMode,
  updateSimEnv,
  type EffectiveSeason,
  type Mode,
  type Run,
  type RunStatus,
  type RunStep,
  type RunStepStatus,
  type SimEnv,
  type SimSeason,
} from '../../api/client.ts'

const SEASON_LABEL: Record<SimSeason, string> = {
  auto: '自动（按月份）',
  spring: '春季',
  summer: '夏季',
  autumn: '秋季',
  winter: '冬季',
}

const EFFECTIVE_SEASON_LABEL: Record<EffectiveSeason, string> = {
  spring: '春季',
  summer: '夏季',
  autumn: '秋季',
  winter: '冬季',
}
import { useLiveStore } from '../../store/liveState.ts'

const LAST_RUN_KEY = 'smarthome:lastRunId'

const RUN_MODE_LABEL: Record<Mode['runMode'], string> = {
  restart: '重跑',
  single: '忽略重复',
  queued: '排队',
  parallel: '并发',
}

const RUN_STATUS_LABEL: Record<RunStatus, { text: string; bg: string; fg: string }> = {
  running: { text: '执行中', bg: '#e0e7ff', fg: '#3730a3' },
  ok: { text: '已完成', bg: '#dcfce7', fg: '#166534' },
  failed: { text: '失败', bg: '#fee2e2', fg: '#991b1b' },
  cancelled: { text: '已取消', bg: '#f3f4f6', fg: '#374151' },
}

const STEP_STATUS_LABEL: Record<RunStepStatus, { text: string; bg: string; fg: string }> = {
  pending: { text: '等待中', bg: '#f3f4f6', fg: '#4b5563' },
  running: { text: '执行中', bg: '#dbeafe', fg: '#1d4ed8' },
  ok: { text: '成功', bg: '#dcfce7', fg: '#166534' },
  failed: { text: '失败', bg: '#fee2e2', fg: '#991b1b' },
  skipped: { text: '已跳过', bg: '#fef3c7', fg: '#92400e' },
}

function formatDuration(startedAt?: string, finishedAt?: string): string {
  if (!startedAt || !finishedAt) return ''
  const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime()
  if (Number.isNaN(ms) || ms < 0) return ''
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

type Props = {
  userRole: 'admin' | 'member'
  onClose: () => void
}

export default function ModesPanel({ userRole, onClose }: Props) {
  const [modes, setModes] = useState<Mode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedModeId, setSelectedModeId] = useState<string | null>(null)
  const [activeRun, setActiveRun] = useState<Run | null>(null)
  const [busyModeId, setBusyModeId] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [simEnv, setSimEnv] = useState<SimEnv | null>(null)
  const [seasonDraft, setSeasonDraft] = useState<SimSeason>('auto')
  const [outdoorTempDraft, setOutdoorTempDraft] = useState<string>('')
  const [savingEnv, setSavingEnv] = useState(false)

  const liveRunSteps = useLiveStore((s) => (activeRun ? s.runSteps[activeRun.id] : undefined))
  const liveRunStatus = useLiveStore((s) => (activeRun ? s.runStatuses[activeRun.id] : undefined))
  const latestRunId = useLiveStore((s) => s.latestRunId)
  const wsConnected = useLiveStore((s) => s.connected)
  const seedRun = useLiveStore((s) => s.seedRun)

  // 初次打开：加载场景列表，并尝试恢复上次执行（刷新/重连恢复时间线）
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    listModes()
      .then((list) => {
        if (cancelled) return
        setModes(list)
        if (list.length > 0) setSelectedModeId((prev) => prev ?? list[0].id)
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : '读取场景列表失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    getSimEnv()
      .then((env) => {
        if (cancelled) return
        setSimEnv(env)
        setSeasonDraft(env.season)
        setOutdoorTempDraft(env.outdoorTemp !== undefined ? String(env.outdoorTemp) : '')
      })
      .catch(() => {})

    const savedRunId = sessionStorage.getItem(LAST_RUN_KEY) ?? useLiveStore.getState().latestRunId
    if (savedRunId) {
      getRun(savedRunId)
        .then((run) => {
          if (cancelled) return
          setActiveRun(run)
          seedRun(run)
        })
        .catch(() => {
          sessionStorage.removeItem(LAST_RUN_KEY)
        })
    }
  return () => {
      cancelled = true
    }
  }, [seedRun])

  // WS 断线重连后主动回拉当前执行详情，补齐断线期间漏掉的 run_step
  useEffect(() => {
    if (!wsConnected) return
    const runId = activeRun?.id ?? sessionStorage.getItem(LAST_RUN_KEY)
    if (!runId) return
    let cancelled = false
    getRun(runId)
      .then((run) => {
        if (cancelled) return
        setActiveRun(run)
        seedRun(run)
      })
      .catch(() => {})
  return () => {
      cancelled = true
    }
  }, [wsConnected, activeRun?.id, seedRun])

  // 双标签页同步：另一个标签页触发新场景时，通过 WS latestRunId 自动拉取详情并展示实时时间线
  useEffect(() => {
    if (!latestRunId || (activeRun && activeRun.id === latestRunId)) return
    let cancelled = false
    getRun(latestRunId)
      .then((run) => {
        if (cancelled) return
        setActiveRun(run)
        sessionStorage.setItem(LAST_RUN_KEY, run.id)
      })
      .catch(() => {})
  return () => {
      cancelled = true
    }
  }, [latestRunId, activeRun])

  const selectedMode = useMemo(
    () => modes.find((m) => m.id === selectedModeId) ?? null,
    [modes, selectedModeId],
  )

  const mergedSteps: RunStep[] = useMemo(() => {
    if (!activeRun) return []
    const map = new Map<string, RunStep>()
    for (const st of activeRun.steps) {
      map.set(st.nodeId, st)
    }
    if (liveRunSteps) {
      for (const live of Object.values(liveRunSteps)) {
        const prev = map.get(live.nodeId)
        map.set(live.nodeId, {
          runId: live.runId,
          nodeId: live.nodeId,
          path: live.path || prev?.path || '',
          alias: live.alias ?? prev?.alias,
          status: live.status,
          error: live.error ?? prev?.error,
          note: live.note ?? prev?.note,
          startedAt: live.startedAt ?? prev?.startedAt,
          finishedAt: live.finishedAt ?? prev?.finishedAt,
        })
      }
    }
    return Array.from(map.values()).sort((a, b) => a.path.localeCompare(b.path))
  }, [activeRun, liveRunSteps])

  const effectiveRunStatus: RunStatus | null = activeRun
    ? liveRunStatus ?? activeRun.status
    : null

  const effectiveRunReason: string | null = useMemo(() => {
    if (activeRun?.reason) return activeRun.reason
    const guardStep = mergedSteps.find((s) => s.nodeId === '__guard__' && s.error)
    if (guardStep?.error) return guardStep.error
    if (effectiveRunStatus === 'failed') {
      const failedStep = mergedSteps.find((s) => s.status === 'failed' && s.error)
      if (failedStep?.error) return failedStep.error
    }
    return null
  }, [activeRun?.reason, mergedSteps, effectiveRunStatus])

  const activeRunModeName = useMemo(() => {
    if (!activeRun?.modeId) return null
    return modes.find((m) => m.id === activeRun.modeId)?.name ?? null
  }, [activeRun, modes])

  const handleTrigger = async (mode: Mode) => {
    setError(null)
    setBusyModeId(mode.id)
    setSelectedModeId(mode.id)
    try {
      const requestId =
        typeof crypto !== 'undefined' && 'randomUUID' in crypto
          ? crypto.randomUUID()
          : undefined
      const run = await runMode(mode.id, requestId)
      setActiveRun(run)
      seedRun(run)
      sessionStorage.setItem(LAST_RUN_KEY, run.id)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : '触发场景执行失败')
    } finally {
      setBusyModeId(null)
    }
  }

  const handleCancel = async () => {
    if (!activeRun) return
    setCancelling(true)
    setError(null)
    try {
      const updated = await cancelRun(activeRun.id)
      setActiveRun(updated)
      seedRun(updated)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : '取消执行失败')
    } finally {
      setCancelling(false)
    }
  }

  const handleSaveEnv = async () => {
    setSavingEnv(true)
    setError(null)
    try {
      const trimmed = outdoorTempDraft.trim()
      const outdoorTemp = trimmed === '' ? undefined : Number(trimmed)
      if (outdoorTemp !== undefined && (Number.isNaN(outdoorTemp) || outdoorTemp < -40 || outdoorTemp > 60)) {
        setError('室外温度需在 -40 到 60℃ 之间（或留空表示未配置室外传感器）')
        return
      }
      const updated = await updateSimEnv({ season: seasonDraft, outdoorTemp })
      setSimEnv(updated)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : '更新仿真环境失败')
    } finally {
      setSavingEnv(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'rgba(15, 23, 42, 0.45)' }}
      role="dialog"
      aria-label="场景编排与运行时间线"
    >
      <div
        className="flex h-[80vh] w-full max-w-4xl flex-col overflow-hidden shadow-xl"
        style={{
          background: 'var(--color-surface, #ffffff)',
          border: '1px solid var(--color-border)',
          borderRadius: 'var(--radius-md, 8px)',
        }}
      >
        <header
          className="flex items-center justify-between px-4 py-3"
          style={{ borderBottom: '1px solid var(--color-border)' }}
        >
          <div>
            <h2 className="text-base font-semibold">场景模式与运行时间线</h2>
            <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
              一键触发内置场景，经由 WebSocket 实时回显每一步状态与耗时
            </p>
          </div>
          <button
            onClick={onClose}
            className="px-2.5 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          >
            关闭
          </button>
        </header>

        {simEnv ? (
          <div
            className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 text-xs"
            style={{
              background: 'var(--color-bg)',
              borderBottom: '1px solid var(--color-border)',
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">温控策略仿真环境：</span>
              <span
                className="px-2 py-0.5 font-medium"
                style={{ borderRadius: '999px', background: '#e0e7ff', color: '#3730a3' }}
              >
                当前生效：{EFFECTIVE_SEASON_LABEL[simEnv.effectiveSeason]}
              </span>
              {simEnv.outdoorTemp !== undefined ? (
                <span style={{ color: 'var(--color-muted)' }}>室外 {simEnv.outdoorTemp}℃</span>
              ) : (
                <span style={{ color: 'var(--color-muted)' }}>未配置室外温度</span>
              )}
            </div>
            {userRole === 'admin' ? (
              <div className="flex flex-wrap items-center gap-2">
                <select
                  aria-label="仿真季节"
                  value={seasonDraft}
                  onChange={(e) => setSeasonDraft(e.target.value as SimSeason)}
                  className="px-2 py-1 text-xs"
                  style={{
                    border: '1px solid var(--color-border)',
                    borderRadius: 'var(--radius-sm)',
                    background: 'var(--color-surface, #fff)',
                  }}
                >
                  {(Object.keys(SEASON_LABEL) as SimSeason[]).map((k) => (
                    <option key={k} value={k}>
                      {SEASON_LABEL[k]}
                    </option>
                  ))}
                </select>
                <input
                  type="number"
                  step="0.5"
                  placeholder="室外℃（可选）"
                  value={outdoorTempDraft}
                  onChange={(e) => setOutdoorTempDraft(e.target.value)}
                  className="w-28 px-2 py-1 text-xs"
                  style={{
                    border: '1px solid var(--color-border)',
                    borderRadius: 'var(--radius-sm)',
                    background: 'var(--color-surface, #fff)',
                  }}
                />
                <button
                  type="button"
                  disabled={savingEnv}
                  onClick={() => void handleSaveEnv()}
                  className="px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50"
                  style={{
                    background: 'var(--color-primary, #4f6bed)',
                    borderRadius: 'var(--radius-sm)',
                  }}
                >
                  {savingEnv ? '应用中…' : '应用季节'}
                </button>
              </div>
            ) : null}
          </div>
        ) : null}

        {error ? (
          <div
            className="mx-4 mt-3 px-3 py-2 text-xs"
            style={{ background: '#fee2e2', color: '#991b1b', borderRadius: 'var(--radius-sm)' }}
          >
            {error}
          </div>
        ) : null}

        <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-12">
          {/* 左侧：场景列表 */}
          <section
            className="overflow-y-auto p-4 md:col-span-5"
            style={{ borderRight: '1px solid var(--color-border)' }}
          >
            <h3 className="mb-2 text-xs font-medium uppercase" style={{ color: 'var(--color-muted)' }}>
              内置场景（{modes.length}）
            </h3>
            {loading ? (
              <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                正在加载场景…
              </p>
            ) : modes.length === 0 ? (
              <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                暂无场景模式
              </p>
            ) : (
              <ul className="space-y-2">
                {modes.map((m) => {
                  const selected = m.id === selectedModeId
                  const busy = busyModeId === m.id
  return (
                    <li
                      key={m.id}
                      onClick={() => setSelectedModeId(m.id)}
                      className="flex cursor-pointer items-center justify-between gap-2 p-3 transition-colors"
                      style={{
                        border: `1px solid ${selected ? 'var(--color-primary)' : 'var(--color-border)'}`,
                        borderRadius: 'var(--radius-sm)',
                        background: selected ? 'var(--color-primary-weak)' : 'var(--color-bg)',
                      }}
                    >
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium">{m.name}</span>
                          <span
                            className="px-1.5 py-0.5 text-[10px]"
                            style={{
                              borderRadius: '4px',
                              background: '#e5e7eb',
                              color: '#374151',
                            }}
                          >
                            {RUN_MODE_LABEL[m.runMode]}
                          </span>
                        </div>
                        <p className="mt-0.5 text-xs" style={{ color: 'var(--color-muted)' }}>
                          {m.definition.steps.length} 个步骤
                          {m.definition.guard ? ' · 含守卫条件' : ''}
                        </p>
                      </div>
                      <button
                        type="button"
                        disabled={busy || !m.enabled}
                        onClick={(e) => {
                          e.stopPropagation()
                          void handleTrigger(m)
                        }}
                        className="shrink-0 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
                        style={{
                          background: 'var(--color-primary, #4f6bed)',
                          borderRadius: 'var(--radius-sm)',
                        }}
                      >
                        {busy ? '启动中…' : '执行'}
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}

            {selectedMode ? (
              <div className="mt-4 pt-3" style={{ borderTop: '1px solid var(--color-border)' }}>
                <h4 className="mb-1.5 text-xs font-medium" style={{ color: 'var(--color-muted)' }}>
                  「{selectedMode.name}」步骤预览
                </h4>
                <ol className="space-y-1 text-xs">
                  {selectedMode.definition.steps.map((st, idx) => (
                    <li key={st.nodeId} className="flex items-center justify-between gap-2 py-0.5">
                      <span className="truncate">
                        {idx + 1}. {st.alias || st.nodeId}
                      </span>
                      <span style={{ color: 'var(--color-muted)' }}>
                        {st.type === 'delay' ? `延时 ${st.ms ?? 0}ms` : st.type}
                      </span>
                    </li>
                  ))}
                </ol>
              </div>
            ) : null}
          </section>

          {/* 右侧：实时运行时间线 */}
          <section className="flex min-h-0 flex-col overflow-y-auto p-4 md:col-span-7">
            <div className="mb-3 flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-medium">
                  运行时间线{activeRunModeName ? ` · ${activeRunModeName}` : ''}
                </h3>
                {effectiveRunStatus ? (
                  <span
                    className="px-2 py-0.5 text-xs font-medium"
                    style={{
                      borderRadius: '999px',
                      background: RUN_STATUS_LABEL[effectiveRunStatus].bg,
                      color: RUN_STATUS_LABEL[effectiveRunStatus].fg,
                    }}
                  >
                    {RUN_STATUS_LABEL[effectiveRunStatus].text}
                  </span>
                ) : null}
              </div>
              {activeRun && effectiveRunStatus === 'running' ? (
                <button
                  type="button"
                  disabled={cancelling}
                  onClick={() => void handleCancel()}
                  className="px-2.5 py-1 text-xs font-medium disabled:opacity-50"
                  style={{
                    border: '1px solid #ef4444',
                    color: '#dc2626',
                    borderRadius: 'var(--radius-sm)',
                    background: '#fff',
                  }}
                >
                  {cancelling ? '取消中…' : '取消执行'}
                </button>
              ) : null}
            </div>

            {!activeRun ? (
              <div
                className="flex flex-1 items-center justify-center text-xs"
                style={{ color: 'var(--color-muted)' }}
              >
                点击左侧任意场景的「执行」按钮，这里会实时显示每一步的执行进度与耗时。
              </div>
            ) : (
              <>
                {effectiveRunReason ? (
                  <div
                    className="mb-3 px-3 py-2 text-xs"
                    style={{
                      background: effectiveRunStatus === 'failed' ? '#fee2e2' : '#fef3c7',
                      color: effectiveRunStatus === 'failed' ? '#991b1b' : '#92400e',
                      borderRadius: 'var(--radius-sm)',
                    }}
                  >
                    {effectiveRunReason}
                  </div>
                ) : null}
                <ol className="space-y-2">
                  {mergedSteps.map((st, idx) => {
                    const badge = STEP_STATUS_LABEL[st.status]
                    const dur = formatDuration(st.startedAt, st.finishedAt)
                    const depth = Math.max(0, st.path.split('.').length - 1)
  return (
                      <li
                        key={st.nodeId}
                        className="p-3"
                        style={{
                          marginLeft: `${depth * 18}px`,
                          border: '1px solid var(--color-border)',
                          borderRadius: 'var(--radius-sm)',
                          background: 'var(--color-bg)',
                        }}
                      >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="text-xs font-mono" style={{ color: 'var(--color-muted)' }}>
                            #{idx + 1}
                          </span>
                          <span className="truncate text-sm font-medium">
                            {st.alias || st.nodeId}
                          </span>
                        </div>
                        <div className="flex shrink-0 items-center gap-2">
                          {dur ? (
                            <span className="text-xs font-mono" style={{ color: 'var(--color-muted)' }}>
                              {dur}
                            </span>
                          ) : null}
                          <span
                            className="px-2 py-0.5 text-xs font-medium"
                            style={{
                              borderRadius: '999px',
                              background: badge.bg,
                              color: badge.fg,
                            }}
                          >
                            {badge.text}
                          </span>
                        </div>
                      </div>
                      {st.note ? (
                        <p
                          className="mt-1.5 px-2 py-1 text-xs"
                          style={{
                            background: '#eff6ff',
                            color: '#1e40af',
                            borderRadius: '4px',
                          }}
                        >
                          调节依据：{st.note}
                        </p>
                      ) : null}
                      {st.error && st.error !== st.note ? (
                        <p className="mt-1 text-xs" style={{ color: st.status === 'failed' ? '#991b1b' : 'var(--color-muted)' }}>
                          {st.error}
                        </p>
                      ) : null}
                    </li>
                  )
                })}
                </ol>
              </>
            )}
          </section>
        </div>
      </div>
    </div>
  )
}
