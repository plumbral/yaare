import { expect, test } from 'bun:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { parseHTML } from 'linkedom'
import TerminalPane from '../frontend/src/features/terminal/TerminalPane'
import { resolveSessionConnectionPrompt } from '../frontend/src/features/workspace/sessionConnectionPrompt'
import type { ITheme } from '@xterm/xterm'
import { createTerminalAppearance, terminalFocusOptions } from '../frontend/src/features/terminal/terminalAppearance'
import { createTerminalFocus, focusTerminalInput, shouldFocusPanePointer } from '../frontend/src/features/terminal/terminalFocus'
import { activateTerminalLink } from '../frontend/src/features/terminal/terminalLinks'
import { openWebLink } from '../frontend/src/shared/bridge/application'
import { tr } from '../frontend/src/shared/i18n/messages'
import { hidePaneView, setPaneView, terminalWorkspaceReducer as reduce, type TerminalWorkspace } from '../frontend/src/features/workspace/terminalLayout'
import { createRememberBusyStore } from '../frontend/src/features/workspace/useRememberWorkspace'
import { initialHosts } from '../frontend/src/features/workspace/mockGateway'

test('every empty connection state keeps its actions outside the inert terminal', () => {
  const noop = () => {}
  for (const live of [undefined, { phase: 'connecting' }, { phase: 'failed' }, { phase: 'disconnected' },
    { phase: 'connecting', automatic: true }, { phase: 'failed', retry: 'waiting' }, { phase: 'failed', retry: 'offline' }] as const) {
    const prompt = resolveSessionConnectionPrompt('idle', live && { ...live, attempt: 2 }, noop, noop)!
    const host = initialHosts[0]!
    const markup = renderToStaticMarkup(createElement(TerminalPane, { host, id: 'test', connectionPrompt: prompt,
      active: true, hideToolbar: true, mode: 'dark', fontSize: 14, cursorBlink: true, scrollback: 1000, onReady: noop, onError: noop }))
    const { document } = parseHTML(markup)
    const buttons = Array.from(document.querySelectorAll('button')).filter(button => !button.closest('[inert]'))
    expect(buttons.map(button => button.textContent)).toEqual(prompt.actions.map(action => action.label))
    const terminal = document.querySelector(`[aria-label="${tr('SSH 终端 {p0}', host.name)}"]`)
    expect(terminal?.closest('[inert]')).toBeTruthy()
  }
})

test('terminal links open only on a modified primary click', () => {
  const url = 'https://browser.geekbench.com/v5/cpu/24852266'
  const opened: string[] = []
  const activate = (button: number, ctrlKey = false, metaKey = false) => {
    activateTerminalLink({ button, ctrlKey, metaKey } as MouseEvent, url, uri => opened.push(uri))
  }
  activate(0)
  activate(1, true)
  activate(2, false, true)
  expect(opened).toEqual([])
  activate(0, true)
  activate(0, false, true)
  expect(opened).toEqual([url, url])
})

test('web links open absolute HTTP and HTTPS URLs with an isolated opener', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'window')
  const opened: string[][] = []
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: { open: (...args: string[]) => opened.push(args) },
  })
  try {
    for (const uri of ['not a URL', '/results', 'file:///tmp/report', 'javascript:void(0)', 'ssh://example.org']) {
      openWebLink(uri)
    }
    openWebLink('http://localhost:8080/results?q=1#score')
    openWebLink('https://example.org/report')
    expect(opened).toEqual([
      ['http://localhost:8080/results?q=1#score', '_blank', 'noopener,noreferrer'],
      ['https://example.org/report', '_blank', 'noopener,noreferrer'],
    ])
  } finally {
    if (previous) Object.defineProperty(globalThis, 'window', previous)
    else Reflect.deleteProperty(globalThis, 'window')
  }
})

test('only the active focused xterm reads output, and toggling this option preserves scrollback', async () => {
  const { Terminal } = await import('@xterm/xterm')
  const focus = { active: true, focused: true, cursorBlink: true, screenReaderMode: true }
  const terminal = new Terminal(terminalFocusOptions(focus))
  try {
    await new Promise<void>(resolve => terminal.write('retained shell history', resolve))
    for (const [active, focused, enabled, expected] of [
      [true, true, false, false], [true, false, true, false], [false, true, true, false], [true, true, true, true],
    ] as const) {
      terminal.options = terminalFocusOptions({ ...focus, active, focused, screenReaderMode: enabled })
      expect(terminal.options.screenReaderMode).toBe(expected)
      expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('retained shell history')
    }
  } finally { terminal.dispose() }
})

test('disconnecting suppresses cursor blink until input resumes in the focused terminal', async () => {
  const { Terminal } = await import('@xterm/xterm')
  const terminal = new Terminal()
  try {
    await new Promise<void>(resolve => terminal.write('previous output', resolve))
    for (const [inputEnabled, focused, cursorBlink, expected] of [
      [false, true, true, false], [true, true, true, true], [true, false, true, false],
      [false, true, true, false], [true, true, false, false], [true, true, true, true],
    ] as const) {
      terminal.options = terminalFocusOptions({ active: true, inputEnabled, focused, cursorBlink, screenReaderMode: true })
      expect(terminal.options.cursorBlink).toBe(expected)
      expect(terminal.options.cursorInactiveStyle).toBe(inputEnabled ? 'underline' : 'none')
      expect(terminal.options.screenReaderMode).toBe(focused)
      expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('previous output')
    }
  } finally { terminal.dispose() }
})

function appearance(initial: ITheme) {
  let theme = initial
  const writes: ITheme[] = [], paints: Array<[number, number]> = []
  const terminal = {
    options: { get theme() { return theme }, set theme(next: ITheme) { theme = next; writes.push(next) } },
    rows: 25,
    refresh: (first: number, last: number) => paints.push([first, last]),
  }
  return { sync: createTerminalAppearance(terminal, initial), writes, paints }
}

test('twenty tab switches with unchanged colors write no theme and request no full refresh', () => {
  const theme = { background: '#222222', foreground: '#eeeeee', black: '#000000' }
  const panes = [appearance(theme), appearance(theme)]
  for (let switchIndex = 1; switchIndex <= 20; switchIndex++) {
    panes.forEach((pane, paneIndex) => pane.sync({ ...theme }, paneIndex === switchIndex % 2))
  }
  expect(panes.map(pane => [pane.writes.length, pane.paints.length])).toEqual([[0, 0], [0, 0]])
})

test('a real palette change redraws a visible terminal once and a hidden one on reveal', () => {
  const light = { background: '#eeeeee', foreground: '#222222', black: '#000000' }
  const dark = { background: '#222222', foreground: '#eeeeee', black: '#000000' }
  const visible = appearance(light), hidden = appearance(light)
  visible.sync(dark, true)
  hidden.sync(dark, false)
  expect(visible.writes).toEqual([dark])
  expect(visible.paints).toEqual([[0, 24]])
  expect(hidden.writes).toEqual([dark])
  expect(hidden.paints).toEqual([])
  hidden.sync(dark, true)
  hidden.sync(dark, false)
  hidden.sync(dark, true)
  expect(hidden.writes).toEqual([dark])
  expect(hidden.paints).toEqual([[0, 24]])
})

function focusFixture() {
  const frames = new Map<number, () => void>()
  const controllers = new Map<string, { focus: () => boolean }>()
  const calls: string[] = []
  let nextFrame = 0
  let focused: Element | null = null
  const queue = createTerminalFocus(id => controllers.get(id), () => focused,
    callback => { const frame = ++nextFrame; frames.set(frame, callback); return frame }, frame => { frames.delete(frame) })
  const register = (id: string, succeeds = true) => {
    controllers.set(id, { focus: () => { calls.push(id); return succeeds } })
    queue.ready(id)
  }
  const flush = () => {
    const scheduled = [...frames.values()]
    frames.clear()
    for (const callback of scheduled) callback()
  }
  return { queue, register, flush, calls, frames, changeFocus: (element: Element) => { focused = element } }
}

test('a series of focus requests shares one frame and only the final committed pane receives focus', () => {
  const { queue, register, flush, calls, frames } = focusFixture()
  register('first'); register('second')
  queue.setVisible('first')
  for (let request = 0; request < 20; request++) queue.request(request % 2 === 0 ? 'first' : 'second')
  queue.setVisible('second')
  expect(frames.size).toBe(1)
  flush()
  expect(calls).toEqual(['second'])
  expect(queue.wasRequested('second')).toBe(true)
  queue.ready('second'); flush()
  expect(calls).toEqual(['second'])
})

test('a slow terminal gets pending focus only if its tab is still visible and focus has not moved', () => {
  const { queue, register, flush, calls, changeFocus } = focusFixture()
  queue.setVisible('loading'); queue.request('loading'); flush()
  expect(calls).toEqual([])
  register('loading'); flush()
  expect(calls).toEqual(['loading'])

  queue.setVisible('abandoned'); queue.request('abandoned'); flush()
  queue.setVisible(''); register('abandoned'); flush()
  expect(calls).toEqual(['loading'])

  queue.setVisible('waiting'); queue.request('waiting'); flush()
  changeFocus({} as Element)
  register('waiting'); flush()
  expect(calls).toEqual(['loading'])
})

test('a terminal that is not ready to receive focus can retry on reveal; cancelling removes the queued callback', () => {
  const { queue, register, flush, calls, frames } = focusFixture()
  queue.setVisible('loading')
  register('loading', false)
  queue.request('loading'); flush()
  register('loading'); flush()
  expect(calls).toEqual(['loading', 'loading'])
  queue.request('loading'); queue.cancel(); flush()
  expect(frames.size).toBe(0)
  expect(calls).toHaveLength(2)
})

test('xterm focus does not run for a hidden pane, under a modal, or when already focused', () => {
  let hidden = true, modal = false, calls = 0
  const document = { activeElement: null as Element | null, querySelector: () => modal ? {} : null }
  const input = { ownerDocument: document, closest: () => hidden ? {} : null } as unknown as HTMLTextAreaElement
  const focus = () => { calls++; document.activeElement = input }
  expect(focusTerminalInput(input, focus)).toBe(false)
  hidden = false; modal = true
  expect(focusTerminalInput(input, focus)).toBe(false)
  modal = false
  expect(focusTerminalInput(input, focus)).toBe(true)
  for (let count = 0; count < 10; count++) expect(focusTerminalInput(input, focus)).toBe(true)
  expect(calls).toBe(1)
})

test('ten repeat activations leave workspace state unchanged; tab switches preserve pane and group identity', () => {
  const empty: TerminalWorkspace = { hosts: [], sessions: [], groups: [], activeId: '' }
  const first = reduce(empty, { type: 'connect', id: 'first', host: initialHosts[0]! })
  const second = reduce(first, { type: 'connect', id: 'second', host: initialHosts[1]! })
  for (let count = 0; count < 10; count++) expect(reduce(second, { type: 'activate', id: 'second' })).toBe(second)
  const result = reduce(second, { type: 'activate', id: 'first' })
  expect(result.activeId).toBe('first')
  expect(result.groups).toBe(second.groups)
  expect(result.sessions).toBe(second.sessions)
})

test('selecting a terminal does not create or replace entries for closed alternate views', () => {
  const unopened: Record<string, boolean> = {}
  expect(hidePaneView(unopened, 'first')).toBe(unopened)
  const previous = { first: 'document', second: 'preview' }
  const hidden = hidePaneView(previous, 'first')
  expect(hidden).toEqual({ first: false, second: 'preview' })
  expect(hidePaneView(hidden, 'first')).toBe(hidden)
  const opened = setPaneView(unopened, 'first', true)
  expect(opened).toEqual({ first: true })
  expect(setPaneView(opened, 'first', true)).toBe(opened)
  const docViews: Record<string, string | false> = { first: 'doc-1' }
  expect(setPaneView(docViews, 'first', 'doc-1')).toBe(docViews)
  expect(setPaneView(docViews, 'first', 'doc-2')).toEqual({ first: 'doc-2' })
})

test('focus restored to a trigger when a picker or command popover closes does not cancel terminal focus', () => {
  const { queue, register, flush, calls, changeFocus } = focusFixture()
  const trigger = { isConnected: true, closest: () => null } as unknown as Element
  const otherControl = { isConnected: true, closest: () => null } as unknown as Element
  const popoverInput = { isConnected: true, closest: () => null } as unknown as Element & { isConnected: boolean }

  register('ready-pane')
  queue.setVisible('ready-pane')
  changeFocus(popoverInput)
  queue.request('ready-pane')
  popoverInput.isConnected = false
  changeFocus(trigger)
  flush()
  expect(calls).toEqual(['ready-pane'])

  let popoverClosed = false
  const commandItem = {
    isConnected: true,
    closest: (selector: string) => popoverClosed && selector.includes(':popover-open') ? {} : null,
  } as unknown as Element
  queue.setVisible('slow-popover')
  changeFocus(commandItem)
  queue.request('slow-popover')
  popoverClosed = true
  changeFocus(trigger)
  flush()
  expect(calls).toEqual(['ready-pane'])
  register('slow-popover')
  flush()
  expect(calls).toEqual(['ready-pane', 'slow-popover'])

  popoverClosed = false
  queue.setVisible('aborted-after-settle')
  changeFocus(commandItem)
  queue.request('aborted-after-settle')
  popoverClosed = true
  changeFocus(trigger)
  flush()
  changeFocus(otherControl)
  register('aborted-after-settle')
  flush()
  expect(calls).toEqual(['ready-pane', 'slow-popover'])

  const activeOrigin = { isConnected: true, closest: () => null } as unknown as Element
  register('kept-origin')
  queue.setVisible('kept-origin')
  changeFocus(activeOrigin)
  queue.request('kept-origin')
  changeFocus(otherControl)
  flush()
  expect(calls).toEqual(['ready-pane', 'slow-popover'])
})

test('pane pointer focus ignores interactive controls while keeping xterm surfaces focusable', () => {
  let receivedSelector = ''
  const target = (matched: boolean) => ({
    closest: (selector: string) => { receivedSelector = selector; return matched ? {} : null },
  }) as unknown as EventTarget
  expect(shouldFocusPanePointer(null)).toBe(true)
  expect(shouldFocusPanePointer(target(false))).toBe(true)
  expect(shouldFocusPanePointer(target(true))).toBe(false)
  expect(receivedSelector).toContain('button')
  expect(receivedSelector).toContain('textarea:not(.xterm-helper-textarea)')
  expect(receivedSelector).toContain('[role="menuitem"]')
})

test('workspace layout save busy state notifies only subscribers on real transitions', () => {
  const store = createRememberBusyStore()
  const events: boolean[] = []
  const unsubscribe = store.subscribe(() => events.push(store.read()))
  expect(store.read()).toBe(false)
  store.set(false)
  store.set(true)
  store.set(true)
  store.set(false)
  expect(events).toEqual([true, false])
  unsubscribe()
  store.set(true)
  expect(events).toEqual([true, false])
})
