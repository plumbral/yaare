import { createPaneSSHSource, type LiveSSH } from '../frontend/src/features/connections/useSSHSessions'
import { afterEach, expect, test } from 'bun:test'
import { createSSHTransport, type SSHEvent } from '../frontend/src/shared/bridge/ssh'
import { decodeBase64, decodeBase64Fallback } from '../frontend/src/shared/bridge/base64'
import { createSessionOutputActivity } from '../frontend/src/features/connections/sessionOutputActivity'

class TestChannel {
  queue: SSHEvent[] = []
  taken = 0
  closed = false
  wake: (() => void) | undefined
  onclose: (() => void) | null = null
  push(event: SSHEvent) { this.queue.push(event); this.wake?.(); this.wake = undefined }
  close() { if (this.closed) return; this.closed = true; this.queue = []; this.wake?.(); this.onclose?.() }
  async *[Symbol.asyncIterator]() {
    while (!this.closed) {
      if (!this.queue.length) await new Promise<void>(resolve => { this.wake = resolve })
      if (this.closed) return
      const event = this.queue.shift()
      if (event) { this.taken++; yield event }
    }
  }
}

function fixture(inlineKey = '', onOutput?: () => void, options?: { attempt?: number; resize?: () => Promise<boolean> }) {
  let channel!: TestChannel
  const calls: { name: string; args: unknown[] }[] = []
  const runtime = {
    channel() { channel = new TestChannel(); return channel },
    callWith(name: string, args: unknown[]) { return this.call(name, ...args) },
    call(name: string, ...args: unknown[]) {
      calls.push({ name, args: name.endsWith('.Connect') ? [structuredClone(args[0])] : structuredClone(args) })
      if (name.endsWith('.Connect')) return new Promise<void>(resolve => { channel.onclose = resolve })
      if (name.endsWith('.Resize') && options?.resize) return options.resize()
      return Promise.resolve(true)
    },
  }
  Object.assign(globalThis, { mygo: runtime })
  const events: SSHEvent[] = []
  const request = { sessionId: 'test-session', attempt: options?.attempt ?? 1, address: '127.0.0.1', port: 22, username: 'test', authentication: inlineKey ? 'key' : 'password', keyId: '', password: inlineKey ? '' : 'test-secret', privateKey: inlineKey, passphrase: '', timeoutSeconds: 15 }
  const transport = createSSHTransport(request, event => events.push(event), onOutput)
  return { transport, calls, events, request, channel }
}
afterEach(() => { delete (globalThis as { mygo?: unknown }).mygo })
const output = (data: Uint8Array): SSHEvent => ({ type: 'output', revision: 0, data: btoa(String.fromCharCode(...data)) })
const tick = () => new Promise(resolve => setTimeout(resolve, 0))
const resizeCalls = (testCase: ReturnType<typeof fixture>) => testCase.calls.filter(call => call.name.endsWith('.Resize')).map(call => call.args)

test('SSH applies the latest pre-connect size immediately, then coalesces to the last distinct size', async () => {
  const testCase = fixture()
  const reader = testCase.transport.read(new AbortController().signal)
  try {
    testCase.transport.resize(90, 30)
    testCase.transport.resize(100, 32)
    expect(resizeCalls(testCase)).toEqual([])
    testCase.channel.push({ type: 'connected', revision: 1 })
    testCase.channel.push({ type: 'connected', revision: 1 })
    testCase.channel.push(output(new Uint8Array([65])))
    await reader.next()
    expect(resizeCalls(testCase)).toEqual([['test-session', 1, 100, 32]])

    testCase.transport.resize(101, 33)
    testCase.transport.resize(120, 36)
    testCase.transport.resize(121, 37)
    expect(resizeCalls(testCase)).toHaveLength(1)
    await Bun.sleep(145)
    expect(resizeCalls(testCase)).toEqual([['test-session', 1, 100, 32], ['test-session', 1, 121, 37]])

    testCase.transport.resize(130, 38)
    testCase.transport.resize(121, 37)
    await Bun.sleep(145)
    expect(resizeCalls(testCase)).toHaveLength(2)
  } finally { testCase.transport.close(); await reader.return?.() }
})

test('a slow SSH resize never overlaps the next and the final measured size is still delivered', async () => {
  let finishResize!: (accepted: boolean) => void
  const testCase = fixture('', undefined, { resize: () => new Promise(resolve => { finishResize = resolve }) })
  const reader = testCase.transport.read(new AbortController().signal)
  try {
    testCase.channel.push({ type: 'connected', revision: 1 })
    testCase.channel.push(output(new Uint8Array([65])))
    await reader.next()
    testCase.transport.resize(110, 36)
    await Bun.sleep(145)
    testCase.transport.resize(114, 37)
    expect(resizeCalls(testCase)).toEqual([['test-session', 1, 80, 24]])
    finishResize(true)
    await Bun.sleep(145)
    expect(resizeCalls(testCase)).toEqual([['test-session', 1, 80, 24], ['test-session', 1, 114, 37]])
  } finally { testCase.transport.close(); finishResize?.(true); await reader.return?.() }
})

test.each(['failed', 'disconnected', 'close', 'abort', 'reader return'] as const)('SSH cancels an old attempt’s pending resize on %s', async reason => {
  const controller = new AbortController()
  const prior = fixture()
  const priorReader = prior.transport.read(controller.signal)
  prior.channel.push({ type: 'connected', revision: 1 })
  prior.channel.push(output(new Uint8Array([65])))
  await priorReader.next()
  prior.transport.resize(96, 32)
  if (reason === 'failed' || reason === 'disconnected') {
    prior.channel.push({ type: reason, revision: 2 })
    prior.channel.push(output(new Uint8Array([66])))
    await priorReader.next()
  } else if (reason === 'close') prior.transport.close()
  else if (reason === 'abort') controller.abort()
  else await priorReader.return?.()

  const next = fixture('', undefined, { attempt: 2 })
  const nextReader = next.transport.read(new AbortController().signal)
  try {
    next.transport.resize(130, 39)
    next.channel.push({ type: 'connected', revision: 1 })
    next.channel.push(output(new Uint8Array([67])))
    await nextReader.next()
    expect(resizeCalls(next)).toEqual([['test-session', 2, 130, 39]])
    await Bun.sleep(145)
    expect(resizeCalls(prior)).toEqual([['test-session', 1, 80, 24]])
    expect(resizeCalls(next)).toEqual([['test-session', 2, 130, 39]])
  } finally {
    prior.transport.close(); await priorReader.return?.()
    next.transport.close(); await nextReader.return?.()
  }
})

test('terminal base64 decoding preserves all byte values, packet boundaries and legacy WebView fallback', () => {
  for (const size of [0, 1, 2, 3, 31, 32768]) {
    const expected = Uint8Array.from({ length: size }, (_, index) => index * 97 % 256)
    const encoded = btoa(String.fromCharCode(...expected))
    expect(decodeBase64(encoded)).toEqual(expected)
    expect(decodeBase64Fallback(encoded)).toEqual(expected)
  }
  for (const decode of [decodeBase64, decodeBase64Fallback]) {
    expect(decode(' A P 8=\n')).toEqual(new Uint8Array([0, 255]))
    expect(() => decode('!!!!')).toThrow()
    expect(() => decode('A')).toThrow()
  }
})

test('pasted private key is serialized once and immediately cleared from the bridge request', () => {
  const f = fixture('synthetic-private-key')
  expect(f.request.privateKey).toBe('')
  expect((f.calls[0]!.args[0] as { privateKey: string }).privateKey).toBe('synthetic-private-key')
  expect(f.events).toEqual([])
  f.transport.close()
})

test('SSH output is byte-exact and pulled only when the terminal consumes it', async () => {
  let packets = 0
  const f = fixture('', () => { packets++ }), controller = new AbortController(), reader = f.transport.read(controller.signal)[Symbol.asyncIterator]()
  const bytes = new TextEncoder().encode('中文\x1b[32m\u0000')
  f.channel.push({ type: 'connected', revision: 2 })
  f.channel.push(output(bytes.subarray(0, 2))); f.channel.push(output(bytes.subarray(2)))
  expect((await reader.next()).value).toEqual(bytes.subarray(0, 2))
  expect(f.channel.taken).toBe(2)
  await tick(); expect(f.channel.taken).toBe(2)
  expect(packets).toBe(1)
  expect((await reader.next()).value).toEqual(bytes.subarray(2))
  expect(packets).toBe(2)
  expect(f.events.map(event => event.type)).toEqual(['connected'])
  expect(f.request.password).toBe('')
  expect((f.calls[0]!.args[0] as { password: string }).password).toBe('test-secret')
  controller.abort(); await reader.return?.()
  expect(f.channel.closed).toBe(true)
})

test('only sustained terminal output marks a pane, and the mark clears after quiet or disconnect', async () => {
  const changes: [string, boolean][] = []
  const activity = createSessionOutputActivity((id, active) => changes.push([id, active]))
  try {
    activity.record('one-shot')
    activity.record('stream')
    activity.record('closing')
    expect(changes).toEqual([])

    await Bun.sleep(300)
    activity.record('stream')
    activity.record('stream')
    activity.record('closing')
    activity.clear('closing')
    expect(changes).toEqual([['stream', true], ['closing', true], ['closing', false]])

    await Bun.sleep(1850)
    expect(changes.at(-1)).toEqual(['stream', false])
    activity.record('one-shot')
    expect(changes).toHaveLength(4)
  } finally {
    activity.dispose()
  }
})

test('SSH input and binary mouse bytes preserve order without local echo', async () => {
  const f = fixture(), reader = f.transport.read(new AbortController().signal)[Symbol.asyncIterator]()
  expect(f.transport.send('before-connected')).toBe(false)
  f.channel.push({ type: 'connected', revision: 1 }); f.channel.push(output(new Uint8Array([65])))
  await reader.next()
  expect(f.transport.send('中文\r')).toBe(true)
  expect(f.transport.sendBinary('\x1b[M\xff\x80')).toBe(true)
  await tick(); await tick()
  const writes = f.calls.filter(call => call.name.endsWith('.Write'))
  expect(writes).toHaveLength(2)
  expect(Uint8Array.from(atob(writes[0]!.args[2] as string), char => char.charCodeAt(0))).toEqual(new TextEncoder().encode('中文\r'))
  expect(Uint8Array.from(atob(writes[1]!.args[2] as string), char => char.charCodeAt(0))).toEqual(new Uint8Array([27, 91, 77, 255, 128]))
  expect(f.events.map(event => event.type)).toEqual(['connected'])
  f.transport.close(); await reader.return?.()
  expect(f.transport.send('after-close')).toBe(false)
})

test('SSH trust replies keep the backend challenge ID and closing is idempotent', async () => {
  const f = fixture()
  expect(await f.transport.confirm('backend-challenge', 'remember')).toBe(true)
  expect(f.calls.at(-1)?.args).toEqual(['test-session', 1, 'backend-challenge', 'remember'])
  f.transport.close(); f.transport.close()
  expect(f.events.map(event => event.type)).toEqual(['disconnected'])
  expect(await f.transport.confirm('stale', 'once')).toBe(false)
})

test('interactive answers are sent once to the scoped attempt and cleared locally',async()=>{
 const f=fixture(),answers=['synthetic-password','123456']
 expect(await f.transport.answerAuthentication('backend-mfa',answers)).toBe(true)
 expect(answers).toEqual(['',''])
 expect(f.calls.at(-1)?.args).toEqual(['test-session',1,'backend-mfa',['synthetic-password','123456']])
 f.transport.close()
 expect(await f.transport.answerAuthentication('old-mfa',['old'])).toBe(false)
})

test('oversized input stops once instead of dropping or persisting terminal input', async () => {
  const f = fixture(), reader = f.transport.read(new AbortController().signal)[Symbol.asyncIterator]()
  f.channel.push({ type: 'connected', revision: 1 }); f.channel.push(output(new Uint8Array([65]))); await reader.next()
  expect(f.transport.send('x'.repeat(262145))).toBe(false)
  expect(f.events.filter(event => event.type === 'failed').map(event => event.code)).toEqual(['INPUT_OVERFLOW'])
  expect(f.channel.closed).toBe(true)
  expect(f.calls.some(call => call.name.endsWith('.Write'))).toBe(false)
  await reader.return?.()
})

test('directory metadata stays separate from PTY bytes and navigation keeps the owned attempt',async()=>{
 const f=fixture(),reader=f.transport.read(new AbortController().signal)[Symbol.asyncIterator]()
 await expect(f.transport.changeDirectory('/tmp')).rejects.toThrow()
 f.channel.push({type:'connected',revision:1})
 f.channel.push({type:'directory',revision:2,directory:{path:'/srv/中文',ready:true}})
 f.channel.push(output(new Uint8Array([65])))
 expect((await reader.next()).value).toEqual(new Uint8Array([65]))
 expect(f.events.find(e=>e.type==='directory')?.directory).toEqual({path:'/srv/中文',ready:true})
 f.transport.send('typed input')
 await f.transport.changeDirectory("/srv/a 'quoted'")
 const last=f.calls.at(-1)!
 expect(last.name.endsWith('.ChangeDirectory')).toBe(true)
 expect(last.args).toEqual(['test-session',1,"/srv/a 'quoted'"])
 expect(f.calls.findIndex(c=>c.name.endsWith('.Write'))).toBeLessThan(f.calls.findIndex(c=>c.name.endsWith('.ChangeDirectory')))
 f.transport.close();await reader.return?.()
 await expect(f.transport.changeDirectory('/old')).rejects.toThrow()
})

test('native login renders early banner and PTY bytes before any shell integration request', async () => {
  const f = fixture('', undefined, { attempt: 2 })
  const reader = f.transport.read(new AbortController().signal)
  const banner = new TextEncoder().encode('SSH access notice\r\n')
  const greeting = new TextEncoder().encode('Ubuntu · 欢迎\r\nLast login: fixture\r\nfixture$ ')
  try {
    await expect(f.transport.enableShellIntegration()).rejects.toThrow()
    f.channel.push(output(banner))
    f.channel.push({ type: 'connected', revision: 1 })
    f.channel.push(output(greeting))
    expect((await reader.next()).value).toEqual(banner)
    expect((await reader.next()).value).toEqual(greeting)
    expect(f.calls.filter(call => /\.(Write|EnableShellIntegration|ApplyBashIntegration)$/.test(call.name))).toHaveLength(0)
    await f.transport.enableShellIntegration()
    expect(f.calls.at(-1)).toEqual({ name: 'SSHService.EnableShellIntegration', args: ['test-session', 2] })
  } finally { f.transport.close(); await reader.return?.() }
})

test('stale and duplicate control events cannot regress a connected transport or revive a closed one',async()=>{
 const f=fixture(),reader=f.transport.read(new AbortController().signal)[Symbol.asyncIterator]()
 f.channel.push({type:'connected',revision:10})
 f.channel.push({type:'connected',revision:10})
 f.channel.push({type:'progress',revision:9,message:'old route progress'})
 f.channel.push({type:'failed',revision:8,code:'CONNECTION_LOST'})
 f.channel.push(output(new Uint8Array([65])))
 expect((await reader.next()).value).toEqual(new Uint8Array([65]))
 expect(f.events.map(e=>e.type)).toEqual(['connected'])
 expect(f.transport.send('still connected')).toBe(true)
 f.channel.push({type:'disconnected',revision:11,code:'CONNECTION_LOST'})
 f.channel.push({type:'connected',revision:12})
 f.channel.push(output(new Uint8Array([66])))
 expect((await reader.next()).value).toEqual(new Uint8Array([66]))
 expect(f.events.map(e=>e.type)).toEqual(['connected','disconnected'])
 expect(f.transport.send('must stay closed')).toBe(false)
 f.transport.close();await reader.return?.()
})

test('pane subscriptions see only the selected pane and release on close', () => {
  const source = createPaneSSHSource()
  const first = { phase: 'connected', attempt: 1 } as LiveSSH
  const next = { ...first, message: 'updated' }
  source.publish('first', first)
  let calls = 0
  const stop = source.subscribe('first', () => { calls++ })
  source.publish('second', first)
  source.publish('first', first)
  expect(calls).toBe(0)
  source.publish('first', next)
  expect(source.read('first')).toBe(next)
  expect(source.read('second')).toBe(first)
  expect(calls).toBe(1)
  source.publish('first')
  expect(source.read('first')).toBeUndefined()
  expect(calls).toBe(2)
  stop()
  source.publish('first', first)
  expect(calls).toBe(2)
})
