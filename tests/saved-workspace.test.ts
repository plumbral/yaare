import { describe, expect, test } from 'bun:test'
import { previewChange, ungrouped, type SavedWorkspace } from '../frontend/src/features/workspace/savedWorkspace'
import { initialHosts, workspaceReducer } from '../frontend/src/features/workspace/mockGateway'
import { supportsDesktopRecovery } from '../frontend/src/shared/bridge/application'
import { needsDraftReview, reviewDraft } from '../frontend/src/features/workspace/draftRevision'
import { createSavedWorkspaceStore } from '../frontend/src/features/workspace/useSavedWorkspace'
import type { Change, Report, Result, SaveConnectionRequest } from '../frontend/src/shared/generated/mygo'
test('group changes and deleting profiles leave active terminal sessions intact', () => {
  const host = { ...initialHosts[0]! }
  const current: SavedWorkspace = { revision: 0, hosts: [host], groups: [ungrouped, host.group], commands: [], commandGroups: [ungrouped] }
  const runtime = workspaceReducer({ hosts: current.hosts, sessions: [], activeId: '' }, { type: 'open', host, id: 'connected-session' })
  const renamed = previewChange(current, { kind: 'renameGroup', name: host.group, newName: 'renamed' })
  expect(renamed.hosts[0]!.group).toBe('renamed'); expect(current.hosts[0]!.group).toBe(host.group)
  const moved = previewChange(renamed, { kind: 'deleteGroup', name: 'renamed' })
  expect(moved.hosts[0]!.group).toBe(ungrouped)
  const removed = previewChange(moved, { kind: 'deleteHost', id: host.id })
  const next = workspaceReducer(runtime, { type: 'replaceHosts', hosts: removed.hosts })
  expect(next.hosts).toHaveLength(0); expect(next.sessions).toBe(runtime.sessions); expect(next.activeId).toBe('connected-session')
  expect(next.sessions[0]!.host.group).toBe(host.group)
})
test('preview copies create unique IDs and empty groups remain independently manageable', () => {
  const host = { ...initialHosts[0]! }
  let saved: SavedWorkspace = { revision: 0, hosts: [host], groups: [ungrouped, host.group], commands: [], commandGroups: [ungrouped] }
  saved = previewChange(saved, { kind: 'duplicateHost', id: host.id }); saved = previewChange(saved, { kind: 'duplicateHost', id: host.id })
  expect(new Set(saved.hosts.map(h => h.id)).size).toBe(3); expect(new Set(saved.hosts.map(h => h.name)).size).toBe(3)
  saved = previewChange(saved, { kind: 'addGroup', name: 'empty' }); expect(saved.groups).toContain('empty')
  expect(() => previewChange(saved, { kind: 'deleteGroup', name: ungrouped })).toThrow()
  expect(() => previewChange(saved, { kind: 'addGroup', name: 'empty' })).toThrow()
})

test('nested groups cascade without losing pinned, recent or note metadata',()=>{
 const host={...initialHosts[0]!,group:'Prod/Database/Primary',notes:'中文备注',pinned:true,lastUsed:12345}
 const current:SavedWorkspace={revision:0,hosts:[host],groups:[ungrouped,'Prod','Prod/Database','Prod/Database/Primary'],commands:[],commandGroups:[ungrouped]}
 expect(()=>previewChange(current,{kind:'renameGroup',name:'Prod',newName:'Prod/Child'})).toThrow()
 const renamed=previewChange(current,{kind:'renameGroup',name:'Prod',newName:'Production'})
 expect(renamed.hosts[0]).toEqual({...host,group:'Production/Database/Primary'})
 const deleted=previewChange(renamed,{kind:'deleteGroup',name:'Production'})
 expect(deleted.groups).toEqual([ungrouped])
 expect(deleted.hosts[0]).toEqual({...host,group:ungrouped})
 expect(current.hosts[0]).toEqual(host)
 const duplicate=previewChange(renamed,{kind:'duplicateHost',id:host.id})
 expect(duplicate.hosts[1]!.lastUsed).toBe(0)
 const pinned=previewChange(renamed,{kind:'pinHost',id:host.id,pinned:false})
 expect(pinned.hosts[0]!.pinned).toBe(false)
 expect(pinned.hosts[0]!.lastUsed).toBe(host.lastUsed)
})

function controlled<Response, Args extends unknown[] = []>() {
  const calls: Array<ReturnType<typeof Promise.withResolvers<Response>> & { args: Args }> = []
  return { calls, request: (...args: Args) => {
    const response = Promise.withResolvers<Response>()
    calls.push({ ...response, args })
    return response.promise
  } }
}

const resultAt = (revision: number): Result => ({
  snapshot: { revision, hosts: [], groups: [ungrouped, 'group-' + revision], commands: [], commandGroups: [ungrouped] },
  entityId: '', warning: 'warning-' + revision,
})
const reportAt = (revision: number): Report => ({ ...resultAt(revision), ready: true, code: '', message: '', canRecover: false, backupCount: 0 })
const failedReport = (): Report => ({ ...reportAt(0), ready: false, canRecover: true, code: 'READ_FAILED', message: 'read failed', warning: 'storage warning' })
const newGroup = { kind: 'addGroup', name: 'new' } as const

function workspaceHarness(desktopRecovery = true) {
  const load = controlled<Report>()
  const retry = controlled<Report>()
  const save = controlled<Result, [Change]>()
  const saveConnection = controlled<Result, [SaveConnectionRequest]>()
  const recover = controlled<Report>()
  const restoreHost = controlled<Result, [string, string, number]>()
  const store = createSavedWorkspaceStore(true, { load: load.request, retry: retry.request, apply: save.request,
    saveConnection: saveConnection.request, recover: recover.request, restoreDeletedHost: restoreHost.request }, desktopRecovery)
  const prime = async (revision: number) => {
    const reading = store.reload()
    load.calls.at(-1)!.resolve(reportAt(revision))
    expect(await reading).toBe(true)
  }
  return { store, load: load.calls, retry: retry.calls, save: save.calls, saveConnection: saveConnection.calls,
    recover: recover.calls, restoreHost: restoreHost.calls, prime }
}

describe('out-of-order workspace responses', () => {
  test('Docker retries a normal load and never offers native database or transfer recovery', async () => {
    expect(supportsDesktopRecovery(true, 'production')).toBe(true)
    expect(supportsDesktopRecovery(true, 'browser')).toBe(false)
    expect(supportsDesktopRecovery(false, 'development')).toBe(false)
    const { store, load, retry, recover } = workspaceHarness(false)
    const reading = store.reload(true)
    load[0]!.resolve(failedReport())
    expect(await reading).toBe(false)
    expect(store.read().canRecover).toBe(false)
    await store.recover()
    expect(retry).toHaveLength(0)
    expect(recover).toHaveLength(0)
    const again = store.reload(true)
    load[1]!.resolve(reportAt(4))
    expect(await again).toBe(true)
  })

  test('a slow load cannot replace a newer retry or its revision for the next save', async () => {
    const { store, load, retry, save } = workspaceHarness()
    const older = store.reload()
    const newer = store.reload(true)
    retry[0]!.resolve(reportAt(41))
    expect(await newer).toBe(true)
    load[0]!.resolve(reportAt(40))
    expect(await older).toBe(true)
    expect(store.read()).toMatchObject({ snapshot: { revision: 41, groups: [ungrouped, 'group-41'] }, warning: 'warning-41', loading: false })
    const saving = store.apply(newGroup)
    await Promise.resolve()
    expect(save[0]!.args[0].revision).toBe(41)
    save[0]!.resolve(resultAt(42))
    expect(await saving).toBe(true)
  })

  test('a read made earlier can carry a higher revision if SQLite services it later', async () => {
    const { store, load } = workspaceHarness()
    const earlierCall = store.reload()
    const laterCall = store.reload()
    load[1]!.resolve(reportAt(41))
    await laterCall
    load[0]!.resolve(reportAt(42))
    await earlierCall
    expect(store.getSnapshot().revision).toBe(42)
  })

  test('an older response at the same revision cannot replace current storage status', async () => {
    const { store, load } = workspaceHarness()
    const older = store.reload()
    const newer = store.reload()
    load[1]!.resolve(reportAt(41))
    await newer
    load[0]!.resolve({ ...reportAt(41), canRecover: true, warning: 'obsolete warning' })
    await older
    expect(store.read()).toMatchObject({ warning: 'warning-41', canRecover: false })
  })

  test.each(['report', 'rejection'])('an obsolete read %s cannot disable editing after a newer successful read', async kind => {
    const { store, load } = workspaceHarness()
    const obsolete = store.reload()
    const current = store.reload()
    load[1]!.resolve(reportAt(41))
    await current
    if (kind === 'report') load[0]!.resolve(failedReport())
    else load[0]!.reject(Error('obsolete IPC failure'))
    expect(await obsolete).toBe(true)
    expect(store.read()).toMatchObject({ ready: true, canRecover: false, loading: false, error: '', warning: 'warning-41' })
  })

  test('a real current read failure retains the last snapshot and blocks writes', async () => {
    const { store, load, save, prime } = workspaceHarness()
    await prime(40)
    const earlier = store.reload()
    const failing = store.reload()
    load[2]!.resolve(failedReport())
    expect(await failing).toBe(false)
    load[1]!.resolve(reportAt(41))
    expect(await earlier).toBe(false)
    expect(store.read()).toMatchObject({ snapshot: { revision: 40 }, ready: false, canRecover: true, error: 'read failed READ_FAILED' })
    expect(await store.apply(newGroup)).toBe(false)
    expect(save).toHaveLength(0)
    await prime(41)
    expect(store.read()).toMatchObject({ ready: true, snapshot: { revision: 41 }, error: '' })
  })

  test('an older read cannot roll back a saved connection, and a late failure cannot disable it', async () => {
    const { store, load, saveConnection, prime } = workspaceHarness()
    await prime(40)
    const oldRead = store.reload()
    const saving = store.apply({ kind: 'saveHost', host: initialHosts[0]! }, undefined, { rememberPassword: false, password: '' })
    const failing = store.reload()
    await Promise.resolve()
    expect(saveConnection[0]!.args[0].revision).toBe(40)
    saveConnection[0]!.resolve(resultAt(41))
    expect(await saving).toBe(true)
    load[1]!.resolve(reportAt(40))
    await oldRead
    load[2]!.reject(Error('obsolete IPC failure'))
    expect(await failing).toBe(true)
    expect(store.read()).toMatchObject({ snapshot: { revision: 41 }, ready: true, error: '', warning: 'warning-41' })
  })

  test('save and connect receives its own committed host even when a newer snapshot arrives first', async () => {
    const { store, saveConnection, prime } = workspaceHarness()
    await prime(40)
    const saving = store.saveHost(initialHosts[0]!, 40, { rememberPassword:false, password:'' })
    await Promise.resolve()
    const committed = resultAt(41)
    committed.snapshot.hosts = [{ ...saveConnection[0]!.args[0].host, authentication:'key', keyId:'new-vault-reference' }]
    await prime(42)
    saveConnection[0]!.resolve(committed)
    expect((await saving)?.hosts[0]?.keyId).toBe('new-vault-reference')
    expect(store.read().snapshot.revision).toBe(42)
    expect(store.read().snapshot.hosts).toHaveLength(0)
  })

  test('a delayed save result cannot overwrite a newer synchronized snapshot', async () => {
    const { store, save, prime } = workspaceHarness()
    await prime(40)
    const saving = store.apply(newGroup)
    await prime(42)
    save[0]!.resolve(resultAt(41))
    expect(await saving).toBe(true)
    expect(store.read()).toMatchObject({ snapshot: { revision: 42 }, warning: 'warning-42', busy: false })
  })

  test('a read requested during a save still applies if it contains a later revision', async () => {
    const { store, load, save, prime } = workspaceHarness()
    await prime(40)
    const saving = store.apply(newGroup)
    const refreshing = store.reload()
    await Promise.resolve()
    save[0]!.resolve(resultAt(41))
    await saving
    load[1]!.resolve(reportAt(42))
    expect(await refreshing).toBe(true)
    expect(store.getSnapshot().revision).toBe(42)
  })

  test('a late read cannot roll back a restored host', async () => {
    const { store, load, restoreHost, prime } = workspaceHarness()
    await prime(40)
    const reading = store.reload()
    const restoring = store.restoreDeletedHost('backup', 'host', 40)
    expect(restoreHost[0]!.args).toEqual(['backup', 'host', 40])
    restoreHost[0]!.resolve(resultAt(41))
    await restoring
    load[1]!.resolve(reportAt(40))
    await reading
    expect(store.getSnapshot().revision).toBe(41)
  })

  test('whole database recovery can lower the revision and discards late reads of the previous database', async () => {
    const { store, load, recover, save, prime } = workspaceHarness()
    await prime(40)
    const obsolete = store.reload()
    const restoring = store.recover()
    await Promise.resolve()
    recover[0]!.resolve(reportAt(7))
    await restoring
    load[1]!.resolve(reportAt(43))
    await obsolete
    expect(store.read()).toMatchObject({ snapshot: { revision: 7 }, warning: 'warning-7', ready: true })
    const saving = store.apply(newGroup)
    await Promise.resolve()
    expect(save[0]!.args[0].revision).toBe(7)
    save[0]!.resolve(resultAt(8))
    expect(await saving).toBe(true)
  })

  test('a refresh requested mid-recovery reads the recovered database', async () => {
    const { store, retry, recover, prime } = workspaceHarness()
    await prime(40)
    const restoring = store.recover()
    const refreshing = store.reload(true)
    await Promise.resolve()
    expect(retry).toHaveLength(0)
    recover[0]!.resolve(reportAt(7))
    await restoring
    expect(retry).toHaveLength(1)
    retry[0]!.resolve(reportAt(8))
    expect(await refreshing).toBe(true)
    expect(store.getSnapshot().revision).toBe(8)
  })

  test('retry can read a lower revision after an uncertain recovery response', async () => {
    const { store, load, retry, recover, prime } = workspaceHarness()
    await prime(40)
    const obsolete = store.reload()
    const restoring = store.recover()
    await Promise.resolve()
    recover[0]!.reject(Error('lost recovery response'))
    await restoring
    expect(store.read()).toMatchObject({ ready: false, snapshot: { revision: 40 } })
    const refreshing = store.reload(true)
    retry[0]!.resolve(reportAt(7))
    expect(await refreshing).toBe(true)
    load[1]!.resolve(reportAt(43))
    await obsolete
    expect(store.read()).toMatchObject({ ready: true, snapshot: { revision: 7 }, error: '' })
  })

  test('an old host or command editor cannot write over a refreshed or deleted record on retry', async () => {
    const { store, prime, load, saveConnection, save } = workspaceHarness()
    await prime(10)
    const openedAt = store.getSnapshot().revision
    await prime(11)
    for (const change of [
      { kind: 'saveHost', host: initialHosts[0]! },
      { kind: 'saveCommand', command: { id: 'old', name: 'old', command: 'pwd' } },
    ] as const) {
      const saving = store.apply(change, openedAt, change.kind === 'saveHost' ? { rememberPassword: true, password: '' } : undefined)
      await Promise.resolve()
      load.at(-1)!.resolve(reportAt(11))
      expect(await saving).toBe(false)
    }
    expect(saveConnection).toHaveLength(0)
    expect(save).toHaveLength(0)
  })

  test('review takes external fields and keeps unrelated local draft edits', () => {
    const original = { ...initialHosts[0]!, notes: 'before', port: 22 }
    const draft = { ...original, name: 'my rename', notes: 'local notes' }
    const latest = { ...original, notes: 'remote notes', port: 2222 }
    expect(reviewDraft(original, draft, latest)).toEqual({ ...latest, name: 'my rename' })
    const command = { id: 'command', name: 'label', command: 'first' }
    expect(reviewDraft(command, { ...command, name: 'edited' }, { ...command, command: 'second' }))
      .toEqual({ id: 'command', name: 'edited', command: 'second' })
  })

  test('a stale batch preview still cannot submit a write', async () => {
    const { store, load, save, prime } = workspaceHarness()
    await prime(41)
    const saving = store.apply(newGroup, 40)
    await Promise.resolve()
    load[1]!.resolve(reportAt(41))
    expect(await saving).toBe(false)
    expect(save).toHaveLength(0)
    expect(store.read()).toMatchObject({ snapshot: { revision: 41 }, ready: true, busy: false })
    expect(store.read().error).not.toBe('')
  })

  test('a real save conflict refreshes the current snapshot without replaying the edit', async () => {
    const { store, load, save, prime } = workspaceHarness()
    await prime(40)
    const obsolete = store.reload()
    const saving = store.apply(newGroup)
    await Promise.resolve()
    expect(save[0]!.args[0].revision).toBe(40)
    save[0]!.reject(Error('CONFLICT'))
    await Promise.resolve()
    load[2]!.resolve(reportAt(42))
    expect(await saving).toBe(false)
    load[1]!.resolve(reportAt(41))
    await obsolete
    expect(store.read()).toMatchObject({ snapshot: { revision: 42 }, error: 'CONFLICT', ready: true, busy: false })
    expect(save).toHaveLength(1)
  })
})

test('concurrent changes are submitted in order using the last committed revision', async () => {
  const { store, save, prime } = workspaceHarness()
  await prime(40)
  const first = store.apply(newGroup)
  const second = store.apply({ kind: 'markHostUsed', id: 'host' })
  await Promise.resolve()
  expect(store.read().busy).toBe(true)
  expect(save).toHaveLength(1)
  save[0]!.resolve(resultAt(41))
  expect(await first).toBe(true)
  await Promise.resolve()
  expect(store.read().busy).toBe(true)
  expect(save[1]!.args[0]).toMatchObject({ kind: 'markHostUsed', revision: 41 })
  save[1]!.resolve(resultAt(41))
  expect(await second).toBe(true)
  expect(store.read().busy).toBe(false)
})

test('preference saves only advance the revision for a contiguous workspace snapshot', async () => {
  const { store, load, prime } = workspaceHarness()
  await prime(40)
  const groups = store.getSnapshot().groups
  store.acknowledgePreferenceWrite(40, 41)
  store.acknowledgePreferenceWrite(42, 43)
  expect(store.getSnapshot().revision).toBe(41)
  expect(store.getSnapshot().groups).toBe(groups)
  expect(load).toHaveLength(1)
})

test('concurrent preference acknowledgement retains a successful activity snapshot at the old revision', async () => {
  const { store, save, prime } = workspaceHarness()
  await prime(40)
  const writing = store.apply({ kind: 'markHostUsed', id: 'host' })
  await Promise.resolve()
  store.acknowledgePreferenceWrite(40, 41)
  const activity = resultAt(40)
  activity.snapshot.groups.push('activity')
  save[0]!.resolve(activity)
  expect(await writing).toBe(true)
  await Promise.resolve()
  expect(store.getSnapshot().revision).toBe(41)
  expect(store.getSnapshot().groups).toContain('activity')
})

test('a new draft can survive unrelated revisions but must review its own committed ID after a lost reply', () => {
  expect(needsDraftReview(undefined, 4, false, false)).toBe(false)
  expect(needsDraftReview(undefined, 5, false, true)).toBe(true)
  expect(needsDraftReview(5, 5, true, true)).toBe(false)
  expect(needsDraftReview(5, 6, true, false)).toBe(true)
})
