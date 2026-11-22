import { describe, expect, it } from 'vitest'

import { axisCross, clamp, distanceToSegment, pointInPolygon, projectOnSegment } from './geometry'

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
