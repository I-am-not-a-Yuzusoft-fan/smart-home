import { useEffect, useState } from 'react'

export type CanvasTheme = {
  bg: string
  grid: string
  gridMajor: string
  roomFill: string
  roomFillSelected: string
  roomStroke: string
  roomStrokeSelected: string
  wall: string
  wallHover: string
  wallSelected: string
  openingFill: string
  openingStroke: string
  openingWindow: string
  openingDoor: string
  selection: string
  deviceOn: string
  deviceOnGlow: string
  deviceCool: string
  deviceCoolGlow: string
  deviceOff: string
  deviceOffline: string
  furnitureFill: string
  deviceFill: string
  label: string
  labelMuted: string
  labelHalo: string
}

export const DEFAULT_CANVAS_THEME: CanvasTheme = {
  bg: '#f1f5f9',
  grid: '#e2e8f0',
  gridMajor: '#cbd5e1',
  roomFill: 'rgba(37, 99, 235, 0.08)',
  roomFillSelected: 'rgba(37, 99, 235, 0.20)',
  roomStroke: 'rgba(37, 99, 235, 0.30)',
  roomStrokeSelected: '#2563eb',
  wall: '#334155',
  wallHover: '#475569',
  wallSelected: '#2563eb',
  openingFill: '#ffffff',
  openingStroke: '#94a3b8',
  openingWindow: '#0ea5e9',
  openingDoor: '#475569',
  selection: '#2563eb',
  deviceOn: '#f59e0b',
  deviceOnGlow: 'rgba(245, 158, 11, 0.35)',
  deviceCool: '#0ea5e9',
  deviceCoolGlow: 'rgba(14, 165, 233, 0.35)',
  deviceOff: '#94a3b8',
  deviceOffline: '#ef4444',
  furnitureFill: '#e8e1d4',
  deviceFill: '#dbeafe',
  label: '#1e293b',
  labelMuted: '#64748b',
  labelHalo: '#ffffff',
}

function readVar(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  const value = styles.getPropertyValue(name).trim()
  return value || fallback
}

export function readCanvasTheme(el?: HTMLElement | null): CanvasTheme {
  if (typeof window === 'undefined' || !el) return DEFAULT_CANVAS_THEME
  const styles = window.getComputedStyle(el)
  const accent = readVar(styles, '--canvas-selection', DEFAULT_CANVAS_THEME.selection)
  return {
    bg: readVar(styles, '--canvas-bg', DEFAULT_CANVAS_THEME.bg),
    grid: readVar(styles, '--canvas-grid', DEFAULT_CANVAS_THEME.grid),
    gridMajor: readVar(styles, '--canvas-grid-major', DEFAULT_CANVAS_THEME.gridMajor),
    roomFill: DEFAULT_CANVAS_THEME.roomFill,
    roomFillSelected: DEFAULT_CANVAS_THEME.roomFillSelected,
    roomStroke: DEFAULT_CANVAS_THEME.roomStroke,
    roomStrokeSelected: accent,
    wall: readVar(styles, '--canvas-wall', DEFAULT_CANVAS_THEME.wall),
    wallHover: DEFAULT_CANVAS_THEME.wallHover,
    wallSelected: readVar(styles, '--canvas-wall-selected', accent),
    openingFill: readVar(styles, '--bg-surface', DEFAULT_CANVAS_THEME.openingFill),
    openingStroke: readVar(styles, '--canvas-room-stroke', DEFAULT_CANVAS_THEME.openingStroke),
    openingWindow: readVar(styles, '--canvas-opening', DEFAULT_CANVAS_THEME.openingWindow),
    openingDoor: readVar(styles, '--canvas-wall', DEFAULT_CANVAS_THEME.openingDoor),
    selection: accent,
    deviceOn: readVar(styles, '--canvas-device-on', DEFAULT_CANVAS_THEME.deviceOn),
    deviceOnGlow: readVar(styles, '--canvas-device-on-glow', DEFAULT_CANVAS_THEME.deviceOnGlow),
    deviceCool: readVar(styles, '--canvas-device-cool', DEFAULT_CANVAS_THEME.deviceCool),
    deviceCoolGlow: readVar(styles, '--canvas-device-cool-glow', DEFAULT_CANVAS_THEME.deviceCoolGlow),
    deviceOff: readVar(styles, '--canvas-device-off', DEFAULT_CANVAS_THEME.deviceOff),
    deviceOffline: readVar(styles, '--canvas-device-offline', DEFAULT_CANVAS_THEME.deviceOffline),
    furnitureFill: DEFAULT_CANVAS_THEME.furnitureFill,
    deviceFill: readVar(styles, '--accent-soft', DEFAULT_CANVAS_THEME.deviceFill),
    label: readVar(styles, '--canvas-label', DEFAULT_CANVAS_THEME.label),
    labelMuted: readVar(styles, '--canvas-label-muted', DEFAULT_CANVAS_THEME.labelMuted),
    labelHalo: readVar(styles, '--bg-surface', DEFAULT_CANVAS_THEME.labelHalo),
  }
}

/**
 * Konva Canvas 主题桥接 Hook。
 * 监听 <html data-theme="..."> 与 style 变更，保证 Phase B 换肤时画布颜色一帧同步。
 */
export function useCanvasTheme(): CanvasTheme {
  const [theme, setTheme] = useState<CanvasTheme>(() =>
    typeof document !== 'undefined' ? readCanvasTheme(document.documentElement) : DEFAULT_CANVAS_THEME,
  )

  useEffect(() => {
    const root = document.documentElement
    const refresh = () => setTheme(readCanvasTheme(root))
    refresh()

    const observer = new MutationObserver(refresh)
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme', 'style', 'class'] })
    return () => observer.disconnect()
  }, [])

  return theme
}
