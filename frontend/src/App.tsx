import { useCallback, useEffect, useState } from 'react'
import { Toaster } from 'sonner'
import { ApiError, getMe, type SessionUser } from './api/client.ts'
import { TooltipProvider } from './components/ui/tooltip.tsx'
import LoginPage from './pages/LoginPage.tsx'
import PlanPage from './pages/PlanPage.tsx'
import WallPage from './pages/WallPage.tsx'

type Phase = 'booting' | 'anonymous' | 'authenticated'

/**
 * 应用外壳：
 * - /wall 走免登录只读挂墙屏（FR-4.7），不触碰会话接口；
 * - 其余路径启动时问一次「我是谁」，401 进登录页。
 */
export default function App() {
  const isWallRoute = typeof window !== 'undefined' && window.location.pathname === '/wall'
  const [phase, setPhase] = useState<Phase>('booting')
  const [user, setUser] = useState<SessionUser | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  useEffect(() => {
    if (isWallRoute) return
    let cancelled = false
    getMe()
      .then((me) => {
        if (cancelled) return
        setUser(me)
        setPhase('authenticated')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 401) {
          setPhase('anonymous')
          return
        }
        setNotice('无法联系后端服务，请确认已运行 start.sh')
        setPhase('anonymous')
      })
    return () => {
      cancelled = true
    }
  }, [])

  const onSignedIn = useCallback((me: SessionUser) => {
    setUser(me)
    setPhase('authenticated')
  }, [])

  const onSignedOut = useCallback(() => {
    setUser(null)
    setPhase('anonymous')
  }, [])

  return (
    <TooltipProvider delayDuration={250}>
      {isWallRoute ? (
        <WallPage />
      ) : phase === 'booting' ? (
        <main className="flex h-dvh flex-col items-center justify-center gap-3 bg-bg-app text-text-muted">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-border-strong border-t-accent" />
          <p className="text-sm">正在连接智能家居中枢…</p>
        </main>
      ) : phase === 'anonymous' || !user ? (
        <LoginPage onSignedIn={onSignedIn} notice={notice} />
      ) : (
        <PlanPage user={user} onSignedOut={onSignedOut} />
      )}
      <Toaster position="bottom-right" richColors closeButton />
    </TooltipProvider>
  )
}
