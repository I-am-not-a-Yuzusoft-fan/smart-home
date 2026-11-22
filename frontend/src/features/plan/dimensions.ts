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
