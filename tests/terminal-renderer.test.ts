import { expect, test } from 'bun:test'
import { parseHTML } from 'linkedom'
import type { WebglAddon } from '@xterm/addon-webgl'
import type { Terminal } from '@xterm/xterm'
import { createTerminalRenderer, observeTerminalGeometry } from '../frontend/src/features/terminal/terminalRenderer'

test('a pane refits on changes to its container or its rendered cell grid', () => {
  const { document } = parseHTML('<main></main>')
  const seen: Element[] = []
  const observer = { observe: (target: Element) => { seen.push(target) } }
  const mount = document.createElement('div')
  mount.innerHTML = '<div class="xterm"><div class="xterm-screen"></div></div>'
  document.querySelector('main')!.append(mount.cloneNode(true), mount)
  observeTerminalGeometry(observer, mount)
  expect(seen).toHaveLength(2)
  expect(seen).toContain(mount)
  expect(seen).toContain(mount.querySelector('.xterm-screen')!)
})

function fixture({ fail = false } = {}) {
  const loaded: WebglAddon[] = []
  const installed = new Set<WebglAddon>()
  const canvases: TestCanvas[] = []
  const callbacks = new Map<WebglAddon, () => void>()
  const counters = { disposed: 0, released: 0 }
  class TestCanvas extends EventTarget {
    lost = false
    getContext() {
      return {
        isContextLost: () => this.lost,
        getExtension: () => ({ loseContext: () => { this.lost = true; counters.released++ } }),
      }
    }
    remove() {
      const index = canvases.indexOf(this)
      if (index >= 0) canvases.splice(index, 1)
    }
    lose() {
      this.lost = true
      this.dispatchEvent(new Event('webglcontextlost'))
    }
  }
  const terminal = {
    element: { querySelectorAll: () => canvases },
    loadAddon(addon: WebglAddon) {
      loaded.push(addon)
      canvases.push(new TestCanvas())
      if (fail) throw new Error('WebGL not available')
      installed.add(addon)
    },
  } as unknown as Pick<Terminal, 'element' | 'loadAddon'>
  const createAddon = () => {
    const addon = {
      onContextLoss(listener: () => void) {
        callbacks.set(addon, listener)
        return { dispose: () => { callbacks.delete(addon) } }
      },
      dispose() { counters.disposed++; installed.delete(addon) },
    } as unknown as WebglAddon
    return addon
  }
  return { terminal, loaded, installed, canvases, callbacks, counters, createAddon }
}

test('WebGL stays lazy and a hidden or disposed pane cannot attach a late import', async () => {
  for (const reason of ['hide', 'dispose']) {
    const testCase = fixture()
    let resolveImport!: (factory: typeof testCase.createAddon) => void
    let imports = 0
    const renderer = createTerminalRenderer(testCase.terminal, () => {
      imports++
      return new Promise(resolve => { resolveImport = resolve })
    })
    renderer.setVisible(false)
    expect(imports).toBe(0)
    renderer.setVisible(true)
    renderer.setVisible(true)
    expect(imports).toBe(1)
    if (reason === 'hide') renderer.setVisible(false)
    else renderer.dispose()
    resolveImport(testCase.createAddon)
    await Promise.resolve()
    expect(testCase.loaded).toHaveLength(0)
    renderer.dispose()
  }
})

test('hiding releases WebGL and reopening uses a new renderer on the same terminal', async () => {
  const testCase = fixture()
  const renderer = createTerminalRenderer(testCase.terminal, async () => testCase.createAddon)
  renderer.setVisible(true)
  await Promise.resolve()
  expect(testCase.installed.size).toBe(1)
  renderer.setVisible(false)
  expect(testCase.installed.size).toBe(0)
  expect(testCase.canvases).toHaveLength(0)
  expect(testCase.counters).toEqual({ disposed: 1, released: 1 })

  renderer.setVisible(true)
  await Promise.resolve()
  expect(testCase.loaded).toHaveLength(2)
  expect(testCase.installed.size).toBe(1)
  renderer.dispose()
  renderer.dispose()
  expect(testCase.counters).toEqual({ disposed: 2, released: 2 })
  expect(testCase.callbacks.size).toBe(0)
})

test.each(['native event', 'addon event', 'activation failure', 'import failure'] as const)(
  'the terminal remains on DOM after %s', async reason => {
    const testCase = fixture({ fail: reason === 'activation failure' })
    let imports = 0
    const renderer = createTerminalRenderer(testCase.terminal, async () => {
      imports++
      if (reason === 'import failure') throw new Error('WebGL chunk could not load')
      return testCase.createAddon
    }, [])
    renderer.setVisible(true)
    await Promise.resolve()
    if (reason === 'native event') testCase.canvases[0]!.lose()
    if (reason === 'addon event') testCase.callbacks.get(testCase.loaded[0]!)!()
    await Promise.resolve()
    expect(testCase.installed.size).toBe(0)
    expect(testCase.canvases).toHaveLength(0)
    expect(testCase.callbacks.size).toBe(0)
    expect(testCase.counters.disposed).toBe(reason === 'import failure' ? 0 : 1)
    expect(testCase.counters.released).toBe(reason === 'addon event' || reason === 'activation failure' ? 1 : 0)
    renderer.setVisible(false)
    renderer.setVisible(true)
    await Promise.resolve()
    expect(imports).toBe(1)
    renderer.dispose()
  },
)

test('WebGL retries with a fresh renderer after a lost context on the same terminal', async () => {
  const testCase = fixture()
  const renderer = createTerminalRenderer(testCase.terminal, async () => testCase.createAddon, [1])
  try {
    renderer.setVisible(true)
    await Promise.resolve()
    testCase.canvases[0]!.lose()
    await Promise.resolve()
    expect(testCase.installed.size).toBe(0)
    await Bun.sleep(10)
    expect(testCase.loaded).toHaveLength(2)
    expect(testCase.installed.size).toBe(1)
    testCase.canvases[0]!.lose()
    await Bun.sleep(10)
    expect(testCase.loaded).toHaveLength(2)
    expect(testCase.installed.size).toBe(0)
  } finally { renderer.dispose() }
})
