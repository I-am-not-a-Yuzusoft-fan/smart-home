import { useCallback, useEffect, useState } from 'react'
import { Toaster } from 'sonner'
import { ApiError, getMe, type SessionUser } from './api/client.ts'
import { TooltipProvider } from './components/ui/tooltip.tsx'
import LoginPage from './pages/LoginPage.tsx'
import PlanPage from './pages/PlanPage.tsx'

type Phase = 'booting' | 'anonymous' | 'authenticated'

/**
 * 应用外壳：启动时问一次「我是谁」。
 * 401 就是没登录，其余错误当作需要重新登录并提示，避免白屏。
 */
export default function App() {
  const [phase, setPhase] = useState<Phase>('booting')
  const [user, setUser] = useState<SessionUser | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  useEffect(() => {
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
      {phase === 'booting' ? (
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
