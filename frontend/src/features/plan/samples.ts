/** 底图示例：公开的老图纸扫描件 + 脚本生成的现代 CAD 风格图。
 *
 * 路径是 `public/` 下的绝对 URL，运行时按需 fetch 成 File 再走上传接口——
 * 不值得在仓库里再造一套「示例图专用的上传接口」。
 */
export type UnderlaySample = {
  /** public 下的绝对路径 */
  path: string
  label: string
  /** 合成图还是档案扫描件，只用于分组标题 */
  group: 'synthetic' | 'scan'
}

const SYNTHETIC_DIR = '/underlays/samples/synthetic'
const SCAN_DIR = '/underlays/samples'

export const UNDERLAY_SAMPLES: UnderlaySample[] = [
  { path: `${SYNTHETIC_DIR}/one-bed-45.png`, label: '一居室 45 ㎡', group: 'synthetic' },
  { path: `${SYNTHETIC_DIR}/two-bed-78.png`, label: '两居室 78 ㎡', group: 'synthetic' },
  { path: `${SYNTHETIC_DIR}/three-bed-110.png`, label: '三居双卫 110 ㎡', group: 'synthetic' },
  { path: `${SYNTHETIC_DIR}/l-shape-88.png`, label: 'L 形异形 88 ㎡', group: 'synthetic' },
  { path: `${SYNTHETIC_DIR}/duplex-96.png`, label: '复式 96 ㎡', group: 'synthetic' },
  { path: `${SYNTHETIC_DIR}/office-180.png`, label: '办公大开间 180 ㎡', group: 'synthetic' },
  { path: `${SCAN_DIR}/massachusetts-ave-1785.jpg`, label: '1922 公寓标准层（含弧形墙）', group: 'scan' },
  { path: `${SCAN_DIR}/nara-dwelling-1f.jpg`, label: '灯塔局住宅一层', group: 'scan' },
  { path: `${SCAN_DIR}/ballymun-flat-1965.jpg`, label: '1965 都柏林公寓', group: 'scan' },
  { path: `${SCAN_DIR}/caroline-court-vancouver.png`, label: '1913 温哥华小公寓', group: 'scan' },
  { path: `${SCAN_DIR}/hampton-court-1912.png`, label: '1912 板式公寓', group: 'scan' },
]

/** 把示例图取成 File，复用上传那条链路：示例与用户上传不该有第二套代码。 */
export async function sampleAsFile(sample: UnderlaySample): Promise<File> {
  const res = await fetch(sample.path)
  if (!res.ok) throw new Error(`示例图加载失败（${res.status}）`)
  const blob = await res.blob()
  const name = sample.path.split('/').pop() ?? 'sample.png'
  return new File([blob], name, { type: blob.type || 'image/png' })
}
