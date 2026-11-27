import { expect, test } from 'bun:test'
import { act, createElement } from 'react'
import { document, dom, storage, calls, clipboard, host, other, fixture, installWorkbenchDom } from './fixtures/workbench-dom'

installWorkbenchDom()

for (const [position, enabled] of [['top', true], ['left', true], ['top', false]] as const) {
  test(`startup shows all hosts with ${position} tabs and layout memory ${enabled ? 'enabled' : 'disabled'}`, async () => {
    const { createRoot } = await import('react-dom/client')
    const { default: App } = await import('../frontend/src/app/App')
    calls.length = clipboard.length = 0
    fixture.selectedText = ''
    storage.set('yaare.tabs-position', position)
    fixture.saved = {
      version: 3, format: 1, enabled, view: enabled ? 'terminal' : 'hosts', activeId: enabled ? 'right' : '', startOpen: true, startWidth: 256, endWidth: 320,
      panes: enabled ? ['left', 'right'].map(id => ({ id, hostId: host.id, pending: false, path: '.', filesOpen: false })) : [],
      groups: enabled ? [{ id: 'saved', name: 'Saved split', activeId: 'right', maximized: false, pinned: false, root: { kind: 'split', id: 'branch', axis: 'horizontal', ratio: 0.37,
        first: { kind: 'leaf', id: 'left' }, second: { kind: 'leaf', id: 'right' } } }] : [],
    }
    const root = createRoot(document.querySelector('main')!)
    try {
      await act(async () => root.render(createElement(App)))
      expect(calls).toContain('SessionLayoutService.Load')
      expect(document.querySelector('#tab-hosts')?.getAttribute('aria-current')).toBe('page')
      expect(document.querySelector('[data-group-filter-value="all"]')?.getAttribute('aria-checked')).toBe('true')
      expect(document.querySelectorAll('#panel-hosts [data-host-context-id]')).toHaveLength(2)
      const tab = document.querySelector<HTMLElement>('#session-tab-strip [data-session-tab="terminal-saved"]')
      expect(!!tab).toBe(enabled)
      expect(document.querySelectorAll('[data-terminal-pane]')).toHaveLength(enabled ? 2 : 0)
      if (tab) {
        expect(tab.getAttribute('aria-selected')).toBe('false')
        expect(tab.closest('[data-terminal-context-pane]')?.getAttribute('data-terminal-context-pane')).toBe('right')
      }
      const subject = document.querySelector('[data-host-context-id="second"] [data-host-name]')!
      for (const label of ['Copy host address', 'Copy host name']) {
        fixture.selectedText = label === 'Copy host name' ? 'Logs' : ''
        await act(async () => {
          const event = new dom.Event('contextmenu', { bubbles: true, cancelable: true })
          subject.dispatchEvent(Object.assign(event, { button: 2, clientX: 60, clientY: 80 }))
        })
        const menu = document.querySelector('[role="menu"][aria-label="Application actions"]')!
        const items = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')]
        expect(items.map(item => item.textContent)).toEqual([
          ...(fixture.selectedText ? ['Copy'] : []), 'Edit connection', 'Copy host address', 'Copy host name', 'Create configuration copy', 'Delete connection', 'New connection',
        ])
        await act(async () => items.find(item => item.textContent === label)!.click())
      }
      expect(clipboard).toEqual([other.address, other.name])
      expect(document.querySelector('#tab-hosts')?.getAttribute('aria-current')).toBe('page')
      if (tab) {
        await act(async () => tab.click())
        expect(tab.getAttribute('aria-selected')).toBe('true')
      }
      expect(calls.filter(method => method.startsWith('SSHService.'))).toEqual([])
    } finally {
      await act(async () => root.unmount())
    }
  })
}
