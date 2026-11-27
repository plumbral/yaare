import { Channel } from 'mygo-runtime'
import { SyncService, type SyncSettings, type SyncStatusEvent, type SyncTaskStatus } from '../generated/mygo'

type Listener = () => void
type SettingsView = Omit<SyncSettings, 'version'>
type OpenWatch = (receive: (event: SyncStatusEvent) => void) => { done: Promise<void>; close: () => void }

function subscription() {
  const listeners = new Set<Listener>()
  return {
    subscribe(listener: Listener) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    notify() { for (const listener of listeners) listener() },
  }
}

function sameFields<Value extends object>(previous: Value, next: Value) {
  return (Object.keys(next) as Array<keyof Value>).every(key => Object.is(previous[key], next[key]))
}

export function createCloudStatus() {
  const taskChanges = subscription()
  const settingsChanges = subscription()
  let task: SyncTaskStatus | null = null
  let settings: { value: SettingsView | null; error: string } = { value: null, error: '' }
  let version = 0
  let applied: number | null = null

  const setError = (error: string) => {
    if (error === settings.error) return
    settings = { ...settings, error }
    settingsChanges.notify()
  }
  const accept = (event: SyncStatusEvent) => {
    if (applied !== null && event.applied < applied) return false
    const changed = applied !== null && event.applied > applied
    applied = event.applied
    if (event.error) {
      setError(event.error)
      return changed
    }
    const { version: nextVersion, target, ...status } = event.settings
    version = nextVersion
    const previous = settings.value
    if (!previous || !sameFields(previous, status) || !sameFields(previous.target, target) || settings.error) {
      settings = { value: { ...status, target }, error: '' }
      settingsChanges.notify()
    }
    if (!task || !sameFields(task, event.task)) {
      task = event.task
      taskChanges.notify()
    }
    return changed
  }
  return {
    accept, setError, getVersion: () => version,
    readTask: () => task, subscribeTask: taskChanges.subscribe,
    readSettings: () => settings, subscribeSettings: settingsChanges.subscribe,
  }
}

export const cloudStatus = createCloudStatus()

export const openCloudWatch: OpenWatch = receive => {
  const channel = new Channel<SyncStatusEvent>(receive)
  return { done: SyncService.watch(channel), close: () => channel.close() }
}

export function followCloudStatus(store: ReturnType<typeof createCloudStatus>, open: OpenWatch, reload: () => Promise<void>, retryDelay = 2_000) {
  let stopped = false
  let close: (() => void) | undefined
  let retry: ReturnType<typeof setTimeout> | undefined
  let delay = retryDelay
  let updating = false
  let pending = false

  async function reloadAppliedConfiguration() {
    updating = true
    try {
      while (pending && !stopped) {
        pending = false
        try { await reload() } catch {}
      }
    } finally { updating = false }
  }

  function connect() {
    let active = true
    const disconnected = (cause?: unknown) => {
      active = false
      if (stopped) return
      close?.()
      close = undefined
      if (cause !== undefined) store.setError(cause instanceof Error ? cause.message : String(cause))
      retry = setTimeout(connect, delay)
      delay = Math.min(30_000, delay * 2)
    }
    try {
      const watch = open(event => {
        if (!active || stopped) return
        delay = retryDelay
        if (!store.accept(event)) return
        pending = true
        if (!updating) void reloadAppliedConfiguration()
      })
      close = watch.close
      void watch.done.then(() => disconnected(), disconnected)
    } catch (cause) { disconnected(cause) }
  }

  connect()
  return () => {
    stopped = true
    clearTimeout(retry)
    close?.()
  }
}
