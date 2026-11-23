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

/** 点到折线的最短距离（弧墙命中用）。 */
export function distanceToPolyline(x: number, y: number, points: Pt[]): number {
  let best = Infinity
  for (let i = 1; i < points.length; i += 1) {
    const d = distanceToSegment(x, y, { ax: points[i - 1].x, ay: points[i - 1].y, bx: points[i].x, by: points[i].y })
    if (d < best) best = d
  }
  return best
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

/** 带 id 的墙；挂墙物品与门窗都靠它把世界坐标换成「墙 + 沿墙偏移」。 */
export type AnchoredWall = Seg & { id: string }

export type WallAnchor = {
  wallId: string
  /** 距墙 A 端点的偏移（厘米） */
  offset: number
  /** 墙的朝向（度，屏幕坐标下顺时针为正） */
  rotation: number
  x: number
  y: number
  dist: number
  length: number
}

/**
 * 找离 (x,y) 最近的那面墙，并把落点投影成沿墙偏移。
 *
 * 挂墙物品的 x/y 是派生值：拖窗帘时前端只报「挂到哪面墙、离起点多远」，
 * 坐标交给后端按墙算，这样墙一转，窗帘自动跟着转。
 * maxDist 是宽容半径（屏幕像素 / 视口缩放），超出返回 null。
 */
export function nearestWallAnchor(
  walls: AnchoredWall[],
  x: number,
  y: number,
  maxDist = Infinity,
): WallAnchor | null {
  let best: WallAnchor | null = null
  for (const wall of walls) {
    const p = projectOnSegment(x, y, wall)
    if (p.dist > maxDist) continue
    if (best && p.dist >= best.dist) continue
    const length = Math.hypot(wall.bx - wall.ax, wall.by - wall.ay)
    best = {
      wallId: wall.id,
      offset: p.t * length,
      rotation: (Math.atan2(wall.by - wall.ay, wall.bx - wall.ax) * 180) / Math.PI,
      x: p.x,
      y: p.y,
      dist: p.dist,
      length,
    }
  }
  return best
}

/** 沿墙偏移要夹在 [0, 墙长 - 物品宽] 之间，否则物品会一半悬在墙外。 */
export function clampWallOffset(offset: number, width: number, length: number): number {
  return clamp(offset, 0, Math.max(0, length - width))
}

/**
 * 弧墙离散成折线顶点（不含两端）。
 *
 * 弧度为正时圆心落在弦的左侧（顺着 a→b 看），与后端 `control.ArcPolyline` 同一套公式；
 * 两边算法必须一致，否则房间推导出的多边形和画布上画出来的弧对不上。
 */
export function arcPolyline(a: Pt, b: Pt, extentDeg: number): Pt[] {
  const theta = (extentDeg * Math.PI) / 180
  if (Math.abs(theta) < 1e-6) return []
  const dx = b.x - a.x
  const dy = b.y - a.y
  const d = Math.hypot(dx, dy)
  if (d < 1e-9) return []
  const h = d / 2 / Math.tan(theta / 2)
  const mx = (a.x + b.x) / 2
  const my = (a.y + b.y) / 2
  const nx = -dy / d
  const ny = dx / d
  const cx = mx + nx * h
  const cy = my + ny * h
  const r = Math.hypot(a.x - cx, a.y - cy)
  const start = Math.atan2(a.y - cy, a.x - cx)
  const end = Math.atan2(b.y - cy, b.x - cx)
  let delta = end - start
  while (delta > Math.PI) delta -= 2 * Math.PI
  while (delta < -Math.PI) delta += 2 * Math.PI
  // 正好 180° 时两段弧等长，归一化后符号唯一，鼓出方向得靠弧度符号补回来。
  if (Math.abs(Math.abs(delta) - Math.PI) < 1e-9) delta = Math.sign(theta) * Math.PI
  const steps = Math.max(2, Math.ceil(Math.abs(delta) / ((5 * Math.PI) / 180)))
  const out: Pt[] = []
  for (let i = 1; i < steps; i += 1) {
    const ang = start + (delta * i) / steps
    out.push({ x: cx + r * Math.cos(ang), y: cy + r * Math.sin(ang) })
  }
  return out
}

/** 弧墙的完整折线（含两端）。 */
export function arcPoints(a: Pt, b: Pt, extentDeg: number): Pt[] {
  return [a, ...arcPolyline(a, b, extentDeg), b]
}

/** 弧长（厘米）；0 弧度时等于弦长。 */
export function arcLength(a: Pt, b: Pt, extentDeg: number): number {
  const theta = Math.abs(extentDeg) * (Math.PI / 180)
  const chord = Math.hypot(b.x - a.x, b.y - a.y)
  if (theta < 1e-6) return chord
  const r = chord / (2 * Math.sin(theta / 2))
  return r * theta
}

/** 弧墙的圆心、半径、起始角与扫过角（弧度）；直墙返回 null。 */
export function arcCircle(
  a: Pt,
  b: Pt,
  extentDeg: number,
): { cx: number; cy: number; r: number; start: number; delta: number } | null {
  const theta = (extentDeg * Math.PI) / 180
  if (Math.abs(theta) < 1e-6) return null
  const dx = b.x - a.x
  const dy = b.y - a.y
  const d = Math.hypot(dx, dy)
  if (d < 1e-9) return null
  const h = d / 2 / Math.tan(theta / 2)
  const cx = (a.x + b.x) / 2 + (-dy / d) * h
  const cy = (a.y + b.y) / 2 + (dx / d) * h
  const r = Math.hypot(a.x - cx, a.y - cy)
  const start = Math.atan2(a.y - cy, a.x - cx)
  const end = Math.atan2(b.y - cy, b.x - cx)
  let delta = end - start
  while (delta > Math.PI) delta -= 2 * Math.PI
  while (delta < -Math.PI) delta += 2 * Math.PI
  return { cx, cy, r, start, delta }
}

/** 圆弧上按角度取点（供尺寸线与箭头用）。 */
export function arcPointAt(c: { cx: number; cy: number; r: number }, angle: number): Pt {
  return { x: c.cx + c.r * Math.cos(angle), y: c.cy + c.r * Math.sin(angle) }
}
