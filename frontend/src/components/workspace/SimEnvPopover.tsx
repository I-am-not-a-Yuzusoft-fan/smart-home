import { CloudSun, FastForward, Loader2, RotateCcw, Sliders, Thermometer } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import {
  advanceSimulation,
  getSimConfig,
  getSimEnv,
  updateSimConfig,
  updateSimEnv,
  type EffectiveSeason,
  type SimEnv,
  type SimSeason,
} from '../../api/client.ts'
import { Badge } from '../ui/badge.tsx'
import { Button } from '../ui/button.tsx'
import { Input } from '../ui/input.tsx'
import { Label } from '../ui/label.tsx'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover.tsx'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select.tsx'
import { Slider } from '../ui/slider.tsx'

const SEASON_LABEL: Record<SimSeason, string> = {
  auto: '自动（按月份）',
  spring: '春季',
  summer: '夏季',
  autumn: '秋季',
  winter: '冬季',
}

const EFFECTIVE_SEASON_LABEL: Record<EffectiveSeason, string> = {
  spring: '春季',
  summer: '夏季',
  autumn: '秋季',
  winter: '冬季',
}

type Props = {
  isAdmin: boolean
}

export default function SimEnvPopover({ isAdmin }: Props) {
  const [env, setEnv] = useState<SimEnv | null>(null)
  const [seasonDraft, setSeasonDraft] = useState<SimSeason>('auto')
  const [outdoorTempDraft, setOutdoorTempDraft] = useState('')
  const [delayMs, setDelayMs] = useState(100)
  const [failurePct, setFailurePct] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savingCfg, setSavingCfg] = useState(false)
  const [advancing, setAdvancing] = useState(false)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let cancelled = false
    getSimEnv()
      .then((data) => {
        if (cancelled) return
        setEnv(data)
        setSeasonDraft(data.season)
        setOutdoorTempDraft(data.outdoorTemp !== undefined ? String(data.outdoorTemp) : '')
      })
      .catch(() => {})
    if (isAdmin) {
      getSimConfig()
        .then((cfg) => {
          if (cancelled) return
          setDelayMs(cfg.delayMs)
          setFailurePct(Math.round(cfg.failureRate * 100))
        })
        .catch(() => {})
    }
    return () => {
      cancelled = true
    }
  }, [open, isAdmin])

  async function handleSaveEnv() {
    const trimmed = outdoorTempDraft.trim()
    const outdoorTemp = trimmed === '' ? undefined : Number(trimmed)
    if (outdoorTemp !== undefined && (Number.isNaN(outdoorTemp) || outdoorTemp < -40 || outdoorTemp > 60)) {
      toast.error('室外温度需在 -40℃ 到 60℃ 之间')
      return
    }
    setSaving(true)
    try {
      const updated = await updateSimEnv({ season: seasonDraft, outdoorTemp })
      setEnv(updated)
      toast.success(`仿真环境已更新为${EFFECTIVE_SEASON_LABEL[updated.effectiveSeason]}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '更新仿真环境失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleSaveSimConfig(nextDelay: number, nextFailurePct: number) {
    setSavingCfg(true)
    try {
      const updated = await updateSimConfig({
        delayMs: nextDelay,
        failureRate: Math.max(0, Math.min(1, nextFailurePct / 100)),
      })
      setDelayMs(updated.delayMs)
      setFailurePct(Math.round(updated.failureRate * 100))
      toast.success(`模拟器参数已更新：延迟 ${updated.delayMs}ms，失败率 ${Math.round(updated.failureRate * 100)}%`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '更新模拟器参数失败')
    } finally {
      setSavingCfg(false)
    }
  }

  async function handleAdvance() {
    setAdvancing(true)
    try {
      await advanceSimulation(60)
      toast.success('已推进模拟时间 60 分钟')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '推进模拟时间失败')
    } finally {
      setAdvancing(false)
    }
  }

  const seasonText = env ? EFFECTIVE_SEASON_LABEL[env.effectiveSeason] : '环境'
  const tempText = env?.outdoorTemp !== undefined ? `${env.outdoorTemp}℃` : '未设室外温'

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="gap-1.5 font-normal">
          <CloudSun className="h-3.5 w-3.5 text-accent" />
          <span className="font-medium text-text-primary">{seasonText}</span>
          <span className="hidden text-text-muted sm:inline">· {tempText}</span>
        </Button>
      </PopoverTrigger>

      <PopoverContent className="w-80 space-y-4">
        <div className="flex items-start justify-between gap-2">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">仿真环境与模拟器</h3>
            <p className="text-xs text-text-muted">驱动动态舒适温控与命令容错演示</p>
          </div>
          {env && <Badge variant="accent">{EFFECTIVE_SEASON_LABEL[env.effectiveSeason]}</Badge>}
        </div>

        {isAdmin ? (
          <>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label>仿真季节</Label>
                <Select value={seasonDraft} onValueChange={(v) => setSeasonDraft(v as SimSeason)}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(Object.keys(SEASON_LABEL) as SimSeason[]).map((k) => (
                      <SelectItem key={k} value={k}>
                        {SEASON_LABEL[k]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="sim-outdoor-temp">室外温度（℃，留空表示未配置）</Label>
                <div className="relative">
                  <Thermometer className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" />
                  <Input
                    id="sim-outdoor-temp"
                    type="number"
                    step="0.5"
                    placeholder="例如 32"
                    value={outdoorTempDraft}
                    onChange={(e) => setOutdoorTempDraft(e.target.value)}
                    className="h-8 pl-8 text-xs"
                  />
                </div>
              </div>

              <div className="flex gap-2">
                <Button
                  variant="default"
                  size="sm"
                  className="flex-1"
                  disabled={saving}
                  onClick={() => void handleSaveEnv()}
                >
                  {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                  应用环境
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={advancing}
                  onClick={() => void handleAdvance()}
                  title="向前推进仿真时钟 60 分钟"
                >
                  {advancing ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <FastForward className="h-3.5 w-3.5" />
                  )}
                  +60 分钟
                </Button>
              </div>
            </div>

            {/* FR-4.6 运行时模拟器容错配置 */}
            <div className="space-y-2.5 border-t border-border-default pt-3">
              <div className="flex items-center justify-between">
                <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-text-secondary">
                  <Sliders className="h-3.5 w-3.5 text-accent" />
                  模拟器延迟与故障率
                </span>
                <Button
                  variant="ghost"
                  size="xs"
                  disabled={savingCfg}
                  onClick={() => void handleSaveSimConfig(100, 0)}
                  title="恢复默认（100ms / 0%）"
                >
                  <RotateCcw className="h-3 w-3" />
                  重置
                </Button>
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-text-muted">命令延迟</span>
                  <span className="font-mono font-medium text-text-primary">{delayMs} ms</span>
                </div>
                <Slider
                  min={0}
                  max={5000}
                  step={50}
                  value={[delayMs]}
                  onValueChange={([v]) => setDelayMs(v)}
                />
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-text-muted">注入失败率</span>
                  <span className="font-mono font-medium text-text-primary">{failurePct}%</span>
                </div>
                <Slider
                  min={0}
                  max={100}
                  step={5}
                  value={[failurePct]}
                  onValueChange={([v]) => setFailurePct(v)}
                />
              </div>

              <Button
                variant="soft"
                size="xs"
                className="w-full"
                disabled={savingCfg}
                onClick={() => void handleSaveSimConfig(delayMs, failurePct)}
              >
                {savingCfg ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                应用模拟器参数
              </Button>
            </div>
          </>
        ) : (
          <div className="rounded-[var(--radius-sm)] bg-bg-subtle p-3 text-xs text-text-secondary">
            当前生效季节：<strong>{seasonText}</strong>
            <br />
            室外温度：{tempText}
            <p className="mt-1.5 text-[11px] text-text-muted">仅管理员可修改仿真季节与模拟器参数。</p>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
