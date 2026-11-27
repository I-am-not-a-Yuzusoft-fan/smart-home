import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../ui/dialog.tsx'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const SHORTCUTS: { keys: string[]; desc: string }[] = [
  { keys: ['['], desc: '折叠 / 展开左侧栏' },
  { keys: [']'], desc: '折叠 / 展开右侧面板' },
  { keys: ['?'], desc: '打开快捷键速查' },
  { keys: ['Esc'], desc: '取消当前选中 / 退出画弧 / 关闭抽屉' },
  { keys: ['Space', '拖拽'], desc: '平移户型画布（或中键拖拽）' },
  { keys: ['滚轮'], desc: '以鼠标为中心缩放画布' },
  { keys: ['Ctrl/Cmd', 'Z'], desc: '撤销户型修改（编辑模式）' },
  { keys: ['Ctrl/Cmd', 'Shift', 'Z'], desc: '重做户型修改（编辑模式）' },
  { keys: ['Shift'], desc: '画墙时解除正交吸附，自由角度' },
  { keys: ['Delete'], desc: '删除当前选中的墙体（编辑模式）' },
]

export default function ShortcutHelpDialog({ open, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>快捷键速查</DialogTitle>
          <DialogDescription>在画布与工作区中使用快捷键提升操控效率</DialogDescription>
        </DialogHeader>
        <ul className="divide-y divide-border-default">
          {SHORTCUTS.map((item) => (
            <li key={item.desc} className="flex items-center justify-between gap-4 py-2 text-xs">
              <span className="text-text-secondary">{item.desc}</span>
              <span className="flex shrink-0 items-center gap-1">
                {item.keys.map((k) => (
                  <kbd
                    key={k}
                    className="rounded border border-border-default bg-bg-subtle px-1.5 py-0.5 font-mono text-[11px] text-text-primary"
                  >
                    {k}
                  </kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  )
}
