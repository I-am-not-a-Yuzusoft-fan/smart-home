import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { calibrate } from './underlay'
import { UNDERLAY_SAMPLES, type UnderlaySample } from './samples'

/** 示例路径写错时，面板里点一下才发现 404 太晚——这里直接对着磁盘查。 */
const publicDir = fileURLToPath(new URL('../../../public', import.meta.url))

type IndexPlan = {
  file: string
  cm_per_px: number
  calibration: { p1_px: [number, number]; p2_px: [number, number]; length_cm: number }
}

const index = JSON.parse(
  readFileSync(`${publicDir}/underlays/samples/synthetic/index.json`, 'utf8'),
) as { plans: IndexPlan[] }

describe('底图示例', () => {
  it('每个示例路径都真的有文件', () => {
    for (const sample of UNDERLAY_SAMPLES) {
      const file = `${publicDir}${sample.path}`
      expect(existsSync(file), `缺少示例文件 ${sample.path}`).toBe(true)
    }
  })

  it('合成图与扫描件都各至少 4 张', () => {
    const synthetic = UNDERLAY_SAMPLES.filter((s) => s.group === 'synthetic')
    const scan = UNDERLAY_SAMPLES.filter((s) => s.group === 'scan')
    expect(synthetic.length).toBeGreaterThanOrEqual(4)
    expect(scan.length).toBeGreaterThanOrEqual(4)
  })

  // 验收口径（#36）：按图自带的定标点算出来的比例，要等于这张图生成时的真实比例。
  it('每张合成图自带的定标点都能还原出它的 cm/px（误差 < 0.1%）', () => {
    for (const plan of index.plans) {
      const p1 = { x: plan.calibration.p1_px[0], y: plan.calibration.p1_px[1] }
      const p2 = { x: plan.calibration.p2_px[0], y: plan.calibration.p2_px[1] }
      // 用一张任意初始变换：比例由两点距离与真实长度唯一确定，与初始值无关。
      const t = {
        widthPx: plan.cm_per_px > 0 ? Math.round(1600) : 0,
        heightPx: plan.cm_per_px > 0 ? Math.round(1600) : 0,
        cmPerPx: 1,
        offsetXCm: 0,
        offsetYCm: 0,
        rotationDeg: 0 as const,
      }
      const got = calibrate(p1, p2, { x: 0, y: 0 }, plan.calibration.length_cm, t).cmPerPx
      const rel = Math.abs(got - plan.cm_per_px) / plan.cm_per_px
      expect(rel, `${plan.file} 定标后比例偏差 ${(rel * 100).toFixed(3)}%`).toBeLessThan(0.001)
    }
  })

  it('示例图都是清单里登记过的合成图', () => {
    const known = new Set(index.plans.map((p) => `/underlays/samples/synthetic/${p.file}`))
    const synthetic = UNDERLAY_SAMPLES.filter((s) => s.group === 'synthetic')
    for (const sample of synthetic) {
      expect(known.has(sample.path), `${sample.path} 不在 index.json 里`).toBe(true)
    }
  })
})

export type { UnderlaySample }
