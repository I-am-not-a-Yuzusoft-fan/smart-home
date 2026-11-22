import { describe, expect, it } from 'vitest'

import {
  calibrate,
  imageCenter,
  imageCorners,
  imageRect,
  imageToPlan,
  nextRotation,
  planToImage,
  type UnderlayTransform,
} from './underlay'

/** 10×10 厘米的一格、100×50 像素的底图：比例与偏移都取好算的值。 */
const base: UnderlayTransform = {
  widthPx: 100,
  heightPx: 50,
  cmPerPx: 4,
  offsetXCm: 100,
  offsetYCm: 200,
  rotationDeg: 0,
}

describe('底图坐标变换', () => {
  it('未旋转时左上角就是 offset，尺寸是像素乘比例', () => {
    expect(imageRect(base)).toEqual({ x: 100, y: 200, w: 400, h: 200 })
    expect(imageCenter(base)).toEqual({ x: 300, y: 300 })
    expect(imageToPlan({ x: 0, y: 0 }, base)).toEqual({ x: 100, y: 200 })
    expect(imageToPlan({ x: 100, y: 50 }, base)).toEqual({ x: 500, y: 400 })
  })

  it('像素与厘米互相可逆', () => {
    for (const u of [
      { x: 0, y: 0 },
      { x: 37, y: 11 },
      { x: 100, y: 50 },
    ]) {
      const p = imageToPlan(u, base)
      const back = planToImage(p, base)
      expect(back.x).toBeCloseTo(u.x, 9)
      expect(back.y).toBeCloseTo(u.y, 9)
    }
  })

  it('旋转 90° 绕图片中心转，四角顺序不变', () => {
    const rotated = { ...base, rotationDeg: 90 }
    expect(imageCenter(rotated)).toEqual(imageCenter(base))
    const corners = imageCorners(rotated)
    // 400×200 的图绕中心转 90°：左上角跑到中心右上方，对角跑到左下方。
    expect(corners[0].x).toBeCloseTo(400, 9)
    expect(corners[0].y).toBeCloseTo(100, 9)
    expect(corners[2].x).toBeCloseTo(200, 9)
    expect(corners[2].y).toBeCloseTo(500, 9)
    // 旋转后仍可逆
    const p = imageToPlan({ x: 25, y: 5 }, rotated)
    const back = planToImage(p, rotated)
    expect(back.x).toBeCloseTo(25, 9)
    expect(back.y).toBeCloseTo(5, 9)
  })

  it('定标：两个像素点对上两个世界点', () => {
    const u1 = { x: 10, y: 10 }
    const u2 = { x: 90, y: 10 }
    const p1 = { x: 0, y: 0 }
    const p2 = { x: 320, y: 0 }
    const patch = calibrate(u1, u2, p1, p2, base)
    // 80 像素对 320 厘米 → 每像素 4 厘米，和原来一样
    expect(patch.cmPerPx).toBeCloseTo(4, 9)

    const next = { ...base, ...patch }
    const mapped1 = imageToPlan(u1, next)
    const mapped2 = imageToPlan(u2, next)
    expect(mapped1.x).toBeCloseTo(p1.x, 9)
    expect(mapped1.y).toBeCloseTo(p1.y, 9)
    // 方向与距离都对上
    expect(mapped2.x - mapped1.x).toBeCloseTo(320, 9)
    expect(mapped2.y - mapped1.y).toBeCloseTo(0, 9)
  })

  it('定标改比例时同步修正偏移', () => {
    const patch = calibrate({ x: 0, y: 0 }, { x: 50, y: 0 }, { x: 10, y: 20 }, { x: 110, y: 20 }, base)
    // 50 像素对 100 厘米 → 每像素 2 厘米
    expect(patch.cmPerPx).toBeCloseTo(2, 9)
    const next = { ...base, ...patch }
    const mapped = imageToPlan({ x: 0, y: 0 }, next)
    expect(mapped.x).toBeCloseTo(10, 9)
    expect(mapped.y).toBeCloseTo(20, 9)
  })

  it('两点重合时拒绝定标而不是算出 Infinity', () => {
    expect(() => calibrate({ x: 5, y: 5 }, { x: 5, y: 5 }, { x: 0, y: 0 }, { x: 1, y: 0 }, base)).toThrow()
    expect(() => calibrate({ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 3, y: 3 }, { x: 3, y: 3 }, base)).toThrow()
  })

  it('旋转按 90° 步进循环', () => {
    expect(nextRotation(0)).toBe(90)
    expect(nextRotation(270)).toBe(0)
    expect(nextRotation(360)).toBe(90)
  })
})
