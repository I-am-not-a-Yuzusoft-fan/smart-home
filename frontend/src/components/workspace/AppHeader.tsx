import {
  ChevronDown,
  Home,
  Keyboard,
  LogOut,
  PanelLeft,
  PanelRight,
  PencilRuler,
  Settings2,
  Shield,
  SlidersHorizontal,
  User,
  Wifi,
  WifiOff,
} from 'lucide-react'
import type { SessionUser } from '../../api/client.ts'
import { cn } from '../../lib/utils.ts'
import { Badge } from '../ui/badge.tsx'
import { Button } from '../ui/button.tsx'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover.tsx'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip.tsx'
import SimEnvPopover from './SimEnvPopover.tsx'

export type WorkspaceMode = 'control' | 'edit'

type Props = {
  user: SessionUser
  mode: WorkspaceMode
  onModeChange: (mode: WorkspaceMode) => void
  liveConnected: boolean
  loading: boolean
  roomCount: number
  deviceCount: number
  leftOpen: boolean
  rightOpen: boolean
  onToggleLeft: () => void
  onToggleRight: () => void
  onOpenTypes: () => void
  onOpenShortcuts: () => void
  onLogout: () => void
}

/**
 * 52px 三段式顶栏：
 * 左：侧栏开关 + 品牌与状态 + WS 实时灯
 * 中：「运行控制 / 户型编辑」双模式切换胶囊（member 隐藏）
 * 右：仿真环境 Popover + 用户菜单（含角色徽章、快捷键速查、退出；预留 Phase B 主题切换槽位）
 */
export default function AppHeader({
  user,
  mode,
  onModeChange,
  liveConnected,
  loading,
  roomCount,
  deviceCount,
  leftOpen,
  rightOpen,
  onToggleLeft,
  onToggleRight,
  onOpenTypes,
  onOpenShortcuts,
  onLogout,
}: Props) {
  const isAdmin = user.role === 'admin'

  return (
    <header className="z-20 flex h-[52px] shrink-0 items-center justify-between gap-3 border-b border-border-default bg-bg-surface px-3">
      {/* 左段：品牌 + 左侧栏开关 + 实时连接灯 */}
      <div className="flex min-w-0 items-center gap-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={leftOpen ? 'soft' : 'ghost'}
              size="icon"
              onClick={onToggleLeft}
              aria-label="切换左侧栏"
            >
              <PanelLeft className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>切换左侧栏（快捷键 [ ）</TooltipContent>
        </Tooltip>

        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)] bg-accent-soft text-accent">
            <Home className="h-4 w-4" />
          </div>
          <div className="min-w-0 leading-tight">
            <h1 className="truncate text-sm font-semibold text-text-primary">智能家居中枢</h1>
            <p className="hidden truncate text-[11px] text-text-muted sm:block">
              {loading ? '读取户型中…' : `${roomCount} 间房 · ${deviceCount} 台设备`}
            </p>
          </div>
        </div>

        <Tooltip>
          <TooltipTrigger asChild>
            <span
              className={cn(
                'ml-1 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium',
                liveConnected
                  ? 'bg-status-success-soft text-status-success'
                  : 'bg-status-danger-soft text-status-danger',
              )}
            >
              {liveConnected ? <Wifi className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
              <span className="hidden md:inline">{liveConnected ? '实时同步' : '重连中'}</span>
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {liveConnected
              ? 'WebSocket 实时状态通道已连接'
              : '实时通道已断开，正在自动重连并同步快照'}
          </TooltipContent>
        </Tooltip>
      </div>

      {/* 中段：顶层双模式切换（仅管理员可见；member 默认锁定运行控制） */}
      {isAdmin ? (
        <nav
          aria-label="工作模式切换"
          className="inline-flex items-center rounded-full border border-border-default bg-bg-subtle p-0.5"
        >
          <button
            type="button"
            onClick={() => onModeChange('control')}
            className={cn(
              'inline-flex cursor-pointer items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-all',
              mode === 'control'
                ? 'bg-bg-surface text-accent shadow-[var(--shadow-sm)]'
                : 'text-text-muted hover:text-text-primary',
            )}
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
            <span>运行控制</span>
          </button>
          <button
            type="button"
            onClick={() => onModeChange('edit')}
            className={cn(
              'inline-flex cursor-pointer items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-all',
              mode === 'edit'
                ? 'bg-bg-surface text-accent shadow-[var(--shadow-sm)]'
                : 'text-text-muted hover:text-text-primary',
            )}
          >
            <PencilRuler className="h-3.5 w-3.5" />
            <span>户型编辑</span>
          </button>
        </nav>
      ) : (
        <div className="hidden sm:block">
          <Badge variant="secondary">家庭控制视图</Badge>
        </div>
      )}

      {/* 右段：仿真环境 Popover + 管理员类型管理（编辑态） + 右侧栏开关 + 用户菜单 */}
      <div className="flex shrink-0 items-center gap-2">
        <SimEnvPopover isAdmin={isAdmin} />

        {isAdmin && mode === 'edit' ? (
          <Button variant="outline" size="sm" onClick={onOpenTypes} className="hidden md:inline-flex">
            <Settings2 className="h-3.5 w-3.5" />
            <span>类型管理</span>
          </Button>
        ) : null}

        {/* Phase B 主题切换器预留槽位 */}

        <Popover>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="sm" className="gap-1.5 pl-2 pr-2">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-accent-soft text-accent">
                {isAdmin ? <Shield className="h-3.5 w-3.5" /> : <User className="h-3.5 w-3.5" />}
              </span>
              <span className="hidden max-w-[84px] truncate text-xs font-medium text-text-primary lg:inline">
                {user.displayName || user.username}
              </span>
              <ChevronDown className="h-3.5 w-3.5 text-text-muted" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-56 p-2">
            <div className="mb-2 border-b border-border-default px-2.5 pb-2.5 pt-1">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-semibold text-text-primary">
                  {user.displayName || user.username}
                </span>
                <Badge variant={isAdmin ? 'accent' : 'default'}>
                  {isAdmin ? '管理员' : '家庭成员'}
                </Badge>
              </div>
              <p className="mt-0.5 text-[11px] text-text-muted">@{user.username}</p>
            </div>
            <div className="flex flex-col gap-0.5">
              {isAdmin ? (
                <button
                  type="button"
                  onClick={onOpenTypes}
                  className="flex w-full cursor-pointer items-center gap-2 rounded-[var(--radius-sm)] px-2.5 py-1.5 text-left text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
                >
                  <Settings2 className="h-3.5 w-3.5" />
                  设备/家具类型管理
                </button>
              ) : null}
              <button
                type="button"
                onClick={onOpenShortcuts}
                className="flex w-full cursor-pointer items-center justify-between rounded-[var(--radius-sm)] px-2.5 py-1.5 text-left text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
              >
                <span className="inline-flex items-center gap-2">
                  <Keyboard className="h-3.5 w-3.5" />
                  快捷键速查
                </span>
                <kbd className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[10px] text-text-muted">?</kbd>
              </button>
              <button
                type="button"
                onClick={onLogout}
                className="flex w-full cursor-pointer items-center gap-2 rounded-[var(--radius-sm)] px-2.5 py-1.5 text-left text-xs text-status-danger hover:bg-status-danger-soft"
              >
                <LogOut className="h-3.5 w-3.5" />
                退出登录
              </button>
            </div>
          </PopoverContent>
        </Popover>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={rightOpen ? 'soft' : 'ghost'}
              size="icon"
              onClick={onToggleRight}
              aria-label="切换右侧面板"
            >
              <PanelRight className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>切换右侧面板（快捷键 ] ）</TooltipContent>
        </Tooltip>
      </div>
    </header>
  )
}
