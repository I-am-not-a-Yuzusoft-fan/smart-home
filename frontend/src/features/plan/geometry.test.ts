import { describe, expect, it } from 'vitest'

import {
  arcLength,
  arcPoints,
  arcPolyline,
  axisCross,
  clamp,
  clampWallOffset,
  distanceToSegment,
  nearestWallAnchor,
  pointInPolygon,
  projectOnSegment,
} from './geometry'

describe('clamp', () => {
  it('夹在区间内', () => {
    expect(clamp(-5, 0, 10)).toBe(0)
    expect(clamp(15, 0, 10)).toBe(10)
    expect(clamp(5, 0, 10)).toBe(5)
  })
})

describe('projectOnSegment', () => {
  const horizontal = { ax: 0, ay: 0, bx: 100, by: 0 }

  it('垂直投影落在段内', () => {
    const p = projectOnSegment(30, 20, horizontal)
    expect(p).toMatchObject({ x: 30, y: 0, t: 0.3 })
    expect(p.dist).toBeCloseTo(20)
  })

  it('投影落在段外时夹到端点', () => {
    const p = projectOnSegment(-40, 0, horizontal)
    expect(p.x).toBe(0)
    expect(p.t).toBe(0)
    expect(p.dist).toBeCloseTo(40)
  })

  it('退化成一个点时不除零', () => {
    const p = projectOnSegment(3, 4, { ax: 0, ay: 0, bx: 0, by: 0 })
    expect(p).toMatchObject({ x: 0, y: 0, t: 0 })
    expect(p.dist).toBeCloseTo(5)
  })

  it('distanceToSegment 与投影距离一致', () => {
    expect(distanceToSegment(30, 20, horizontal)).toBeCloseTo(20)
    expect(distanceToSegment(120, 0, horizontal)).toBeCloseTo(20)
  })
})

describe('axisCross', () => {
  const vertical = { ax: 400, ay: 0, bx: 400, by: 300 }

  it('水平轴与非水平线段求交', () => {
    expect(axisCross({ x: 0, y: 150 }, 'h', vertical)).toEqual({ x: 400, y: 150 })
  })

  it('竖轴与水平线段求交', () => {
    const horizontal = { ax: 0, ay: 100, bx: 300, by: 100 }
    expect(axisCross({ x: 200, y: 0 }, 'v', horizontal)).toEqual({ x: 200, y: 100 })
  })

  it('平行时不产生交点', () => {
    expect(axisCross({ x: 0, y: 150 }, 'h', { ax: 0, ay: 200, bx: 300, by: 200 })).toBeNull()
  })

  it('交点落在线段之外时返回 null', () => {
    expect(axisCross({ x: 0, y: 500 }, 'h', vertical)).toBeNull()
  })
})

describe('pointInPolygon', () => {
  const square = [
    { x: 0, y: 0 },
    { x: 100, y: 0 },
    { x: 100, y: 100 },
    { x: 0, y: 100 },
  ]

  it('判内部与外部', () => {
    expect(pointInPolygon({ x: 50, y: 50 }, square)).toBe(true)
    expect(pointInPolygon({ x: 150, y: 50 }, square)).toBe(false)
    expect(pointInPolygon({ x: -1, y: 50 }, square)).toBe(false)
  })

  it('边界与顶点算在里面', () => {
    expect(pointInPolygon({ x: 100, y: 50 }, square)).toBe(true)
    expect(pointInPolygon({ x: 0, y: 0 }, square)).toBe(true)
    expect(pointInPolygon({ x: 50, y: 100 }, square)).toBe(true)
  })

  // 射线法最容易在凹多边形上翻车：L 形的缺口必须判在外面。
  it('凹多边形：缺口算外面', () => {
    const l = [
      { x: 0, y: 0 },
      { x: 100, y: 0 },
      { x: 100, y: 40 },
      { x: 40, y: 40 },
      { x: 40, y: 100 },
      { x: 0, y: 100 },
    ]
    expect(pointInPolygon({ x: 20, y: 20 }, l)).toBe(true)
    expect(pointInPolygon({ x: 70, y: 70 }, l)).toBe(false)
    expect(pointInPolygon({ x: 20, y: 70 }, l)).toBe(true)
  })

  it('不足三个点不构成多边形', () => {
    expect(pointInPolygon({ x: 0, y: 0 }, [])).toBe(false)
    expect(pointInPolygon({ x: 0, y: 0 }, [{ x: 0, y: 0 }, { x: 10, y: 10 }])).toBe(false)
  })
})

describe('nearestWallAnchor', () => {
  const walls = [
    { id: 'bottom', ax: 0, ay: 300, bx: 400, by: 300 },
    { id: 'left', ax: 0, ay: 0, bx: 0, by: 300 },
  ]

  it('投影到最近的墙上并给出沿墙偏移', () => {
    const anchor = nearestWallAnchor(walls, 120, 320)
    expect(anchor).not.toBeNull()
    expect(anchor!.wallId).toBe('bottom')
    expect(anchor!.offset).toBeCloseTo(120)
    expect(anchor!.y).toBeCloseTo(300)
    expect(anchor!.rotation).toBeCloseTo(0)
  })

  it('超出宽容半径就不吸', () => {
    expect(nearestWallAnchor(walls, 200, 200, 40)).toBeNull()
    expect(nearestWallAnchor(walls, 200, 290, 40)!.wallId).toBe('bottom')
  })

  it('两端之外会夹到端点', () => {
    const anchor = nearestWallAnchor(walls, -50, 280)!
    expect(anchor.wallId).toBe('left')
    expect(anchor.offset).toBeCloseTo(280)
  })
})

describe('clampWallOffset', () => {
  it('物品比墙宽时夹到 0', () => {
    expect(clampWallOffset(50, 200, 150)).toBe(0)
  })
  it('靠右超出时夹到墙长减物品宽', () => {
    expect(clampWallOffset(300, 200, 400)).toBe(200)
  })
})

describe('弧墙几何', () => {
  const a = { x: 0, y: 0 }
  const b = { x: 100, y: 0 }

  it('60° 弧的采样点落在同一个圆上，矢高约为 13.4', () => {
    const pts = arcPolyline(a, b, 60)
    expect(pts.length).toBeGreaterThan(2)
    const mid = pts[Math.floor(pts.length / 2)]
    expect(mid.y).toBeCloseTo(-13.4, 0)
    for (const p of pts) {
      expect(Math.hypot(p.x - 50, p.y - 86.6)).toBeCloseTo(100, 0)
    }
  })

  it('直墙不产生采样点', () => {
    expect(arcPolyline(a, b, 0)).toEqual([])
    expect(arcPoints(a, b, 0)).toHaveLength(2)
  })

  it('弧长始终大于弦长，90° 时是 πr/2', () => {
    expect(arcLength(a, b, 0)).toBeCloseTo(100, 6)
    // 90°：弦 100 → r = 50/sin45 = 70.71，弧长 = r*π/2 = 111.07
    expect(arcLength(a, b, 90)).toBeCloseTo(111.07, 1)
    expect(arcLength(a, b, -90)).toBeCloseTo(111.07, 1)
  })
})

describe('弧墙 180° 的鼓出方向', () => {
  const a = { x: 0, y: 0 }
  const b = { x: 400, y: 0 }
  it('正负 180° 各自一边，且与 90° 同侧', () => {
    const up = arcPolyline(a, b, 180)
    const down = arcPolyline(a, b, -180)
    expect(up.every((p) => p.y <= 0)).toBe(true)
    expect(down.every((p) => p.y >= 0)).toBe(true)
    expect(arcPolyline(a, b, 90).every((p) => p.y <= 0)).toBe(true)
  })
})
