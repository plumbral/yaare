import type { WebglAddon } from '@xterm/addon-webgl'
import type { Terminal } from '@xterm/xterm'

type LoadWebgl = () => Promise<() => WebglAddon>

const loadWebgl: LoadWebgl = async () => {
  const { WebglAddon } = await import('@xterm/addon-webgl')
  return () => new WebglAddon()
}

export function createTerminalRenderer(terminal: Pick<Terminal, 'element' | 'loadAddon'>, load: LoadWebgl = loadWebgl, retryDelays = [500, 1500, 4000]) {
  let visible = false
  let disposed = false
  let failures = 0
  let retry: ReturnType<typeof setTimeout> | undefined
  let loading = false
  let release: (() => void) | undefined

  const deactivate = () => {
    const cleanup = release
    release = undefined
    cleanup?.()
  }

  const canvases = () => Array.from(terminal.element?.querySelectorAll<HTMLCanvasElement>('.xterm-screen > canvas') ?? [])

  const cancelRetry = () => {
    if (retry !== undefined) clearTimeout(retry)
    retry = undefined
  }

  const scheduleRetry = () => {
    const delay = retryDelays[failures++]
    if (!visible || disposed || delay === undefined) return
    retry = setTimeout(() => {
      retry = undefined
      void enable()
    }, delay)
  }

  const enable = async () => {
    if (!visible || disposed || failures > retryDelays.length || retry !== undefined || loading || release) return
    loading = true
    try {
      const createAddon = await load()
      if (!visible || disposed || !terminal.element) return
      const before = new Set(canvases())
      const addon = createAddon()
      let added: HTMLCanvasElement[] = []
      let canvas: HTMLCanvasElement | undefined
      let context: WebGL2RenderingContext | null | undefined
      const fallback = () => {
        if (release !== cleanup) return
        deactivate()
        scheduleRetry()
      }
      const onCanvasLoss = () => queueMicrotask(fallback)
      const cleanup = () => {
        canvas?.removeEventListener('webglcontextlost', onCanvasLoss)
        contextLoss.dispose()
        try {
          addon.dispose()
        } finally {
          if (context && !context.isContextLost()) context.getExtension('WEBGL_lose_context')?.loseContext()
          for (const addedCanvas of added) addedCanvas.remove()
        }
      }
      const contextLoss = addon.onContextLoss(onCanvasLoss)
      release = cleanup
      try {
        terminal.loadAddon(addon)
      } finally {
        added = canvases().filter(candidate => !before.has(candidate))
        canvas = added.at(-1)
        context = canvas?.getContext('webgl2')
        canvas?.addEventListener('webglcontextlost', onCanvasLoss)
      }
    } catch {
      deactivate()
      scheduleRetry()
    } finally {
      loading = false
    }
  }

  return {
    setVisible(shown: boolean) {
      visible = shown
      if (shown) void enable()
      else { cancelRetry(); deactivate() }
    },
    dispose() {
      disposed = true
      cancelRetry()
      deactivate()
    },
  }
}
