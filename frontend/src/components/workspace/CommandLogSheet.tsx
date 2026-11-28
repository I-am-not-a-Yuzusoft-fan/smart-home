import { RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { listCommandLogs, type CommandLog, type Item } from '../../api/client.ts'
import { cn } from '../../lib/utils.ts'
import { Badge, type BadgeVariant } from '../ui/badge.tsx'
import { Button } from '../ui/button.tsx'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../ui/sheet.tsx'
import { Skeleton } from '../ui/skeleton.tsx'

type StatusFilter = 'all' | CommandLog['status']

const STATUS_META: Record<CommandLog['status'], { label: string; variant: BadgeVariant }> = {
  ok: { label: '成功', variant: 'success' },
  failed: { label: '失败', variant: 'danger' },
  timeout: { label: '超时', variant: 'warning' },
  rejected: { label: '拒绝', variant: 'danger' },
}

const SOURCE_LABEL: Record<CommandLog['source'], string> = {
  manual: '手动操作',
  automation: '场景/自动',
  device: '设备上报',
  timeout: '超时',
}

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  items: Item[]
}

/**
 * F4 操作日志只读抽屉（对接 GET /api/command-logs）。
 */
export default function CommandLogSheet({ open, onOpenChange, items }: Props) {
  const [logs, setLogs] = useState<CommandLog[]>([])
  const [loading, setLoading] = useState(false)
  const [filter, setFilter] = useState<StatusFilter>('all')

  const itemNameById = useMemo(() => new Map(items.map((it) => [it.id, it.name])), [items])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setLogs(await listCommandLogs(100))
    } catch {
      setLogs([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  const visible = useMemo(
    () => (filter === 'all' ? logs : logs.filter((l) => l.status === filter)),
    [logs, filter],
  )

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full max-w-md">
        <SheetHeader>
          <SheetTitle>操作与命令日志</SheetTitle>
          <SheetDescription>记录所有手动操控、场景下发与模拟器执行结果（保留 90 天）</SheetDescription>
        </SheetHeader>

        <div className="flex items-center justify-between gap-2 border-b border-border-default px-5 py-2.5">
          <div className="flex flex-wrap gap-1">
            {(
              [
                ['all', '全部'],
                ['ok', '成功'],
                ['failed', '失败'],
                ['timeout', '超时'],
                ['rejected', '拒绝'],
              ] as const
            ).map(([val, label]) => (
              <button
                key={val}
                type="button"
                onClick={() => setFilter(val)}
                className={cn(
                  'cursor-pointer rounded-full px-2.5 py-0.5 text-xs font-medium transition-colors',
                  filter === val
                    ? 'bg-accent-soft text-accent'
                    : 'text-text-muted hover:bg-bg-subtle hover:text-text-primary',
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => void load()} aria-label="刷新日志">
            <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
          </Button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-3">
          {loading && logs.length === 0 ? (
            <div className="space-y-2">
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
          ) : visible.length === 0 ? (
            <p className="py-10 text-center text-xs text-text-muted">暂无符合条件的操作记录</p>
          ) : (
            <ul className="space-y-2">
              {visible.map((log) => {
                const meta = STATUS_META[log.status]
                const devName = log.itemId ? itemNameById.get(log.itemId) ?? log.itemId.slice(0, 8) : '未知设备'
                return (
                  <li
                    key={log.id}
                    className="space-y-1.5 rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/40 p-2.5 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-medium text-text-primary">
                        {devName}
                        {log.capabilityKey ? ` · ${log.capabilityKey}` : ''}
                      </span>
                      <Badge variant={meta.variant}>{meta.label}</Badge>
                    </div>
                    <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-text-muted">
                      <span>
                        {SOURCE_LABEL[log.source]}
                        {log.target !== undefined ? ` → ${JSON.stringify(log.target)}` : ''}
                      </span>
                      <span className="font-mono">
                        {log.durationMs !== undefined ? `${log.durationMs}ms · ` : ''}
                        {new Date(log.createdAt).toLocaleTimeString('zh-CN')}
                      </span>
                    </div>
                    {log.error ? (
                      <p className="rounded bg-status-danger-soft px-2 py-1 text-[11px] text-status-danger">
                        {log.error}
                      </p>
                    ) : null}
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
