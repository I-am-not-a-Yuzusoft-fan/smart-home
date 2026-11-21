import { useCallback, useEffect, useState } from 'react'
import { ApiError, getMe, type SessionUser } from './api/client.ts'
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

  if (phase === 'booting') {
    return (
      <main className="flex h-full items-center justify-center text-sm" style={{ color: 'var(--color-muted)' }}>
        正在载入…
      </main>
    )
  }

  if (phase === 'anonymous' || !user) {
    return <LoginPage onSignedIn={onSignedIn} notice={notice} />
  }

  return <PlanPage user={user} onSignedOut={onSignedOut} />
}
