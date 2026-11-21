import { useState, type FormEvent } from 'react'
import { ApiError, login, type SessionUser } from '../api/client.ts'

type Props = {
  onSignedIn: (user: SessionUser) => void
  notice: string | null
}

/**
 * 登录页。错误提示分两种：口令错（401，后端统一说「用户名或口令错误」）
 * 与其他失败（网络、500）。密码框不自动填充，避免演示机上残留口令。
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
      onSignedIn(await login(username.trim(), password))
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

  return (
    <main className="flex h-full items-center justify-center px-6">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm space-y-4 p-8"
        style={{
          background: 'var(--color-surface)',
          border: '1px solid var(--color-border)',
          borderRadius: 'var(--radius-md)',
        }}
      >
        <header className="space-y-1">
          <h1 className="text-xl font-semibold">智能家居系统</h1>
          <p className="text-sm" style={{ color: 'var(--color-muted)' }}>
            用家庭账号登录，登录后进入平面图
          </p>
        </header>

        {notice && (
          <p
            className="text-sm"
            style={{ color: 'var(--color-danger)' }}
            role="status"
          >
            {notice}
          </p>
        )}

        <label className="block space-y-1">
          <span className="text-sm">用户名</span>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
            className="w-full px-3 py-2 text-sm outline-none"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          />
        </label>

        <label className="block space-y-1">
          <span className="text-sm">口令</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
            className="w-full px-3 py-2 text-sm outline-none"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
            }}
          />
        </label>

        {error && (
          <p className="text-sm" style={{ color: 'var(--color-danger)' }} role="alert">
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={submitting}
          className="w-full px-3 py-2 text-sm font-medium disabled:opacity-60"
          style={{
            background: 'var(--color-primary)',
            color: '#fff',
            borderRadius: 'var(--radius-sm)',
          }}
        >
          {submitting ? '登录中…' : '登录'}
        </button>

        <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
          演示账号：zhang / demo1234（管理员），li / demo1234（家庭成员）
        </p>
      </form>
    </main>
  )
}
