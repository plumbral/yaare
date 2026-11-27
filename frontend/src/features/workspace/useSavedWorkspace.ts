import { tr } from '../../shared/i18n/messages'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { desktopAvailable, supportsDesktopRecovery } from '../../shared/bridge/application'
import { WorkspaceService, type Host, type Report, type Result, type Snapshot } from '../../shared/generated/mygo'
import { mockGateway } from './mockGateway'
import { commandGroup, commandUngrouped } from './shortcuts'
import { previewChange, type SavedWorkspace, type WorkspaceChange } from './savedWorkspace'
import type {ConnectionPasswordSave} from '../connections/authenticationDraft'

const fromNative = (snapshot: Snapshot): SavedWorkspace => ({ ...snapshot, hosts: snapshot.hosts.map(h => ({ ...h, icon: h.icon as SavedWorkspace['hosts'][number]['icon'], authentication: h.authentication as SavedWorkspace['hosts'][number]['authentication'] })) })
export function createSavedWorkspaceStore(native: boolean, service: Pick<typeof WorkspaceService, 'load' | 'retry' | 'apply' | 'saveConnection' | 'recover' | 'restoreDeletedHost'> = WorkspaceService, desktopRecovery = supportsDesktopRecovery(native)) {
  const snapshot: SavedWorkspace = {
    revision: 0,
    hosts: native ? [] : mockGateway.listHosts(),
    groups: native ? [] : mockGateway.listGroups(),
    commands: [],
    commandGroups: native ? [] : [commandUngrouped],
  }
  let state = { snapshot, ready: !native, loading: native, busy: false, error: '', warning: '', canRecover: false }
  let issuedReads = 0
  let reportedThrough = 0
  let previousDatabaseReads = 0
  let snapshotIsCurrent = !native
  let recovery: Promise<void> | null = null
  const listeners = new Set<() => void>()
  const update = (patch: Partial<typeof state>) => {
    if (patch.snapshot) snapshotIsCurrent = true
    state = { ...state, ...patch }
    for (const listener of listeners) listener()
  }
  const reportState = (report: Report, error: string) => ({
    ready: report.ready, loading: false, canRecover: desktopRecovery && report.canRecover, warning: report.warning,
    ...(report.ready ? { snapshot: fromNative(report.snapshot), error: '' } : { error }),
  })
  const publishReadStatus = (request: number, patch: Partial<typeof state>) => {
    reportedThrough = request
    update(patch)
  }
  const acceptWrite = (result: Result) => {
    const next = fromNative(result.snapshot)
    if (next.revision < state.snapshot.revision) return next
    if (state.ready) publishReadStatus(issuedReads, { snapshot: next, warning: result.warning, error: '' })
    else update({ snapshot: next })
    return next
  }
  const reload = async (retry = false) => {
    if (!native) return true
    while (recovery) await recovery
    const request = ++issuedReads
    const isCurrentFailure = () => request === issuedReads && request > reportedThrough && request > previousDatabaseReads
    try {
      const report = await (retry && desktopRecovery ? service.retry() : service.load())
      if (request <= previousDatabaseReads) return state.ready
      if (!report.ready) {
        if (isCurrentFailure()) publishReadStatus(request, reportState(report, `${report.message} ${report.code}`))
      } else if (!snapshotIsCurrent || report.snapshot.revision >= state.snapshot.revision) {
        if (request > reportedThrough) publishReadStatus(request, reportState(report, ''))
        else if (state.ready && report.snapshot.revision > state.snapshot.revision) {
          update({ snapshot: fromNative(report.snapshot), warning: report.warning })
        }
      }
    } catch {
      if (isCurrentFailure()) publishReadStatus(request, { ready: false, loading: false, error: tr("配置读取失败，已暂停编辑。请重试。") })
    }
    return state.ready
  }
  let writeQueue = Promise.resolve()
  let queuedWrites = 0
  const commit = (change: WorkspaceChange, expectedRevision?:number,passwordSave?:ConnectionPasswordSave): Promise<SavedWorkspace | null> => {
    if (recovery || (!queuedWrites && state.busy) || !state.ready) return Promise.resolve(null)
    queuedWrites++
    const write = writeQueue.then(async () => {
      if (!state.ready) return null
      try {
        const revision = state.snapshot.revision
        if(expectedRevision!==undefined&&expectedRevision!==revision)throw Error(tr("配置已变更，请重新载入后再保存。"))
        if (native) {
          const emptyHost: Host = { id: '', name: '', address: '', username: '', port: 22, group: '', system: '', icon: '', authentication: '', keyId: '', identityId: '',notes:'',pinned:false,lastUsed:0,jumpHostIds:[],forwards:[],timeoutSeconds:0,proxy:{kind:'none',address:'',port:0,useAuthentication:false,username:''} }
          const host = change.kind === 'saveHost' ? { ...change.host,proxy:change.host.proxy??{kind:'none',address:'',port:0,useAuthentication:false,username:''},jumpHostIds:change.host.jumpHostIds??[],forwards:change.host.forwards??[],timeoutSeconds:change.host.timeoutSeconds??0,notes:change.host.notes??'',pinned:change.host.pinned??false,lastUsed:change.host.lastUsed??0, identityId: change.host.identityId ?? '', icon: change.host.icon ?? 'linux', authentication: change.host.authentication ?? 'none', keyId: change.host.authentication === 'key' ? change.host.keyId ?? '' : '' } : emptyHost
          const emptyCommand = { id: '', name: '', command: '', group: '' }
          const hosts=change.kind==='saveHosts'?change.hosts.map(item=>({...emptyHost,...item,icon:item.icon??'linux',authentication:item.authentication??'password',identityId:item.identityId??'',keyId:item.keyId??''})):[]
          const result = passwordSave&&change.kind==='saveHost'?await service.saveConnection({revision,host,...passwordSave}):await service.apply({ revision, id: '', name: '', newName: '', pinned:false, commandGroups: [],groups:[],ids:[], ...change, host,hosts,
            command: change.kind === 'saveCommand' ? { ...change.command, group: commandGroup(change.command) } : emptyCommand,
            commands: 'commands' in change ? change.commands.map(c => ({ ...c, group: commandGroup(c) })) : [] })
          return acceptWrite(result)
        }
        const next = previewChange(state.snapshot, change)
        update({ snapshot: next })
        return next
      } catch (cause) {
        await reload()
        update({ error: cause instanceof Error ? cause.message : tr("保存失败，编辑内容已保留，请重试。") })
        return null
      }
    }).finally(() => {
      if (--queuedWrites === 0) update({ busy: false })
    })
    writeQueue = write.then(() => undefined, () => undefined)
    if (!state.busy) update({ busy: true, error: '' })
    return write
  }
  const apply = async (change: WorkspaceChange, expectedRevision?: number, credentials?: ConnectionPasswordSave) =>
    !!await commit(change, expectedRevision, credentials)
  const saveHost = (host: SavedWorkspace['hosts'][number], expectedRevision?: number, credentials?: ConnectionPasswordSave) =>
    commit({ kind: 'saveHost', host }, expectedRevision, credentials)
  const recover = async () => {
    if (!desktopRecovery || state.busy) return
    previousDatabaseReads = issuedReads
    snapshotIsCurrent = false
    recovery = Promise.resolve().then(async () => {
      try {
        const report = await service.recover()
        update(reportState(report, report.message))
      } catch (cause) {
        update({ ready: false, error: cause instanceof Error ? cause.message : tr("备份恢复失败，原文件已保留，请重试。") })
      } finally {
        recovery = null
        update({ busy: false, loading: false })
      }
    })
    update({ busy: true })
    await recovery
  }
  const restoreDeletedHost = async (backupId: string, hostId: string, revision: number) => {
    if (!native || !state.ready || state.busy) throw Error(tr('恢复未完成，请刷新后重试。'))
    update({ busy: true })
    try {
      const result = await service.restoreDeletedHost(backupId, hostId, revision)
      acceptWrite(result)
    } catch (cause) {
      await reload()
      throw cause
    } finally { update({ busy: false }) }
  }
  const subscribe = (listener: () => void) => {
    listeners.add(listener)
    return () => { listeners.delete(listener) }
  }
  const acknowledgePreferenceWrite = (previous: number, current: number) => {
    const acknowledge = () => {
      if (!state.ready || state.snapshot.revision !== previous) return
      publishReadStatus(issuedReads, { snapshot: { ...state.snapshot, revision: current } })
    }
    if (queuedWrites) void writeQueue.then(acknowledge)
    else acknowledge()
  }
  return { read: () => state, subscribe, acknowledgePreferenceWrite, getSnapshot: () => state.snapshot, apply, saveHost, reload, recover, restoreDeletedHost, native }
}

export function useSavedWorkspace() {
  const [store] = useState(() => createSavedWorkspaceStore(desktopAvailable()))
  const { read, subscribe, ...actions } = store
  const state = useSyncExternalStore(subscribe, read, read)
  useEffect(() => { void store.reload() }, [store])
  return { ...state, ...actions }
}
