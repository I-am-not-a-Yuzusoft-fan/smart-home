import { AlertCircle, Home, KeyRound, Loader2, Shield, User } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { ApiError, login, type SessionUser } from '../api/client.ts'
import { Badge } from '../components/ui/badge.tsx'
import { Button } from '../components/ui/button.tsx'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card.tsx'
import { Input } from '../components/ui/input.tsx'
import { Label } from '../components/ui/label.tsx'

type Props = {
  onSignedIn: (user: SessionUser) => void
  notice: string | null
}

/**
 * 登录页：使用统一 Design Tokens 与表单原语，并提供演示账号一键填入。
 */
export default function LoginPage({ onSignedIn, notice }: Props) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const me = await login(username.trim(), password)
      toast.success(`欢迎回来，${me.displayName || me.username}`)
      onSignedIn(me)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.status === 401 ? '用户名或口令错误' : err.message)
      } else {
        setError('网络异常，请稍后重试')
      }
    } finally {
      setSubmitting(false)
    }
  }

  function fillDemo(user: string, pass: string) {
    setUsername(user)
    setPassword(pass)
    setError(null)
  }

  return (
    <main className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-bg-app px-4 py-8">
      {/* 柔和环境光晕背景 */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-32 left-1/2 h-96 w-[36rem] -translate-x-1/2 rounded-full bg-accent/10 blur-3xl"
      />

      <Card className="relative w-full max-w-md border-border-default/90 shadow-[var(--shadow-lg)]">
        <CardHeader className="space-y-3 pb-4">
          <div className="flex items-center justify-between">
            <div className="flex h-10 w-10 items-center justify-center rounded-[var(--radius-md)] bg-accent-soft text-accent">
              <Home className="h-5 w-5" />
            </div>
            <Badge variant="accent">智能家居可视化中枢</Badge>
          </div>
          <div>
            <CardTitle className="text-xl">欢迎登录</CardTitle>
            <CardDescription className="mt-1 text-sm">
              使用家庭账号登录，进入平面图实时控制与场景编排
            </CardDescription>
          </div>
        </CardHeader>

        <CardContent className="space-y-5">
          {notice && (
            <div
              role="status"
              className="flex items-start gap-2 rounded-[var(--radius-sm)] border border-status-warning/30 bg-status-warning-soft px-3 py-2.5 text-xs text-status-warning"
            >
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{notice}</span>
            </div>
          )}

          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="login-username">用户名</Label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" />
                <Input
                  id="login-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  placeholder="请输入家庭账号"
                  required
                  className="pl-9"
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="login-password">口令</Label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" />
                <Input
                  id="login-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  placeholder="请输入登录口令"
                  required
                  className="pl-9"
                />
              </div>
            </div>

            {error && (
              <div
                role="alert"
                className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-status-danger/25 bg-status-danger-soft px-3 py-2 text-xs text-status-danger"
              >
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <Button type="submit" variant="default" size="lg" className="w-full" disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  登录中…
                </>
              ) : (
                '进入智能家居工作台'
              )}
            </Button>
          </form>

          <div className="space-y-2 border-t border-border-default pt-4">
            <p className="text-xs font-medium text-text-muted">演示账号（点击一键填入）</p>
            <div className="grid grid-cols-2 gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => fillDemo('zhang', 'demo1234')}
                className="justify-start"
              >
                <Shield className="h-3.5 w-3.5 text-accent" />
                <span>管理员 (zhang)</span>
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => fillDemo('li', 'demo1234')}
                className="justify-start"
              >
                <User className="h-3.5 w-3.5 text-text-muted" />
                <span>家庭成员 (li)</span>
              </Button>
            </div>
            <div className="pt-1 text-center">
              <a
                href="/wall"
                className="text-xs text-accent hover:underline"
              >
                免登录进入只读挂墙大屏 (/wall) →
              </a>
            </div>
          </div>
        </CardContent>
      </Card>
    </main>
  )
}
