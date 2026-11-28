import { expect, test } from 'bun:test'
import { canReconnect, createReconnectQueue, reconnectAttempt, reconnectDelay, type ReconnectWait } from '../frontend/src/features/connections/sshReconnect'
import { readPreferences, savePreferences } from '../frontend/src/features/workspace/preferences'
import { resolveSessionConnectionPrompt } from '../frontend/src/features/workspace/sessionConnectionPrompt'
import { tr } from '../frontend/src/shared/i18n/messages'

test('connection prompts follow the live attempt and offer the action for its current phase', () => {
  const connect = () => {}, disconnect = () => {}
  const idle = resolveSessionConnectionPrompt(undefined, undefined, connect, disconnect)!
  expect(idle.kind).toBe('idle')
  expect(idle.actions.map(action => action.kind)).toEqual(['connect'])
  expect(idle.actions[0]!.run).toBe(connect)
  for (const [phase, attempt, automatic, kind, action, busy] of [
    ['connecting', 1, false, 'connecting', 'cancel', true],
    ['hostkey', 2, false, 'reconnecting', 'cancel', true],
    ['authentication', 2, true, 'retrying', 'stop', true],
    ['disconnected', 2, true, 'disconnected', 'reconnect', false],
    ['failed', 2, true, 'failed', 'reconnect', false],
  ] as const) {
    const prompt = resolveSessionConnectionPrompt('connected', { phase, attempt, automatic, message: 'server message' }, connect, disconnect)!
    expect(prompt.kind).toBe(kind)
    expect(prompt.busy).toBe(busy)
    expect(prompt.actions.map(item => item.kind)).toEqual([action])
    expect(prompt.actions[0]!.run).toBe(busy ? disconnect : connect)
    expect(prompt.message).toBe(phase === 'hostkey' ? tr('等待指纹确认') : phase === 'authentication' ? tr('等待认证') : 'server message')
  }
  expect(resolveSessionConnectionPrompt('failed', { phase: 'connected', attempt: 2 })).toBeUndefined()
  expect(resolveSessionConnectionPrompt('connected', undefined, connect)?.kind).toBe('disconnected')
})

test('waiting and offline retries expose the available callbacks and disable retry while offline', () => {
  const live = { phase: 'failed', attempt: 1, message: 'connection refused' }
  const connect = () => {}, disconnect = () => {}
  for (const retry of ['waiting', 'offline'] as const) {
    const prompt = resolveSessionConnectionPrompt('failed', { ...live, retry }, connect, disconnect)!
    expect(prompt.kind).toBe(retry)
    expect(prompt.busy).toBe(retry === 'waiting')
    expect(prompt.message).toBe(retry === 'offline' ? tr('当前离线，网络恢复后会自动重试。') : live.message)
    expect(prompt.actions.map(action => action.kind)).toEqual(['stop', 'retry'])
    expect(prompt.actions.map(action => action.run)).toEqual([disconnect, connect])
    expect(prompt.actions[1]!.disabled).toBe(retry === 'offline')
  }
  expect(resolveSessionConnectionPrompt('failed', { ...live, retry: 'waiting' }, connect)?.actions.map(action => action.kind)).toEqual(['retry'])
  expect(resolveSessionConnectionPrompt(undefined)?.actions).toEqual([])
})

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
