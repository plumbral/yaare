import { afterEach, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolveLanguage, normalizeLanguagePreference, setLanguagePreference, getLanguage, subscribeLanguage, languageOptions, initializeLanguage } from '../frontend/src/shared/i18n/locale'
import { getAstryxLocale, getAstryxMessages } from '../frontend/src/shared/i18n/catalogs'
import { tr, localizeMessage, messagesIn } from '../frontend/src/shared/i18n/messages'
import { normalizePreferences, savePreferences, readPreferences } from '../frontend/src/features/workspace/preferences'
import { initialHosts } from '../frontend/src/features/workspace/mockGateway'
import { commandUngrouped } from '../frontend/src/features/workspace/shortcuts'
import sources from '../frontend/src/shared/i18n/sources.json'
import en from '../locales/en.json'
import zh from '../locales/zh-Hans.json'
import tw from '../locales/zh-Hant.json'
import ja from '../locales/ja.json'

afterEach(() => setLanguagePreference('system'))
test('a cold locale is published atomically and late loads cannot override the newest choice', async () => {
  await setLanguagePreference('en')
  const published: string[] = []
  const stop = subscribeLanguage(() => {
    const language = getLanguage()
    const locale = getAstryxLocale(language)
    expect(getAstryxMessages(language)?.[locale]).toBeDefined()
    published.push(tr('保存'))
  })
  try {
    const superseded = setLanguagePreference('ja')
    const selected = setLanguagePreference('zh-Hant')
    expect(getLanguage()).toBe('en')
    expect(tr('保存')).toBe('Save')
    expect(published).toEqual([])
    await initializeLanguage()
    await Promise.all([superseded, selected])
    expect(getLanguage()).toBe('zh-Hant')
    expect(published).toEqual(['儲存'])
    expect(messagesIn('zh-Hant')[sources['保存'] as keyof ReturnType<typeof messagesIn>]()).toBe('儲存')
  } finally { stop() }
})
test('language changes leave host snapshots, command bytes, and canonical group values unchanged', async () => {
  const commands = [{ id: 'test-command', name: '用户命令', command: 'printf "例文"\n', group: commandUngrouped }]
  const before = JSON.stringify({ hosts: initialHosts, commands })
  for (const language of ['en', 'ja', 'zh-Hant', 'zh-Hans'] as const) {
    await setLanguagePreference(language)
    expect(JSON.stringify({ hosts: initialHosts, commands })).toBe(before)
    expect(commandUngrouped).toBe('未分组')
  }
})
test('system language resolution respects script, region, and unsupported-language fallback', () => {
  for (const tag of ['zh-TW', 'zh-HK', 'zh-MO', 'zh-Hant', 'zh-Hant-TW']) expect(resolveLanguage(tag)).toBe('zh-Hant')
  for (const tag of ['zh', 'zh-CN', 'zh-Hans-SG']) expect(resolveLanguage(tag)).toBe('zh-Hans')
  expect(resolveLanguage('ja-JP')).toBe('ja'); expect(resolveLanguage('en-AU')).toBe('en'); expect(resolveLanguage('fr-FR')).toBe('en')
  expect(normalizeLanguagePreference('ja-JP')).toBe('system'); expect(normalizeLanguagePreference('zh-Hant')).toBe('zh-Hant')
  expect(languageOptions.map(option => option.label)).toEqual(['跟随系统', '简体中文', 'English', '繁體中文', '日本語'])
})
test('all four catalogs and the native index have complete matching keys and valid formatting', async () => {
  const keys = Object.values(sources).sort()
  expect(new Set(keys).size).toBe(keys.length)
  for (const catalog of [en, zh, tw, ja]) expect(Object.keys(catalog).filter(key => !key.startsWith('@')).sort()).toEqual(keys)
  expect(JSON.parse(readFileSync(new URL('../locales/source/index.json', import.meta.url), 'utf8'))).toEqual(sources)
  for (const language of ['en', 'zh-Hans', 'zh-Hant', 'ja'] as const) {
    await setLanguagePreference(language)
    for (const source of Object.keys(sources)) {
      const count = Math.max(-1, ...[...source.matchAll(/\{p(\d+)\}/g)].map(match => Number(match[1]))) + 1
      const result = tr(source, ...Array.from({ length: count }, (_, index) => index + 1))
      expect(result).not.toMatch(/\{p\d+[,}]/)
      expect(result).not.toMatch(/^(?:ui|be)_\d+$/)
      if (language === 'en') expect(result).not.toMatch(/[\p{Script=Han}]/u)
    }
  }
})
test('switches notify mounted consumers without remounting and format named parameters in translated order', async () => {
  let changes = 0; const off = subscribeLanguage(() => { changes++ })
  await setLanguagePreference('en')
  expect(getLanguage()).toBe('en'); expect(tr('保存')).toBe('Save')
  expect(tr('已更新 {p0} 台主机的{p1}。', 2, 'Notes')).toBe('Updated Notes on 2 hosts.')
  expect(tr('{p0} 个会话 · {p1} 个 SSH 已连接', 1, 0)).toBe('1 session · 0 SSH connections')
  await setLanguagePreference('ja'); expect(tr('保存')).toBe('保存')
  await setLanguagePreference('zh-Hant'); expect(tr('保存')).toBe('儲存')
  expect(tr('服务器')).toBe('伺服器'); expect(tr('端口')).toBe('連接埠')
  off(); expect(changes).toBe(3)
})
test('existing errors and dynamic backend diagnostics switch language while user parameters stay verbatim', async () => {
  await setLanguagePreference('zh-Hans')
  const old = tr('已恢复 {p0} 个标签、{p1} 个窗格。', 2, 3)
  await setLanguagePreference('en')
  expect(localizeMessage(old)).toBe('Restored 2 tabs and 3 panes.')
  expect(localizeMessage('存储请求未完成（HTTP 401）。')).toContain('HTTP 401')
  expect(localizeMessage('存储请求未完成（HTTP 401）。')).not.toMatch(/[\p{Script=Han}]/u)
  const value = '<img src=x>设置 / 日本語 / 保存'
  expect(tr('删除「{p0}」？', value)).toBe('Delete “' + value + '”?')
  expect(localizeMessage('external diagnostic: xyz')).toBe('external diagnostic: xyz')
  const copied = tr('已复制所选内容')
  await setLanguagePreference('ja')
  expect(localizeMessage(copied)).toBe(tr('已复制所选内容'))
  expect(localizeMessage(copied)).not.toBe(copied)
})
test('language is normalized, saved, and restored without losing other preferences', async () => {
  const previous = globalThis.localStorage
  const values = new Map<string,string>()
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) } })
  try {
    expect(normalizePreferences({ mode: 'light' }).language).toBe('system')
    const preferences = { ...normalizePreferences({ mode: 'light', fontSize: 18 }), language: 'ja' as const }
    expect(savePreferences(preferences)).toBe(true)
    await initializeLanguage()
    expect(getLanguage()).toBe('ja'); expect(readPreferences()).toEqual(preferences)
  } finally { Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: previous }) }
})
