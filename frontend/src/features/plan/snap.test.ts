import { describe, expect, it } from 'vitest'

import { snap, DEFAULT_RULES } from './snap'

const wall = { id: 'w1', ax: 0, ay: 0, bx: 100, by: 0 }

const base = {
  scale: 1,
  corners: [] as { x: number; y: number }[],
  walls: [] as typeof wall[],
  orthogonal: false,
  gridCm: 20,
  rules: DEFAULT_RULES,
}

describe('snap 优先级', () => {
  it('端点比墙线优先', () => {
    const r = snap({
      ...base,
      x: 3,
      y: 2,
      corners: [{ x: 0, y: 0 }],
      walls: [wall],
    })
    expect(r.kind).toBe('point')
    expect(r.x).toBe(0)
    expect(r.y).toBe(0)
  })

  it('没有端点时吸附到墙线并回报是哪面墙', () => {
    const r = snap({ ...base, x: 50, y: 8, walls: [wall] })
    expect(r.kind).toBe('segment')
    expect(r).toMatchObject({ x: 50, y: 0, wallId: 'w1' })
  })

  it('离得远就什么都不吸', () => {
    const r = snap({ ...base, x: 50, y: 40, walls: [wall] })
    expect(r.kind).toBe('none')
    expect(r).toMatchObject({ x: 50, y: 40 })
  })
})

describe('snap 正交约束', () => {
  it('主导轴为水平时锁住 y', () => {
    const r = snap({ ...base, x: 100, y: 3, anchor: { x: 0, y: 0 }, orthogonal: true })
    expect(r).toMatchObject({ x: 100, y: 0 })
  })

  it('主导轴为竖直时锁住 x', () => {
    const r = snap({ ...base, x: 3, y: 100, anchor: { x: 0, y: 0 }, orthogonal: true })
    expect(r).toMatchObject({ x: 0, y: 100 })
  })

  it('正交与墙线同时成立时取交点，而不是投影点', () => {
    const vertical = { id: 'w2', ax: 400, ay: 0, bx: 400, by: 300 }
    const r = snap({
      ...base,
      x: 398,
      y: 152,
      anchor: { x: 0, y: 150 },
      orthogonal: true,
      walls: [vertical],
    })
    expect(r.kind).toBe('segment')
    expect(r).toMatchObject({ x: 400, y: 150, wallId: 'w2' })
  })

  it('关掉正交后同样的指针位置不锁轴', () => {
    const r = snap({ ...base, x: 398, y: 152, anchor: { x: 0, y: 150 }, orthogonal: false })
    expect(r.kind).toBe('none')
    expect(r.y).toBe(152)
  })
})

describe('snap 网格', () => {
  it('默认不吸附网格', () => {
    const r = snap({ ...base, x: 37, y: 43 })
    expect(r.kind).toBe('none')
    expect(r).toMatchObject({ x: 37, y: 43 })
  })

  it('打开后落到 20 厘米模数', () => {
    const rules = DEFAULT_RULES.map((rule) =>
      rule.kind === 'grid' ? { ...rule, enabled: true } : rule,
    )
    const r = snap({ ...base, x: 37, y: 43, rules })
    expect(r.kind).toBe('grid')
    expect(r).toMatchObject({ x: 40, y: 40 })
  })

  it('端点在网格之前，不会被网格抢走', () => {
    const rules = DEFAULT_RULES.map((rule) =>
      rule.kind === 'grid' ? { ...rule, enabled: true } : rule,
    )
    const r = snap({ ...base, x: 41, y: 41, corners: [{ x: 40, y: 40 }], rules })
    expect(r.kind).toBe('point')
  })
})

describe('snap 其它', () => {
  it('忽略指定墙后不再吸到它', () => {
    const r = snap({ ...base, x: 50, y: 8, walls: [wall], ignoreWallId: 'w1' })
    expect(r.kind).toBe('none')
  })

  it('半径按屏幕像素折算到世界坐标，缩放后不会变得贪心', () => {
    const near = snap({ ...base, x: 5, y: 0, scale: 2, corners: [{ x: 0, y: 0 }] })
    expect(near.kind).toBe('point')

    // scale=2 时 12px 相当于 6 厘米，8 厘米已经超出
    const far = snap({ ...base, x: 8, y: 0, scale: 2, corners: [{ x: 0, y: 0 }] })
    expect(far.kind).toBe('none')
  })
})
