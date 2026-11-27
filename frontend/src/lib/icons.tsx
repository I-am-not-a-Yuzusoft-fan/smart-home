import {
  AirVent,
  Armchair,
  Bath,
  BedDouble,
  Blinds,
  Camera,
  Cpu,
  DoorOpen,
  Droplets,
  Fan,
  Flame,
  Home,
  Lamp,
  LampCeiling,
  Lightbulb,
  Lock,
  Monitor,
  Plug,
  Radar,
  Radio,
  ShieldAlert,
  Sofa,
  Speaker,
  Sun,
  Table2,
  Thermometer,
  Tv,
  Utensils,
  Wind,
  type LucideIcon,
} from 'lucide-react'
import type { ItemKind } from '../api/client.ts'

const ICON_MAP: Record<string, LucideIcon> = {
  lightbulb: Lightbulb,
  light: Lightbulb,
  lamp: Lamp,
  ceiling_light: LampCeiling,
  'lamp-ceiling': LampCeiling,
  air_conditioner: AirVent,
  ac: AirVent,
  'air-vent': AirVent,
  fan: Fan,
  wind: Wind,
  curtain: Blinds,
  blinds: Blinds,
  tv: Tv,
  monitor: Monitor,
  speaker: Speaker,
  plug: Plug,
  socket: Plug,
  lock: Lock,
  smart_lock: Lock,
  camera: Camera,
  thermometer: Thermometer,
  temp_sensor: Thermometer,
  humidity: Droplets,
  droplets: Droplets,
  lux: Sun,
  light_sensor: Sun,
  sun: Sun,
  motion: Radar,
  presence: Radar,
  occupancy: Radar,
  radar: Radar,
  sensor: Radio,
  alarm: ShieldAlert,
  smoke: Flame,
  sofa: Sofa,
  couch: Sofa,
  chair: Armchair,
  armchair: Armchair,
  bed: BedDouble,
  table: Table2,
  desk: Table2,
  door: DoorOpen,
  bath: Bath,
  kitchen: Utensils,
  home: Home,
}

/**
 * 解析 item_type.icons 或 typeKey 对应的 Lucide 图标组件。
 * 后端未指定图标名时，按类型键关键字和 kind 自动回退到最贴切的图标。
 */
export function resolveItemIcon(
  iconName?: string | null,
  typeKey?: string,
  kind?: ItemKind,
): LucideIcon {
  const candidates = [iconName, typeKey]
    .filter((s): s is string => Boolean(s && s.trim()))
    .map((s) => s.trim().toLowerCase())

  for (const raw of candidates) {
    if (ICON_MAP[raw]) return ICON_MAP[raw]
    const normalized = raw.replace(/[\s-]+/g, '_')
    if (ICON_MAP[normalized]) return ICON_MAP[normalized]
    for (const [key, icon] of Object.entries(ICON_MAP)) {
      if (normalized.includes(key)) return icon
    }
  }

  return kind === 'device' ? Cpu : Armchair
}

export function ItemIcon({
  iconName,
  typeKey,
  kind,
  className = 'h-4 w-4',
}: {
  iconName?: string | null
  typeKey?: string
  kind?: ItemKind
  className?: string
}) {
  const Icon = resolveItemIcon(iconName, typeKey, kind)
  return <Icon className={className} />
}
