import { afterEach, expect, test } from 'bun:test'
import { defaultPreferences, type Preferences } from '../frontend/src/features/workspace/preferences'
import { settingsSections, readSettingsNavigation, saveSettingsNavigation, resetSettingsSection, canResetSettingsSection } from '../frontend/src/features/workspace/settingsNavigation'

const originalStorage = globalThis.localStorage
afterEach(() => Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: originalStorage }))
function storage() {
  const values = new Map<string, string>()
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  } })
  return values
}

test('settings remember their category and independent scroll positions without writing portable preferences', () => {
  const values = storage()
  localStorage.setItem('yaare.preview.preferences.v1', '{"language":"ja"}')
  expect(readSettingsNavigation()).toEqual({ section: 'general', scroll: {} })
  saveSettingsNavigation({ section: 'terminal', scroll: { terminal: 530.5, sync: 120, general: 0 } })
  expect(readSettingsNavigation()).toEqual({ section: 'terminal', scroll: { terminal: 530.5, sync: 120, general: 0 } })
  expect(values.get('yaare.preview.preferences.v1')).toBe('{"language":"ja"}')
  expect(values.size).toBe(2)
})

test('invalid navigation data falls back locally and never prevents opening settings', () => {
  const values = storage()
  values.set('yaare.settings-navigation.v1', '{"section":"retired","scroll":{"terminal":-1,"sync":200,"about":"30","retired":200}}')
  expect(readSettingsNavigation()).toEqual({ section: 'general', scroll: { sync: 200 } })
  values.set('yaare.settings-navigation.v1', '{broken')
  expect(readSettingsNavigation()).toEqual({ section: 'general', scroll: {} })
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, get() { throw Error('Storage unavailable') } })
  expect(readSettingsNavigation()).toEqual({ section: 'general', scroll: {} })
  expect(() => saveSettingsNavigation({ section: 'terminal', scroll: {} })).not.toThrow()
})

test('each preference page resets its own values while preserving every other preference', () => {
  const changed: Preferences = {
    language: 'ja', mode: 'light', interfacePalette: 'green', terminalPalette: 'ayu-auto', uiFont: 'system',
    fontFamily: 'system-mono', fallbackFont: 'system', fontSize: 20, lineHeight: 2, scrollback: 5000, cursorBlink: true, screenReaderMode: true,
    iconStyle: 'color', autoReconnect: false, detectServerSystem: true, directorySync: true, transferConcurrency: 8,
  }
  const sections = {
    general: ['language', 'detectServerSystem'], appearance: ['uiFont', 'mode', 'interfacePalette', 'iconStyle'],
    terminal: ['terminalPalette', 'fontFamily', 'fallbackFont', 'fontSize', 'lineHeight', 'scrollback', 'cursorBlink', 'screenReaderMode'],
    connection: ['autoReconnect', 'directorySync', 'transferConcurrency'],
  }
  for (const section of settingsSections) {
    const result = resetSettingsSection(section, changed)
    expect(canResetSettingsSection(section)).toBe(section in sections)
    for (const key of Object.keys(changed) as (keyof Preferences)[]) {
      const reset = section in sections && sections[section as keyof typeof sections].includes(key)
      expect(result[key], `${section}/${key}`).toBe(reset ? defaultPreferences[key] : changed[key])
    }
    if (!(section in sections)) expect(result).toBe(changed)
  }
  expect(changed.language).toBe('ja')
  expect(changed.directorySync).toBe(true)
})
