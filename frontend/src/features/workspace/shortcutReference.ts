import { usesPreviewStorage } from '../../shared/previewPreferences'
import { tr } from '../../shared/i18n/messages'
import { useSyncExternalStore } from 'react'
import { isMyGo } from 'mygo-runtime'
import { detectFontPlatform } from '../../shared/fonts/platformFonts'
import { desktopPreferences } from './desktopPreferences'

const groups = [
  { id: 'workbench', label: () => tr('工作台'), items: [
    ['hosts', () => tr('主机'), 'alt+z', true],
    ['identities', () => tr('身份'), 'alt+x', true],
    ['commands', () => tr('命令'), 'alt+c', true],
    ['settings', () => tr('打开设置'), 'mod+,'],
    ['host-search', () => tr('搜索服务器'), 'mod+shift+p'],
    ['new-connection', () => tr('新建连接'), 'mod+shift+n'],
    ['new-tab', () => tr('新建终端或连接'), 'ctrl+shift+t', true],
    ['close-tab', () => tr('关闭当前标签'), 'ctrl+shift+w', true],
  ] },
  { id: 'terminal', label: () => tr('终端'), items: [
    ['terminal-search', () => tr('搜索终端'), 'ctrl+shift+f'],
    ['terminal-copy', () => tr('复制'), 'ctrl+shift+c'],
    ['terminal-paste', () => tr('粘贴'), 'ctrl+shift+v'],
    ['session-focus', () => tr('返回会话标签'), 'shift+escape'],
    ['next-pane', () => tr('下一个窗格'), 'f6'],
    ['previous-pane', () => tr('上一个窗格'), 'shift+f6'],
    ['split-right', () => tr('向右拆分'), 'mod+shift+d'],
    ['split-down', () => tr('向下拆分'), 'mod+shift+e'],
    ['maximize-pane', () => tr('放大或还原窗格'), 'mod+shift+enter'],
  ] },
  { id: 'editing', label: () => tr('编辑与认证'), items: [
    ['save', () => tr('保存'), 'mod+s'],
    ['submit', () => tr('提交连接或认证表单'), 'mod+enter'],
  ] },
  { id: 'files', label: () => tr('文件与排序'), items: [
    ['file-select-all', () => tr('全选'), 'mod+a'],
    ['file-rename', () => tr('重命名'), 'f2'],
    ['file-back', () => tr('后退'), 'alt+left'],
    ['file-forward', () => tr('前进'), 'alt+right'],
    ['move-up', () => tr('上移'), 'alt+up'],
    ['move-down', () => tr('下移'), 'alt+down'],
  ] },
] as const

type Definition = typeof groups[number]['items'][number]
export type ShortcutId = Definition[0]
export const managementPages = ['hosts', 'identities', 'commands'] as const
export type ManagementPage = typeof managementPages[number]
type Overrides = Readonly<Partial<Record<ShortcutId, string | null>>>
const definitions = new Map<ShortcutId, Definition>(groups.flatMap(group => [...group.items]).map(item => [item[0], item]))
const macDefaults: Partial<Record<ShortcutId, string>> = {
  hosts: 'meta+1', identities: 'meta+2', commands: 'meta+3', 'host-search': 'meta+shift+f',
  'new-tab': 'meta+t', 'close-tab': 'meta+w', 'terminal-search': 'meta+f',
  'terminal-copy': 'meta+c', 'terminal-paste': 'meta+v',
}
const defaultKeys = (id: ShortcutId, mac: boolean) =>
  (mac && macDefaults[id]) || definitions.get(id)![2].replace('mod', mac ? 'meta' : 'ctrl')
const revisedDefaults = new Set<ShortcutId>(['new-tab', 'close-tab', 'terminal-search', 'host-search'])
function currentKeys(id: ShortcutId, overrides: Overrides, mac: boolean) {
  if (overrides[id] !== undefined) return overrides[id]
  const keys = defaultKeys(id, mac)
  if (!mac && revisedDefaults.has(id) && Object.values(overrides).includes(keys)) return null
  return keys
}
const onMac = () => detectFontPlatform() === 'macos'
const storageKey = 'yaare.preview.shortcuts.v1'
const namedCodes: Record<string, string> = {Comma: ',', Enter: 'enter', Escape: 'escape', ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down', Home: 'home', End: 'end', PageUp: 'pageup', PageDown: 'pagedown', Space: 'space'}
const reservedKeys = new Set(['alt+f4', 'meta+q', 'meta+h', 'meta+m', 'meta+space', 'f1', 'f5', 'f11', 'f12', 'shift+f10'])

function validKeys(keys: string) {
  if (reservedKeys.has(keys)) return false
  const parts = keys.split('+'), key = parts.pop()!
  if (!/^(?:[a-z0-9,]|f(?:[1-9]|1[0-9]|2[0-4])|enter|escape|left|right|up|down|home|end|pageup|pagedown|space)$/.test(key)) return false
  if (parts.join('+') !== ['ctrl', 'alt', 'shift', 'meta'].filter(modifier => parts.includes(modifier)).join('+')) return false
  return /^f[0-9]+$/.test(key) || keys === 'shift+escape' || parts.some(modifier => modifier !== 'shift')
}

type ShortcutEvent = Pick<KeyboardEvent, 'code'|'ctrlKey'|'metaKey'|'shiftKey'|'altKey'|'isComposing'|'defaultPrevented'> & Partial<Pick<KeyboardEvent, 'key'|'keyCode'|'getModifierState'>>

export function shortcutFromEvent(event: ShortcutEvent): string | undefined {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.getModifierState?.('AltGraph') || event.key === 'Dead') return
  const code = event.code
  let key = namedCodes[code]
  if (!key && /^Key[A-Z]$/.test(code)) key = code.slice(3).toLowerCase()
  if (!key && /^Digit[0-9]$/.test(code)) key = code.slice(5)
  if (!key && /^F[0-9]+$/.test(code)) key = code.toLowerCase()
  const logical = event.key
  if (!key && logical && (!code || code === 'Unidentified' || logical.startsWith('Arrow'))) {
    key = logical === ' ' ? 'space' : namedCodes[logical]
    if (!key && /^(?:[a-z0-9,]|F[0-9]+)$/i.test(logical)) key = logical.toLowerCase()
  }
  if (!key) return
  const keys = [event.ctrlKey && 'ctrl', event.altKey && 'alt', event.shiftKey && 'shift', event.metaKey && 'meta', key].filter(Boolean).join('+')
  return validKeys(keys) ? keys : undefined
}

function conflictingShortcut(id: ShortcutId, keys: string, overrides: Overrides, mac: boolean) {
  return [...definitions.keys()].find(other => other !== id && currentKeys(other, overrides, mac) === keys)
}

export function readShortcutOverrides(raw: string | null, mac: boolean): Overrides {
  try {
    const data: unknown = JSON.parse(raw ?? '{}')
    if (!data || typeof data !== 'object' || Array.isArray(data)) return {}
    const result: Partial<Record<ShortcutId, string | null>> = {}
    for (const id of definitions.keys()) {
      const value = (data as Record<string, unknown>)[id]
      if (value === null || typeof value === 'string' && validKeys(value) && value !== defaultKeys(id, mac)) result[id] = value
    }
    let changed = true
    while (changed) {
      changed = false
      const collisions = [...definitions.keys()].filter(id => typeof result[id] === 'string' && conflictingShortcut(id, result[id], result, mac))
      for (const id of collisions) { delete result[id]; changed = true }
    }
    return result
  } catch { return {} }
}

function loadPreviewShortcuts(): Overrides {
  try { return readShortcutOverrides(window.localStorage.getItem(storageKey), onMac()) } catch { return {} }
}

let overrides: Overrides = usesPreviewStorage() ? loadPreviewShortcuts() : {}
const subscribers = new Set<() => void>()
const publish = (next: Overrides) => { overrides = next; subscribers.forEach(listener => listener()) }
const subscribe = (listener: () => void) => {
  if (isMyGo()) return desktopPreferences.subscribe(listener)
  subscribers.add(listener)
  return () => { subscribers.delete(listener) }
}
const snapshot = (): Overrides => isMyGo() ? desktopPreferences.read().shortcuts : overrides
if (typeof window !== 'undefined') window.addEventListener('storage', event => {
  if (usesPreviewStorage() && (event.key === storageKey || event.key === null)) publish(loadPreviewShortcuts())
})

export function useShortcutOverrides() { return useSyncExternalStore(subscribe, snapshot, snapshot) }

type ShortcutChange = { ok: true; overrides: Overrides } | { ok: false; reason: 'conflict'; conflict: ShortcutId } | { ok: false; reason: 'invalid' | 'storage' }
export function changeShortcut(current: Overrides, id: ShortcutId, keys: string | null | undefined, mac: boolean): ShortcutChange {
  const next = { ...current }
  if (keys === undefined || keys === defaultKeys(id, mac)) delete next[id]
  else if (keys === null || validKeys(keys)) next[id] = keys
  else return { ok: false, reason: 'invalid' }
  const effective = keys === undefined ? defaultKeys(id, mac) : keys
  const conflict = effective && conflictingShortcut(id, effective, current, mac)
  return conflict ? { ok: false, reason: 'conflict', conflict } : { ok: true, overrides: next }
}

function saveOverrides(next: Overrides): ShortcutChange {
  try { window.localStorage.setItem(storageKey, JSON.stringify(next)) } catch { return { ok: false, reason: 'storage' } }
  publish(next)
  return { ok: true, overrides: next }
}

export function setShortcut(id: ShortcutId, keys: string | null | undefined): ShortcutChange {
  const result = changeShortcut(overrides, id, keys, onMac())
  return result.ok ? saveOverrides(result.overrides) : result
}
export const resetShortcuts = () => saveOverrides({})
export async function persistShortcut(id: ShortcutId, keys: string | null | undefined): Promise<ShortcutChange> {
  if (!isMyGo()) return setShortcut(id, keys)
  const result = changeShortcut(snapshot(), id, keys, onMac())
  if (!result.ok || await desktopPreferences.saveShortcuts(result.overrides)) return result
  return { ok: false, reason: 'storage' }
}
export async function restoreShortcuts() {
  return isMyGo() ? desktopPreferences.saveShortcuts({}) : resetShortcuts().ok
}
export const shortcutLabel = (id: ShortcutId) => definitions.get(id)![1]()

export function shortcutReference(showDesktopShortcuts: boolean, value: Overrides = snapshot(), mac = onMac()) {
  return groups.map(group => ({ id: group.id, label: group.label(), items: group.items
    .filter(item => !item[3] || showDesktopShortcuts)
    .map(([id, label]) => ({id, label: label(), keys: currentKeys(id, value, mac), customized: value[id] !== undefined})) }))
}

export function findShortcut<Id extends ShortcutId>(event: ShortcutEvent, ids: readonly Id[], value: Overrides = snapshot(), mac = onMac()): Id | undefined {
  const keys = shortcutFromEvent(event)
  return keys ? ids.find(id => currentKeys(id, value, mac) === keys) : undefined
}

export const matchesShortcut = (event: ShortcutEvent, id: ShortcutId) => findShortcut(event, [id]) === id
let recorder: ((event: KeyboardEvent) => boolean) | undefined
export function installShortcutRecorder(handler: (event: KeyboardEvent) => boolean) {
  recorder = handler
  return () => { if (recorder === handler) recorder = undefined }
}
export const recordShortcutEvent = (event: KeyboardEvent) => recorder?.(event) ?? false

const keyNames: Record<string, string> = {ctrl:'Ctrl', alt:'Alt', shift:'Shift', meta:'Meta', enter:'Enter', escape:'Esc', left:'←', right:'→', up:'↑', down:'↓', pageup:'Page Up', pagedown:'Page Down', home:'Home', end:'End', space:'Space'}
const ariaNames: Record<string, string> = {...keyNames, ctrl:'Control', escape:'Escape', left:'ArrowLeft', right:'ArrowRight', up:'ArrowUp', down:'ArrowDown', pageup:'PageUp', pagedown:'PageDown'}
export const formatShortcut = (keys: string | null) => keys?.split('+').map(key => key === 'meta' && onMac() ? '⌘' : keyNames[key] ?? key.toUpperCase()).join('+')
export const shortcutText = (id: ShortcutId, value: Overrides = snapshot()) => formatShortcut(currentKeys(id, value, onMac()))
export const shortcutAria = (id: ShortcutId, value: Overrides = snapshot()) => currentKeys(id, value, onMac())?.split('+').map(key => ariaNames[key] ?? key.toUpperCase()).join('+')
export const shortcutTooltip = (label: string, id: ShortcutId, value: Overrides = snapshot()) => {
  const keys = shortcutText(id, value)
  return keys ? tr('{p0} · {p1}', label, keys) : label
}
