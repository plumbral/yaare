import { expect, test } from 'bun:test'
import { normalizePreferences } from '../frontend/src/features/workspace/preferences'
import { validCommand, validateQuickCommand } from '../frontend/src/features/workspace/shortcuts'
import { initialHosts, validateHost, workspaceReducer } from '../frontend/src/features/workspace/mockGateway'
import { authenticationError, clearAuthentication, credentialsFromDraft, emptyAuthentication } from '../frontend/src/features/connections/authenticationDraft'

test('quick commands support source lines but reject terminal control sequences', () => {
  for (const text of ['', ' ', 'ls\r', '\x1b[2J', 'ls\u2028pwd', 'x'.repeat(65537)]) expect(validCommand(text)).toBe(false)
  expect(validCommand('ls\npwd')).toBe(true)
  expect(validCommand('echo $(whoami); literal')).toBe(true)
  expect(validateQuickCommand({ id: 'x', name: '列出文件', command: 'ls' })).toBeNull()
  expect(validateQuickCommand({ id: 'x', name: ' ', command: 'ls' })).not.toBeNull()
})
test('preferences whitelist excludes secrets and bounds resource settings', () => {
  expect(normalizePreferences({ mode: 'light', fontSize: 18, scrollback: 5000, cursorBlink: true, privateKey: 'secret', password: 'secret' }))
    .toEqual({ language:'system', mode: 'light', interfacePalette:'neutral',terminalPalette:'auto',iconStyle:'monochrome',fontSize: 18, scrollback: 5000, cursorBlink: true,screenReaderMode:false,uiFont:'Inter',fontFamily:'Google Sans Code',fallbackFont:'Maple Mono CN',lineHeight:1.5,autoReconnect:true,detectServerSystem:false,transferConcurrency:2,directorySync:false })
  expect(normalizePreferences({ fontSize: -1, scrollback: 1e9, cursorBlink: 'true' }))
    .toEqual({ language:'system', mode: 'system', interfacePalette:'neutral',terminalPalette:'auto',iconStyle:'monochrome',fontSize: 14, scrollback: 1000, cursorBlink: false,screenReaderMode:false,uiFont:'Inter',fontFamily:'Google Sans Code',fallbackFont:'Maple Mono CN',lineHeight:1.5,autoReconnect:true,detectServerSystem:false,transferConcurrency:2,directorySync:false })
})
test('key authentication may defer a secret but editing does not alter an existing session key', () => {
  const host = { ...initialHosts[0]!, authentication: 'key' as const, keyId: 'first-key' }
  expect(validateHost({ ...host, keyId: '' })).toEqual({})
  expect(authenticationError(emptyAuthentication({ ...host, keyId: '' }), [])).not.toBe('')
  expect(validateHost(host)).toEqual({})
  let state = { hosts: [host], sessions: [{ id: 'live', host: { ...host } }], activeId: 'live' }
  const next = workspaceReducer(state, { type: 'save', host: { ...host, keyId: 'second-key' } })
  expect(next.hosts[0]!.keyId).toBe('second-key')
  expect(next.sessions[0]!.host.keyId).toBe('first-key')
})

test('authentication drafts send only the selected secret and clear transient material', () => {
  const draft = { ...emptyAuthentication(initialHosts[0]!), authentication: 'key' as const, keySource: 'text' as const,
    keyId: 'old-file', password: 'unused-password', privateKey: 'test-private-key', passphrase: 'test-phrase' }
  expect(authenticationError(draft, [])).toBe('')
  expect(credentialsFromDraft(draft)).toEqual({ authentication: 'key', useAgent: false, keyId: '', password: '', privateKey: 'test-private-key', passphrase: 'test-phrase', timeoutSeconds: 15 })
  expect(credentialsFromDraft({ ...draft, authentication: 'password' })).toEqual({ authentication: 'password', useAgent: false, keyId: '', password: 'unused-password', privateKey: '', passphrase: '', timeoutSeconds: 15 })
  expect(clearAuthentication(draft)).toMatchObject({ password: '', privateKey: '', passphrase: '' })
  expect(authenticationError({ ...draft, privateKey: '' }, [])).not.toBe('')
  expect(authenticationError({ ...draft, privateKey: 'x'.repeat(1048577) }, [])).not.toBe('')
})

test('save and connect opens an independent snapshot and preserves existing sessions', () => {
  const host = initialHosts[0]!
  const original = { id: 'live', host: { ...host } }
  const changed = { ...host, address: '203.0.113.99' }
  const next = workspaceReducer({ hosts: [changed], sessions: [original], activeId: 'live' }, { type: 'connect', host: changed, id: 'new-connection' })
  expect(next.sessions).toHaveLength(2)
  expect(next.sessions[0]).toBe(original)
  expect(next.sessions[1]!.host.address).toBe(changed.address)
  expect(next.activeId).toBe('new-connection')
})
