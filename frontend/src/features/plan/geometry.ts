/** 平面图的纯几何工具。
 *
 * 这里只放「输入数字、输出数字」的函数——不碰 Konva、不碰 React、不碰 DOM，
 * 这样吸附与命中判定的规则可以在没有浏览器的环境下单测。
 */

export type Pt = { x: number; y: number }

/** 一条线段；与 `Wall` 的字段同名，方便直接传入。 */
export type Seg = { ax: number; ay: number; bx: number; by: number }

export function clamp(v: number, min: number, max: number): number {
  return Math.min(Math.max(v, min), max)
}

/**
 * 把点投影到线段上。
 *
 * 返回 `t`（0–1，越界会被夹住）、投影点与到线段的距离。
 * 用投影点而不是无限长直线，是因为悬空的墙不该在延长线上产生吸附。
 */
export function projectOnSegment(
  x: number,
  y: number,
  s: Seg,
): { x: number; y: number; t: number; dist: number } {
  const dx = s.bx - s.ax
  const dy = s.by - s.ay
  const len2 = dx * dx + dy * dy
  if (len2 === 0) {
    return { x: s.ax, y: s.ay, t: 0, dist: Math.hypot(x - s.ax, y - s.ay) }
  }
  const t = clamp(((x - s.ax) * dx + (y - s.ay) * dy) / len2, 0, 1)
  const px = s.ax + t * dx
  const py = s.ay + t * dy
  return { x: px, y: py, t, dist: Math.hypot(x - px, y - py) }
}

export function distanceToSegment(x: number, y: number, s: Seg): number {
  return projectOnSegment(x, y, s).dist
}

/**
 * 过锚点的水平 / 竖直线与线段 `s` 的交点。
 *
 * 画墙时「正交」和「贴到某面墙」经常同时成立：从 (0,150) 往右拖，
 * 目标位置落在 x=400 那面竖墙附近。此时正确的落点是**交点** (400,150)，
 * 而不是投影点 (400,152)——后者会让预览是直的、落库是斜的。
 *
 * 平行或交点落在线段之外时返回 null。
 */
export function axisCross(anchor: Pt, axis: 'h' | 'v', s: Seg): Pt | null {
  if (axis === 'h') {
    const dy = s.by - s.ay
    if (Math.abs(dy) < 1e-9) return null
    const t = (anchor.y - s.ay) / dy
    if (t < 0 || t > 1) return null
    return { x: s.ax + t * (s.bx - s.ax), y: anchor.y }
  }
  const dx = s.bx - s.ax
  if (Math.abs(dx) < 1e-9) return null
  const t = (anchor.x - s.ax) / dx
  if (t < 0 || t > 1) return null
  return { x: anchor.x, y: s.ay + t * (s.by - s.ay) }
}

/** 点是否落在以 `(cx, cy)` 为心、`r` 为半径的圆内。 */
export function withinRadius(x: number, y: number, cx: number, cy: number, r: number): boolean {
  return Math.hypot(x - cx, y - cy) <= r
}

/** 一组点的算术平均；空数组返回原点。 */
export function averagePoint(points: Pt[]): Pt {
  if (points.length === 0) return { x: 0, y: 0 }
  let sx = 0
  let sy = 0
  for (const p of points) {
    sx += p.x
    sy += p.y
  }
  return { x: sx / points.length, y: sy / points.length }
}

/**
 * 点是否落在多边形内（含边界）。
 *
 * 房间的点击选中不能靠 Konva 的 hit graph（自定义 Stage 变换下它恒返回 none），
 * 所以拿后端推出来的 `ring` 做射线法：向右发一条射线，数穿过的边数，奇数次在里面。
 * 边界与顶点算在里面——贴着墙点房间应该选中，而不是"差一像素没中"。
 */
export function pointInPolygon(p: Pt, ring: Pt[]): boolean {
  if (ring.length < 3) return false
  let inside = false
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const a = ring[j]
    const b = ring[i]
    const cross = (b.x - a.x) * (p.y - a.y) - (b.y - a.y) * (p.x - a.x)
    const onSegment =
      Math.abs(cross) < 1e-9 &&
      p.x >= Math.min(a.x, b.x) - 1e-9 &&
      p.x <= Math.max(a.x, b.x) + 1e-9 &&
      p.y >= Math.min(a.y, b.y) - 1e-9 &&
      p.y <= Math.max(a.y, b.y) + 1e-9
    if (onSegment) return true
    if (a.y > p.y !== b.y > p.y) {
      const hitX = a.x + ((p.y - a.y) * (b.x - a.x)) / (b.y - a.y)
      if (p.x < hitX) inside = !inside
    }
  }
  return inside
}
