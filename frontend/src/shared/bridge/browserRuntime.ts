import { tr } from '../i18n/messages'
import { getLanguage } from '../i18n/locale'
import type { Channel, Runtime } from 'mygo-runtime'
type Envelope = {
  type: string
  id: string
  seq?: number
  data?: unknown
  error?: string
}
type Pending = {
  resolve: (v: unknown) => void
  reject: (e: Error) => void
  channel?: WebChannel<unknown>
}
export type BrowserState = {
  connected: boolean
  connecting: boolean
  message: string
  user: string
  protected: boolean
}
let current: BrowserRuntime | null = null,
  state: BrowserState | null = null
const listeners = new Set<() => void>()
export const browserState = () => state
export const subscribeBrowser = (listener: () => void) => {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
function publish(next: BrowserState) {
  state = next
  for (const listener of listeners) listener()
}
export const browserMode = () => current !== null
export const browserReconnect = () => current?.connect()
export const browserLogout = () => current?.logout()

class WebChannel<T> implements Channel<T> {
  private messages: { value: T; seq: number }[] = []
  private waiting: ((v: IteratorResult<T>) => void) | null = null
  private last = 0
  private handler: ((value: T) => void) | null = null
  id = ''
  closed = false
  onclose: (() => void) | null = null
  constructor(
    private runtime: BrowserRuntime,
    onmessage?: (value: T) => void,
  ) {
    this.handler = onmessage ?? null
  }
  get onmessage() {
    return this.handler
  }
  set onmessage(value: ((v: T) => void) | null) {
    this.handler = value
    this.drain()
  }
  push(value: T, seq: number) {
    if (this.closed) return
    this.messages.push({ value, seq })
    this.drain()
  }
  private drain() {
    if (this.closed) return
    if (this.handler) {
      while (this.messages.length) {
        const message = this.messages.shift()!
        try {
          this.handler(message.value)
        } finally {
          this.runtime.ack(this.id, message.seq)
        }
      }
    } else if (this.waiting && this.messages.length) {
      const callback = this.waiting,
        message = this.messages.shift()!
      this.waiting = null
      this.last = message.seq
      callback({ done: false, value: message.value })
    }
  }
  finish() {
    if (this.closed) return
    this.closed = true
    this.messages = []
    this.waiting?.({ done: true, value: undefined })
    this.waiting = null
    this.onclose?.()
  }
  close() {
    if (this.closed) return
    this.runtime.cancel(this.id)
    this.finish()
  }
  [Symbol.asyncIterator](): AsyncIterator<T> {
    return {
      next: () => {
        if (this.last) {
          this.runtime.ack(this.id, this.last)
          this.last = 0
        }
        if (this.closed)
          return Promise.resolve({ done: true, value: undefined })
        return new Promise((resolve) => {
          this.waiting = resolve
          this.drain()
        })
      },
      return: async () => {
        this.close()
        return { done: true, value: undefined }
      },
    }
  }
}

export class BrowserRuntime {
  private socket: WebSocket | null = null
  private serial = 0
  private pending = new Map<string, Pending>()
  private protectionRevision=0
  private stopped = false
  private csrf: string
  private connectingReject: ((e: Error) => void) | null = null
  readonly runtime: Runtime
  private beforeUnload = (event: BeforeUnloadEvent) => {
    if (state?.protected) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  constructor(
    private session: { user: string; csrf: string },
    private onLogout: () => void,
  ) {
    this.csrf = session.csrf
    const unavailable = () =>
      Promise.reject(new Error(tr('此操作需要 Yaare 桌面客户端。')))
    const rt = {
      call: <T>(method: string, ...args: unknown[]) =>
        this.call(method, args) as Promise<T>,
      channel: <T>(onmessage?: (value: T) => void) =>
        new WebChannel<T>(this, onmessage),
      on: () => () => {},
      once: () => () => {},
      platform: 'linux',
      windowId: 0,
      version: 'yaare-server-v1',
      preferences: {
        accent: null,
        reduceMotion: matchMedia('(prefers-reduced-motion: reduce)').matches,
        reduceTransparency: false,
        increaseContrast: false,
      },
      locale: navigator.language,
      dir: 'ltr',
      windowActive: true,
      window: {
        minimize: unavailable,
        maximize: unavailable,
        unmaximize: unavailable,
        toggleMaximize: unavailable,
        isMaximized: async () => false,
        toggleFullScreen: async () => {
          if (document.fullscreenElement) await document.exitFullscreen()
          else await document.documentElement.requestFullscreen()
        },
        close: unavailable,
        setTitle: async (title: string) => {
          document.title = title
        },
      },
    }
    this.runtime = {
      ...rt,
      callWith: <T>(
        method: string,
        args: unknown[],
        options?: { signal?: AbortSignal },
      ) => this.call(method, args, options?.signal) as Promise<T>,
      platform: 'linux',
      dir: 'ltr',
      preferences: {
        accent: null,
        reduceMotion: rt.preferences.reduceMotion,
        reduceTransparency: false,
        highContrast: false,
        textScale: 1,
      },
    }
    window.addEventListener('beforeunload', this.beforeUnload)
    current = this
    publish({
      connected: false,
      connecting: false,
      message: '',
      user: session.user,
      protected: false,
    })
  }
  async connect() {
    if (
      this.socket?.readyState === WebSocket.OPEN ||
      this.socket?.readyState === WebSocket.CONNECTING
    )
      return
    this.stopped = false
    publish({ ...state!, connecting: true, message: '' })
    try {
      const response = await fetch('/api/session', {
          cache: 'no-store',
          credentials: 'same-origin',
        }),
        value = await response.json()
      if (!value.authenticated)
        throw Error(tr('登录已过期，请退出后重新登录。'))
      this.csrf = value.csrf
      await new Promise<void>((resolve, reject) => {
        this.connectingReject = reject
        const url = new URL('/api/ws', location.href)
        url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
        const socket = new WebSocket(url, ['yaare.v1', this.csrf])
        this.socket = socket
        this.serial = 0
        socket.onopen = () => {
          if (this.stopped) {
            socket.close()
            reject(Error(tr('已停止连接。')))
            return
          }
          ;(window as unknown as { mygo?: Runtime }).mygo = this.runtime
          publish({
            ...state!,
            connected: true,
            connecting: false,
            message: '',
          })
          void this.call('ApplicationService.SetLanguage', [
            getLanguage(),
          ]).catch(() => {})
          resolve()
        }
        socket.onmessage = (event) => {
          try {
            this.receive(JSON.parse(String(event.data)))
          } catch {
            socket.close(1002, 'Invalid message')
          }
        }
        socket.onclose = () => {
          if (this.socket !== socket) return
          this.socket = null
          const error = Error(
            tr('服务连接已中断，请核对操作结果；不会自动重试。'),
          )
          for (const call of this.pending.values()) {
            call.channel?.finish()
            call.reject(error)
          }
          this.pending.clear()
          publish({
            ...state!,
            connected: false,
            connecting: false,
            message: this.stopped ? '' : error.message,
          })
          reject(error)
        }
        socket.onerror = () =>
          reject(Error(tr('无法连接服务，请检查网络或重新登录。')))
      })
    } catch (e) {
      if (!this.stopped)
        publish({
          ...state!,
          connected: false,
          connecting: false,
          message: e instanceof Error ? e.message : tr('无法连接服务。'),
        })
      throw e
    } finally {
      this.connectingReject = null
    }
  }
  private send(value: unknown) {
    if (this.socket?.readyState !== WebSocket.OPEN)
      throw Error(tr('服务连接已断开，请先重新连接。'))
    this.socket.send(JSON.stringify(value))
  }
  private async call(
    method: string,
    args: unknown[],
    signal?: AbortSignal,
  ): Promise<unknown> {
    signal?.throwIfAborted()
    if (
      method === 'FileService.SelectLocal' ||
      method === 'FileService.OpenLocalHome'
    )
      return this.call('BrowserFiles.Begin', [])
    if (method === 'SSHConfigService.Preview') {
      const { pickBrowserFiles } = await import('./browserFiles')
      const files = await pickBrowserFiles(false)
      if (!files) return null
      if (files.length !== 1 || files[0]!.size > 1048576)
        throw Error(tr('请选择不超过 1 MiB 的普通 SSH Config 文件。'))
      const { encodeBase64 } = await import('./base64')
      return this.call(
        'BrowserConfig.Preview',
        [encodeBase64(new Uint8Array(await files[0]!.arrayBuffer()))],
        signal,
      )
    }
    if (method === 'BackupService.ChooseImport') {
      const { pickBrowserFiles, uploadBrowserFiles } = await import(
        './browserFiles'
      )
      const files = await pickBrowserFiles(false)
      if (!files) return { id: '', name: '', size: 0 }
      if (files.length !== 1 || files[0]!.size > 24 * 1024 * 1024)
        throw Error(tr('请选择有效的 Yaare 加密配置文件。'))
      const file = files[0]!,
        stage = 'import-' + crypto.randomUUID()
      const folder = await uploadBrowserFiles(
        [new File([file], stage)],
        signal ?? new AbortController().signal,
        () => {},
      )
      return this.call('BrowserBackup.Import', [folder.token, stage, file.name])
    }
    if (
      method === 'BackupService.Export' ||
      method === 'BackupService.SaveRestorePoint'
    ) {
      const { saveBrowserURL } = await import('./browserFiles')
      const href = await this.call(
        method === 'BackupService.Export'
          ? 'BrowserBackup.Export'
          : 'BrowserBackup.RestorePoint',
        args,
        signal,
      )
      saveBrowserURL(String(href))
      return true
    }
    if (method === 'ApplicationService.SetWindowAppearance')
      return Promise.resolve()
    if (method === 'ApplicationService.WriteClipboardText')
      return navigator.clipboard.writeText(String(args[0] ?? ''))
    if (method === 'ApplicationService.ReadClipboardText')
      return navigator.clipboard.readText()
    if (method === 'FileService.SetTextProtection') {
      const revision=Number(args[0]);if(revision>this.protectionRevision){this.protectionRevision=revision;publish({ ...state!, protected: !!args[1] })}
      return Promise.resolve()
    }
    if (this.socket?.readyState !== WebSocket.OPEN)
      return Promise.reject(Error(tr('服务连接已断开，请先重新连接。')))
    const id = String(++this.serial),
      last = args[args.length - 1],
      channel =
        last instanceof WebChannel ? (last as WebChannel<unknown>) : undefined
    if (channel) {
      channel.id = id
      args = args.slice(0, -1)
    }
    return new Promise((resolve, reject) => {
      const abort = () => {
        this.pending.delete(id)
        this.cancel(id)
        channel?.finish()
        reject(signal?.reason)
      }
      signal?.addEventListener('abort', abort, { once: true })
      const clean = () => signal?.removeEventListener('abort', abort)
      this.pending.set(id, {
        resolve: (v) => {
          clean()
          resolve(v)
        },
        reject: (e) => {
          clean()
          reject(e)
        },
        channel,
      })
      try {
        this.send({ type: 'call', id, method, args })
      } catch (e) {
        clean()
        this.pending.delete(id)
        channel?.finish()
        reject(e)
      }
    })
  }
  private receive(message: Envelope) {
    const call = this.pending.get(message.id)
    if (!call) return
    if (message.type === 'event' && call.channel && message.seq) {
      call.channel.push(message.data, message.seq)
      return
    }
    if (message.type === 'result') {
      this.pending.delete(message.id)
      call.channel?.finish()
      if (message.error) call.reject(Error(message.error))
      else call.resolve(message.data)
    }
  }
  ack(id: string, seq: number) {
    if (this.socket?.readyState === WebSocket.OPEN)
      this.send({ type: 'ack', id, seq })
  }
  cancel(id: string) {
    if (id && this.socket?.readyState === WebSocket.OPEN)
      this.send({ type: 'cancel', id })
  }
  async logout() {
    const response = await fetch('/api/logout', {
      method: 'POST',
      headers: { 'X-Yaare-CSRF': this.csrf },
      credentials: 'same-origin',
      cache: 'no-store',
    })
    if (!response.ok) throw Error(tr('退出未完成，请重试。'))
    this.dispose()
    this.onLogout()
  }
  dispose() {
    this.stopped = true
    window.removeEventListener('beforeunload', this.beforeUnload)
    const socket = this.socket
    this.socket = null
    const error = Error(tr('工作区已关闭。'))
    this.connectingReject?.(error)
    this.connectingReject = null
    socket?.close()
    for (const p of this.pending.values()) {
      p.channel?.finish()
      p.reject(error)
    }
    this.pending.clear()
    if (window.mygo === this.runtime)
      delete (window as unknown as { mygo?: Runtime }).mygo
    if (current === this) {
      current = null
      state = null
      for (const l of listeners) l()
    }
  }
}
