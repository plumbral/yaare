import { describe, expect, test } from 'bun:test'
import { initialHosts, mockGateway, validateHost, workspaceReducer, type WorkspaceState } from '../frontend/src/features/workspace/mockGateway'

const empty = (): WorkspaceState => ({ hosts: mockGateway.listHosts(), sessions: [], activeId: '' })
describe('preview workspace session lifecycle', () => {
  test('opening an existing host reuses the session and preserves its snapshot', () => {
    const host = { ...initialHosts[0]! }
    let state = workspaceReducer(empty(), { type: 'open', host, id: 'first' })
    state = workspaceReducer(state, { type: 'save', host: { ...host, address: '203.0.113.22' } })
    state = workspaceReducer(state, { type: 'open', host: state.hosts[0]!, id: 'duplicate' })
    expect(state.sessions).toHaveLength(1)
    expect(state.activeId).toBe('first')
    expect(state.sessions[0]!.host.address).toBe(host.address)
    expect(state.hosts[0]!.address).toBe('203.0.113.22')
  })
  test('closing the active session chooses its neighbor and can reach empty state', () => {
    let state = empty()
    for (let i = 0; i < 3; i++) state = workspaceReducer(state, { type: 'open', host: initialHosts[i]!, id: String(i) })
    state = workspaceReducer(state, { type: 'activate', id: '1' })
    state = workspaceReducer(state, { type: 'close', id: '1' })
    expect(state.activeId).toBe('0')
    state = workspaceReducer(state, { type: 'close', id: '0' })
    expect(state.activeId).toBe('2')
    state = workspaceReducer(state, { type: 'close', id: '2' })
    expect(state.activeId).toBe('')
    expect(state.sessions).toHaveLength(0)
  })
  test('closing a background tab preserves the active session and its snapshot', () => {
    let state = empty()
    for (let i = 0; i < 3; i++) state = workspaceReducer(state, { type: 'open', host: initialHosts[i]!, id: String(i) })
    const active = state.sessions[2]!
    state = workspaceReducer(state, { type: 'close', id: '0' })
    expect(state.activeId).toBe('2')
    expect(state.sessions.find(session => session.id === '2')).toBe(active)
    expect(state.sessions.map(session => session.id)).toEqual(['1', '2'])
  })
})
describe('preview workspace isolation', () => {
  test('rejects invalid connection fields before saving', () => {
    const valid = { ...initialHosts[0]! }
    expect(validateHost(valid)).toEqual({})
    for (const port of [0, 65536, 22.5, NaN]) expect(validateHost({ ...valid, port })).toHaveProperty('port')
    expect(validateHost({ ...valid, name: ' ', address: 'ssh://server/path', username: '', group: '' })).toMatchObject({ name: expect.any(String), address: expect.any(String), username: expect.any(String), group: expect.any(String) })
  })
  test('mock collections are isolated from consumers', () => {
    const hosts = mockGateway.listHosts()
    hosts[0]!.name = 'changed'
    expect(mockGateway.listHosts()[0]!.name).toBe('东京生产机')
    const groups = mockGateway.listGroups()
    const expected = [...groups]
    expect(expected.length).toBeGreaterThan(0)
    groups.pop()
    expect(mockGateway.listGroups()).toEqual(expected)
  })
})
