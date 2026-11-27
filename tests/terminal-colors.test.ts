import { expect, test } from 'bun:test'
import { resolveThemeTokens } from '@astryxdesign/core/theme/tokens'
import { terminalColors, terminalTheme, terminalContrastRatio, getTerminalColors, terminalToken } from '../frontend/src/shared/theme/terminalColors'
import { terminalPaletteFamilies, terminalPresetSources, normalizeTerminalPalette, resolveTerminalPalette, terminalPaletteSelection, ansiNames, terminalPaletteMode } from '../frontend/src/shared/theme/terminalPresets'
import { contrastRatio, luminance } from '../frontend/src/shared/theme/contrast'
import { interfacePaletteIds } from '../frontend/src/shared/theme/palettes'
import { yaareTheme } from '../frontend/src/shared/theme/yaare.js'
import { yaarePaperTheme } from '../frontend/src/shared/theme/yaare-paper.js'
import { yaarePurpleTheme } from '../frontend/src/shared/theme/yaare-purple.js'
import { yaareBlueTheme } from '../frontend/src/shared/theme/yaare-blue.js'
import { yaareGreenTheme } from '../frontend/src/shared/theme/yaare-green.js'

test('terminal picker separates following the interface from a named automatic theme', () => {
  expect(terminalPaletteSelection('auto').mode).toBe('follow')
  for (const family of terminalPaletteFamilies) {
    expect(terminalPaletteSelection(family.id)).toEqual({ family, mode: 'system' })
    expect(terminalPaletteSelection(family.light)).toEqual({ family, mode: 'light' })
    expect(terminalPaletteSelection(family.dark)).toEqual({ family, mode: 'dark' })
    expect(resolveTerminalPalette(family.id, 'light')).toBe(family.light)
    expect(resolveTerminalPalette(family.id, 'dark')).toBe(family.dark)
    expect(resolveTerminalPalette(family.dark, 'light')).toBe(family.dark)
  }
})

const themes = { neutral: yaareTheme, paper: yaarePaperTheme, purple: yaarePurpleTheme, blue: yaareBlueTheme, green: yaareGreenTheme }
const modes = ['light', 'dark'] as const
const colorKeys = Object.keys(terminalColors) as (keyof typeof terminalColors)[]

function checkContrast(colors: ReturnType<typeof getTerminalColors>, readableBlack: string, label: string) {
  for (const name of ['foreground', ...ansiNames.filter(name => name !== 'black')] as const)
    expect(contrastRatio(colors[name], colors.background), `${label}/${name}`).toBeGreaterThanOrEqual(terminalContrastRatio)
  expect(contrastRatio(readableBlack, colors.background), `${label}/black as text`).toBeGreaterThanOrEqual(terminalContrastRatio)
  expect(contrastRatio(colors.selectionForeground, colors.selectionBackground), `${label}/selection`).toBeGreaterThanOrEqual(terminalContrastRatio)
  expect(contrastRatio(colors.selectionForeground, colors.selectionInactiveBackground), `${label}/inactive selection`).toBeGreaterThanOrEqual(terminalContrastRatio)
  expect(contrastRatio(colors.cursor, colors.background), `${label}/cursor`).toBeGreaterThanOrEqual(terminalContrastRatio)
  expect(contrastRatio(colors.cursorAccent, colors.cursor), `${label}/cursor text`).toBeGreaterThanOrEqual(terminalContrastRatio)
}

for (const id of interfacePaletteIds) for (const mode of modes) {
  test(`automatic terminal palette follows ${id}/${mode} and meets contrast requirements`, () => {
    const tokens=resolveThemeTokens(themes[id],{mode})
    const resolved=terminalTheme(tokens)
    const colors=getTerminalColors('auto',mode,id)
    const frame = tokens['--color-workspace-frame']!
    expect(luminance(colors.background)).toBeGreaterThan(luminance(frame))
    expect(contrastRatio(colors.background, frame)).toBeLessThan(1.2)
    for(const name of colorKeys) {
      expect(resolved[name]).toBe(colors[name])
      expect(resolved[name]).toMatch(/^#[a-f0-9]{6}$/)
    }
    expect(colors.black).toBe(terminalColors.black[mode==='light'?0:1])
    checkContrast(colors,tokens[terminalToken('readableBlack')]!,`${id}/${mode}`)
  })
}

test('all ten automatic variants have distinct backgrounds and every interface palette tunes ANSI hues', () => {
  const backgrounds = new Set(interfacePaletteIds.flatMap(id=>modes.map(mode=>getTerminalColors('auto',mode,id).background)))
  expect(backgrounds.size).toBe(10)
  for(const mode of modes)for(const name of ['red','green','yellow','blue','magenta','cyan'] as const)
    expect(new Set(interfacePaletteIds.map(id=>getTerminalColors('auto',mode,id)[name])).size).toBe(5)
  expect(terminalPaletteMode('auto', 'light')).toBe('light')
  expect(terminalPaletteMode('auto', 'dark')).toBe('dark')
})

test('fixed terminal palettes resolve the same readable colors under every interface palette and mode', () => {
  for(const id of terminalPaletteFamilies.flatMap(family=>[family.light,family.dark])) {
    const colors=getTerminalColors(id,'light')
    for (const interfacePalette of interfacePaletteIds) for (const mode of modes) {
      const tokens=resolveThemeTokens(themes[interfacePalette],{mode})
      expect(getTerminalColors(id,mode,interfacePalette)).toEqual(colors)
      const result=terminalTheme(tokens,id)
      for(const name of colorKeys)expect(result[name]).toBe(colors[name])
      checkContrast(colors,tokens[terminalToken('readableBlack',id)]!,id)
    }
  }
  expect(terminalPaletteMode('ayu-dark', 'light')).toBe('dark')
  expect(terminalPaletteMode('solarized-light', 'dark')).toBe('light')
})

test('every paired palette follows only brightness and has two concrete, readable variants', () => {
  expect(terminalPresetSources.map(preset=>preset.id).sort()).toEqual(terminalPaletteFamilies.flatMap(family=>[family.light,family.dark]).sort())
  expect(normalizeTerminalPalette('dracula')).toBe('auto')
  expect(normalizeTerminalPalette('nord')).toBe('auto')
  for(const family of terminalPaletteFamilies) {
    expect(getTerminalColors(family.id,'light').background).not.toBe(getTerminalColors(family.id,'dark').background)
    for(const mode of modes)for(const ui of interfacePaletteIds) {
      const fixed=resolveTerminalPalette(family.id,mode)
      expect(fixed).toBe(family[mode])
      expect(terminalPaletteMode(fixed,mode)).toBe(mode)
      expect(getTerminalColors(family.id,mode,ui)).toEqual(getTerminalColors(family[mode],mode))
    }
  }
})
