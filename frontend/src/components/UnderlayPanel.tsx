import { useEffect, useState, type ChangeEvent, type FormEvent } from 'react'

import type { Underlay, UnderlayPatch } from '../api/client'
import { nextRotation } from '../features/plan/underlay.ts'
import { UNDERLAY_SAMPLES, type UnderlaySample } from '../features/plan/samples.ts'

type Props = {
  underlay: Underlay | null
  isAdmin: boolean
  busy: boolean
  onUpload: (file: File) => void
  onClear: () => void
  onClose: () => void
  calibrating: boolean
  calibCount: number
  calibMeasureCm: number | null
  onStartCalibration: () => void
  onCancelCalibration: () => void
  onCalibrate: (lengthCm: number) => void
  onPatch: (patch: UnderlayPatch) => void
  onSample: (sample: UnderlaySample) => void
}

/**
 * 底图面板：上传 / 替换 / 删除。
 *
 * 只做"有哪些图、用哪张"这件事；定标与显示参数在 #34/#35 里接到这里。
 */
export function UnderlayPanel({
  underlay,
  isAdmin,
  busy,
  onUpload,
  onClear,
  onClose,
  calibrating,
  calibCount,
  calibMeasureCm,
  onStartCalibration,
  onCancelCalibration,
  onCalibrate,
  onPatch,
  onSample,
}: Props) {
  const [length, setLength] = useState('')
  const [lengthError, setLengthError] = useState<string | null>(null)
  const [opacity, setOpacity] = useState(underlay?.opacity ?? 0.45)
  const [samplePath, setSamplePath] = useState('')

  // 底图换了（上传/删除/重新加载）就跟着服务端的值走，拖动过程中的本地值不覆盖。
  useEffect(() => {
    if (underlay) setOpacity(underlay.opacity)
  }, [underlay?.opacity, underlay?.sha256])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const value = Number(length)
    if (!Number.isFinite(value) || value <= 0) {
      setLengthError('真实长度要填一个大于 0 的数字')
      return
    }
    setLengthError(null)
    onCalibrate(value)
  }

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    // 选同一个文件两次不会再触发 change，所以清空 value。
    e.target.value = ''
    if (file) onUpload(file)
  }

  return (
    <section
      aria-label="底图"
      className="absolute left-3 top-3 z-10 w-72 max-w-[calc(100%-1.5rem)] p-3 text-sm"
      style={{
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        borderRadius: 'var(--radius-md)',
        boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
      }}
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">底图</h2>
        <button onClick={onClose} className="px-1 text-xs" style={{ color: 'var(--color-muted)' }}>
          关闭
        </button>
      </div>

      {underlay ? (
        <div className="space-y-2">
          <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
            {underlay.widthPx} × {underlay.heightPx} 像素 ·{' '}
            {underlay.contentType.replace('image/', '').toUpperCase()}
          </p>
          <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
            1 像素 = {underlay.cmPerPx.toFixed(2)} 厘米
          </p>
        </div>
      ) : (
        <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
          垫一张户型图在画布下面，照着描墙。支持 PNG / JPEG / WebP，不超过 5 MiB。
        </p>
      )}

      {underlay && isAdmin ? (
        <div className="mt-3 border-t pt-3" style={{ borderColor: 'var(--color-border)' }}>
          {calibrating ? (
            <div className="space-y-2">
              <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                {calibCount === 2
                  ? '两个点已选好，填它们的真实距离。'
                  : `在底图上点两个已知距离的点（已点 ${calibCount}/2）。`}
              </p>
              {calibCount === 2 && calibMeasureCm !== null ? (
                <form onSubmit={submit} className="space-y-2">
                  <p className="text-xs" style={{ color: 'var(--color-muted)' }}>
                    当前按图上量得 {calibMeasureCm} 厘米
                  </p>
                  <label className="flex items-center gap-2 text-xs">
                    真实长度
                    <input
                      type="number"
                      min="1"
                      step="1"
                      value={length}
                      onChange={(e) => setLength(e.target.value)}
                      placeholder={String(calibMeasureCm)}
                      className="w-20 px-1 py-0.5"
                      style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
                    />
                    厘米
                  </label>
                  {lengthError ? (
                    <p className="text-xs" style={{ color: 'var(--color-danger)' }}>
                      {lengthError}
                    </p>
                  ) : null}
                  <div className="flex gap-2">
                    <button
                      type="submit"
                      disabled={busy}
                      className="px-2 py-1 text-xs"
                      style={{
                        background: 'var(--color-primary)',
                        color: '#fff',
                        borderRadius: 'var(--radius-sm)',
                        opacity: busy ? 0.5 : 1,
                      }}
                    >
                      应用定标
                    </button>
                    <button
                      type="button"
                      onClick={onCancelCalibration}
                      className="px-2 py-1 text-xs"
                      style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
                    >
                      取消
                    </button>
                  </div>
                </form>
              ) : (
                <button
                  type="button"
                  onClick={onCancelCalibration}
                  className="px-2 py-1 text-xs"
                  style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
                >
                  退出定标
                </button>
              )}
            </div>
          ) : (
            <button
              type="button"
              onClick={onStartCalibration}
              className="px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: 'var(--color-bg)',
              }}
            >
              两点定标
            </button>
          )}
        </div>
      ) : null}

      {underlay ? (
        <div className="mt-3 space-y-2">
          <label className="flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={underlay.visible}
              disabled={!isAdmin || busy}
              onChange={(e) => onPatch({ visible: e.target.checked })}
            />
            显示底图
          </label>
          <label className="block text-xs" style={{ color: 'var(--color-muted)' }}>
            不透明度 {Math.round(opacity * 100)}%
            <input
              type="range"
              min="10"
              max="100"
              step="5"
              value={Math.round(opacity * 100)}
              disabled={!isAdmin || busy}
              className="mt-1 w-full"
              onChange={(e) => setOpacity(Number(e.target.value) / 100)}
              onPointerUp={() => onPatch({ opacity })}
              onKeyUp={() => onPatch({ opacity })}
            />
          </label>
          {isAdmin ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => onPatch({ rotationDeg: nextRotation(underlay.rotationDeg) })}
              className="px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: 'var(--color-bg)',
                opacity: busy ? 0.5 : 1,
              }}
            >
              旋转 90°（当前 {underlay.rotationDeg}°）
            </button>
          ) : null}
        </div>
      ) : null}

      {isAdmin ? (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <label
            className="cursor-pointer px-2 py-1 text-xs"
            style={{
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--color-bg)',
              opacity: busy ? 0.5 : 1,
            }}
          >
            {underlay ? '换一张' : '选择图片'}
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp"
              className="hidden"
              disabled={busy}
              onChange={pick}
            />
          </label>
          <label className="flex items-center gap-2 text-xs">
            <select
              value={samplePath}
              disabled={busy}
              onChange={(e) => setSamplePath(e.target.value)}
              className="max-w-[9rem] px-1 py-0.5"
              style={{ border: '1px solid var(--color-border)', borderRadius: 'var(--radius-sm)' }}
            >
              <option value="">示例户型图…</option>
              <optgroup label="合成图（现代 CAD 风格）">
                {UNDERLAY_SAMPLES.filter((s) => s.group === 'synthetic').map((s) => (
                  <option key={s.path} value={s.path}>
                    {s.label}
                  </option>
                ))}
              </optgroup>
              <optgroup label="档案扫描件（公有领域）">
                {UNDERLAY_SAMPLES.filter((s) => s.group === 'scan').map((s) => (
                  <option key={s.path} value={s.path}>
                    {s.label}
                  </option>
                ))}
              </optgroup>
            </select>
            <button
              type="button"
              disabled={busy || !samplePath}
              onClick={() => {
                const sample = UNDERLAY_SAMPLES.find((s) => s.path === samplePath)
                if (sample) onSample(sample)
              }}
              className="px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-border)',
                borderRadius: 'var(--radius-sm)',
                background: 'var(--color-bg)',
                opacity: busy || !samplePath ? 0.5 : 1,
              }}
            >
              垫示例图
            </button>
          </label>
          {underlay ? (
            <button
              onClick={onClear}
              disabled={busy}
              className="px-2 py-1 text-xs"
              style={{
                border: '1px solid var(--color-danger)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--color-danger)',
                background: 'transparent',
                opacity: busy ? 0.5 : 1,
              }}
            >
              删除底图
            </button>
          ) : null}
        </div>
      ) : (
        <p className="mt-3 text-xs" style={{ color: 'var(--color-muted)' }}>
          只有管理员能上传或更换底图。
        </p>
      )}
    </section>
  )
}
