import { useEffect, useState } from 'react'

/**
 * 每 intervalMs 更新一次的「当前时间」，用来让「超过更新时间窗口」这类判断
 * 在页面静止时也能生效（否则只有用户操作触发重渲染时才会变陈旧）。
 */
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(timer)
  }, [intervalMs])
  return now
}
