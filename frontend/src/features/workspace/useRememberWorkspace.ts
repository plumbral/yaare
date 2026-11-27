import { tr } from '../../shared/i18n/messages'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { SessionLayoutService, type SessionLayout } from '../../shared/generated/mygo'
import { desktopAvailable } from '../../shared/bridge/application'

const fingerprint = (layout: SessionLayout) => JSON.stringify({...layout, version: 0})
const disabledLayout: SessionLayout = {version:0,format:1,enabled:false,groups:[],panes:[],activeId:'',view:'hosts',startOpen:false,startWidth:256,endWidth:320}

export interface RememberWorkspaceState {
  enabled: boolean
  ready: boolean
  error: string
  subscribeBusy: (listener: () => void) => () => void
  readBusy: () => boolean
  onChange: (value: boolean) => void
  retry: () => void
}

export function createRememberBusyStore() {
  let busy = false
  const listeners = new Set<() => void>()
  return {
    read: () => busy,
    subscribe(listener: () => void) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    set(next: boolean) {
      if (busy === next) return
      busy = next
      for (const listener of listeners) listener()
    },
  }
}

export function useRememberWorkspace(current: SessionLayout, ready: boolean, onRestore: (saved: SessionLayout) => void): RememberWorkspaceState {
  const [loaded, setLoaded] = useState(false), [enabled, setEnabled] = useState(true)
  const [error, setError] = useState(''), [retry, setRetry] = useState(0)
  const [busyStore] = useState(createRememberBusyStore)
  const version = useRef(0), mounted = useRef(true), working = useRef(false), accepted = useRef('')
  const wanted = useRef<{layout: SessionLayout; digest: string} | null>(null)
  const pointers = useRef(new Set<number>()), releaseTimer = useRef<number | undefined>(undefined)
  const debounceTimer = useRef<number | undefined>(undefined)
  const restore = useRef(onRestore), attempted = useRef(false), loadedRef = useRef(loaded)
  restore.current = onRestore
  loadedRef.current = loaded

  const persist = useCallback(async () => {
    if (!mounted.current || working.current || pointers.current.size || debounceTimer.current !== undefined || !wanted.current) return
    if (wanted.current.digest === accepted.current) { wanted.current = null; return }
    working.current = true
    busyStore.set(true)
    try {
      while (mounted.current && !pointers.current.size && debounceTimer.current === undefined && wanted.current) {
        const {layout, digest} = wanted.current
        wanted.current = null
        if (digest === accepted.current) continue
        const committed = await SessionLayoutService.save({...layout, version: version.current})
        version.current = committed.version
        accepted.current = digest
        if (mounted.current) setError('')
      }
    } catch (cause) {
      wanted.current = null
      try {
        const actual = await SessionLayoutService.load()
        version.current = actual.version
        accepted.current = fingerprint(actual)
      } catch {}
      if (mounted.current) setError(cause instanceof Error ? cause.message : tr("工作区保存未完成，请重试。"))
    } finally {
      working.current = false
      busyStore.set(false)
    }
  }, [busyStore])

  useEffect(() => {
    mounted.current = true
    const held = pointers.current
    const down = (event: PointerEvent) => { held.add(event.pointerId) }
    const schedule = () => {
      if (held.size || !wanted.current || releaseTimer.current !== undefined) return
      releaseTimer.current = window.setTimeout(() => {
        releaseTimer.current = undefined
        window.clearTimeout(debounceTimer.current)
        debounceTimer.current = undefined
        void persist()
      }, 0)
    }
    const up = (event: PointerEvent) => { if (held.delete(event.pointerId)) schedule() }
    const blur = () => { if (held.size) { held.clear(); schedule() } }
    const flush = () => {
      window.clearTimeout(debounceTimer.current)
      debounceTimer.current = undefined
      void persist()
    }
    window.addEventListener('pagehide', flush)
    window.addEventListener('pointerdown', down, true)
    window.addEventListener('pointerup', up, true)
    window.addEventListener('pointercancel', up, true)
    window.addEventListener('blur', blur)
    return () => {
      mounted.current = false
      held.clear()
      window.clearTimeout(releaseTimer.current)
      releaseTimer.current = undefined
      window.removeEventListener('pointerdown', down, true)
      window.removeEventListener('pointerup', up, true)
      window.removeEventListener('pointercancel', up, true)
      window.removeEventListener('blur', blur)
      window.removeEventListener('pagehide', flush)
      window.clearTimeout(debounceTimer.current)
      debounceTimer.current = undefined
    }
  }, [persist])

  useEffect(() => {
    if (!ready || attempted.current || !desktopAvailable()) return
    attempted.current = true
    void SessionLayoutService.load().then(saved => {
      if (!mounted.current) return
      version.current = saved.version
      accepted.current = fingerprint(saved)
      setEnabled(saved.enabled)
      if (saved.enabled) restore.current(saved)
      setLoaded(true)
      setError('')
    }).catch(cause => {
      if (mounted.current) setError(cause instanceof Error ? cause.message : tr("工作区读取失败，当前布局不会覆盖原记录。"))
    })
  }, [ready, retry])

  useEffect(() => {
    if (!loaded || !desktopAvailable()) return
    const layout = enabled ? {...current, enabled: true} : disabledLayout
    const digest = fingerprint(layout)
    wanted.current = digest === accepted.current && !working.current ? null : {layout, digest}
    window.clearTimeout(debounceTimer.current)
    debounceTimer.current = undefined
    if (!wanted.current || pointers.current.size || releaseTimer.current !== undefined) return
    if (!enabled) { void persist(); return }
    debounceTimer.current = window.setTimeout(() => {
      debounceTimer.current = undefined
      void persist()
    }, 300)
  }, [current, loaded, enabled, retry, persist])

  const retryAction = useCallback(() => {
    if (!loadedRef.current) attempted.current = false
    setRetry(previous => previous + 1)
  }, [])

  return useMemo(() => ({
    enabled,
    ready: loaded,
    error,
    subscribeBusy: busyStore.subscribe,
    readBusy: busyStore.read,
    onChange: setEnabled,
    retry: retryAction,
  }), [enabled, loaded, error, busyStore, retryAction])
}
