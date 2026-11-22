import { existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { UNDERLAY_SAMPLES } from './samples'

/** 示例路径写错时，面板里点一下才发现 404 太晚——这里直接对着磁盘查。 */
const publicDir = fileURLToPath(new URL('../../../public', import.meta.url))

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
})
