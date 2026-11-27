import { describe, expect, it } from 'vitest'
import { DEFAULT_CANVAS_THEME, readCanvasTheme } from './useCanvasTheme.ts'
import { resolveItemIcon } from './icons.tsx'

describe('useCanvasTheme', () => {
  it('returns default canvas theme when no element is provided', () => {
    expect(readCanvasTheme(null)).toEqual(DEFAULT_CANVAS_THEME)
    expect(DEFAULT_CANVAS_THEME.wall).toBeTruthy()
    expect(DEFAULT_CANVAS_THEME.deviceOn).toBeTruthy()
  })
})

describe('resolveItemIcon', () => {
  it('resolves explicit lucide icon names and falls back by typeKey/kind', () => {
    const light = resolveItemIcon('lightbulb', 'light', 'device')
    const ac = resolveItemIcon(undefined, 'air_conditioner', 'device')
    const sofa = resolveItemIcon(undefined, 'sofa_3seat', 'furniture')
    const unknownDevice = resolveItemIcon(undefined, 'custom_xyz', 'device')
    const unknownFurniture = resolveItemIcon(undefined, 'custom_xyz', 'furniture')

    expect(light).toBeDefined()
    expect(ac).toBeDefined()
    expect(sofa).toBeDefined()
    expect(unknownDevice).not.toBe(unknownFurniture)
  })
})
