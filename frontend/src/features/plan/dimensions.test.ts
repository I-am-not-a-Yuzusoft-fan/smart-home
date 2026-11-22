import { describe, expect, it } from 'vitest'
import { collinearRuns, outerRuns, type DimWall } from './dimensions'

/** 造一面墙，参数用「厘米」的世界坐标。 */
function w(id: string, ax: number, ay: number, bx: number, by: number): DimWall {
  return { id, ax, ay, bx, by }
}

/** 400 × 300 的矩形（左上角在原点，y 轴向下）。 */
const rect: DimWall[] = [
  w('n', 0, 0, 400, 0),
  w('e', 400, 0, 400, 300),
  w('s', 400, 300, 0, 300),
  w('w', 0, 300, 0, 0),
]

describe('collinearRuns', () => {
  it('矩形四条边各成一条链，互不合并', () => {
    const runs = collinearRuns(rect)
    expect(runs).toHaveLength(4)
    expect(runs.map((r) => Math.round(r.total)).sort((a, b) => a - b)).toEqual([300, 300, 400, 400])
    // 每条链只含一面墙
    expect(runs.every((r) => r.wallIds.length === 1)).toBe(true)
  })

  it('闭合矩形不会被「端点度为 2」判成没有链', () => {
    // 这是最容易写错的退化：用度数为 1 的端点求链两端，矩形会一条都推不出来
    expect(collinearRuns(rect)).not.toHaveLength(0)
  })

  it('共线且首尾相接的两段合并成一条 400 的链', () => {
    const runs = collinearRuns([w('a', 0, 0, 200, 0), w('b', 200, 0, 400, 0)])
    expect(runs).toHaveLength(1)
    expect(runs[0].total).toBeCloseTo(400, 6)
    expect(runs[0].wallIds.sort()).toEqual(['a', 'b'])
    expect(runs[0].horizontal).toBe(true)
  })

  it('两端顺序颠倒也能合并（端点相同但方向相反）', () => {
    const runs = collinearRuns([w('a', 0, 0, 200, 0), w('b', 400, 0, 200, 0)])
    expect(runs).toHaveLength(1)
    expect(runs[0].total).toBeCloseTo(400, 6)
  })

  it('垂直相接不合并', () => {
    const runs = collinearRuns([w('a', 0, 0, 200, 0), w('b', 200, 0, 200, 200)])
    expect(runs).toHaveLength(2)
  })

  it('共线但中间断开（不共享端点）不合并', () => {
    const runs = collinearRuns([w('a', 0, 0, 150, 0), w('b', 250, 0, 400, 0)])
    expect(runs).toHaveLength(2)
    expect(runs.map((r) => Math.round(r.total)).sort((x, y) => x - y)).toEqual([150, 150])
  })

  it('三段同一直线合并成一条总长', () => {
    const runs = collinearRuns([
      w('a', 0, 0, 100, 0),
      w('b', 100, 0, 250, 0),
      w('c', 250, 0, 400, 0),
    ])
    expect(runs).toHaveLength(1)
    expect(runs[0].total).toBeCloseTo(400, 6)
    expect(runs[0].wallIds).toHaveLength(3)
  })

  it('竖直墙的链标记为非水平', () => {
    const runs = collinearRuns([w('a', 0, 0, 0, 150), w('b', 0, 150, 0, 300)])
    expect(runs).toHaveLength(1)
    expect(runs[0].horizontal).toBe(false)
    expect(runs[0].total).toBeCloseTo(300, 6)
  })

  it('零长度墙被跳过', () => {
    expect(collinearRuns([w('a', 0, 0, 0, 0)])).toHaveLength(0)
  })

  it('没墙时返回空数组', () => {
    expect(collinearRuns([])).toEqual([])
  })

  it('长链排在前面', () => {
    const runs = collinearRuns([...rect, w('x', 0, 400, 600, 400)])
    expect(runs[0].total).toBeGreaterThanOrEqual(runs[runs.length - 1].total)
  })

  it('L 形：两条共线的横墙合并，竖墙各自独立', () => {
    const runs = collinearRuns([
      w('a', 0, 0, 200, 0),
      w('b', 200, 0, 400, 0),
      w('c', 400, 0, 400, 200),
      w('d', 0, 0, 0, 200),
    ])
    expect(runs).toHaveLength(3)
    expect(Math.round(runs[0].total)).toBe(400)
  })
})

describe('outerRuns', () => {
  it('矩形取最长的一条水平链与一条竖直链', () => {
    const picked = outerRuns(collinearRuns(rect))
    expect(picked).toHaveLength(2)
    expect(picked.some((r) => r.horizontal)).toBe(true)
    expect(picked.some((r) => !r.horizontal)).toBe(true)
  })

  it('空输入返回空', () => {
    expect(outerRuns([])).toEqual([])
  })
})
