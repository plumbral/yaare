import { expect, test } from 'bun:test'
import { resolveThemeTokens } from '@astryxdesign/core/theme/tokens'
import { interfacePaletteIds, interfacePalettes, interfaceTokens } from '../frontend/src/shared/theme/palettes'
import { terminalPaletteIds } from '../frontend/src/shared/theme/terminalPresets'
import { contrastRatio, luminance } from '../frontend/src/shared/theme/contrast'
import { normalizePreferences, savePreferences, readPreferences } from '../frontend/src/features/workspace/preferences'
import { yaareTheme } from '../frontend/src/shared/theme/yaare.js'
import { yaarePaperTheme } from '../frontend/src/shared/theme/yaare-paper.js'
import { yaarePurpleTheme } from '../frontend/src/shared/theme/yaare-purple.js'
import { yaareBlueTheme } from '../frontend/src/shared/theme/yaare-blue.js'
import { yaareGreenTheme } from '../frontend/src/shared/theme/yaare-green.js'
import { builtInIconColors } from '../frontend/src/shared/icons/builtInIconColors'
import { osIconOptions } from '../frontend/src/shared/icons/OsIcon'

test('every built-in icon has readable colors and appearance choice is device-local', () => {
  expect(Object.keys(builtInIconColors).sort()).toEqual(osIconOptions.map(option => option.value).sort())
  for (const colors of Object.values(builtInIconColors)) for (const palette of Object.values(interfacePalettes)) {
    expect(contrastRatio(colors[0], palette.surface[0])).toBeGreaterThanOrEqual(3)
    expect(contrastRatio(colors[1], palette.surface[1])).toBeGreaterThanOrEqual(3)
  }
  expect(normalizePreferences({}).iconStyle).toBe('monochrome')
  expect(normalizePreferences({iconStyle:'color'}).iconStyle).toBe('color')
})

test('production themes contain the reviewed palettes and work without runtime style injection', () => {
  const built = [yaareTheme, yaarePaperTheme, yaarePurpleTheme, yaareBlueTheme, yaareGreenTheme]
  for (const [i, id] of interfacePaletteIds.entries()) for (const mode of ['light', 'dark'] as const) {
    expect(built[i]!.__built).toBe(true)
    const resolved = resolveThemeTokens(built[i]!, { mode }), expected = resolveThemeTokens({ name: id, tokens: interfaceTokens(id) }, { mode })
    expect(resolved['--focus-outline-style'], id + '/' + mode).toBe('none')
    for (const token of Object.keys(interfaceTokens(id))) expect(resolved[token]).toBe(expected[token])
  }
})

test('five distinct UI palettes keep text, controls, and semantic states readable in both modes', () => {
  for (const [index, mode] of ['light', 'dark'].entries()) {
    expect(new Set(interfacePaletteIds.map(id => interfacePalettes[id].body[index])).size).toBe(5)
    for (const id of interfacePaletteIds) {
      const p = interfacePalettes[id], tokens = resolveThemeTokens({ name: id, tokens: interfaceTokens(id) }, { mode: mode as 'light' | 'dark' })
      const rgb = p.body[index]!.slice(1).match(/../g)!.map(c => parseInt(c, 16) / 255)
      expect(Math.max(...rgb) - Math.min(...rgb), `${id}/${mode} surface chroma`).toBeLessThan(.1)
      for (const bg of [p.body[index]!, p.surface[index]!, p.popover[index]!, p.muted[index]!]) expect(contrastRatio(p.text[index]!, bg), `${id}/${mode} text`).toBeGreaterThanOrEqual(4.5)
      for (const bg of [p.body[index]!, p.surface[index]!, p.popover[index]!]) {
        expect(contrastRatio(p.secondary[index]!, bg), `${id}/${mode} secondary`).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(tokens['--focus-outline-color']!, bg), `${id}/${mode} focus`).toBeGreaterThanOrEqual(3)
        expect(contrastRatio(p.controlBorder[index]!, bg), `${id}/${mode} control`).toBeGreaterThanOrEqual(3)
        for (const role of ['info','success','warning','error']) expect(contrastRatio(tokens['--color-text-'+role]!, bg), `${id}/${mode}/${role}`).toBeGreaterThanOrEqual(4.5)
      }
      expect(contrastRatio(tokens['--color-on-accent']!, p.accent[index]!)).toBeGreaterThanOrEqual(4.5)
    }
  }
  expect(luminance(interfacePalettes.neutral.surface[0])).toBeLessThan(luminance('#ffffff'))
  expect(luminance(interfacePalettes.neutral.text[1])).toBeLessThan(luminance('#ffffff'))
})
test('partial and unknown preferences normalize while retaining font, language and connection choices', () => {
  const partial = { mode: 'light', language: 'ja', fontSize: 18, directorySync: true }
  expect(normalizePreferences(partial)).toMatchObject({ ...partial, interfacePalette: 'neutral', terminalPalette: 'auto' })
  expect(normalizePreferences({ ...partial, interfacePalette: 'future', terminalPalette: 'future' })).toEqual(normalizePreferences(partial))
})
test('UI and terminal choices persist independently through the preference path used by imports', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage'), values = new Map<string, string>()
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) } })
  try {
    for (const interfacePalette of interfacePaletteIds) for (const terminalPalette of terminalPaletteIds) {
      const p = normalizePreferences({ interfacePalette, terminalPalette, mode: 'system', fontSize: 18 })
      expect(savePreferences(p)).toBe(true); expect(readPreferences()).toEqual(p)
    }
  } finally { if (previous) Object.defineProperty(globalThis, 'localStorage', previous); else Reflect.deleteProperty(globalThis, 'localStorage') }
})
