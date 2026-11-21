import { useEffect, useRef, useState } from 'react'
import { Layer, Line, Stage, Text } from 'react-konva'
import { logout, type SessionUser } from '../api/client.ts'

type Props = {
  user: SessionUser
  onSignedOut: () => void
}

const GRID = 20

/**
 * 平面图主页（首屏）。
 *
 * T1 只承诺「能登录、能看到一个空平面图」。墙、房间、家具在 T2–T7 逐步长进来，
 * 所以这里先画一张栅格底图 + 一句下一步提示，避免假装已经有了功能。
 */
export default function PlanPage({ user, onSignedOut }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 800, height: 600 })
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      setSize({ width: Math.max(width, 320), height: Math.max(height, 240) })
    })
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  async function onLogout() {
    try {
      await logout()
      onSignedOut()
    } catch {
      setError('退出失败，请稍后重试')
    }
  }

  const gridLines = []
  for (let x = 0; x <= size.width; x += GRID) {
    gridLines.push(<Line key={`v${x}`} points={[x, 0, x, size.height]} stroke="#e8e8f2" strokeWidth={1} />)
  }
  for (let y = 0; y <= size.height; y += GRID) {
    gridLines.push(<Line key={`h${y}`} points={[0, y, size.width, y]} stroke="#e8e8f2" strokeWidth={1} />)
  }

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex items-center justify-between px-4 py-3"
        style={{ background: 'var(--color-surface)', borderBottom: '1px solid var(--color-border)' }}
      >
        <div className="flex items-baseline gap-3">
          <h1 className="text-base font-semibold">平面图</h1>
          <span className="text-xs" style={{ color: 'var(--color-muted)' }}>
            画墙、房间与布放将在后续版本补齐
          </span>
        </div>
        <div className="flex items-center gap-3 text-sm">
          <span>
            {user.displayName}
            <span className="ml-2 text-xs" style={{ color: 'var(--color-muted)' }}>
              {user.role === 'admin' ? '管理员' : '家庭成员'}
            </span>
          </span>
          <button
            onClick={onLogout}
            className="px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          >
            退出
          </button>
        </div>
      </header>

      {error && (
        <p className="px-4 py-2 text-sm" style={{ color: 'var(--color-danger)' }} role="alert">
          {error}
        </p>
      )}

      <main ref={hostRef} className="relative flex-1 overflow-hidden">
        <Stage width={size.width} height={size.height}>
          <Layer>{gridLines}</Layer>
          <Layer>
            <Text
              x={32}
              y={32}
              text="空地。T2 起可在这里拖出墙线，闭合后自动生成房间。"
              fontSize={14}
              fill="#6b6b80"
            />
          </Layer>
        </Stage>
      </main>
    </div>
  )
}
