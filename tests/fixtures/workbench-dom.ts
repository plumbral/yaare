import { afterAll, beforeAll } from 'bun:test'
import { parseHTML } from 'linkedom'
import type { SessionLayout } from '../../frontend/src/shared/generated/mygo'

export const { document, window: dom } = parseHTML('<html><head></head><body><main></main></body></html>')
export const storage = new Map<string, string>()
export const calls: string[] = []
export const clipboard: string[] = []
export const host = { authentication: 'key', useAgent: true, id: 'first', name: 'First host', address: 'first.example.test', port: 2202, username: 'qa', group: '未分组', system: 'Linux' }
export const other = { ...host, id: 'second', name: '東京 · Logs', address: '2001:db8::8' }
export const fixture = {
  saved: undefined as SessionLayout | undefined, selectedText: '', revision: 1,
  dialed: [] as {address: string; id: string; drop: () => void}[],
  sync: (_event: unknown) => {},
}
const restore: (() => void)[] = []

function stub(target: object, key: string, descriptor: PropertyDescriptor) {
  const original = Object.getOwnPropertyDescriptor(target, key)
  Object.defineProperty(target, key, { configurable: true, ...descriptor })
  restore.push(() => {
    if (original) Object.defineProperty(target, key, original)
    else Reflect.deleteProperty(target, key)
  })
}

export function installWorkbenchDom() {
  beforeAll(() => {
    const rect = { width: 1000, height: 640, x: 0, y: 0, top: 0, left: 0, bottom: 640, right: 1000 }
    Object.assign(document, { hasFocus: () => true, fonts: { ready: Promise.resolve(), load: async () => [] } })
    stub(document, 'oninput', { value: null })
    const prototype = dom.HTMLElement.prototype
    const matches = prototype.matches
    stub(prototype, 'matches', { value: function (this: HTMLElement, selector: string) {
      return selector === ':focus-visible' ? false : matches.call(this, selector)
    } })
    stub(prototype, 'getBoundingClientRect', { value: () => rect })
    stub(prototype, 'getClientRects', { value: () => [rect] })
    stub(prototype, 'getAnimations', { value: () => [] })
    stub(prototype, 'showModal', { value(this: HTMLElement) { this.setAttribute('open', '') } })
    stub(prototype, 'close', { value(this: HTMLElement) { this.removeAttribute('open') } })
    stub(prototype, 'scrollIntoView', { value: () => {} })
    stub(prototype, 'offsetHeight', { get() { return this.hasAttribute('data-row-key') ? 48 : rect.height } })
    stub(prototype, 'offsetWidth', { get: () => rect.width })
    const globals = {
      window: dom, document, Node: dom.Node, Element: dom.Element, HTMLElement: dom.HTMLElement, ShadowRoot: dom.ShadowRoot,
      CSS: { escape: (id: string) => id.replace(/[^\w-]/g, '\\$&') },
      HTMLInputElement: dom.HTMLInputElement, HTMLTextAreaElement: dom.HTMLTextAreaElement, MutationObserver: dom.MutationObserver,
      innerWidth: 1400, innerHeight: 800, IS_REACT_ACT_ENVIRONMENT: true,
      localStorage: { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value) },
      getSelection: () => fixture.selectedText ? { toString: () => fixture.selectedText } : null, getComputedStyle: (element: HTMLElement) => element.style,
      matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
      ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
      requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 1),
      cancelAnimationFrame: clearTimeout,
      mygo: {
        locale: 'en', dir: 'ltr', on: () => () => {}, window: { isMaximized: async () => false },
        channel: (receive?: (event: unknown) => void) => {
          let finish = () => {}
          const end = new Promise<void>(resolve => { finish = resolve })
          return { receive, end, close: finish, async *[Symbol.asyncIterator]() { yield { type: 'connected', revision: 1 }; await end } }
        },
        async callWith(method: string, args: unknown[]) {
          calls.push(method)
          switch (method) {
            case 'WorkspaceService.Apply': return {snapshot: {revision: ++fixture.revision, hosts: [host, other], groups: ['未分组'], commands: [], commandGroups: ['未分组']}, warning: ''}
            case 'FileService.Tasks': return { tasks: [], concurrency: 1 }
            case 'SSHService.Connect': {
              const request = args[0] as {address: string; sessionId: string}
              const channel = args[1] as { end: Promise<void>; close: () => void }
              fixture.dialed.push({address: request.address, id: request.sessionId, drop: channel.close})
              return channel.end
            }
            case 'WorkspaceService.Load': return { ready: true, snapshot: { revision: fixture.revision, hosts: [host, other], groups: ['未分组'], commands: [], commandGroups: ['未分组'] } }
            case 'SessionLayoutService.Load': return fixture.saved
            case 'SessionLayoutService.Save': return args[0]
            case 'SyncService.Watch': fixture.sync = (args[0] as {receive: typeof fixture.sync}).receive; return new Promise<void>(resolve => { (args[0] as { close: () => void }).close = resolve })
            case 'ApplicationService.WriteClipboardText': clipboard.push(String(args[0])); return
            default: return []
          }
        },
      },
    }
    for (const [key, value] of Object.entries(globals)) stub(globalThis, key, { value, writable: true })
  })

  afterAll(() => {
    for (const undo of restore.reverse()) undo()
  })
}
