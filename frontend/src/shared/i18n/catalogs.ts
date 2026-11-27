import type { ProviderMessagesByLocale } from '@astryxdesign/core/i18n'
import en from '../../../../locales/en.json'
import type { Language } from './locale'

const loaders = {
  'zh-Hans': () => Promise.all([import('../../../../locales/zh-Hans.json'), import('@astryxdesign/core/locales/zh-CN.generated.js')]),
  'zh-Hant': () => Promise.all([import('../../../../locales/zh-Hant.json'), import('@astryxdesign/core/locales/zh-TW.generated.js')]),
  ja: () => Promise.all([import('../../../../locales/ja.json'), import('@astryxdesign/core/locales/ja-JP.generated.js')]),
}
const astryxLocales = { en: 'en', 'zh-Hans': 'zh-CN', 'zh-Hant': 'zh-TW', ja: 'ja-JP' } as const
export const appCatalogs: Partial<Record<Language, Record<string, string>>> = { en }
const astryxMessages: Partial<Record<Language, ProviderMessagesByLocale>> = {}
const loading = new Map<Language, Promise<void>>()

export function loadLanguage(language: Exclude<Language, 'en'>): Promise<void> {
  if (appCatalogs[language]) return Promise.resolve()
  const pending = loading.get(language)
  if (pending) return pending
  const request = loaders[language]().then(([app, astryx]) => {
    appCatalogs[language] = app.default
    astryxMessages[language] = { [astryxLocales[language]]: astryx.default }
  }).finally(() => { loading.delete(language) })
  loading.set(language, request)
  return request
}

export const getAstryxLocale = (language: Language) => astryxLocales[language]
export const getAstryxMessages = (language: Language) => astryxMessages[language]
