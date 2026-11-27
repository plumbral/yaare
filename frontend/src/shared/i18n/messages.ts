import { createCatalog } from 'mygo-runtime/i18n'
import type { Messages } from '../generated/locales'
import { appCatalogs } from './catalogs'
import { getSourceKeys } from './sources.generated'
import { getLanguage, type Language } from './locale'

let sourcesByKey: Map<string, string> | undefined
const params: Record<string, string[]> = {}
const catalog = createCatalog<Messages>({ source: 'en', languages: ['en', 'zh-Hans', 'zh-Hant', 'ja'],
  fallbacks: { en: ['en'], 'zh-Hans': ['zh-Hans', 'en'], 'zh-Hant': ['zh-Hant', 'en'], ja: ['ja', 'en'] },
  params, messages: appCatalogs as Record<string, Record<string, string>> })
let indexedParameters = false
export function messagesIn(language: Language) {
  if (!indexedParameters) {
    for (const [source, key] of Object.entries(getSourceKeys())) {
      params[key] = [...source.matchAll(/\{(p\d+)\}/g)].map(match => match[1]!)
    }
    indexedParameters = true
  }
  return catalog.messagesIn(language)
}
const parameters = new Map<string, { source: string; values: unknown[] }>()
const reverse = new Map<string, string>()
const indexedLanguages = new Set<Language>()
let templates: { source: string; prefix: string; pattern: RegExp }[] | undefined
export function tr(source: string, ...values: unknown[]): string {
  const key = getSourceKeys()[source]
  if (!key) return source
  const translated = catalog.format(getLanguage(), key, Object.fromEntries(values.map((value, index) => ['p' + index, value])))
  if (values.length) {
    if (parameters.size >= 256) parameters.delete(parameters.keys().next().value!)
    parameters.set(translated, { source, values })
  }
  return translated
}
export function localizeMessage(value: string): string {
  if (!value) return value
  const message = parameters.get(value)
  if (message) return tr(message.source, ...message.values)
  if (getSourceKeys()[value]) return tr(value)
  sourcesByKey ??= new Map(Object.entries(getSourceKeys()).map(([source, key]) => [key, source]))
  for (const language of catalog.languages as readonly Language[]) {
    if (!appCatalogs[language] || indexedLanguages.has(language)) continue
    for (const [key, source] of sourcesByKey) {
      if (/\{p\d+\}/.test(source)) continue
      reverse.set(catalog.format(language, key), source)
    }
    indexedLanguages.add(language)
  }
  const source = reverse.get(value)
  if (source) return tr(source)
  templates ??= Object.keys(getSourceKeys()).filter(source => /\{p\d+\}/.test(source)).map(source => {
    const segments = source.split(/\{p\d+\}/)
    const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    return { source, prefix: segments[0]!, pattern: new RegExp('^' + segments.map(escape).join('([\\s\\S]*?)') + '$') }
  })
  if (value.length <= 8192) for (const template of templates) {
    if (!value.startsWith(template.prefix)) continue
    const match = template.pattern.exec(value)
    if (match) return tr(template.source, ...match.slice(1))
  }
  return value
}
