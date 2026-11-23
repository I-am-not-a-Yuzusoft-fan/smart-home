/** 底图的坐标变换。
 *
 * 底图存在三个坐标系里：图片像素、世界厘米、屏幕像素。
 * 屏幕像素那层由 Konva 的视口负责，这里只做「图片像素 ↔ 世界厘米」——
 * 它是纯数学，可以在没有画布的环境里单测，也是定标/#34 与旋转/#35 的共同地基。
 */

import type { Pt } from './geometry'

/** 底图的变换参数，字段与后端 `Underlay` 元数据一一对应。 */
export type UnderlayTransform = {
  widthPx: number
  heightPx: number
  /** 一个图片像素等于多少世界厘米 */
  cmPerPx: number
  /** 未旋转时图片左上角在世界坐标里的位置 */
  offsetXCm: number
  offsetYCm: number
  /** 绕图片中心旋转的角度，0 / 90 / 180 / 270 */
  rotationDeg: number
}

export type Rect = { x: number; y: number; w: number; h: number }

const DEG = Math.PI / 180

/** 底图在世界坐标里占据的矩形（未旋转时）。 */
export function imageRect(t: UnderlayTransform): Rect {
  return {
    x: t.offsetXCm,
    y: t.offsetYCm,
    w: t.widthPx * t.cmPerPx,
    h: t.heightPx * t.cmPerPx,
  }
}

/** 旋转中心：图片中心。旋转不改 offset，所以这个点只由 offset 与尺寸决定。 */
export function imageCenter(t: UnderlayTransform): Pt {
  const r = imageRect(t)
  return { x: r.x + r.w / 2, y: r.y + r.h / 2 }
}

/** 图片像素 → 世界厘米。 */
export function imageToPlan(u: Pt, t: UnderlayTransform): Pt {
  const c = imageCenter(t)
  const halfW = (t.widthPx * t.cmPerPx) / 2
  const halfH = (t.heightPx * t.cmPerPx) / 2
  // 相对图片中心的未旋转向量
  const dx = u.x * t.cmPerPx - halfW
  const dy = u.y * t.cmPerPx - halfH
  const rad = t.rotationDeg * DEG
  const cos = Math.cos(rad)
  const sin = Math.sin(rad)
  return { x: c.x + dx * cos - dy * sin, y: c.y + dx * sin + dy * cos }
}

/** 世界厘米 → 图片像素。是 `imageToPlan` 的逆，定标与命中都用它。 */
export function planToImage(p: Pt, t: UnderlayTransform): Pt {
  const c = imageCenter(t)
  const halfW = (t.widthPx * t.cmPerPx) / 2
  const halfH = (t.heightPx * t.cmPerPx) / 2
  const rad = -t.rotationDeg * DEG
  const cos = Math.cos(rad)
  const sin = Math.sin(rad)
  const dx = p.x - c.x
  const dy = p.y - c.y
  // 逆旋转后再换回像素
  const ux = dx * cos - dy * sin
  const uy = dx * sin + dy * cos
  return { x: (ux + halfW) / t.cmPerPx, y: (uy + halfH) / t.cmPerPx }
}

/** 四角在世界坐标里的位置，按 左上 → 右上 → 右下 → 左下 顺序。 */
export function imageCorners(t: UnderlayTransform): Pt[] {
  const { widthPx, heightPx } = t
  return [
    imageToPlan({ x: 0, y: 0 }, t),
    imageToPlan({ x: widthPx, y: 0 }, t),
    imageToPlan({ x: widthPx, y: heightPx }, t),
    imageToPlan({ x: 0, y: heightPx }, t),
  ]
}

/**
 * 两点定标：用户在图上点了两个点，并给出这两点在现实中的距离。
 *
 * `u1`/`u2` 是点击位置换算出的图片像素，`anchor` 是点击位置的世界坐标（用来
 * 钉住底图，让第一次点的那个点不动），`lengthCm` 是用户输入的真实长度。
 * 只解比例与平移，不改旋转——旋转是独立的一步（#35），把两件事混在一个方程
 * 里，用户先转后定标就会得到反直觉的结果。
 */
export function calibrate(
  u1: Pt,
  u2: Pt,
  anchor: Pt,
  lengthCm: number,
  t: UnderlayTransform,
): { cmPerPx: number; offsetXCm: number; offsetYCm: number } {
  const pixelDist = Math.hypot(u2.x - u1.x, u2.y - u1.y)
  // 两点太近时比例会被一次点击误差放大到没意义，直接拒绝（spec 约定 2px）。
  if (pixelDist < 2) throw new Error('两点的图上距离太近（不到 2 像素），请重新点选')
  if (!Number.isFinite(lengthCm) || lengthCm <= 0) throw new Error('真实长度必须大于 0')

  const cmPerPx = lengthCm / pixelDist
  // 比例变了以后，第一个点会跟着漂；把整张图平移回去，让它仍然落在 anchor 上。
  const next: UnderlayTransform = { ...t, cmPerPx }
  const mapped = imageToPlan(u1, next)
  return {
    cmPerPx,
    offsetXCm: next.offsetXCm + (anchor.x - mapped.x),
    offsetYCm: next.offsetYCm + (anchor.y - mapped.y),
  }
}

/** 旋转 90° 的整数倍并归一到 0/90/180/270。 */
export function nextRotation(deg: number): 0 | 90 | 180 | 270 {
  return ((((deg + 90) % 360) + 360) % 360) as 0 | 90 | 180 | 270
}
