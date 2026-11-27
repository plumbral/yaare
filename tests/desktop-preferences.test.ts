import { expect, test } from 'bun:test'
import { createDesktopPreferenceStore, type DesktopPreferenceSnapshot } from '../frontend/src/features/workspace/desktopPreferences'
import { defaultPreferences } from '../frontend/src/features/workspace/preferences'

function snapshot(): DesktopPreferenceSnapshot {
  return {
    version: 1, revision: 0, portable: {},
    device: { uiFont: '', fontFamily: '', fallbackFont: '', autoReconnect: true, detectServerSystem: false, directorySync: false, transferConcurrency: 2, shortcuts: {} },
  }
}

function mockStore(initial = snapshot()) {
  let stored = initial
  type Bridge = Parameters<typeof createDesktopPreferenceStore>[0]
  const writes: Parameters<Bridge['save']>[0][] = []
  const store = createDesktopPreferenceStore({
    async load() { return stored },
    async save(request) {
      writes.push(request)
      if (request.revision !== stored.revision) throw Error('PREFERENCES_CONFLICT')
      stored = {
        ...stored, revision: stored.revision + 1,
        portable: request.portable ?? stored.portable,
        device: request.device ?? stored.device,
      }
      return stored
    },
  })
  return { store, writes, commitFromSync(portable: DesktopPreferenceSnapshot['portable']) { stored = { ...stored, portable, revision: stored.revision + 1 } } }
}

test('first load reads defaults without writing, and edits keep device settings separate', async () => {
  const { store, writes } = mockStore()
  expect(await store.refresh()).toEqual(defaultPreferences)
  expect(writes).toHaveLength(0)
  store.update(prefs => ({ ...prefs, mode: 'dark', uiFont: 'local:Segoe UI', directorySync: true, screenReaderMode: true }))
  await store.refresh()
  expect(writes).toHaveLength(1)
  expect(writes[0].portable).toEqual({ mode: 'dark' })
  expect(writes[0].device).toMatchObject({ uiFont: 'local:Segoe UI', directorySync: true, screenReaderMode: true })
  expect(await store.saveShortcuts({ save: null })).toBe(true)
  await store.refresh()
  expect(store.read().shortcuts).toEqual({ save: null })
  expect(writes).toHaveLength(2)
})

test('unavailable storage leaves defaults visible and does not save fallback settings', async () => {
  const store = createDesktopPreferenceStore({
    async load() { throw Error('storage unavailable') },
    async save() { throw Error('unexpected write') },
  })
  expect(await store.refresh()).toEqual(defaultPreferences)
  expect(store.read().saved).toBe(false)
})

test('desktop preferences reread a pull and rebase queued user edits without writing device data to the portable view', async () => {
  const { store, writes, commitFromSync } = mockStore()
  await store.refresh()
  commitFromSync({ interfacePalette: 'blue', language: 'ja' })
  store.update(previous => ({ ...previous, fontSize: 19 }))
  store.update(previous => ({ ...previous, transferConcurrency: 5 }))
  expect(store.read().preferences).toMatchObject({ fontSize: 19, transferConcurrency: 5 })
  await store.refresh()
  expect(store.read().preferences).toMatchObject({ fontSize: 19, transferConcurrency: 5, interfacePalette: 'blue', language: 'ja' })
  expect(writes).toHaveLength(2)
  expect(writes[1].portable).toEqual({ fontSize: 19, interfacePalette: 'blue', language: 'ja' })
  expect(writes[1].device?.transferConcurrency).toBe(5)
  expect(await store.saveShortcuts({ hosts: null })).toBe(true)
  expect(writes[2].portable).toBeUndefined()
  commitFromSync({})
  await store.refresh()
  expect(store.read().preferences).toMatchObject({ fontSize: 14, transferConcurrency: 5, interfacePalette: 'neutral' })
  expect(store.read().shortcuts).toEqual({ hosts: null })
})

test('unrelated device edits preserve shortcut references even when IPC returns new objects', async () => {
  const base = snapshot()
  base.device.shortcuts = { hosts: null }
  const { store } = mockStore(base)
  await store.refresh()
  const shortcuts = store.read().shortcuts
  store.update(previous => ({ ...previous, transferConcurrency: 5 }))
  await store.refresh()
  expect(store.read().shortcuts).toBe(shortcuts)
  await store.saveShortcuts({ hosts: 'Ctrl+H' })
  expect(store.read().shortcuts).not.toBe(shortcuts)
})
