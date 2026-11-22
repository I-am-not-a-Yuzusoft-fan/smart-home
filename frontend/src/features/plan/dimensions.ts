import type { Pt } from './geometry'

/**
 * 尺寸标注的分组。
 *
 * 一面墙一个长度数字只是「分段」。用户真正要读的是「这一整条边多长」——
 * 所以把共线且首尾相接的墙合并成一条**标注链**，由它出总长，墙本身出分段。
 * 两级标注的差别就在这里：链是"这一面有多宽"，分段是"每段多长"。
 */

export interface DimWall {
  id: string
  ax: number
  ay: number
  bx: number
  by: number
}

export interface Run {
  /** 链的两个端点（世界坐标） */
  a: Pt
  b: Pt
  /** 总长（厘米） */
  total: number
  wallIds: string[]
  /** 水平或竖直：决定标注往外偏移的方向 */
  horizontal: boolean
}

const EPS = 0.5 // 半厘米以内视为共线

function samePoint(p: Pt, q: Pt): boolean {
  return Math.abs(p.x - q.x) < 0.01 && Math.abs(p.y - q.y) < 0.01
}

/** 两条墙共线：方向平行（叉积≈0）且落在同一条直线上（端点到直线距离≈0）。 */
function collinear(w: DimWall, v: DimWall): boolean {
  const dx = w.bx - w.ax
  const dy = w.by - w.ay
  const len = Math.hypot(dx, dy)
  if (len < 1e-6) return false
  const cross = (dx * (v.by - w.ay) - dy * (v.bx - w.ax)) / len
  const cross2 = (dx * (v.ay - w.ay) - dy * (v.ax - w.ax)) / len
  return Math.abs(cross) <= EPS && Math.abs(cross2) <= EPS
}

function touches(w: DimWall, v: DimWall): boolean {
  const a = { x: w.ax, y: w.ay }
  const b = { x: w.bx, y: w.by }
  return samePoint(a, { x: v.ax, y: v.ay }) || samePoint(a, { x: v.bx, y: v.by }) ||
    samePoint(b, { x: v.ax, y: v.ay }) || samePoint(b, { x: v.bx, y: v.by })
}

/**
 * 把墙合并成标注链。
 *
 * 规则：共享端点 **且** 共线才合并。只共线不共端点（中间隔着别的墙）不合并——
 * 那是两条独立的边，画一条贯通的尺寸线会让人误以为中间没有隔断。
 */
export function collinearRuns(walls: DimWall[]): Run[] {
  const parent = walls.map((_, i) => i)
  const find = (i: number): number => (parent[i] === i ? i : (parent[i] = find(parent[i])))
  const union = (i: number, j: number) => {
    const a = find(i)
    const b = find(j)
    if (a !== b) parent[a] = b
  }

  for (let i = 0; i < walls.length; i++) {
    for (let j = i + 1; j < walls.length; j++) {
      if (touches(walls[i], walls[j]) && collinear(walls[i], walls[j])) union(i, j)
    }
  }

  const groups = new Map<number, number[]>()
  for (let i = 0; i < walls.length; i++) {
    const root = find(i)
    const list = groups.get(root) ?? []
    list.push(i)
    groups.set(root, list)
  }

  const runs: Run[] = []
  for (const idxs of groups.values()) {
    // 链的两端不是「度为 1 的端点」——闭合环里每个端点都被垂直的墙共享着，
    // 那样算会导致矩形推不出任何一条尺寸链。沿组内的方向轴取投影极值即可。
    const first = walls[idxs[0]]
    const dx = first.bx - first.ax
    const dy = first.by - first.ay
    const len = Math.hypot(dx, dy)
    if (len < 1e-6) continue
    const ux = dx / len
    const uy = dy / len

    let minPt: Pt | null = null
    let maxPt: Pt | null = null
    let minS = Infinity
    let maxS = -Infinity
    for (const i of idxs) {
      const w = walls[i]
      for (const p of [{ x: w.ax, y: w.ay }, { x: w.bx, y: w.by }]) {
        const s = p.x * ux + p.y * uy
        if (s < minS) { minS = s; minPt = p }
        if (s > maxS) { maxS = s; maxPt = p }
      }
    }
    if (!minPt || !maxPt) continue
    const total = maxS - minS
    if (total < 1) continue
    runs.push({
      a: minPt,
      b: maxPt,
      total,
      wallIds: idxs.map((i) => walls[i].id),
      horizontal: Math.abs(ux) >= Math.abs(uy),
    })
  }

  // 长的先画，短的后画（后画的压在上面，视觉上更清楚）
  return runs.sort((x, y) => y.total - x.total)
}

/** 从一个户型里挑出「外轮廓」四条边的链：按包围盒的四条边找最接近的链。 */
export function outerRuns(runs: Run[]): Run[] {
  if (runs.length === 0) return []
  const horizontal = runs.filter((r) => r.horizontal).sort((x, y) => y.total - x.total)
  const vertical = runs.filter((r) => !r.horizontal).sort((x, y) => y.total - x.total)
  const picked: Run[] = []
  if (horizontal[0]) picked.push(horizontal[0])
  if (vertical[0]) picked.push(vertical[0])
  return picked
}

/**
 * 房间的开间 × 进深。
 *
 * 用**最小面积外包矩形**而不是包围盒：L 形、斜墙的房间用包围盒会把凹口也算进去，
 * 报出来的尺寸比实际大一圈。最小矩形沿凸包某条边贴住，所以轴对齐的房间依然是
 * 「宽 × 深」，斜放的房间也能报出真正的两个边长。
 *
 * 返回厘米，angle 是矩形长边相对水平方向的角度（-45°, 45°]。
 */
export interface RoomExtent {
  width: number
  height: number
  angle: number
}

/** 凸包（Andrew monotone chain），房间顶点最多十几个，O(n log n) 足够。 */
function convexHull(points: Pt[]): Pt[] {
  const pts = points
    .slice()
    .sort((p, q) => (p.x === q.x ? p.y - q.y : p.x - q.x))
  const cross = (o: Pt, a: Pt, b: Pt) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x)
  const lower: Pt[] = []
  for (const p of pts) {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) lower.pop()
    lower.push(p)
  }
  const upper: Pt[] = []
  for (let i = pts.length - 1; i >= 0; i--) {
    const p = pts[i]
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) upper.pop()
    upper.push(p)
  }
  lower.pop()
  upper.pop()
  return lower.concat(upper)
}

/** 多边形形心（面积加权）。三角形就是三个顶点的平均，退化时退回顶点平均。 */
export function polygonCentroid(ring: Pt[]): Pt {
  let twiceArea = 0
  let cx = 0
  let cy = 0
  for (let i = 0; i < ring.length; i++) {
    const a = ring[i]
    const b = ring[(i + 1) % ring.length]
    const cross = a.x * b.y - b.x * a.y
    twiceArea += cross
    cx += (a.x + b.x) * cross
    cy += (a.y + b.y) * cross
  }
  if (Math.abs(twiceArea) < 1e-9) {
    const n = ring.length || 1
    return { x: ring.reduce((sum, p) => sum + p.x, 0) / n, y: ring.reduce((sum, p) => sum + p.y, 0) / n }
  }
  return { x: cx / (3 * twiceArea), y: cy / (3 * twiceArea) }
}

export function roomExtent(ring: Pt[]): RoomExtent | null {
  if (ring.length < 3) return null
  const hull = convexHull(ring)
  if (hull.length < 3) return null
  let best: RoomExtent | null = null
  let bestArea = Infinity
  for (let i = 0; i < hull.length; i++) {
    const a = hull[i]
    const b = hull[(i + 1) % hull.length]
    const dx = b.x - a.x
    const dy = b.y - a.y
    const len = Math.hypot(dx, dy)
    if (len < 1e-9) continue
    const ux = dx / len
    const uy = dy / len
    let minU = Infinity
    let maxU = -Infinity
    let minV = Infinity
    let maxV = -Infinity
    for (const p of hull) {
      const u = p.x * ux + p.y * uy
      const v = -p.x * uy + p.y * ux
      if (u < minU) minU = u
      if (u > maxU) maxU = u
      if (v < minV) minV = v
      if (v > maxV) maxV = v
    }
    const w = maxU - minU
    const h = maxV - minV
    const area = w * h
    if (area >= bestArea) continue
    // 角度归一到 (-45°, 45°]：矩形的另一条边与它垂直，换一次边就把宽高调个位置，
    // 这样「宽」始终是更接近水平的那一维，读图的人不用自己转脖子。
    let angle = (Math.atan2(dy, dx) * 180) / Math.PI
    angle = ((angle + 90) % 180 + 180) % 180 - 90
    let width = w
    let height = h
    if (angle > 45) {
      angle -= 90
      width = h
      height = w
    }
    bestArea = area
    best = { width, height, angle }
  }
  return best
}

/**
 * 只保留「贴在户型外轮廓上」的尺寸链。
 *
 * 内墙不该有外侧尺寸线：它的"外侧"是隔壁房间，画出来就是一串数字飘在屋里。
 * 内墙的分段长度由墙上的标签负责，外轮廓才走「尺寸线 + 尺寸界线 + 45° 起止符」。
 */
export function outerRunsInBounds(
  runs: Run[],
  bounds: { minX: number; minY: number; maxX: number; maxY: number },
  eps = 1,
): Run[] {
  const { minX, minY, maxX, maxY } = bounds
  const near = (v: number, target: number) => Math.abs(v - target) <= eps
  return runs.filter((run) => {
    // 「贴边」不只是端点在包围盒上：一道从顶墙连到底墙的室内隔墙，两个端点
    // 也都在包围盒上，但它不是外轮廓——拿它当外轮廓画总尺寸，会凭空多出
    // 一条 7000 的标注压在房间中间。所以还要求整条链贴着同一条边。
    if (run.horizontal) {
      if (!near(run.a.y, run.b.y)) return false
      if (!near(run.a.y, minY) && !near(run.a.y, maxY)) return false
      return near(run.a.x, minX) && near(run.b.x, maxX) || near(run.a.x, maxX) && near(run.b.x, minX)
    }
    if (!near(run.a.x, run.b.x)) return false
    if (!near(run.a.x, minX) && !near(run.a.x, maxX)) return false
    return near(run.a.y, minY) && near(run.b.y, maxY) || near(run.a.y, maxY) && near(run.b.y, minY)
  })
}


export interface AxisSegment {
  /** 两个轴线位置（世界坐标：水平链是 x，竖直链是 y） */
  a: number
  b: number
  /** 开间 / 进深（厘米） */
  length: number
}

/**
 * 轴线尺寸：施工图的第二排链。
 *
 * 第一排（`Run`）是「这一整条边多长」，第二排是「开间/进深各是多少」——
 * 差别在于它要读的是**墙中线的位置**，所以内隔墙也参与：一道把客厅分成两半的
 * 隔墙，在外墙链上看不出任何东西，在轴线上就是 3300 + 3300 的由来。
 *
 * 只收「垂直于这条边」的墙（水平链看竖墙，反之亦然），并且墙的轴线要落在
 * 这条边的跨度内。端点取链的两端，所以三段以上会自然地串成一排。
 */
export function axisSegments(
  walls: DimWall[],
  horizontal: boolean,
  min: number,
  max: number,
  eps = 1,
  /**
   * 参与定轴的墙最短长度（厘米），默认取跨度的一半×0.25。
   * 没有这条限制时，房间里随手画的一小段隔断（例如 0.7 m 的围栏）也会被当成
   * 一道轴线，把「3300 | 3300」切成「800 | 2500 | 3300」，读图的人会以为那是个开间。
   */
  minWallLength = (Math.abs(max - min) * 0.25),
): AxisSegment[] {
  const positions: number[] = [Math.min(min, max), Math.max(min, max)]
  for (const w of walls) {
    const dx = Math.abs(w.bx - w.ax)
    const dy = Math.abs(w.by - w.ay)
    // 水平链要的是竖墙（dx≈0），竖直链要的是横墙（dy≈0）。
    if (horizontal ? dx > eps : dy > eps) continue
    if (Math.hypot(dx, dy) < minWallLength) continue
    const at = horizontal ? w.ax : w.ay
    if (at <= positions[0] + eps || at >= positions[1] - eps) continue
    positions.push(at)
  }
  positions.sort((a, b) => a - b)
  const unique: number[] = []
  for (const p of positions) {
    if (unique.length === 0 || Math.abs(p - unique[unique.length - 1]) > eps) unique.push(p)
  }
  const out: AxisSegment[] = []
  for (let i = 1; i < unique.length; i += 1) {
    out.push({ a: unique[i - 1], b: unique[i], length: unique[i] - unique[i - 1] })
  }
  return out
}
