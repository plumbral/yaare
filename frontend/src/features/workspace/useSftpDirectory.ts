import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { FileEntry, FileLocation, LocalFolder, fileGateway } from '../../shared/bridge/files'
import { tr } from '../../shared/i18n/messages'
import { directoryStep, emptyHistory, visitDirectory, type DirectoryHistory } from './directoryHistory'

interface DirectoryOptions {
  active: boolean
  tab: string
  native: boolean
  connected: boolean
  side: 'local' | 'remote'
  sessionId?: string
  attempt?: number
  phase?: string
  workspaceView: boolean
  folder: LocalFolder | null
  currentPath: string
  reloadVersion: number
  directorySync: boolean
  linkedPath: string
  gateway: typeof fileGateway
  location: (side: string, path?: string, folder?: LocalFolder | null) => FileLocation
  onPath: (path: string) => void
  onLocalDirectory: (path: string, folder?: LocalFolder) => void
  onMessage: (message: string) => void
  onError: (message: string) => void
}

type LoadedDirectory = { scope: string; path: string; entries: FileEntry[] }
type Navigate = { kind?: 'navigate' | 'follow' | 'history'; index?: number; root?: LocalFolder }

export function useSftpDirectory({
  active, tab, native, connected, side, sessionId, attempt, phase, workspaceView,
  folder, currentPath, reloadVersion, directorySync, linkedPath, gateway, location,
  onPath, onLocalDirectory, onMessage, onError,
}: DirectoryOptions) {
  const suffix = workspaceView ? ':workspace' : ''
  const localScope = useCallback((token?: string) => `${sessionId ?? 'none'}:local:${token}${suffix}`, [sessionId, suffix])
  const treeScope = side === 'local' ? localScope(folder?.token) : `${sessionId ?? 'none'}:${attempt}:remote${suffix}`
  const [loaded, setLoaded] = useState<LoadedDirectory>()
  const [pathDraft, setPathDraft] = useState(currentPath)
  const [busy, setBusy] = useState(false)
  const mounted = useRef(true)
  const requests = useRef(0)
  const followed = useRef('')
  const skipRootLoad = useRef('')
  const histories = useRef(new Map<string, DirectoryHistory>())
  const scopeNow = useRef(treeScope)
  const currentHistory = histories.current.get(treeScope) ?? emptyHistory()

  useLayoutEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  useLayoutEffect(() => {
    scopeNow.current = treeScope
    return () => { requests.current++ }
  }, [treeScope, active, tab, phase, reloadVersion, directorySync, linkedPath])

  const load = useCallback(async (next?: string, options: Navigate = {}) => {
    if (!mounted.current || scopeNow.current !== treeScope || !active || tab === 'tasks' || !native ||
        (side === 'local' ? !options.root && !folder : !connected)) return false
    const request = ++requests.current
    const targetRoot = options.root ?? folder
    const targetScope = side === 'local' ? localScope(targetRoot?.token) : treeScope
    const isCurrent = () => mounted.current && request === requests.current
    setBusy(true)
    onError('')
    try {
      const result = await gateway.list(location(side, next, targetRoot))
      if (!isCurrent()) return false
      histories.current.set(targetScope, visitDirectory(
        histories.current.get(targetScope) ?? emptyHistory(), result.path, options.kind, options.index,
      ))
      while (histories.current.size > 4) histories.current.delete(histories.current.keys().next().value!)
      setLoaded({ scope: targetScope, path: result.path, entries: result.entries })
      setPathDraft(result.path)
      if (side === 'local') {
        const newRoot = options.root && options.root.token !== folder?.token ? options.root : undefined
        if (newRoot) skipRootLoad.current = targetScope
        onLocalDirectory(result.path, newRoot)
      } else {
        onPath(result.path)
      }
      if (next) onMessage(tr("已进入 {p0}", result.path))
      return true
    } catch (cause) {
      if (isCurrent()) {
        onError(cause instanceof Error ? cause.message : tr("目录读取失败，请刷新重试。"))
        setPathDraft(previous => previous === (next ?? currentPath)
          ? (loaded?.scope === treeScope ? loaded.path : currentPath) : previous)
      }
      return false
    } finally {
      if (isCurrent()) setBusy(false)
    }
  }, [treeScope, active, tab, native, side, folder, connected, localScope, gateway, location, onLocalDirectory, onPath, onError, onMessage, currentPath, loaded])

  const loadRef = useRef(load)
  useLayoutEffect(() => { loadRef.current = load }, [load])
  useEffect(() => {
    if (skipRootLoad.current === treeScope) {
      skipRootLoad.current = ''
      return
    }
    const key = treeScope + ':' + linkedPath
    const target = active && linkedPath && followed.current !== key ? linkedPath : undefined
    followed.current = active && linkedPath ? key : ''
    if (!active || tab === 'tasks' || !native || (side === 'local' ? !folder : !connected)) {
      setBusy(false)
      return
    }
    void loadRef.current(target, target ? { kind: 'follow' } : undefined)
  }, [treeScope, sessionId, attempt, phase, folder?.token, reloadVersion, active, tab, directorySync, linkedPath, native, side, connected])

  const browseHistory = (step: number) => {
    const target = directoryStep(currentHistory, step)
    if (target && !busy) void load(target.path, { kind: 'history', index: target.index })
  }

  return { treeScope, scopeNow, mounted, currentHistory, browseHistory, load, loadRef, loaded, busy, pathDraft, setPathDraft }
}
