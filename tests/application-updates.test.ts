import { expect, test } from 'bun:test'
import { createUpdateController, firstUpdateCheckDelay, updateCheckInterval, type UpdateStatus } from '../frontend/src/features/workspace/applicationUpdates'

const status = (state = 'idle', currentVersion = '0.1.0'): UpdateStatus => ({ state, currentVersion, latestVersion: '', notes: '', checkedAt: 0 })
function storage() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
}

test('unconfigured and preview builds never pretend to check a feed', async () => {
  for (const state of ['unconfigured', 'preview']) {
    let calls = 0
    const controller = createUpdateController({ status: async () => status(state), check: async () => { calls++; return status('current') } }, () => storage())
    await controller.automaticCheck(); await controller.check()
    expect(calls).toBe(0)
    expect(controller.getSnapshot().status.state).toBe(state)
    expect(controller.getSnapshot().busy).toBe(false)
  }
})

test('half-hour checks survive restart and honor the saved opt-out', async () => {
  let time = 100_000_000, calls = 0
  const local = storage(), gateway = { status: async () => status(), check: async () => { calls++; return { ...status('current'), checkedAt: time } } }
  let controller = createUpdateController(gateway, () => local, () => time)
  expect(firstUpdateCheckDelay).toBe(10_000)
  expect(updateCheckInterval).toBe(1_800_000)
  expect(await controller.automaticCheck()).toBe(updateCheckInterval)
  await controller.automaticCheck()
  expect(calls).toBe(1)
  controller = createUpdateController(gateway, () => local, () => time)
  await controller.automaticCheck()
  expect(calls).toBe(1)
  time += updateCheckInterval - 1
  expect(await controller.automaticCheck()).toBe(1)
  expect(calls).toBe(1)
  time++
  expect(await controller.automaticCheck()).toBe(updateCheckInterval)
  expect(calls).toBe(2)
  controller.setAutomatic(false)
  controller = createUpdateController(gateway, () => local, () => time)
  time += updateCheckInterval
  await controller.automaticCheck()
  expect(calls).toBe(2)
  expect(controller.getSnapshot().automatic).toBe(false)
  controller.setAutomatic(true)
  await controller.automaticCheck()
  expect(calls).toBe(3)
})

test('startup checks once even if a previous launch checked recently', async () => {
  const local = storage()
  let calls = 0
  const gateway = { status: async () => status(), check: async () => { calls++; return status('current') } }
  await createUpdateController(gateway, () => local).automaticCheck(true)
  const restarted = createUpdateController(gateway, () => local)
  await restarted.automaticCheck(true)
  await restarted.automaticCheck()
  expect(calls).toBe(2)
  restarted.setAutomatic(false)
  await createUpdateController(gateway, () => local).automaticCheck(true)
  expect(calls).toBe(2)
})

test('already-open windows notice a check or opt-out saved by another window', async () => {
  const local = storage()
  let calls = 0
  const gateway = { status: async () => status(), check: async () => { calls++; return status('current') } }
  const a = createUpdateController(gateway, () => local), b = createUpdateController(gateway, () => local)
  await a.initialize(); await b.initialize()
  await a.automaticCheck(); await b.automaticCheck()
  expect(calls).toBe(1)
  a.setAutomatic(false)
  await b.automaticCheck()
  expect(b.getSnapshot().automatic).toBe(false)
  await b.check()
  expect(calls).toBe(2)
})

test('repeated clicks share one check and a failed check is never reported as up to date', async () => {
  let finish!: (value: UpdateStatus) => void, calls = 0
  const controller = createUpdateController({ status: async () => status(), check: () => { calls++; return new Promise(resolve => { finish = resolve }) } }, () => storage())
  await controller.initialize()
  const a = controller.check(), b = controller.check()
  expect(a).toBe(b)
  await Promise.resolve()
  expect(calls).toBe(1)
  expect(controller.getSnapshot().busy).toBe(true)
  finish(status('failed')); await a
  expect(controller.getSnapshot().status.state).toBe('failed')
  expect(controller.getSnapshot().busy).toBe(false)
})

test('a recent known update is restored for the same build and discarded after upgrading or expiry', async () => {
  let time = 100_000_000
  const local = storage(), available = { ...status('available'), latestVersion: '0.2.0', notes: '修复 · fixes', checkedAt: time }
  const gateway = { status: async () => status(), check: async () => available }
  const a = createUpdateController(gateway, () => local, () => time)
  await a.check()
  const b = createUpdateController(gateway, () => local, () => time)
  await b.initialize()
  expect(b.getSnapshot().status).toEqual(available)
  const c = createUpdateController({ status: async () => status('idle', '0.2.0'), check: async () => status('current', '0.2.0') }, () => local)
  await c.initialize()
  expect(c.getSnapshot().status.state).toBe('idle')
  expect(c.getSnapshot().status.latestVersion).toBe('')
  time += updateCheckInterval
  const expired = createUpdateController(gateway, () => local, () => time)
  await expired.initialize()
  expect(expired.getSnapshot().status.state).toBe('idle')
})

test('a failed refresh clears a previously confirmed update and retries after half an hour', async () => {
  let time = 100_000_000, calls = 0
  const local = storage()
  const controller = createUpdateController({ status: async () => status(), check: async () => {
    calls++
    if (calls === 2) throw Error('offline')
    return { ...status('available'), latestVersion: '0.2.0', checkedAt: time }
  } }, () => local, () => time)
  await controller.automaticCheck()
  time += updateCheckInterval
  expect(await controller.automaticCheck()).toBe(updateCheckInterval)
  expect(controller.getSnapshot().status.state).toBe('failed')
  time += updateCheckInterval
  await controller.automaticCheck()
  expect(controller.getSnapshot().status.state).toBe('available')
})

test('blocked local storage and interrupted requests leave the app usable and retryable', async () => {
  let calls = 0
  const controller = createUpdateController({ status: async () => status(), check: async () => { if (++calls === 1) throw Error('offline'); return status('current') } }, () => { throw Error('blocked storage') })
  await controller.check()
  expect(controller.getSnapshot().status.state).toBe('failed')
  await controller.check()
  expect(controller.getSnapshot().status.state).toBe('current')
  controller.setAutomatic(false)
  expect(controller.getSnapshot().automatic).toBe(false)
})
