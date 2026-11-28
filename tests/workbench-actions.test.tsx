import { expect, mock, test } from 'bun:test'
import { act, createElement, useEffect, useLayoutEffect, useRef } from 'react'
import type { ComponentProps } from 'react'
import type TerminalPane from '../frontend/src/features/terminal/TerminalPane'
import type { TerminalController } from '../frontend/src/features/terminal/TerminalPane'
import type { FileTask } from '../frontend/src/shared/bridge/files'
import { document, dom, storage, fixture, clipboard, host, other, installWorkbenchDom } from './fixtures/workbench-dom'

installWorkbenchDom()

mock.module('../frontend/src/features/terminal/TerminalPane', () => ({
  default: function TerminalSurface({ id, host, transport, inputEnabled, onReady, onFocusAvailable, connectionPrompt }: ComponentProps<typeof TerminalPane>) {
    const input = useRef<HTMLTextAreaElement>(null)
    useLayoutEffect(() => {
      onReady(id, { focus: () => {
        if (!inputEnabled || !input.current) return false
        input.current.dataset.focused = 'true'
        return true
      } } as TerminalController)
      if (inputEnabled) onFocusAvailable?.(id)
      return () => onReady(id, null)
    }, [id, inputEnabled, onReady, onFocusAvailable])
    useEffect(() => {
      if (!transport) return
      const stop = new AbortController()
      void (async () => { for await (const _bytes of transport.read(stop.signal)) {} })()
      return () => stop.abort()
    }, [transport])
    const retry = connectionPrompt?.actions.find(action => action.kind === 'retry')
    return <section className="xterm" data-address={host.address}>
      <textarea ref={input} data-input={id} data-connected={String(inputEnabled)}/>
      {retry && <button data-retry-now="" disabled={retry.disabled} onClick={retry.run}/>}
    </section>
  },
}))

async function mountApp(retainSplit = false, tabPosition: 'top' | 'left' = 'top') {
  const { createRoot } = await import('react-dom/client')
  const { default: App } = await import('../frontend/src/app/App')
  storage.set('yaare.tabs-position', tabPosition)
  fixture.dialed.length = 0
  fixture.saved = {
    version: 3, format: 1, enabled: retainSplit, view: 'hosts', activeId: retainSplit ? 'left' : '', startOpen: true, startWidth: 256, endWidth: 320,
    panes: retainSplit ? ['left','right'].map(id => ({ id, hostId: host.id, pending: false, path: '.', filesOpen: false })) : [],
    groups: retainSplit ? [{ id: 'retained', workspaceHostId: host.id, activeId: 'left', pinned: false, maximized: false, name: 'QA split',
      root: { kind: 'split', id: 'branch', axis: 'horizontal', ratio: 0.5, first: { kind: 'leaf', id: 'left' }, second: { kind: 'leaf', id: 'right' } } }] : [],
  }
  const root = createRoot(document.querySelector('main')!)
  await act(async () => root.render(<App/>))
  return () => act(async () => root.unmount())
}

async function click(selector: string, detail?: number) {
  const node = document.querySelector<HTMLElement>(selector)
  if (!node) throw Error('Missing test control ' + selector)
  await act(async () => {
    if (detail === undefined) node.click()
    else node.dispatchEvent(Object.assign(new dom.Event('click', { bubbles: true, cancelable: true }), { detail }))
  })
}
let appliedVersion = 0
const chosen = () => document.querySelector('#session-tab-strip [role=tab][aria-selected="true"]')?.id

test('retry now requests keyboard focus for the reconnected shell', async () => {
  const close = await mountApp()
  const nextFrame = () => act(async () => { await new Promise<number>(requestAnimationFrame) })
  try {
    await click('#manager-open-first')
    const first = fixture.dialed[0]!
    const input = document.querySelector<HTMLTextAreaElement>('textarea[data-input="' + first.id + '"]')!
    await nextFrame()
    expect(input.dataset.focused).toBe('true')
    delete input.dataset.focused
    await act(async () => first.drop())
    expect(input.dataset.connected).toBe('false')
    await click('[data-retry-now]')
    await nextFrame()
    expect(fixture.dialed.map(attempt => attempt.id)).toEqual([first.id, first.id])
    expect(input.dataset.connected).toBe('true')
    await nextFrame()
    expect(input.dataset.focused).toBe('true')
  } finally { await close() }
})

test('a single mouse click selects, a double click connects, and the terminal owns Ctrl+W', async () => {
  const close = await mountApp()
  try {
    const label = '[data-host-context-id="first"] [data-host-name]'
    await click(label, 1)
    expect(fixture.dialed).toBeEmpty()
    expect(document.querySelector('#tab-hosts')?.getAttribute('aria-current')).toBe('page')
    await click(label, 2)
    const input = document.querySelector('textarea[data-connected="true"]')
    expect(input).toBeTruthy()
    for (const [key, shift, consumed] of [['w',false,false], ['n',false,false], ['f',false,false], ['w',true,true]] as const) {
      const event = Object.assign(new dom.Event('keydown', { bubbles: true, cancelable: true }), { key, code: 'Key'+key.toUpperCase(), ctrlKey:true, shiftKey:shift })
      Object.defineProperty(event, 'target', { get: () => input, set() {}, configurable: true })
      await act(async () => { dom.dispatchEvent(event) })
      expect(event.defaultPrevented).toBe(consumed)
      expect(document.querySelectorAll('#session-tab-strip [role=tab]')).toHaveLength(consumed ? 0 : 1)
    }
  } finally { await close() }
})

test('Enter session returns to the last used shell even after visiting another host', async () => {
  const close = await mountApp()
  try {
    await click('#manager-open-first')
    const first = chosen()
    await click('#connection-new-terminal')
    const last = chosen()
    expect(last).toBeTruthy()
    expect(last).not.toBe(first)
    await click('#tab-hosts')
    await click('#manager-open-second')
    await click('#tab-hosts')
    await click('#manager-open-first')
    expect(chosen()).toBe(last)
    expect(fixture.dialed).toHaveLength(3)
  } finally { await close() }
})

test('expanded rail adds a terminal to the requested host and reveals its collapsed tabs', async () => {
  const close = await mountApp(false, 'left')
  const add = `[data-new-terminal-for][aria-label="New terminal in ${host.name}"]`
  const groupToggle = () => [...document.querySelectorAll<HTMLElement>('#session-tab-strip button[aria-expanded]')]
    .find(button => button.textContent === host.name)
  try {
    await click('#manager-open-first')
    const first = chosen()!
    await click('#tab-hosts')
    await click('#manager-open-second')
    const second = chosen()!
    expect(document.querySelectorAll('[data-new-terminal-for]')).toHaveLength(2)
    expect(document.querySelector('#connection-new-terminal')).toBeNull()

    await click(add)
    expect(fixture.dialed.map(connection => connection.address)).toEqual([host.address, other.address, host.address])
    expect(chosen()).toBe('tab-terminal-' + fixture.dialed[2]!.id)
    expect(document.querySelectorAll('[data-new-terminal-for]')).toHaveLength(2)

    await click('#' + second)
    await act(async () => groupToggle()!.click())
    expect(groupToggle()?.getAttribute('aria-expanded')).toBe('false')
    expect(document.getElementById(first)).toBeNull()
    await click(add)
    expect(groupToggle()?.getAttribute('aria-expanded')).toBe('true')
    expect(fixture.dialed.map(connection => connection.address)).toEqual([host.address, other.address, host.address, host.address])
    expect(chosen()).toBe('tab-terminal-' + fixture.dialed[3]!.id)
    expect(document.querySelectorAll('#session-tab-strip [role=tab]')).toHaveLength(4)
  } finally { await close() }
})

test('the row menu and More expose the same host actions, with a chooser for two connected shells', async () => {
  const close = await mountApp()
  try {
    await click('#manager-open-first')
    const first = chosen()
    await click('#connection-new-terminal')
    await click('#tab-hosts')
    await act(async () => document.querySelector('[data-host-context-id="first"] [data-host-name]')!
      .dispatchEvent(Object.assign(new dom.Event('contextmenu', { bubbles:true, cancelable:true }), {button:2, clientX:60, clientY:80})))
    const actions = (menu: Element) => [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].filter(item => item.closest('[role="menu"]') === menu)
    const menu = document.querySelector('[role="menu"][aria-label="Application actions"]')!
    const shared = ['Choose session','Edit connection','Copy host address','Copy host name','Create configuration copy','Delete connection']
    expect(actions(menu).map(item=>item.textContent)).toEqual([...shared,'New connection'])
    const chooser = actions(menu)[0]!
    await act(async () => chooser.dispatchEvent(Object.assign(new dom.Event('keydown', { bubbles:true, cancelable:true }), {key:'ArrowRight',code:'ArrowRight'})))
    const flyout = document.getElementById(chooser.getAttribute('aria-controls') ?? '')
    const option = flyout && actions(flyout).find(item=>item.textContent?.startsWith('Session 1'))
    expect(option).toBeTruthy()
    await act(async () => option!.click())
    expect(chosen()).toBe(first)
    await click('#tab-hosts')
    await act(async () => document.querySelector('#manager-more-second')!
      .dispatchEvent(Object.assign(new dom.Event('keydown',{bubbles:true,cancelable:true}),{key:'ArrowDown',code:'ArrowDown'})))
    const more = document.querySelector('#manager-more-second')!
    const moreMenu = document.getElementById(more.getAttribute('aria-controls') ?? '')!
    expect(actions(moreMenu).map(item=>item.textContent)).toEqual(shared.slice(1))
    clipboard.length = 0
    await act(async () => actions(moreMenu).find(item=>item.textContent==='Copy host address')!.click())
    expect(clipboard).toEqual([other.address])
  } finally { await close() }
})

test('a changed endpoint opens in a new pane and the old split retains its targets and focus marker', async () => {
  const original = host.address
  const close = await mountApp(true)
  try {
    const notify = (applied: number) => fixture.sync({ applied, settings: { version: 1, target: {} }, task: {} })
    await act(async () => notify(++appliedVersion))
    host.address = 'replacement.example.test'
    fixture.revision++
    await act(async () => notify(++appliedVersion))
    expect(document.querySelector('#manager-host-first')?.textContent).toContain(host.address)
    await click('#manager-open-first')
    const { id, address } = fixture.dialed[0]!
    expect(address).toBe(host.address)
    expect(['left','right']).not.toContain(id)
    expect(document.getElementById('pane-'+id)?.querySelector('.xterm')?.getAttribute('data-address')).toBe(address)
    await click('#tab-terminal-retained')
    expect(document.querySelector('#pane-left')?.textContent).toContain('qa@'+original)
    expect(document.querySelector('[data-split-active]')?.id).toBe('pane-left')
    await act(async () => {
      document.querySelector('#pane-right')!.dispatchEvent(new dom.Event('pointerdown', { bubbles: true }))
    })
    expect([...document.querySelectorAll('[data-split-active]')].map(pane => pane.id)).toEqual(['pane-right'])
    expect(fixture.dialed).toHaveLength(1)
  } finally { host.address = original; await close() }
})

async function openTerminalTabMenu(id: string) {
  const tab = document.getElementById(id)!
  await act(async () => tab.dispatchEvent(Object.assign(new dom.Event('contextmenu', { bubbles: true, cancelable: true }), { button: 2, clientX: 60, clientY: 80 })))
  const menu = document.querySelector('[role="menu"][aria-label="More session actions"]')!
  return [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].filter(item => item.closest('[role="menu"]') === menu)
}

test('a single terminal tab offers only tab actions and pins the clicked tab', async () => {
  const close = await mountApp()
  try {
    await click('#manager-open-first')
    const tabId = chosen()!
    const menu = await openTerminalTabMenu(tabId)
    expect(menu.map(item => item.textContent)).toEqual(['New terminal', 'Split', 'Rename…', 'Pin tab', 'Close tab'])
    await act(async () => menu.find(item => item.textContent === 'Pin tab')!.click())
    expect((await openTerminalTabMenu(tabId)).map(item => item.textContent)).toContain('Unpin tab')
  } finally { await close() }
})

test('a split tab lists its panes in a submenu and switches to the chosen pane', async () => {
  const close = await mountApp(true)
  try {
    await click('#tab-terminal-retained')
    const menu = await openTerminalTabMenu('tab-terminal-retained')
    const chooser = menu.find(item => item.textContent === 'Switch pane')!
    expect(menu).toHaveLength(6)
    await act(async () => chooser.dispatchEvent(Object.assign(new dom.Event('keydown', { bubbles: true, cancelable: true }), { key: 'ArrowRight', code: 'ArrowRight' })))
    const flyout = document.getElementById(chooser.getAttribute('aria-controls')!)!
    const panes = flyout.querySelectorAll<HTMLElement>('[role="menuitem"]')
    expect(panes).toHaveLength(2)
    await act(async () => panes[1]!.click())
    expect(document.querySelector('[data-split-active]')?.id).toBe('pane-right')
  } finally { await close() }
})

test('a background tab only offers directions it can move and keeps its own target', async () => {
  const close = await mountApp()
  try {
    await click('#manager-open-first')
    const first = chosen()!
    await click('#tab-hosts')
    await click('#manager-open-second')
    const second = chosen()!
    const menu = await openTerminalTabMenu(first)
    const organize = menu.find(item => item.textContent === 'Organize tabs')!
    await act(async () => organize.dispatchEvent(Object.assign(new dom.Event('keydown', { bubbles: true, cancelable: true }), { key: 'ArrowRight', code: 'ArrowRight' })))
    const flyout = document.getElementById(organize.getAttribute('aria-controls')!)!
    const items = [...flyout.querySelectorAll<HTMLElement>('[role="menuitem"]')]
    expect(items.map(item => item.textContent)).toEqual(['Move tab later'])
    await act(async () => items[0]!.click())
    expect([...document.querySelectorAll('#session-tab-strip [role=tab]')].map(tab => tab.id)).toEqual([second, first])
    expect(chosen()).toBe(second)
  } finally { await close() }
})

test('editing the address behind a saved jump ID retires the old route without reconnecting it', async () => {
  const destination = host as typeof host & {jumpHostIds?: string[]}
  const original = other.address
  destination.jumpHostIds = [other.id]
  const close = await mountApp()
  try {
    await click('#manager-open-first')
    const previous = chosen()
    await click('#tab-hosts')
    const notify = (applied:number) => fixture.sync({ applied, settings:{version:1,target:{}}, task:{} })
    await act(async () => notify(++appliedVersion))
    other.address = 'replacement-jump.example.test'
    fixture.revision++
    await act(async () => notify(++appliedVersion))
    expect(document.querySelector('#manager-open-first')?.textContent).toContain('Connect')
    await click('#manager-open-first')
    expect(chosen()).not.toBe(previous)
    expect(fixture.dialed).toHaveLength(2)
    await click('#'+previous)
    await click('#connection-new-terminal')
    expect(fixture.dialed).toHaveLength(2)
    expect(document.querySelectorAll('#session-tab-strip [role=tab]')).toHaveLength(2)
  } finally { other.address = original; delete destination.jumpHostIds; await close() }
})

test('relay conflict names both endpoints and sends the reviewed conflict ID', async () => {
  const { createRoot } = await import('react-dom/client')
  const { default: TransferTasks } = await import('../frontend/src/features/workspace/TransferTasks')
  const { fileGateway } = await import('../frontend/src/shared/bridge/files')
  const root = createRoot(document.querySelector('main')!)
  const decisions: string[][] = []
  const gateway = { ...fileGateway, resolveConflict: async (...args: Parameters<typeof fileGateway.resolveConflict>) => { decisions.push(args) } }
  const task = { id:'transfer', sourceLabel:'Build A · qa@a:22', destinationLabel:'Build B · qa@b:2202', sourceSession:'a', destinationSession:'b',
    conflictId:'reviewed-file', conflictSource:'/from/nested/file', conflictTarget:'/to/nested (2)/file', direction:'remote-remote',
    destinationPath:'/to', name:'nested', state:'conflict', phase:'transferring', done:0, total:128 } as FileTask
  try {
    await act(async () => root.render(<TransferTasks tasks={[task]} gateway={gateway} onError={message => { throw Error(message) }}/>))
    await click('button[aria-label*="nested"]')
    const content = document.querySelector('main')!.textContent
    expect(content).toContain('Build A · qa@a:22 · /from/nested/file')
    expect(content).toContain('Build B · qa@b:2202 · /to/nested (2)/file')
    await act(async () => [...document.querySelectorAll('button')].find(button => button.textContent === 'Overwrite')!.click())
    expect(decisions).toEqual([['transfer','reviewed-file','overwrite']])
  } finally { await act(async () => root.unmount()) }
})
