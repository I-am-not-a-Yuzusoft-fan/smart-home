import { describe, expect, it } from 'vitest'
import {
  axisSegments,
  collinearRuns,
  outerRuns,
  outerRunsInBounds,
  polygonCentroid,
  roomExtent,
  type DimWall,
} from './dimensions'

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

describe('roomExtent', () => {
  const rect = (w: number, h: number) => [
    { x: 0, y: 0 },
    { x: w, y: 0 },
    { x: w, y: h },
    { x: 0, y: h },
  ]

  it('轴对齐矩形就是宽×深', () => {
    expect(roomExtent(rect(400, 300))).toEqual({ width: 400, height: 300, angle: 0 })
  })

  it('顶点顺序反过来也一样', () => {
    expect(roomExtent(rect(400, 300).reverse())).toEqual({ width: 400, height: 300, angle: 0 })
  })

  it('纵长矩形报 300×400，宽就是水平方向的开间', () => {
    expect(roomExtent(rect(300, 400))).toEqual({ width: 300, height: 400, angle: 0 })
  })

  it('斜放的房间报真实边长而不是包围盒', () => {
    const a = (30 * Math.PI) / 180
    const pts = rect(400, 300).map((p) => ({
      x: p.x * Math.cos(a) - p.y * Math.sin(a),
      y: p.x * Math.sin(a) + p.y * Math.cos(a),
    }))
    const e = roomExtent(pts)!
    expect(e.width).toBeCloseTo(400, 6)
    expect(e.height).toBeCloseTo(300, 6)
    expect(e.angle).toBeCloseTo(30, 6)
  })

  it('L 形用最小矩形，不把凹口算进去', () => {
    // 600×600 的 L：切掉右上 300×300，最小矩形是 600×600；包围盒也是 600×600，
    // 但斜 45° 后包围盒会胀到 848，最小矩形不会。
    const l = [
      { x: 0, y: 0 },
      { x: 600, y: 0 },
      { x: 600, y: 300 },
      { x: 300, y: 300 },
      { x: 300, y: 600 },
      { x: 0, y: 600 },
    ]
    const e = roomExtent(l)!
    expect(e.width * e.height).toBeLessThan(600 * 600 + 1)
  })

  it('点太少返回 null', () => {
    expect(roomExtent([{ x: 0, y: 0 }, { x: 1, y: 1 }])).toBeNull()
  })
})


describe('axisSegments：轴线尺寸（第二排链）', () => {
  it('矩形本身没有内墙时就是一段总长', () => {
    expect(axisSegments(rect, true, 0, 400)).toEqual([{ a: 0, b: 400, length: 400 }])
  })

  it('中间加一道竖隔墙，水平链断成两段开间', () => {
    const withPartition = [...rect, w('p', 250, 0, 250, 300)]
    // 3300 + 3300 那种读法：位置来自竖墙的墙中线，不是墙表面。
    expect(axisSegments(withPartition, true, 0, 400)).toEqual([
      { a: 0, b: 250, length: 250 },
      { a: 250, b: 400, length: 150 },
    ])
  })

  it('竖直链只看横墙', () => {
    const withPartition = [...rect, w('p', 0, 120, 400, 120)]
    expect(axisSegments(withPartition, false, 0, 300)).toEqual([
      { a: 0, b: 120, length: 120 },
      { a: 120, b: 300, length: 180 },
    ])
  })

  it('跨度外的墙不参与，重复轴线只算一次', () => {
    const extra = [
      ...rect,
      w('outside', 900, 0, 900, 300), // 超出 0–400 的跨度
      w('dup', 250, 300, 250, 0), // 与 p 同一根轴线，只是方向相反
      w('p', 250, 0, 250, 300),
    ]
    const segs = axisSegments(extra, true, 0, 400)
    expect(segs.map((s) => s.length)).toEqual([250, 150])
  })
})


describe('outerRunsInBounds：只认贴着同一条边的链', () => {
  const bounds = { minX: 0, minY: 0, maxX: 400, maxY: 300 }

  it('四条外轮廓都在', () => {
    const out = outerRunsInBounds(collinearRuns(rect), bounds)
    expect(out).toHaveLength(4)
  })

  it('两端顶着上下边界的室内隔墙不算外轮廓', () => {
    // 这道竖隔墙 (200,0)-(200,300) 的两个端点都落在包围盒边界上，
    // 但整条链不贴边——以前它会凭空多出一条「300」的总尺寸压在房间里。
    const withPartition = [...rect, w('p', 200, 0, 200, 300)]
    const out = outerRunsInBounds(collinearRuns(withPartition), bounds)
    expect(out).toHaveLength(4)
    expect(out.every((run) => run.wallIds.length === 1)).toBe(true)
    expect(out.some((run) => run.wallIds.includes('p'))).toBe(false)
  })
})

describe('polygonCentroid：异形房间的标签落点', () => {
  const tri = [{ x: 0, y: 0 }, { x: 120, y: 0 }, { x: 0, y: 200 }]

  it('三角形形心在内部', () => {
    const c = polygonCentroid(tri)
    expect(c.x).toBeCloseTo(40, 6)
    expect(c.y).toBeCloseTo(200 / 3, 6)
  })
})

describe('axisSegments 的短墙过滤', () => {
  const ring: DimWall[] = [
    { id: 'b', ax: 0, ay: 0, bx: 300, by: 0 },
    { id: 'r', ax: 300, ay: 0, bx: 300, by: 400 },
    { id: 't', ax: 300, ay: 400, bx: 0, by: 400 },
    { id: 'l', ax: 0, ay: 400, bx: 0, by: 0 },
    { id: 'stub', ax: 100, ay: 175, bx: 100, by: 225 },
  ]

  it('房间里的小隔断不参与定轴', () => {
    const segs = axisSegments(ring, true, 0, 300)
    expect(segs.map((s) => Math.round(s.length))).toEqual([300])
  })

  it('显式给出更小的下限时它会参与', () => {
    const segs = axisSegments(ring, true, 0, 300, 1, 10)
    expect(segs.map((s) => Math.round(s.length))).toEqual([100, 200])
  })
})
