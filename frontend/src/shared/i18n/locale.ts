import { previewPreferencesKey, usesPreviewStorage } from '../previewPreferences'
import { useSyncExternalStore } from 'react'
import { isMyGo, locale as hostLocale, onLocaleChange } from 'mygo-runtime'
import { appCatalogs, loadLanguage } from './catalogs'

export type Language = 'en' | 'zh-Hans' | 'zh-Hant' | 'ja'
export type LanguagePreference = Language | 'system'
export const languageOptions = [
  { value: 'system', label: '跟随系统' },
  { value: 'zh-Hans', label: '简体中文' },
  { value: 'en', label: 'English' },
  { value: 'zh-Hant', label: '繁體中文' },
  { value: 'ja', label: '日本語' },
] as const
export function normalizeLanguagePreference(value: unknown): LanguagePreference {
  return value === 'en' || value === 'zh-Hans' || value === 'zh-Hant' || value === 'ja' ? value : 'system'
}
export function resolveLanguage(tag: string): Language {
  if (/^zh(?:-|$)/i.test(tag)) return /(?:^|-)(?:Hant|TW|HK|MO)(?:-|$)/i.test(tag) ? 'zh-Hant' : 'zh-Hans'
  return /^ja(?:-|$)/i.test(tag) ? 'ja' : 'en'
}
function systemLanguage(): Language {
  if (typeof window === 'undefined') return 'zh-Hans'
  return resolveLanguage(isMyGo() ? hostLocale() : navigator.language)
}
function readLanguage(): LanguagePreference {
  try { return normalizeLanguagePreference(JSON.parse(localStorage.getItem(previewPreferencesKey) || '{}').language) }
  catch { return 'system' }
}
let preference: LanguagePreference = usesPreviewStorage() ? readLanguage() : 'system'
let language: Language = 'en'
let revision = 0
let pending: Promise<void> | undefined
const listeners = new Set<() => void>()
export const getLanguage = () => language
function publishLanguage(next: Language) {
  const changed = next !== language
  language = next
  if (typeof document !== 'undefined') document.documentElement.lang = language
  if (changed) for (const listener of listeners) listener()
}
export function setLanguagePreference(next: LanguagePreference): Promise<void> {
  preference = next
  const resolved = next === 'system' ? systemLanguage() : next
  const requested = ++revision
  if (resolved === 'en' || appCatalogs[resolved]) {
    pending = undefined
    publishLanguage(resolved)
    return Promise.resolve()
  }
  pending = loadLanguage(resolved).then(
    () => { if (requested === revision) publishLanguage(resolved) },
    () => { if (requested === revision) publishLanguage(language) },
  ).finally(() => { if (requested === revision) pending = undefined })
  return pending
}
export async function initializeLanguage() {
  void setLanguagePreference(preference)
  while (pending) await pending
}
export const subscribeLanguage = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
export const useLocale = () => useSyncExternalStore(subscribeLanguage, getLanguage, getLanguage)
if (typeof window !== 'undefined') {
  window.addEventListener('languagechange', () => setLanguagePreference(preference))
  window.addEventListener('storage', event => { if (usesPreviewStorage() && event.key === previewPreferencesKey) setLanguagePreference(readLanguage()) })
  if (isMyGo()) onLocaleChange(() => setLanguagePreference(preference))
}
