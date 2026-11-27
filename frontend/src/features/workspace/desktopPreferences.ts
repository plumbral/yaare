import { PreferencesService, type Device as DevicePreferences, type Portable as PortablePreferences, type PreferencesSnapshot, type SaveRequest } from '../../shared/generated/mygo'
import { defaultPreferences, normalizePreferences, type Preferences } from './preferences'

export type DesktopPreferenceSnapshot = PreferencesSnapshot
interface PreferenceBridge {
  load(): Promise<DesktopPreferenceSnapshot>
  save(request: SaveRequest): Promise<DesktopPreferenceSnapshot>
}
type PreferenceView = { preferences: Preferences; shortcuts: DevicePreferences['shortcuts']; saved: boolean }
type Change = { portable: PortablePreferences; device: Partial<DevicePreferences> }
type Update = Preferences | ((previous: Preferences) => Preferences)

function splitPreferences(value: Preferences, shortcuts: DevicePreferences['shortcuts']) {
  const { uiFont, fontFamily, fallbackFont, autoReconnect, detectServerSystem, directorySync, transferConcurrency, screenReaderMode, ...visual } = value
  const portable: PortablePreferences = { ...visual }
  const device: DevicePreferences = {
    uiFont: uiFont === defaultPreferences.uiFont ? '' : uiFont,
    fontFamily: fontFamily === defaultPreferences.fontFamily ? '' : fontFamily,
    fallbackFont: fallbackFont === defaultPreferences.fallbackFont ? '' : fallbackFont,
    autoReconnect, detectServerSystem, directorySync, transferConcurrency, screenReaderMode, shortcuts,
  }
  return { portable, device }
}

function joinPreferences(portable: PortablePreferences, device: DevicePreferences): Preferences {
  const { uiFont, fontFamily, fallbackFont, shortcuts: _shortcuts, ...behavior } = device
  return normalizePreferences({
    ...portable, ...behavior,
    uiFont: uiFont || defaultPreferences.uiFont,
    fontFamily: fontFamily || defaultPreferences.fontFamily,
    fallbackFont: fallbackFont || defaultPreferences.fallbackFont,
  })
}

function diff<Value extends object>(previous: Value, next: Value): Partial<Value> {
  return Object.fromEntries((Object.keys(next) as (keyof Value)[])
    .filter(key => previous[key] !== next[key]).map(key => [key, next[key]])) as Partial<Value>
}
const noChange = (): Change => ({ portable: {}, device: {} })
const merge = (earlier: Change, later: Change): Change => ({ portable: { ...earlier.portable, ...later.portable }, device: { ...earlier.device, ...later.device } })
const changed = (value: object) => Object.keys(value).length > 0
const isConflict = (error: unknown) => String(error).includes('PREFERENCES_CONFLICT')
const preferenceVersion = 1

export function createDesktopPreferenceStore(bridge: PreferenceBridge) {
  const defaults = splitPreferences(defaultPreferences, {})
  let current: DesktopPreferenceSnapshot | undefined
  let view: PreferenceView = { preferences: defaultPreferences, shortcuts: {}, saved: false }
  let failed = noChange()
  const pending: Change[] = []
  const listeners = new Set<() => void>()
  const portableListeners = new Set<(previous: number, current: number) => void>()
  let timer: ReturnType<typeof setTimeout> | undefined
  let queue = Promise.resolve()

  function serialize<Result>(operation: () => Promise<Result>): Promise<Result> {
    const next = queue.then(operation)
    queue = next.then(() => undefined, () => undefined)
    return next
  }

  let scheduled = noChange()

  function publish() {
    const { portable, device } = current ?? defaults
    const edits = merge(pending.reduce(merge, failed), scheduled)
    const shortcuts = device.shortcuts ?? {}
    const previous = view.shortcuts
    const sameShortcuts = Object.keys(shortcuts).length === Object.keys(previous).length &&
      Object.keys(shortcuts).every(key => Object.hasOwn(previous, key) && previous[key] === shortcuts[key])
    view = {
      preferences: joinPreferences({ ...portable, ...edits.portable }, { ...device, ...edits.device }),
      shortcuts: sameShortcuts ? previous : shortcuts,
      saved: !!current && !changed(failed.portable) && !changed(failed.device),
    }
    listeners.forEach(listener => listener())
  }

  async function loadReady(): Promise<DesktopPreferenceSnapshot> {
    const stored = await bridge.load()
    if (stored.version !== preferenceVersion) throw new Error('Unsupported desktop preference version')
    return stored
  }

  async function write(change: Change) {
    for (let attempt = 0; ; attempt++) {
      const stored = current ??= await loadReady()
      try {
        current = await bridge.save({
          version: preferenceVersion, revision: stored.revision,
          ...(changed(change.portable) ? { portable: { ...stored.portable, ...change.portable } } : {}),
          ...(changed(change.device) ? { device: { ...stored.device, ...change.device } } : {}),
        })
        const revision = current.workspaceRevision
        if (revision) portableListeners.forEach(listener => listener(revision.before, revision.after))
        return
      } catch (error) {
        if (attempt || !isConflict(error)) throw error
        current = await loadReady()
      }
    }
  }

  function flush() {
    clearTimeout(timer)
    timer = undefined
    const change = scheduled
    if (!changed(change.portable) && !changed(change.device)) return
    scheduled = noChange()
    pending.push(change)
    void serialize(async () => {
      const edits = merge(failed, change)
      try { await write(edits); failed = noChange() }
      catch { failed = edits }
      finally { pending.shift(); publish() }
    })
  }

  function refresh() {
    flush()
    return serialize(async () => {
      try { current = await loadReady() }
      catch {}
      publish()
      return view.preferences
    })
  }

  return {
    refresh, flush,
    read: () => view,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
    subscribePortableWrite(listener: (previous: number, current: number) => void) { portableListeners.add(listener); return () => { portableListeners.delete(listener) } },
    update(update: Update) {
      const previous = view.preferences
      const next = normalizePreferences(typeof update === 'function' ? update(previous) : update)
      const oldValues = splitPreferences(previous, view.shortcuts)
      const newValues = splitPreferences(next, view.shortcuts)
      const change = { portable: diff(oldValues.portable, newValues.portable), device: diff(oldValues.device, newValues.device) }
      if (!changed(change.portable) && !changed(change.device)) return
      scheduled = merge(scheduled, change)
      clearTimeout(timer)
      timer = setTimeout(flush, 300)
      publish()
    },
    saveShortcuts(shortcuts: DevicePreferences['shortcuts']) {
      flush()
      return serialize(async () => {
        try { await write({ portable: {}, device: { shortcuts } }); return true }
        catch { return false }
        finally { publish() }
      })
    },
  }
}

export const desktopPreferences = createDesktopPreferenceStore(PreferencesService)
