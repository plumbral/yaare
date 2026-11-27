import { defaultPreferences, type Preferences } from './preferences'

export const settingsSections = ['general', 'appearance', 'terminal', 'connection', 'sync', 'shortcuts', 'about'] as const
export type SettingsSection = typeof settingsSections[number]
export interface SettingsNavigationState {
  section: SettingsSection
  scroll: Partial<Record<SettingsSection, number>>
}
const storageKey = 'yaare.settings-navigation.v1'
export function isSettingsSection(value: unknown): value is SettingsSection {
  return settingsSections.includes(value as SettingsSection)
}
export function readSettingsNavigation(): SettingsNavigationState {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || 'null')
    const scroll: SettingsNavigationState['scroll'] = {}
    for (const section of settingsSections) {
      const position = saved?.scroll?.[section]
      if (typeof position === 'number' && Number.isFinite(position) && position >= 0) scroll[section] = position
    }
    return { section: isSettingsSection(saved?.section) ? saved.section : 'general', scroll }
  } catch { return { section: 'general', scroll: {} } }
}
export function saveSettingsNavigation(value: SettingsNavigationState) {
  try { localStorage.setItem(storageKey, JSON.stringify(value)) } catch {}
}

const resetKeys = {
  general: ['language', 'detectServerSystem'],
  appearance: ['uiFont', 'mode', 'interfacePalette', 'iconStyle'],
  terminal: ['terminalPalette', 'fontFamily', 'fallbackFont', 'fontSize', 'lineHeight', 'scrollback', 'cursorBlink', 'screenReaderMode'],
  connection: ['autoReconnect', 'directorySync', 'transferConcurrency'],
} as const satisfies Partial<Record<SettingsSection, readonly (keyof Preferences)[]>>
export function canResetSettingsSection(section: SettingsSection): section is keyof typeof resetKeys {
  return section in resetKeys
}
export function resetSettingsSection(section: SettingsSection, current: Preferences): Preferences {
  if (!canResetSettingsSection(section)) return current
  return { ...current, ...Object.fromEntries(resetKeys[section].map(key => [key, defaultPreferences[key]])) }
}
