import { expect, test } from 'bun:test'
import { canReconnect, createReconnectQueue, reconnectAttempt, reconnectDelay, type ReconnectWait } from '../frontend/src/features/connections/sshReconnect'
import { readPreferences, savePreferences } from '../frontend/src/features/workspace/preferences'

test('only transient SSH failures qualify, with retries capped at about one minute apart', () => {
  for (const error of ['CONNECTION_REFUSED', 'CONNECTION_TIMEOUT', 'NETWORK_UNREACHABLE', 'PROXY_TARGET_UNREACHABLE', 'CONNECTION_LOST']) expect(canReconnect(error)).toBe(true)
  for (const error of ['REMOTE_CLOSED', 'CANCELLED', 'HOST_KEY_CHANGED', 'HOST_KEY_REJECTED', 'AUTHENTICATION_FAILED', 'KEY_UNAVAILABLE', 'DNS_NOT_FOUND', 'INVALID_CONFIG', 'PROXY_AUTH_FAILED']) expect(canReconnect(error)).toBe(false)
  expect([0, 1, 2, 3, 90].map(retries => reconnectDelay(retries, 0))).toEqual([1000, 2000, 4000, 8000, 60000])
  expect(reconnectDelay(90, 0.5)).toBe(55500)
})

test('queued panes wait offline, wake once and can be stopped or retried independently', () => {
  let reason: 'offline' | undefined
  let serial = 0
  const pending = new Map<number, { run: () => void; delay: number }>()
  const states = new Map<string, ReconnectWait | undefined>()
  const called: string[] = []
  const queue = createReconnectQueue({
    blocked: () => reason,
    changed: (id, state) => states.set(id, state),
    setTimer(run, delay) { const id = ++serial; pending.set(id, { run: () => { pending.delete(id); run() }, delay }); return id as unknown as ReturnType<typeof setTimeout> },
    clearTimer: timer => { pending.delete(timer as unknown as number) },
  })
  queue.schedule('a', 0, () => called.push('a'))
  queue.schedule('b', 6, () => called.push('b'))
  expect(pending.size).toBe(2)
  reason = 'offline'
  queue.refresh()
  queue.tryNow('a')
  expect(pending.size).toBe(0)
  expect(states.get('a')).toBe('offline')
  expect(states.get('b')).toBe('offline')
  reason = undefined
  queue.refresh()
  queue.stop('b')
  queue.tryNow('a')
  expect(pending.size).toBe(1)
  const next = [...pending.values()][0]!
  expect(next.delay).toBe(0)
  next.run()
  expect(called).toEqual(['a'])
  expect(states.get('a')).toBeUndefined()
  queue.schedule('a', 7, () => called.push('again'))
  const stale = [...pending.values()][0]!
  queue.clear()
  stale.run()
  expect(called).toEqual(['a'])
})

test('the preview uses automatic reconnect by default and saves an explicit opt-out', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  let data: string | null = null
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: () => data, setItem: (_key: string, value: string) => { data = value },
  } })
  try {
    expect(readPreferences().autoReconnect).toBe(true)
    expect(savePreferences({ ...readPreferences(), autoReconnect: false })).toBe(true)
    expect(readPreferences().autoReconnect).toBe(false)
  } finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})

test('short SSH sessions share an outage budget and stop after five automatic retries', () => {
  const delays: number[] = []
  let used = 0
  for (;;) {
    const attempt = reconnectAttempt(used, 200)
    if (attempt === undefined) break
    delays.push(reconnectDelay(attempt, 0))
    used = attempt + 1
  }
  expect(delays).toEqual([1000, 2000, 4000, 8000, 16000])
  expect(reconnectAttempt(used, undefined)).toBeUndefined()
  expect(reconnectAttempt(used, 29_999)).toBeUndefined()
  expect(reconnectAttempt(used, 30_000)).toBe(0)
})
