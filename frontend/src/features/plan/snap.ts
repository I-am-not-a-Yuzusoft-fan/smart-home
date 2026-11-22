import { axisCross, distanceToSegment, projectOnSegment, type Pt } from './geometry'

/**
 * 吸附规则。
 *
 * 做成注册表而不是一串写死的 if，是因为「以后要加一类吸附」是可预见的
 * （对齐到家具边缘、对齐到房间中线）。新增一类只需往表里加一行，
 * 不用改求值顺序，也不容易压坏已有优先级。
 */
export type SnapKind = 'point' | 'segment' | 'grid'

export interface SnapRule {
  kind: SnapKind
  enabled: boolean
  /** 命中半径，屏幕像素；换算成世界单位时要除以缩放。网格规则不使用它。 */
  radiusPx: number
  /** 数字越小越优先。点 > 线 > 网格。 */
  priority: number
}

export const DEFAULT_RULES: SnapRule[] = [
  { kind: 'point', enabled: true, radiusPx: 12, priority: 1 },
  { kind: 'segment', enabled: true, radiusPx: 12, priority: 2 },
  { kind: 'grid', enabled: false, radiusPx: 12, priority: 3 },
]

/** 参与吸附的最小墙信息；带上 `id` 才能回报「吸到了哪面墙」。 */
export interface SnapWall {
  id: string
  ax: number
  ay: number
  bx: number
  by: number
}

export type Guide<W extends SnapWall = SnapWall> =
  | { kind: 'none' }
  | { kind: 'point'; x: number; y: number }
  | { kind: 'segment'; wall: W }
  | { kind: 'grid' }

export interface SnapInput<W extends SnapWall = SnapWall> {
  x: number
  y: number
  /** 当前缩放，用来把像素半径折算成世界单位。 */
  scale: number
  corners: Pt[]
  walls: W[]
  /** 起笔点；只有正交约束需要它。 */
  anchor?: Pt | null
  /** 是否锁定 0°/90°。它是一条**约束**，不参与优先级竞争。 */
  orthogonal: boolean
  gridCm: number
  rules?: SnapRule[]
  ignoreWallId?: string | null
}

export type SnapKindOut = SnapKind | 'none'

export interface SnapOut<W extends SnapWall = SnapWall> {
  x: number
  y: number
  /** 命中类型，给界面做提示与给测试做断言都方便。 */
  kind: SnapKindOut
  /** 命中在哪面墙上（仅 segment）。 */
  wallId?: string
  /** 画参考线需要的信息。 */
  guide: Guide<W>
}

/**
 * 计算吸附后的落点。
 *
 * 求值顺序由规则的 `priority` 决定：先试优先级最高的，命中即返回。
 * 正交不是候选，而是**约束**——它决定「往哪个轴锁」，锁轴之后
 * 再让墙线吸附去求交点，所以贴墙时既直又准。
 */
export function snap<W extends SnapWall>(input: SnapInput<W>): SnapOut<W> {
  const rules = [...(input.rules ?? DEFAULT_RULES)].filter((r) => r.enabled).sort((a, b) => a.priority - b.priority)
  const anchor = input.anchor ?? null
  const axis: 'h' | 'v' | null =
    anchor && input.orthogonal
      ? Math.abs(input.x - anchor.x) >= Math.abs(input.y - anchor.y)
        ? 'h'
        : 'v'
      : null

  for (const rule of rules) {
    const radius = rule.radiusPx / input.scale

    if (rule.kind === 'point') {
      let best: Pt | null = null
      let bestDist = radius
      for (const c of input.corners) {
        const d = Math.hypot(c.x - input.x, c.y - input.y)
        if (d <= bestDist) {
          bestDist = d
          best = c
        }
      }
      if (best) {
        return { x: best.x, y: best.y, kind: 'point', guide: { kind: 'point', x: best.x, y: best.y } }
      }
      continue
    }

    if (rule.kind === 'segment') {
      let hitWall: W | null = null
      let hitX = 0
      let hitY = 0
      let bestDist = radius
      for (const w of input.walls) {
        if (w.id === input.ignoreWallId) continue
        if (anchor && axis) {
          const cross = axisCross(anchor, axis, w)
          if (!cross) continue
          const d = axis === 'h' ? Math.abs(cross.x - input.x) : Math.abs(cross.y - input.y)
          if (d < bestDist) {
            bestDist = d
            hitWall = w
            hitX = cross.x
            hitY = cross.y
          }
          continue
        }
        const foot = projectOnSegment(input.x, input.y, w)
        if (foot.dist < bestDist) {
          bestDist = foot.dist
          hitWall = w
          hitX = foot.x
          hitY = foot.y
        }
      }
      if (hitWall) {
        return { x: hitX, y: hitY, kind: 'segment', wallId: hitWall.id, guide: { kind: 'segment', wall: hitWall } }
      }
      continue
    }

    // 网格：打开就无条件对齐到模数（不设半径），这是用户打开它的预期。
    const gx = Math.round(input.x / input.gridCm) * input.gridCm
    const gy = Math.round(input.y / input.gridCm) * input.gridCm
    return { x: gx, y: gy, kind: 'grid', guide: { kind: 'grid' } }
  }

  // 没有任何规则命中：仍然服从正交约束（预览必须和落库一致）。
  let x = input.x
  let y = input.y
  if (anchor && axis === 'h') y = anchor.y
  if (anchor && axis === 'v') x = anchor.x
  return { x, y, kind: 'none', guide: { kind: 'none' } }
}

export { distanceToSegment }
