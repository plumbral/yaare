import { previewPreferencesKey } from '../../shared/previewPreferences'
import { normalizeLanguagePreference, setLanguagePreference, type LanguagePreference } from '../../shared/i18n/locale'
import { normalizeUIFont, normalizeTerminalFont, normalizeFallbackFont, type UIFont, type TerminalFont, type FallbackFont } from '../../shared/fonts/fonts'
import { normalizeInterfacePalette, type InterfacePalette } from '../../shared/theme/palettes'
import { normalizeTerminalPalette, type TerminalPalette } from '../../shared/theme/terminalPresets'
import type { BuiltInIconStyle } from '../../shared/icons/OsIcon'
export interface Preferences {
  language: LanguagePreference
  mode: 'light' | 'dark' | 'system'
  interfacePalette: InterfacePalette
  terminalPalette: TerminalPalette
  iconStyle: BuiltInIconStyle
  fontSize: number
  scrollback: number
  cursorBlink: boolean
  screenReaderMode: boolean
  uiFont: UIFont
  fontFamily: TerminalFont
  fallbackFont: FallbackFont
  lineHeight: number
  autoReconnect: boolean
  detectServerSystem: boolean
  transferConcurrency: number
  directorySync: boolean
}
export const minTerminalFontSize = 8
export const maxTerminalFontSize = 32

export function stepTerminalFontSize(fontSize: number, step: number): number {
  return Math.min(maxTerminalFontSize, Math.max(minTerminalFontSize, fontSize + step))
}

export const defaultPreferences: Preferences = {
  language: 'system',
  mode: 'system',
  interfacePalette: 'neutral',
  terminalPalette: 'auto',
  iconStyle: 'monochrome',
  fontSize: 14,
  scrollback: 1000,
  cursorBlink: false,
  screenReaderMode: false,
  uiFont: 'Inter',
  fontFamily: 'Google Sans Code',
  fallbackFont: 'Maple Mono CN',
  lineHeight: 1.5,
  autoReconnect: true,
  detectServerSystem: false,
  transferConcurrency: 2,
  directorySync: false,
}
export function resolveAppearance(mode: Preferences['mode'], systemDark: boolean): 'light' | 'dark' {
  return mode === 'system' ? (systemDark ? 'dark' : 'light') : mode
}
export function normalizePreferences(value: unknown): Preferences {
  const source = value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  return {
    language: normalizeLanguagePreference(source.language),
    mode: source.mode === 'light' || source.mode === 'dark' ? source.mode : 'system',
    interfacePalette: normalizeInterfacePalette(source.interfacePalette),
    terminalPalette: normalizeTerminalPalette(source.terminalPalette),
    iconStyle: source.iconStyle === 'color' ? 'color' : 'monochrome',
    fontSize:
      typeof source.fontSize === 'number' &&
      Number.isInteger(source.fontSize) &&
      source.fontSize >= minTerminalFontSize &&
      source.fontSize <= maxTerminalFontSize
        ? source.fontSize
        : defaultPreferences.fontSize,
    scrollback: [1000, 3000, 5000].includes(source.scrollback as number)
      ? (source.scrollback as number)
      : 1000,
    cursorBlink: source.cursorBlink === true,
    screenReaderMode: source.screenReaderMode === true,
    uiFont: normalizeUIFont(source.uiFont),
    fontFamily: normalizeTerminalFont(source.fontFamily),
    fallbackFont: normalizeFallbackFont(source.fallbackFont),
    lineHeight:
      typeof source.lineHeight === 'number' && [1, 1.2, 1.5, 1.8, 2].includes(source.lineHeight)
        ? source.lineHeight
        : 1.5,
    autoReconnect: source.autoReconnect !== false,
    detectServerSystem: source.detectServerSystem === true,
    directorySync: source.directorySync === true,
    transferConcurrency: typeof source.transferConcurrency === 'number' && Number.isInteger(source.transferConcurrency) && source.transferConcurrency >= 1 && source.transferConcurrency <= 8 ? source.transferConcurrency : 2,
  }
}
export function readPreferences(): Preferences {
  try {
    const saved = localStorage.getItem(previewPreferencesKey)
    return normalizePreferences(JSON.parse(saved ?? 'null'))
  } catch {
    return { ...defaultPreferences }
  }
}
export function savePreferences(value: Preferences): boolean {
  setLanguagePreference(value.language)
  try {
    localStorage.setItem(previewPreferencesKey, JSON.stringify(normalizePreferences(value)))
    return true
  } catch {
    return false
  }
}
