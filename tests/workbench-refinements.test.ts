import { detectFontPlatform } from '../frontend/src/shared/fonts/platformFonts'
import { describe, expect, test } from 'bun:test'
import { normalizePreferences, resolveAppearance, stepTerminalFontSize } from '../frontend/src/features/workspace/preferences'
import { compactRailWidth, panelLayout, railHeaderPlacement } from '../frontend/src/features/workspace/panelLayout'
import { idleConnection, reduceConnection } from '../frontend/src/features/workspace/sessionConnection'
import { initialHosts, workspaceReducer, type WorkspaceState } from '../frontend/src/features/workspace/mockGateway'
import { writeRemotePacket } from '../frontend/src/features/terminal/remoteOutput'
import { changeShortcut, findShortcut, installShortcutRecorder, managementPages, matchesShortcut, readShortcutOverrides, recordShortcutEvent, resetShortcuts, setShortcut, shortcutAria, shortcutFromEvent, shortcutReference } from '../frontend/src/features/workspace/shortcutReference'
import { horizontalWheelDelta } from '../frontend/src/shared/ui/useHorizontalWheel'
import { handleTerminalFontWheel } from '../frontend/src/features/terminal/terminalFontWheel'

test('wheel scrolling distinguishes horizontal strips from the surrounding host rows', () => {
  const vertical = { deltaX: 0, deltaY: 60, shiftKey: false }
  expect(horizontalWheelDelta(vertical, false)).toBe(60)
  expect(horizontalWheelDelta({ ...vertical, deltaY: -60 }, false)).toBe(-60)
  expect(horizontalWheelDelta(vertical, true)).toBe(0)
  expect(horizontalWheelDelta({ ...vertical, shiftKey: true }, true)).toBe(60)
  expect(horizontalWheelDelta({ ...vertical, shiftKey: true }, false)).toBe(0)
  const horizontal = { deltaX: -80, deltaY: 2, shiftKey: false }
  expect(horizontalWheelDelta(horizontal, true)).toBe(-80)
  expect(horizontalWheelDelta(horizontal, false)).toBe(0)
})

test('Ctrl+wheel cancels page zoom and uses the same terminal font range as Settings', () => {
  let fontSize = 14
  const wheel = (ctrlKey: boolean, deltaY: number, overTerminal: boolean) => {
    let prevented = false, stopped = false
    handleTerminalFontWheel({
      ctrlKey, deltaY,
      preventDefault: () => { prevented = true },
      stopPropagation: () => { stopped = true },
    }, overTerminal, step => { fontSize = stepTerminalFontSize(fontSize, step) })
    return { prevented, stopped }
  }
  expect(wheel(false, -120, true)).toEqual({ prevented: false, stopped: false })
  expect(wheel(true, -120, false)).toEqual({ prevented: true, stopped: true })
  expect(fontSize).toBe(14)
  expect(wheel(true, -120, true)).toEqual({ prevented: true, stopped: true })
  expect(fontSize).toBe(15)
  wheel(true, 120, true)
  wheel(true, 0, true)
  expect(fontSize).toBe(14)

  fontSize = 8
  wheel(true, 120, true)
  expect(fontSize).toBe(8)
  expect(normalizePreferences({ fontSize }).fontSize).toBe(8)
  fontSize = 32
  wheel(true, -120, true)
  expect(fontSize).toBe(32)
  expect(normalizePreferences({ fontSize }).fontSize).toBe(32)
  for (const invalid of [7, 33, 14.5])
    expect(normalizePreferences({ fontSize: invalid }).fontSize).toBe(14)
})

const eventFor = (keys: string) => {
  const parts = keys.split('+'), key = parts.pop()!
  const codes: Record<string, string> = {',':'Comma',left:'ArrowLeft',right:'ArrowRight',up:'ArrowUp',down:'ArrowDown',enter:'Enter',escape:'Escape',tab:'Tab'}
  return { code: codes[key] ?? (key.length === 1 ? 'Key' + key.toUpperCase() : key.toUpperCase()),
    ctrlKey: parts.includes('ctrl'), shiftKey: parts.includes('shift'), altKey: parts.includes('alt'), metaKey: parts.includes('meta'),
    defaultPrevented:false, isComposing:false }
}

test('the Windows workbench defaults still route management and session bindings', () => {
  expect(['alt+z','alt+x','alt+c'].map(keys=>findShortcut(eventFor(keys),managementPages,{},false))).toEqual([...managementPages])
  const ids = ['new-tab','new-connection','close-tab'] as const
  expect(['ctrl+shift+t','ctrl+shift+n','ctrl+shift+w'].map(keys=>findShortcut(eventFor(keys),ids,{},false))).toEqual([...ids])
  for (const original of ['ctrl+w','ctrl+n','ctrl+f']) expect(findShortcut(eventFor(original),[...ids,'terminal-search'],{},false)).toBeUndefined()
  expect(findShortcut(eventFor('ctrl+shift+f'),['terminal-search'],{},false)).toBe('terminal-search')
  expect(findShortcut(eventFor('meta+s'),['save'],{},true)).toBe('save')
  expect(findShortcut(eventFor('ctrl+s'),['save'],{},true)).toBeUndefined()
})

test('Mac terminal leaves control signals and IME input alone and uses Command for app actions', () => {
  const terminal = ['terminal-copy','terminal-paste','terminal-search'] as const
  expect(['meta+c','meta+v','meta+f'].map(k=>findShortcut(eventFor(k),terminal,{},true))).toEqual([...terminal])
  expect(findShortcut(eventFor('ctrl+c'),terminal,{},true)).toBeUndefined()
  expect(findShortcut({...eventFor('meta+c'),isComposing:true},terminal,{},true)).toBeUndefined()
  const tabs = ['new-tab','close-tab'] as const
  expect(['meta+t','meta+w'].map(k=>findShortcut(eventFor(k),tabs,{},true))).toEqual([...tabs])
  expect(findShortcut({...eventFor('meta+1'),code:'Digit1'},managementPages,{},true)).toBe('hosts')
  expect(findShortcut(eventFor('meta+c'),terminal,{'terminal-copy':null},true)).toBeUndefined()
})

test('shortcut reference lists the 25 Windows bindings once and omits desktop-only keys elsewhere', () => {
  const groups = shortcutReference(true,{},false)
  expect(groups.map(group => group.items.length)).toEqual([8, 9, 2, 6])
  const keys = groups.flatMap(group => group.items.map(item => item.keys))
  expect(new Set(keys).size).toBe(25)
  expect(shortcutReference(false).flatMap(group => group.items).length).toBe(20)
  for (const key of ['alt+f', 'alt+q', 'ctrl+tab', 'ctrl+pageup']) expect(keys).not.toContain(key)
})

test('all 25 actions can replace their old key and be disabled', () => {
  const actions = shortcutReference(true,{},false).flatMap(group=>group.items)
  const ids = actions.map(action=>action.id)
  for (const {id, keys} of actions) {
    expect(findShortcut(eventFor(keys!),ids,{},false)).toBe(id)
    const change = changeShortcut({},id,'ctrl+alt+shift+j',false)
    if(!change.ok)throw Error('Failed to rebind ' + id)
    expect(findShortcut(eventFor('ctrl+alt+shift+j'),ids,change.overrides,false)).toBe(id)
    expect(findShortcut(eventFor(keys!),ids,change.overrides,false)).toBeUndefined()
    const disabled = changeShortcut(change.overrides,id,null,false)
    if(!disabled.ok)throw Error('Failed to disable ' + id)
    expect(findShortcut(eventFor(keys!),ids,disabled.overrides,false)).toBeUndefined()
    expect(findShortcut(eventFor('ctrl+alt+shift+j'),ids,disabled.overrides,false)).toBeUndefined()
  }
})

test('the recorder rejects typing, focus navigation, reserved keys and input methods', () => {
  for(const keys of ['a','shift+a','escape','tab','ctrl+tab','alt+f4','f12','shift+f10','left','enter'])
    expect(shortcutFromEvent(eventFor(keys))).toBeUndefined()
  expect(shortcutFromEvent(eventFor('shift+escape'))).toBe('shift+escape')
  expect(shortcutFromEvent({...eventFor('alt+down'),code:'Numpad2',key:'ArrowDown'})).toBe('alt+down')
  expect(shortcutFromEvent({...eventFor('ctrl+s'),code:'',key:'s'})).toBe('ctrl+s')
  expect(shortcutFromEvent({...eventFor('ctrl+s'),key:'o'})).toBe('ctrl+s')
  expect(shortcutFromEvent({...eventFor('ctrl+s'),code:'',key:'s',isComposing:true})).toBeUndefined()
  for(const flag of ['defaultPrevented','isComposing'] as const)
    expect(shortcutFromEvent({...eventFor('ctrl+s'),[flag]:true})).toBeUndefined()
  expect(shortcutFromEvent({...eventFor('ctrl+s'),keyCode:229})).toBeUndefined()
  expect(shortcutFromEvent({...eventFor('ctrl+alt+s'),getModifierState:key=>key==='AltGraph'})).toBeUndefined()
})

test('conflicts keep both shortcuts intact and malformed saved values fall back', () => {
  expect(changeShortcut({},'save','ctrl+shift+w',false)).toEqual({ok:false,reason:'conflict',conflict:'close-tab'})
  expect(changeShortcut({save:null,'close-tab':'ctrl+s'},'save',undefined,false)).toEqual({ok:false,reason:'conflict',conflict:'close-tab'})
  expect(changeShortcut({save:'alt+s'},'save',undefined,false)).toEqual({ok:true,overrides:{}})
  expect(readShortcutOverrides('{bad JSON',false)).toEqual({})
  expect(readShortcutOverrides(JSON.stringify({save:'ctrl+shift+w','close-tab':'a',hosts:null,unknownAction:'ctrl+t'}),false)).toEqual({save:'ctrl+shift+w',hosts:null})
  const previous = readShortcutOverrides(JSON.stringify({save:'ctrl+shift+w'}), false)
  expect(findShortcut(eventFor('ctrl+shift+w'),['close-tab','save'],previous,false)).toBe('save')
  expect(changeShortcut(previous,'close-tab',undefined,false)).toEqual({ok:false,reason:'conflict',conflict:'save'})
})

test('a focused recorder receives shortcut keys before the workbench dispatches them', () => {
  const binding = detectFontPlatform() === 'macos' ? 'meta+w' : 'ctrl+shift+w'
  const close = eventFor(binding) as KeyboardEvent
  let captured: string | undefined
  const dispose = installShortcutRecorder(event => { captured = shortcutFromEvent(event); return true })
  try {
    expect(recordShortcutEvent(close)).toBe(true)
    expect(captured).toBe(binding)
  } finally { dispose() }
  expect(recordShortcutEvent(close)).toBe(false)
  expect(matchesShortcut(close,'close-tab')).toBe(true)
})

test('the shortcut store saves before applying a binding and reads back the saved value', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis,'window')
  let raw: string | null = null
  let writable = true
  Object.defineProperty(globalThis,'window',{configurable:true,value:{localStorage:{setItem(_:string,value:string){if(!writable)throw Error('Storage blocked');raw=value}}}})
  try {
    expect(setShortcut('save','alt+s').ok).toBe(true)
    expect(shortcutAria('save')).toBe('Alt+S')
    expect(findShortcut(eventFor('alt+s'),['save'],readShortcutOverrides(raw,false),false)).toBe('save')
    expect(matchesShortcut(eventFor('ctrl+s'),'save')).toBe(false)
    writable = false
    expect(setShortcut('save',null)).toEqual({ok:false,reason:'storage'})
    expect(matchesShortcut(eventFor('alt+s'),'save')).toBe(true)
  } finally {
    writable = true
    expect(resetShortcuts().ok).toBe(true)
    if(previous)Object.defineProperty(globalThis,'window',previous)
    else Reflect.deleteProperty(globalThis,'window')
  }
})

test('automatic appearance tracks the system without changing manual overrides', () => {
  expect(normalizePreferences({ mode: 'system' }).mode).toBe('system')
  for (const dark of [false, true]) {
    expect(resolveAppearance('system', dark)).toBe(dark ? 'dark' : 'light')
    expect(resolveAppearance('dark', dark)).toBe('dark')
    expect(resolveAppearance('light', dark)).toBe('light')
  }
})
test('panel budgets keep terminal space across continuous resize and both priorities', () => {
  for (let width = 320 + compactRailWidth + 2; width <= 2560; width += 11) for (const priority of ['start', 'end'] as const) {
    const layout = panelLayout(width, 360, 480, true, true, priority)
    const left = layout.leftCollapsed ? compactRailWidth : Math.min(360, layout.leftMax)
    const right = layout.rightCollapsed ? 0 : Math.min(480, layout.rightMax)
    expect(width - left - right - 2).toBeGreaterThanOrEqual(320)
  }
  expect(panelLayout(720, 256, 320, true, true, 'start')).toMatchObject({ leftCollapsed: false, rightCollapsed: true })
  expect(panelLayout(720, 256, 320, true, true, 'end')).toMatchObject({ leftCollapsed: true, rightCollapsed: false })
  expect(panelLayout(1440, 256, 320, false, false, 'start')).toMatchObject({ leftCollapsed: true, rightCollapsed: true })
  expect(panelLayout(649, 256, 320, false, true, 'end')).toMatchObject({ leftCollapsed: true, rightCollapsed: true })
  expect(panelLayout(650, 256, 320, false, true, 'end')).toMatchObject({ leftCollapsed: true, rightCollapsed: false, rightMax: 280 })
})
test('floating rail header keeps compact top-bar footprint while overlaying expanded navigation', () => {
  const collapsed = railHeaderPlacement(false, false, '48px', '216px', '0px')
  const floating = railHeaderPlacement(false, true, '48px', '216px', '0px')
  const docked = railHeaderPlacement(true, true, '216px', '216px', '0px')
  expect(collapsed.overlay).toBe(true)
  expect(floating.overlay).toBe(true)
  expect(floating.slotWidth).toBe(collapsed.slotWidth)
  expect(floating.slotMinWidth).toBe(collapsed.slotMinWidth)
  expect(floating.surfaceWidth).toBe('max(48px, calc(216px - 0px))')
  expect(docked.overlay).toBe(false)
  expect(docked.slotWidth).toBe('max(0px, calc(216px - 0px))')
  expect(docked.surfaceWidth).toBe('100%')
})
describe('connection events preserve UI state', () => {
  test('duplicates and stale events cannot revive disconnected or older attempts', () => {
    const connecting = reduceConnection(idleConnection, { type: 'begin', attempt: 1 })
    const live = reduceConnection(connecting, { type: 'status', attempt: 1, revision: 1, status: 'connected' })
    const closed = reduceConnection(live, { type: 'status', attempt: 1, revision: 2, status: 'disconnected' })
    expect(reduceConnection(closed, { type: 'status', attempt: 1, revision: 3, status: 'connected' })).toBe(closed)
    const retry = reduceConnection(closed, { type: 'begin', attempt: 2, reconnect: true })
    expect(retry.status).toBe('reconnecting')
    expect(reduceConnection(retry, { type: 'status', attempt: 1, revision: 99, status: 'failed' })).toBe(retry)
    const current = reduceConnection(retry, { type: 'status', attempt: 2, revision: 2, status: 'connected' })
    expect(reduceConnection(current, { type: 'status', attempt: 2, revision: 1, status: 'failed' })).toBe(current)
    expect(reduceConnection(current, { type: 'status', attempt: 2, revision: 2, status: 'connected' })).toBe(current)
  })
  test('connection changes preserve session/tab/profile identity; closed session ignores late callbacks', () => {
    const state: WorkspaceState = { hosts: initialHosts, sessions: [{ id: 'a', host: initialHosts[0]! }, { id: 'b', host: initialHosts[1]! }], activeId: 'b' }
    const next = workspaceReducer(state, { type: 'connection', id: 'a', event: { type: 'begin', attempt: 1 } })
    expect(next.activeId).toBe('b'); expect(next.hosts).toBe(state.hosts)
    expect(next.sessions[1]).toBe(state.sessions[1]); expect(next.sessions[0]!.host).toBe(state.sessions[0]!.host)
    const closed = workspaceReducer(next, { type: 'close', id: 'a' })
    expect(workspaceReducer(closed, { type: 'connection', id: 'a', event: { type: 'status', attempt: 1, revision: 1, status: 'connected' } })).toBe(closed)
  })
})
test('remote output is byte-exact, bounded per write and awaits parser acknowledgements', async () => {
  const source = new TextEncoder().encode(('远端内容\x1b[32m\r\n').repeat(20000))
  const written: Uint8Array[] = []
  let acknowledge: (() => void) | undefined
  const pending = writeRemotePacket(source, (bytes, done) => { written.push(bytes); acknowledge = done }, () => false)
  expect(written).toHaveLength(1)
  while (written.reduce((count, chunk) => count + chunk.byteLength, 0) < source.byteLength) {
    acknowledge!(); await Promise.resolve()
    expect(written.at(-1)!.byteLength).toBeLessThanOrEqual(65536)
  }
  acknowledge!(); await pending
  const actual = new Uint8Array(source.byteLength)
  let offset = 0
  for (const chunk of written) { actual.set(chunk, offset); offset += chunk.byteLength }
  expect(actual).toEqual(source)
})
test('closing a terminal stops pending packet processing without another write', async () => {
  let cancelled = false; let calls = 0
  await writeRemotePacket(new Uint8Array(200000), (_, done) => { calls++; cancelled = true; done() }, () => cancelled)
  expect(calls).toBe(1)
})
