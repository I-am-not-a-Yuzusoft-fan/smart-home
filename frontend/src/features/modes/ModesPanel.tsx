import {
  AlertCircle,
  CheckCircle2,
  Clock,
  Loader2,
  Pencil,
  Play,
  Plus,
  ShieldCheck,
  Sparkles,
  Square,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  cancelRun,
  deleteMode,
  getRun,
  listItems,
  listItemTypes,
  listModes,
  runMode,
  type Item,
  type ItemType,
  type Mode,
  type Run,
  type RunStatus,
  type RunStep,
  type RunStepStatus,
} from '../../api/client.ts'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../components/ui/alert-dialog.tsx'
import { Badge, type BadgeVariant } from '../../components/ui/badge.tsx'
import { Button } from '../../components/ui/button.tsx'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../components/ui/sheet.tsx'
import { Skeleton } from '../../components/ui/skeleton.tsx'
import { cn } from '../../lib/utils.ts'
import { useLiveStore } from '../../store/liveState.ts'
import ModeEditor from './ModeEditor.tsx'

const LAST_RUN_KEY = 'smarthome:lastRunId'

const RUN_STATUS_META: Record<RunStatus, { text: string; variant: BadgeVariant }> = {
  running: { text: '执行中', variant: 'accent' },
  ok: { text: '已完成', variant: 'success' },
  failed: { text: '失败', variant: 'danger' },
  cancelled: { text: '已取消', variant: 'secondary' },
}

const STEP_STATUS_META: Record<RunStepStatus, { text: string; variant: BadgeVariant }> = {
  pending: { text: '等待中', variant: 'secondary' },
  running: { text: '执行中', variant: 'accent' },
  ok: { text: '成功', variant: 'success' },
  failed: { text: '失败', variant: 'danger' },
  skipped: { text: '已跳过', variant: 'warning' },
}

function formatDuration(startedAt?: string, finishedAt?: string): string {
  if (!startedAt || !finishedAt) return ''
  const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime()
  if (Number.isNaN(ms) || ms < 0) return ''
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

type Props = {
  userRole: 'admin' | 'member'
  items?: Item[]
  itemTypes?: ItemType[]
}

/**
 * 右侧非遮罩「场景模式」Tab 面板：
 * - 一键启动场景时户型图完全可见并实时联动；
 * - 实时展示步骤进度与动态舒适温控 note；
 * - 仅在新建/编辑模式时从右侧滑出宽 Sheet。
 */
export default function ModesPanel({ userRole, items: propItems, itemTypes: propTypes }: Props) {
  const [modes, setModes] = useState<Mode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeRun, setActiveRun] = useState<Run | null>(null)
  const [busyModeId, setBusyModeId] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [fallbackItems, setFallbackItems] = useState<Item[]>([])
  const [fallbackTypes, setFallbackTypes] = useState<ItemType[]>([])
  const [editingMode, setEditingMode] = useState<Mode | 'new' | null>(null)
  const [deletingMode, setDeletingMode] = useState<Mode | null>(null)

  const items = propItems ?? fallbackItems
  const itemTypes = propTypes ?? fallbackTypes

  const typeMap = useMemo(() => new Map(itemTypes.map((t) => [t.key, t])), [itemTypes])
  const deviceItems = useMemo(
    () => items.filter((it) => typeMap.get(it.typeKey)?.kind === 'device'),
    [items, typeMap],
  )

  const liveRunSteps = useLiveStore((s) => (activeRun ? s.runSteps[activeRun.id] : undefined))
  const liveRunStatus = useLiveStore((s) => (activeRun ? s.runStatuses[activeRun.id] : undefined))
  const latestRunId = useLiveStore((s) => s.latestRunId)
  const wsConnected = useLiveStore((s) => s.connected)
  const seedRun = useLiveStore((s) => s.seedRun)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    listModes()
      .then((list) => {
        if (!cancelled) setModes(list)
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : '读取场景列表失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    if (userRole === 'admin' && (!propItems || !propTypes)) {
      Promise.all([listItems(), listItemTypes()])
        .then(([its, types]) => {
          if (cancelled) return
          setFallbackItems(its)
          setFallbackTypes(types)
        })
        .catch(() => {})
    }

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
  }, [seedRun, userRole, propItems, propTypes])

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

  const mergedSteps: RunStep[] = useMemo(() => {
    if (!activeRun) return []
    const map = new Map<string, RunStep>()
    for (const st of activeRun.steps) map.set(st.nodeId, st)
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

  const effectiveRunStatus: RunStatus | null = activeRun ? liveRunStatus ?? activeRun.status : null

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
    try {
      const requestId =
        typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : undefined
      const run = await runMode(mode.id, requestId)
      setActiveRun(run)
      seedRun(run)
      sessionStorage.setItem(LAST_RUN_KEY, run.id)
      toast.success(`已触发场景「${mode.name}」`)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : '触发场景执行失败'
      setError(msg)
      toast.error(msg)
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
      toast.info('已取消当前场景执行')
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : '取消执行失败'
      setError(msg)
      toast.error(msg)
    } finally {
      setCancelling(false)
    }
  }

  const confirmDelete = async () => {
    if (!deletingMode) return
    const target = deletingMode
    setDeletingMode(null)
    setError(null)
    try {
      await deleteMode(target.id)
      toast.success(`已删除场景「${target.name}」`)
      const list = await listModes()
      setModes(list)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : '删除场景失败'
      setError(msg)
      toast.error(msg)
    }
  }

  const handleModeSaved = async (saved: Mode) => {
    setEditingMode(null)
    try {
      const list = await listModes()
      setModes(list)
    } catch {
      setModes((prev) => {
        const exists = prev.some((m) => m.id === saved.id)
        return exists ? prev.map((m) => (m.id === saved.id ? saved : m)) : [...prev, saved]
      })
    }
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto p-3.5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <div>
          <h2 className="text-xs font-semibold text-text-secondary">一键场景模式</h2>
          <p className="text-[11px] text-text-muted">启动时画布保持可见并实时联动</p>
        </div>
        {userRole === 'admin' ? (
          <Button variant="soft" size="xs" onClick={() => setEditingMode('new')}>
            <Plus className="h-3.5 w-3.5" />
            新建
          </Button>
        ) : null}
      </div>

      {error ? (
        <div
          role="alert"
          className="mb-3 flex items-start gap-2 rounded-[var(--radius-sm)] border border-status-danger/25 bg-status-danger-soft px-3 py-2 text-xs text-status-danger"
        >
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{error}</span>
        </div>
      ) : null}

      {/* 场景卡片列表 */}
      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      ) : modes.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-[var(--radius-md)] border border-dashed border-border-strong bg-bg-subtle/40 px-4 py-7 text-center">
          <Sparkles className="mb-2 h-6 w-6 text-text-muted" />
          <p className="text-xs font-medium text-text-primary">暂无场景模式</p>
          <p className="mt-1 text-[11px] text-text-muted">
            创建你的第一个一键联动场景，如回家、观影或睡眠模式
          </p>
          {userRole === 'admin' ? (
            <Button variant="soft" size="xs" className="mt-3" onClick={() => setEditingMode('new')}>
              <Plus className="h-3.5 w-3.5" />
              新建场景
            </Button>
          ) : null}
        </div>
      ) : (
        <ul className="space-y-2">
          {modes.map((m) => {
            const busy = busyModeId === m.id
            const isActive = activeRun?.modeId === m.id && effectiveRunStatus === 'running'
            return (
              <li
                key={m.id}
                className={cn(
                  'rounded-[var(--radius-md)] border p-2.5 transition-colors',
                  isActive
                    ? 'border-accent bg-accent-soft/50'
                    : 'border-border-default bg-bg-surface hover:border-border-strong',
                )}
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-sm font-medium text-text-primary">{m.name}</span>
                      {!m.enabled ? <Badge variant="secondary">已停用</Badge> : null}
                    </div>
                    <p className="mt-0.5 flex items-center gap-1.5 text-[11px] text-text-muted">
                      <span>{m.definition.steps.length} 个步骤</span>
                      {m.definition.guard ? (
                        <span className="inline-flex items-center gap-0.5 text-accent">
                          · <ShieldCheck className="h-3 w-3" /> 含前提条件
                        </span>
                      ) : null}
                    </p>
                  </div>

                  <div className="flex shrink-0 items-center gap-1">
                    {userRole === 'admin' ? (
                      <>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={() => setEditingMode(m)}
                          aria-label={`编辑 ${m.name}`}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 text-status-danger hover:bg-status-danger-soft"
                          onClick={() => setDeletingMode(m)}
                          aria-label={`删除 ${m.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </>
                    ) : null}
                    <Button
                      variant={isActive ? 'soft' : 'default'}
                      size="xs"
                      disabled={busy || !m.enabled}
                      onClick={() => void handleTrigger(m)}
                    >
                      {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
                      {busy ? '启动中' : '执行'}
                    </Button>
                  </div>
                </div>
              </li>
            )
          })}
        </ul>
      )}

      {/* 实时运行时间线 */}
      <section className="mt-4 space-y-2.5 border-t border-border-default pt-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            <h3 className="truncate text-xs font-semibold text-text-secondary">
              运行进度{activeRunModeName ? ` · ${activeRunModeName}` : ''}
            </h3>
            {effectiveRunStatus ? (
              <Badge variant={RUN_STATUS_META[effectiveRunStatus].variant}>
                {RUN_STATUS_META[effectiveRunStatus].text}
              </Badge>
            ) : null}
          </div>
          {activeRun && effectiveRunStatus === 'running' ? (
            <Button
              variant="outline"
              size="xs"
              disabled={cancelling}
              onClick={() => void handleCancel()}
              className="border-status-danger/40 text-status-danger hover:bg-status-danger-soft"
            >
              <Square className="h-3 w-3" />
              {cancelling ? '取消中' : '停止'}
            </Button>
          ) : null}
        </div>

        {!activeRun ? (
          <div className="rounded-[var(--radius-sm)] bg-bg-subtle/60 px-3 py-4 text-center text-[11px] text-text-muted">
            点击上方任意场景的「执行」，这里会实时回显每一步状态与舒适温控依据。
          </div>
        ) : (
          <div className="space-y-2">
            {effectiveRunReason ? (
              <div
                className={cn(
                  'rounded-[var(--radius-sm)] border px-2.5 py-2 text-xs',
                  effectiveRunStatus === 'failed'
                    ? 'border-status-danger/25 bg-status-danger-soft text-status-danger'
                    : 'border-status-warning/30 bg-status-warning-soft text-status-warning',
                )}
              >
                {effectiveRunReason}
              </div>
            ) : null}

            <ol className="space-y-1.5">
              {mergedSteps.map((st, idx) => {
                const meta = STEP_STATUS_META[st.status]
                const dur = formatDuration(st.startedAt, st.finishedAt)
                const depth = Math.max(0, st.path.split('.').length - 1)
                return (
                  <li
                    key={st.nodeId}
                    style={{ marginLeft: `${depth * 12}px` }}
                    className="rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/40 p-2.5 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-1.5">
                        {st.status === 'ok' ? (
                          <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-status-success" />
                        ) : st.status === 'running' ? (
                          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
                        ) : (
                          <Clock className="h-3.5 w-3.5 shrink-0 text-text-muted" />
                        )}
                        <span className="truncate font-medium text-text-primary">
                          {idx + 1}. {st.alias || st.nodeId}
                        </span>
                      </div>
                      <div className="flex shrink-0 items-center gap-1.5">
                        {dur ? <span className="font-mono text-[10px] text-text-muted">{dur}</span> : null}
                        <Badge variant={meta.variant}>{meta.text}</Badge>
                      </div>
                    </div>

                    {st.note ? (
                      <p className="mt-1.5 rounded bg-accent-soft/70 px-2 py-1 text-[11px] text-accent">
                        {st.note}
                      </p>
                    ) : null}
                    {st.error && st.error !== st.note ? (
                      <p
                        className={cn(
                          'mt-1 text-[11px]',
                          st.status === 'failed' ? 'text-status-danger' : 'text-text-muted',
                        )}
                      >
                        {st.error}
                      </p>
                    ) : null}
                  </li>
                )
              })}
            </ol>
          </div>
        )}
      </section>

      {/* 右侧宽抽屉：新建/编辑场景模式 */}
      <Sheet open={editingMode !== null && userRole === 'admin'} onOpenChange={(open) => { if (!open) setEditingMode(null) }}>
        <SheetContent side="right" className="w-full max-w-[620px]">
          <SheetHeader>
            <SheetTitle>
              {editingMode && editingMode !== 'new' ? `编辑场景 · ${editingMode.name}` : '新建自定义场景'}
            </SheetTitle>
            <SheetDescription>
              使用自然语言步骤卡片编排前提条件、设备动作、等待与条件分支
            </SheetDescription>
          </SheetHeader>
          {editingMode !== null && userRole === 'admin' ? (
            <ModeEditor
              key={editingMode === 'new' ? 'new' : editingMode.id}
              initial={editingMode === 'new' ? null : editingMode}
              devices={deviceItems}
              typeMap={typeMap}
              onSaved={(saved) => void handleModeSaved(saved)}
              onCancel={() => setEditingMode(null)}
            />
          ) : null}
        </SheetContent>
      </Sheet>

      {/* 删除确认 AlertDialog */}
      <AlertDialog open={Boolean(deletingMode)} onOpenChange={(open) => { if (!open) setDeletingMode(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除场景「{deletingMode?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该场景将从列表中移除，历史运行记录保留只读。此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDelete()}>确认删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
