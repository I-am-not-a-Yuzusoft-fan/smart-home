import type { Seg } from './geometry'

/** 墙的编辑运算：改长度、改角度、改端点。
 *
 * 全部是「旧的进去、新的出来」的纯函数——面板与画布拖动共用同一套换算，
 * 不允许两边各写一份三角函数。
 */

export function wallLength(w: Seg): number {
  return Math.hypot(w.bx - w.ax, w.by - w.ay)
}

/** 与水平方向的夹角，度，范围 (-180, 180]。屏幕坐标 y 向下，所以正值朝下。 */
export function wallAngle(w: Seg): number {
  return (Math.atan2(w.by - w.ay, w.bx - w.ax) * 180) / Math.PI
}

/**
 * 保持起点与方向，把长度改成 `len`。
 *
 * 长度非法（≤0）或原墙退化成一点（方向不确定）时，沿 +x 方向铺开——
 * 总比返回 NaN 或原地不动好，用户能立刻看见自己改了什么。
 */
export function withLength(w: Seg, len: number): Seg {
  const cur = wallLength(w)
  if (cur < 1e-9) {
    return { ax: w.ax, ay: w.ay, bx: w.ax + Math.max(len, 0), by: w.ay }
  }
  const k = Math.max(len, 0) / cur
  return { ax: w.ax, ay: w.ay, bx: w.ax + (w.bx - w.ax) * k, by: w.ay + (w.by - w.ay) * k }
}

/** 保持起点与长度，把方向转到 `deg`（度）。 */
export function withAngle(w: Seg, deg: number): Seg {
  const len = wallLength(w)
  const rad = (deg * Math.PI) / 180
  return {
    ax: w.ax,
    ay: w.ay,
    bx: w.ax + Math.cos(rad) * len,
    by: w.ay + Math.sin(rad) * len,
  }
}

/** 把一个端点移到新坐标，另一端保持不动。 */
export function withEndpoint(w: Seg, end: 'a' | 'b', x: number, y: number): Seg {
  if (end === 'a') return { ax: x, ay: y, bx: w.bx, by: w.by }
  return { ax: w.ax, ay: w.ay, bx: x, by: y }
}

/** 端点坐标取 0.1 厘米精度：键入 420 不该在库里变成 419.99999999999994。 */
export function tidy(v: number): number {
  return Math.round(v * 10) / 10
}
