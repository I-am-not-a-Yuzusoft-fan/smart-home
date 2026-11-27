import {
  Download,
  Grid3X3,
  Image,
  Maximize2,
  Minus,
  Plus,
  Redo2,
  Ruler,
  Spline,
  Undo2,
} from 'lucide-react'
import { Button } from '../ui/button.tsx'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip.tsx'
import type { WorkspaceMode } from './AppHeader.tsx'

type Props = {
  mode: WorkspaceMode
  scale: number
  gridSnap: boolean
  gridCm: number
  showDims: boolean
  arcMode: boolean
  showUnderlay: boolean
  canUndo: boolean
  canRedo: boolean
  saving: boolean
  onZoomIn: () => void
  onZoomOut: () => void
  onFit: () => void
  onToggleGrid: () => void
  onToggleDims: () => void
  onToggleArc: () => void
  onToggleUnderlay: () => void
  onUndo: () => void
  onRedo: () => void
  onExportPng: () => void
}

/**
 * 画布底部悬浮胶囊控制条：
 * 通用：缩放 / 全览 / 网格 / 尺寸标注 / 导出 PNG
 * 编辑模式附加：画弧 / 底图定标 / 撤销 / 重做
 */
export default function CanvasFloatingBar({
  mode,
  scale,
  gridSnap,
  gridCm,
  showDims,
  arcMode,
  showUnderlay,
  canUndo,
  canRedo,
  saving,
  onZoomIn,
  onZoomOut,
  onFit,
  onToggleGrid,
  onToggleDims,
  onToggleArc,
  onToggleUnderlay,
  onUndo,
  onRedo,
  onExportPng,
}: Props) {
  return (
    <div
      className="surface-glass pointer-events-auto absolute bottom-4 left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 rounded-full border px-2 py-1.5 shadow-[var(--shadow-md)]"
      onMouseDown={(e) => e.stopPropagation()}
    >
      {mode === 'edit' ? (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant={arcMode ? 'soft' : 'ghost'}
                size="xs"
                disabled={saving}
                onClick={onToggleArc}
                className="rounded-full"
              >
                <Spline className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">画弧</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>画弧墙：拖出弦后移动鼠标定弧度，单击落定</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant={showUnderlay ? 'soft' : 'ghost'}
                size="xs"
                onClick={onToggleUnderlay}
                className="rounded-full"
              >
                <Image className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">底图</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>上传户型参考底图与两点定标</TooltipContent>
          </Tooltip>

          <div className="mx-0.5 h-4 w-px bg-border-default" />

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 rounded-full"
                disabled={!canUndo || saving}
                onClick={onUndo}
                aria-label="撤销"
              >
                <Undo2 className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>撤销（Ctrl/Cmd+Z）</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 rounded-full"
                disabled={!canRedo || saving}
                onClick={onRedo}
                aria-label="重做"
              >
                <Redo2 className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>重做（Ctrl/Cmd+Shift+Z）</TooltipContent>
          </Tooltip>

          <div className="mx-0.5 h-4 w-px bg-border-default" />
        </>
      ) : null}

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant={gridSnap ? 'soft' : 'ghost'}
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onToggleGrid}
            aria-label="切换网格吸附"
          >
            <Grid3X3 className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>网格吸附 {gridCm}cm（{gridSnap ? '已开' : '已关'}）</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant={showDims ? 'soft' : 'ghost'}
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onToggleDims}
            aria-label="切换尺寸标注"
          >
            <Ruler className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>建筑尺寸标注（{showDims ? '显示' : '隐藏'}）</TooltipContent>
      </Tooltip>

      <div className="mx-0.5 h-4 w-px bg-border-default" />

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onZoomOut}
            aria-label="缩小画布"
          >
            <Minus className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>缩小画布</TooltipContent>
      </Tooltip>

      <button
        type="button"
        onClick={onFit}
        className="min-w-[44px] cursor-pointer rounded-full px-1.5 py-0.5 text-center font-mono text-[11px] font-medium text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
        title="点击回到全览"
      >
        {Math.round(scale * 100)}%
      </button>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onZoomIn}
            aria-label="放大画布"
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>放大画布</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onFit}
            aria-label="适配全览"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>回到全览</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 rounded-full"
            onClick={onExportPng}
            aria-label="导出 PNG"
          >
            <Download className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>导出平面图 PNG</TooltipContent>
      </Tooltip>
    </div>
  )
}
