import {systemMonoFamilies} from './platformFonts'
import {isLocalFont, localFontName, type LocalFont} from './localFontId'
import {localFonts} from './localFonts'
export const uiFonts = ['Inter', 'system'] as const
export const terminalFonts = ['Google Sans Code', 'system-mono'] as const
export const fallbackFonts = ['Maple Mono CN', 'system'] as const
export type UIFont = typeof uiFonts[number] | LocalFont
export type TerminalFont = typeof terminalFonts[number] | LocalFont
export type FallbackFont = typeof fallbackFonts[number] | LocalFont
export const normalizeUIFont = (value: unknown): UIFont => isLocalFont(value) || uiFonts.includes(value as typeof uiFonts[number]) ? value as UIFont : 'Inter'
export const normalizeTerminalFont = (value: unknown): TerminalFont => isLocalFont(value) || terminalFonts.includes(value as typeof terminalFonts[number]) ? value as TerminalFont : 'Google Sans Code'
export const normalizeFallbackFont = (value: unknown): FallbackFont => isLocalFont(value) || fallbackFonts.includes(value as typeof fallbackFonts[number]) ? value as FallbackFont : 'Maple Mono CN'
const bundled = new Set(['Inter', 'Google Sans Code', 'Maple Mono CN'])
export const isBundledFont = (id: string) => bundled.has(id)
export const isSystemFont = (id: string) => id === 'system' || id === 'system-mono'
const systemUI = (language: string) => 'system-ui, -apple-system, "Segoe UI", ' + (language === 'zh-Hant' ? '"Microsoft JhengHei UI"' : language === 'ja' ? '"Yu Gothic UI"' : '"Microsoft YaHei UI"') + ', sans-serif'
const quote = (value: string) => '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'
export function fontFamilies(id: string): string[] {
  const local = localFontName(id)
  if (local) return [local]
  if (id === 'Maple Mono CN') return ['Yaare Maple Mono CN']
  if (id === 'system-mono') return systemMonoFamilies()
  return [id]
}
export const uiFontStack = (font: UIFont, language: string) => font === 'system' ? systemUI(language) : fontFamilies(font).map(quote).join(', ') + ', ' + systemUI(language)
export function terminalFontStack(primary: TerminalFont, fallback: FallbackFont): string {
  const families = fontFamilies(primary)
  const main = families.length ? families.map(quote) : ['monospace']
  const extra = fallback === 'system' ? [] : fontFamilies(fallback).map(quote)
  return [...main, ...extra, '"Google Sans Code"', 'monospace'].join(', ')
}
const loaded = new Map<string, Promise<boolean>>()
export function fontAvailable(id: string): Promise<boolean> {
  if (isSystemFont(id)) return Promise.resolve(true)
  const old = loaded.get(id)
  if (old) return old
  if (isLocalFont(id)) {
    const promise = localFonts.list().then(async families => {
      const name = localFontName(id)!
      if (!families.some(family => family.name.toLowerCase() === name.toLowerCase())) return false
      if (typeof FontFace !== 'undefined') await new FontFace('Yaare Local Check', 'local(' + quote(name) + ')').load()
      return true
    }).catch(() => false)
    loaded.set(id, promise)
    void promise.then(ok => {if (!ok) loaded.delete(id)})
    return promise
  }
  if (!isBundledFont(id)) return Promise.resolve(false)
  if (typeof document === 'undefined') return Promise.resolve(true)
  const promise = (async () => {
    const sample = id === 'Maple Mono CN' ? 'Aa中文繁體日本語' : 'Aa0O1lI'
    const faces = await Promise.all(fontFamilies(id).map(family => document.fonts.load('400 14px ' + quote(family),sample)))
    return faces.every(result => result.length > 0)
  })().catch(() => false)
  loaded.set(id, promise)
  void promise.then(ok => { if (!ok) loaded.delete(id) })
  return promise
}
let uiRevision = 0
export async function applyUIFont(font: UIFont, language: string): Promise<boolean> {
  const revision = ++uiRevision
  const available = await fontAvailable(font)
  if (!available) await fontAvailable('Inter')
  if (revision === uiRevision && typeof document !== 'undefined') document.documentElement.style.setProperty('--yaare-ui-font', uiFontStack(available ? font : 'Inter', language))
  return available
}
export async function prepareTerminalPrimary(primary: TerminalFont): Promise<TerminalFont> {
  const [available] = await Promise.all([fontAvailable(primary), fontAvailable('Google Sans Code')])
  return available ? primary : 'Google Sans Code'
}
export async function prepareTerminalFonts(primary: TerminalFont, fallback: FallbackFont): Promise<string> {
  const [main, extra] = await Promise.all([prepareTerminalPrimary(primary), fontAvailable(fallback)])
  if (!extra) await fontAvailable('Maple Mono CN')
  return terminalFontStack(main, extra ? fallback : 'Maple Mono CN')
}
