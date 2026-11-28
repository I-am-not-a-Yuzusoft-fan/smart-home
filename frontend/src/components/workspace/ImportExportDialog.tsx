import { AlertCircle, CheckCircle2, Download, FileSpreadsheet, FileUp, Loader2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { toast } from 'sonner'
import { exportItemsUrl, importItems, type ImportReport } from '../../api/client.ts'
import { Badge } from '../ui/badge.tsx'
import { Button } from '../ui/button.tsx'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../ui/dialog.tsx'
import { Label } from '../ui/label.tsx'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImported: () => Promise<void> | void
}

/**
 * F1 物品批量导入导出弹窗（JSON / CSV，支持 skip / overwrite 冲突策略与逐行报告）。
 */
export default function ImportExportDialog({ open, onOpenChange, onImported }: Props) {
  const [mode, setMode] = useState<'skip' | 'overwrite'>('skip')
  const [busy, setBusy] = useState(false)
  const [report, setReport] = useState<ImportReport | null>(null)
  const [fileName, setFileName] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  async function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const format: 'json' | 'csv' = file.name.toLowerCase().endsWith('.csv') ? 'csv' : 'json'
    setFileName(file.name)
    setBusy(true)
    setReport(null)
    try {
      const content = await file.text()
      const res = await importItems({ format, mode, content })
      setReport(res)
      if (res.created > 0 || res.updated > 0) {
        await onImported()
      }
      toast.success(`导入完成：新增 ${res.created}，覆盖 ${res.updated}，跳过 ${res.skipped}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '导入失败')
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg space-y-5">
        <DialogHeader className="mb-0">
          <DialogTitle>物品批量导入与导出</DialogTitle>
          <DialogDescription>
            支持完整 JSON 与 Excel 兼容 CSV（UTF-8 BOM），方便批量迁移与备份户型物品
          </DialogDescription>
        </DialogHeader>

        {/* 导出区 */}
        <section className="space-y-2 rounded-[var(--radius-md)] border border-border-default bg-bg-subtle/40 p-3.5">
          <h3 className="text-xs font-semibold text-text-secondary">批量导出当前户型物品</h3>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" onClick={() => window.open(exportItemsUrl('json'), '_blank')}>
              <Download className="h-3.5 w-3.5" />
              导出 JSON（完整结构）
            </Button>
            <Button variant="outline" size="sm" onClick={() => window.open(exportItemsUrl('csv'), '_blank')}>
              <FileSpreadsheet className="h-3.5 w-3.5" />
              导出 CSV（表格兼容）
            </Button>
          </div>
        </section>

        {/* 导入区 */}
        <section className="space-y-3 rounded-[var(--radius-md)] border border-border-default bg-bg-surface p-3.5">
          <h3 className="text-xs font-semibold text-text-secondary">批量导入物品</h3>

          <div className="space-y-1.5">
            <Label htmlFor="import-conflict-mode">同名物品冲突策略</Label>
            <select
              id="import-conflict-mode"
              value={mode}
              onChange={(e) => setMode(e.target.value as 'skip' | 'overwrite')}
              className="h-8 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-2.5 text-xs text-text-primary outline-none focus:border-accent"
            >
              <option value="skip">跳过已存在的同名物品（默认，安全）</option>
              <option value="overwrite">覆盖同名物品的房间、坐标与旋转</option>
            </select>
          </div>

          <input
            ref={fileRef}
            type="file"
            accept=".json,.csv,application/json,text/csv"
            onChange={(e) => void handleFileChange(e)}
            className="hidden"
          />

          <div className="flex items-center gap-2">
            <Button
              variant="default"
              size="sm"
              disabled={busy}
              onClick={() => fileRef.current?.click()}
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileUp className="h-3.5 w-3.5" />}
              {busy ? '正在导入…' : '选择 JSON / CSV 文件并导入'}
            </Button>
            {fileName ? <span className="truncate text-xs text-text-muted">{fileName}</span> : null}
          </div>

          {report ? (
            <div className="space-y-2 rounded-[var(--radius-sm)] border border-border-default bg-bg-subtle/50 p-3 text-xs">
              <div className="flex flex-wrap items-center gap-2">
                <CheckCircle2 className="h-4 w-4 text-status-success" />
                <Badge variant="success">新增 {report.created}</Badge>
                <Badge variant="accent">覆盖 {report.updated}</Badge>
                <Badge variant="secondary">跳过 {report.skipped}</Badge>
                {(report.errors ?? []).length > 0 ? (
                  <Badge variant="danger">失败 {(report.errors ?? []).length}</Badge>
                ) : null}
              </div>

              {(report.errors ?? []).length > 0 ? (
                <ul className="max-h-32 space-y-1 overflow-y-auto border-t border-border-default pt-2 text-[11px] text-status-danger">
                  {(report.errors ?? []).map((err, i) => (
                    <li key={i} className="flex items-start gap-1.5">
                      <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
                      <span>
                        {err.row > 0 ? `第 ${err.row} 行：` : ''}
                        {err.message}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
        </section>
      </DialogContent>
    </Dialog>
  )
}
