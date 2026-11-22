import { describe, expect, it } from 'vitest'

import { tidy, wallAngle, wallLength, withAngle, withEndpoint, withLength } from './edit'

const horizontal = { ax: 0, ay: 0, bx: 420, by: 0 }

describe('wallLength / wallAngle', () => {
  it('水平墙长度与角度', () => {
    expect(wallLength(horizontal)).toBeCloseTo(420)
    expect(wallAngle(horizontal)).toBeCloseTo(0)
  })

  it('竖墙角度是 90 度（屏幕坐标 y 向下）', () => {
    expect(wallAngle({ ax: 0, ay: 0, bx: 0, by: 300 })).toBeCloseTo(90)
  })

  it('3-4-5 直角三角形', () => {
    expect(wallLength({ ax: 0, ay: 0, bx: 300, by: 400 })).toBeCloseTo(500)
  })
})

describe('withLength', () => {
  it('保持起点与方向，只改长度', () => {
    const r = withLength({ ax: 100, ay: 50, bx: 300, by: 250 }, 400)
    expect(r.ax).toBe(100)
    expect(r.ay).toBe(50)
    expect(wallLength(r)).toBeCloseTo(400)
    // 方向不变：斜 45 度
    expect(wallAngle(r)).toBeCloseTo(45)
  })

  it('零长度墙沿 +x 铺开而不是给出 NaN', () => {
    const r = withLength({ ax: 10, ay: 10, bx: 10, by: 10 }, 200)
    expect(r).toEqual({ ax: 10, ay: 10, bx: 210, by: 10 })
  })

  it('负长度被夹到 0', () => {
    expect(wallLength(withLength(horizontal, -50))).toBeCloseTo(0)
  })
})

describe('withAngle', () => {
  it('保持长度，只转方向', () => {
    const r = withAngle(horizontal, 90)
    expect(wallLength(r)).toBeCloseTo(420)
    expect(wallAngle(r)).toBeCloseTo(90)
    expect(r.ax).toBe(0)
    expect(r.ay).toBe(0)
  })

  it('转回去能回到原位', () => {
    const r = withAngle(withAngle(horizontal, 37), 0)
    expect(r.bx).toBeCloseTo(420)
    expect(r.by).toBeCloseTo(0)
  })
})

describe('withEndpoint', () => {
  it('只动被点的那个端点', () => {
    const r = withEndpoint(horizontal, 'b', 500, 120)
    expect(r).toEqual({ ax: 0, ay: 0, bx: 500, by: 120 })
    expect(withEndpoint(horizontal, 'a', 10, 10)).toEqual({ ax: 10, ay: 10, bx: 420, by: 0 })
  })
})

describe('tidy', () => {
  it('消掉浮点尾数', () => {
    expect(tidy(419.99999999999994)).toBe(420)
    expect(tidy(120.04)).toBe(120)
    expect(tidy(120.06)).toBe(120.1)
  })
})
