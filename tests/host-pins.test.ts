import { expect, test } from 'bun:test'
import { filterHostList, sidebarHostRows } from '../frontend/src/features/connections/hostOrdering'
import { initialHosts, workspaceReducer, type HostProfile } from '../frontend/src/features/workspace/mockGateway'
import { previewChange, type SavedWorkspace } from '../frontend/src/features/workspace/savedWorkspace'
import { patchBatchHosts, toggleHostSelection } from '../frontend/src/features/connections/batchHosts'

const hosts: HostProfile[] = [
  { ...initialHosts[0]!, id: 'a', name: 'Alpha', group: 'Prod/API', pinned: false, lastUsed: 300 },
  { ...initialHosts[0]!, id: 'b', name: 'Beta', group: 'Dev', pinned: true, lastUsed: 100 },
  { ...initialHosts[0]!, id: 'c', name: 'Gamma', group: 'Prod', pinned: false, lastUsed: 500 },
  { ...initialHosts[0]!, id: 'd', name: 'Delta', group: 'Dev', pinned: true, lastUsed: 0 },
  { ...initialHosts[0]!, id: 'e', name: 'Epsilon', group: '未分组', pinned: false, lastUsed: 0 },
]
const ids = (value: readonly HostProfile[]) => value.map(host => host.id)
test('pins have stable order; unpinning restores ordinary order without mutating storage', () => {
  const before = JSON.stringify(hosts)
  expect(ids(filterHostList(hosts, ''))).toEqual(['b', 'd', 'a', 'c', 'e'])
  const saved: SavedWorkspace = { revision: 1, groups: ['未分组', 'Prod', 'Prod/API', 'Dev'], hosts, commands: [], commandGroups: ['未分组'] }
  const pinned = previewChange(saved, { kind: 'pinHost', id: 'c', pinned: true })
  expect(ids(filterHostList(pinned.hosts, ''))).toEqual(['b', 'c', 'd', 'a', 'e'])
  const unpinned = previewChange(pinned, { kind: 'pinHost', id: 'c', pinned: false })
  expect(ids(filterHostList(unpinned.hosts, ''))).toEqual(['b', 'd', 'a', 'c', 'e'])
  expect(ids(unpinned.hosts)).toEqual(['a', 'b', 'c', 'd', 'e'])
  expect(JSON.stringify(hosts)).toBe(before)
})
test('filter first, pin second: groups, descendants and search never leak unrelated pinned hosts', () => {
  expect(ids(filterHostList(hosts, '', 'group:Prod'))).toEqual(['a', 'c'])
  expect(ids(filterHostList(hosts, '  GAMMA  '))).toEqual(['c'])
  expect(ids(filterHostList(hosts, '', 'all', 'pinned'))).toEqual(['b', 'd'])
  expect(filterHostList(hosts, '', 'group:Prod', 'pinned')).toEqual([])
  expect(filterHostList(hosts, 'missing')).toEqual([])
  expect(filterHostList([], '')).toEqual([])
})
test('recent connections retain time order rather than pin order, excluding never-connected hosts', () => {
  expect(ids(filterHostList(hosts, '', 'all', 'recent'))).toEqual(['c', 'a', 'b'])
  expect(ids(filterHostList(hosts, 'Dev', 'all', 'recent'))).toEqual(['b'])
})
test('sidebar pins appear once, omit empty old groups and keep actual group assignments', () => {
  const rows = sidebarHostRows(filterHostList(hosts, ''))
  expect(rows[0]).toMatchObject({ pinned: true, count: 2 })
  expect(rows.filter(row => !('count' in row)).map(row => row.id)).toEqual(['b', 'd', 'a', 'c', 'e'])
  expect(new Set(rows.map(row => row.id)).size).toBe(rows.length)
  expect(rows.filter(row => 'count' in row).map(row => row.group)).toEqual(['', 'Prod/API', 'Prod'])
  expect(hosts[1]!.group).toBe('Dev')
  expect(sidebarHostRows(hosts.filter(host => !host.pinned)).some(row => 'count' in row && row.pinned)).toBe(false)
  expect(sidebarHostRows([])).toEqual([])
  expect(sidebarHostRows(hosts.filter(host => host.pinned))).toHaveLength(3)
})
test('batch pin/unpin preserves other fields and active SSH snapshots, range selection follows displayed order', () => {
  const patched = patchBatchHosts(hosts, { pinned: true })
  expect(filterHostList(patched, '', 'all', 'pinned')).toHaveLength(hosts.length)
  expect(patchBatchHosts(patched, { pinned: false }).every(host => !host.pinned)).toBe(true)
  for (let i = 0; i < hosts.length; i++) expect(patched[i]).toEqual({ ...hosts[i]!, pinned: true })
  const runtime = workspaceReducer({ hosts, sessions: [], activeId: '' }, { type: 'open', host: hosts[0]!, id: 'ssh-running' })
  const next = workspaceReducer(runtime, { type: 'replaceHosts', hosts: patched })
  expect(next.sessions).toBe(runtime.sessions)
  expect(next.sessions[0]!.host.pinned).toBe(false)
  expect(toggleHostSelection(['d'], ids(filterHostList(hosts, '')), 'c', true, 'd', true)).toEqual(['d', 'a', 'c'])
})
