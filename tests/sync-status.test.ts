import { expect, test } from 'bun:test'
import { createCloudStatus, followCloudStatus } from '../frontend/src/shared/bridge/cloud'
import type { SyncStatusEvent } from '../frontend/src/shared/generated/mygo'

function snapshot(): SyncStatusEvent {
  return {
    applied: 0, error: '',
    settings: { version: 1, target: { provider: 'webdav', endpoint: 'https://fixture.invalid', username: '', bucket: '', region: '', prefix: 'Yaare' },
      paused: false, hasCredential: true, hasPassword: true, lastSync: 0, lastBackup: 0, issue: '', canSync: true },
    task: { configured: true, paused: false, kind: '', stage: '', issue: '', lastSync: 0, lastBackup: 0 },
  }
}

function engine(reload: () => Promise<void> = async () => {}) {
  const store = createCloudStatus()
  const connections: Array<{ receive: (event: SyncStatusEvent) => void; disconnect: (cause: unknown) => void; closed: boolean }> = []
  const reconnected = Promise.withResolvers<void>()
  const stop = followCloudStatus(store, receive => {
    const { promise, reject } = Promise.withResolvers<void>()
    const connection = { receive, disconnect: reject, closed: false }
    connections.push(connection)
    if (connections.length === 2) reconnected.resolve()
    return { done: promise, close: () => { connection.closed = true } }
  }, reload, 1)
  return { store, connections, reconnected: reconnected.promise, stop }
}

test('one connection serves both status panels; 150 identical updates invalidate neither', () => {
  const { store, connections, stop } = engine()
  let taskUpdates = 0, settingsUpdates = 0
  const removeTask = store.subscribeTask(() => { taskUpdates++ })
  const removeSettings = store.subscribeSettings(() => { settingsUpdates++ })
  try {
    const { receive } = connections[0]!
    receive(snapshot())
    expect([taskUpdates, settingsUpdates]).toEqual([1, 1])
    const task = store.readTask(), settings = store.readSettings()
    for (let count = 0; count < 150; count++) receive(snapshot())
    expect(connections).toHaveLength(1)
    expect([taskUpdates, settingsUpdates]).toEqual([1, 1])
    expect(store.readTask()).toBe(task)
    expect(store.readSettings()).toBe(settings)
  } finally { removeTask(); removeSettings(); stop() }
})

test('task progress and settings update independently; version-only changes still refresh the resume token', () => {
  const store = createCloudStatus()
  store.accept(snapshot())
  const initial = store.readSettings()
  const progress = snapshot()
  progress.task = { ...progress.task, kind: 'sync', stage: 'upload' }
  progress.settings.version = 2
  store.accept(progress)
  expect(store.readTask()?.stage).toBe('upload')
  expect(store.readSettings()).toBe(initial)
  expect(store.getVersion()).toBe(2)

  const stage = store.readTask()
  progress.settings.target = { ...progress.settings.target, prefix: 'new-prefix' }
  store.accept(progress)
  expect(store.readSettings().value?.target.prefix).toBe('new-prefix')
  expect(store.readTask()).toBe(stage)
})

test('only committed imports reload the workspace; commits received during reload coalesce', async () => {
  const firstReload = Promise.withResolvers<void>()
  let reloads = 0
  const { connections, stop } = engine(async () => { reloads++; if (reloads === 1) await firstReload.promise })
  try {
    const { receive } = connections[0]!
    const event = snapshot()
    receive(event)
    receive({ ...event, settings: { ...event.settings, lastBackup: 2000 } })
    expect(reloads).toBe(0)
    receive({ ...event, applied: 1 })
    receive({ ...event, applied: 1 })
    receive({ ...event, applied: 2 })
    receive({ ...event, applied: 3 })
    expect(reloads).toBe(1)
    firstReload.resolve()
    await firstReload.promise
    await Promise.resolve()
    expect(reloads).toBe(2)
    receive({ ...event, applied: 3 })
    receive({ ...event, applied: 2 })
    expect(reloads).toBe(2)
  } finally { stop() }
})

test('a dropped watch reconnects, catches up once, and ignores the old channel', async () => {
  let reloads = 0
  const { store, connections, reconnected, stop } = engine(async () => { reloads++ })
  try {
    const old = connections[0]!
    old.receive(snapshot())
    old.disconnect(Error('connection closed'))
    await reconnected
    expect(old.closed).toBe(true)
    expect(store.readSettings().error).toBe('connection closed')
    const fresh = connections[1]!
    const event = { ...snapshot(), applied: 1 }
    fresh.receive(event)
    fresh.receive(event)
    old.receive({ ...event, applied: 2 })
    expect(store.readSettings().error).toBe('')
    expect(reloads).toBe(1)
    stop()
    expect(fresh.closed).toBe(true)
    fresh.receive({ ...event, applied: 2 })
    expect(reloads).toBe(1)
  } finally { stop() }
})

test('repeated status errors keep the last snapshot and notify settings only once', () => {
  const store = createCloudStatus()
  const valid = snapshot()
  store.accept(valid)
  let taskUpdates = 0, settingsUpdates = 0
  store.subscribeTask(() => { taskUpdates++ })
  store.subscribeSettings(() => { settingsUpdates++ })
  const unavailable = { ...snapshot(), error: 'settings unavailable' }
  store.accept(unavailable)
  store.accept(unavailable)
  expect(store.readSettings()).toMatchObject({ error: 'settings unavailable', value: { hasPassword: true } })
  expect([taskUpdates, settingsUpdates]).toEqual([0, 1])
  store.accept(valid)
  expect(store.readSettings().error).toBe('')
  expect([taskUpdates, settingsUpdates]).toEqual([0, 2])
})

test('a new window seeds its baseline with the first existing applied revision', () => {
  let reloads = 0
  const { connections, stop } = engine(async () => { reloads++ })
  try {
    const receive = connections[0]!.receive
    receive({ ...snapshot(), applied: 9 })
    expect(reloads).toBe(0)
    receive({ ...snapshot(), applied: 10 })
    expect(reloads).toBe(1)
  } finally { stop() }
})
