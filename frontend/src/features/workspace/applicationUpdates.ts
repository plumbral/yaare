export type UpdateStatus = {
  state: string; currentVersion: string; latestVersion: string; notes: string; checkedAt: number
}
export type UpdateSnapshot = { automatic: boolean; busy: boolean; status: UpdateStatus }
type Storage = Pick<globalThis.Storage, 'getItem' | 'setItem'>
type Gateway = { status(): Promise<UpdateStatus>; check(automatic: boolean): Promise<UpdateStatus> }
const storageKey = 'yaare.application-updates.v1'
export const updateCheckInterval = 30 * 60 * 1000
export const firstUpdateCheckDelay = 10_000
const emptyStatus: UpdateStatus = { state: 'loading', currentVersion: '', latestVersion: '', notes: '', checkedAt: 0 }

export function createUpdateController(gateway: Gateway, storage: () => Storage | undefined, now = Date.now) {
  let snapshot: UpdateSnapshot = { automatic: true, busy: false, status: emptyStatus }
  let lastAttempt = 0, initialized = false, pending: Promise<void> | undefined, initializing: Promise<void> | undefined
  const listeners = new Set<() => void>()
  function publish(next: UpdateSnapshot) { snapshot = next; for (const listener of listeners) listener() }
  function save() {
    try { storage()?.setItem(storageKey, JSON.stringify({ automatic: snapshot.automatic, lastAttempt, status: snapshot.status })) } catch {}
  }
  async function initialize() {
    if (initialized) return
    if (initializing) return initializing
    initializing = (async () => {
      let cached: UpdateStatus | undefined
      try {
        const value = JSON.parse(storage()?.getItem(storageKey) ?? '{}')
        if (typeof value.automatic === 'boolean') snapshot = { ...snapshot, automatic: value.automatic }
        if (Number.isFinite(value.lastAttempt) && value.lastAttempt >= 0 && value.lastAttempt <= now()) lastAttempt = value.lastAttempt
        if (['available', 'current'].includes(value.status?.state) && typeof value.status.currentVersion === 'string' && typeof value.status.latestVersion === 'string' && typeof value.status.notes === 'string' && Number.isFinite(value.status.checkedAt)) cached = value.status
      } catch {}
      let status: UpdateStatus
      try { status = await gateway.status() } catch { status = { ...emptyStatus, state: 'failed' } }
      if (status.state === 'idle' && cached?.currentVersion === status.currentVersion && cached.checkedAt <= now() && now() - cached.checkedAt < updateCheckInterval) status = cached
      initialized = true
      publish({ ...snapshot, status })
    })()
    return initializing
  }
  function check(automatic = false) {
    if (pending) return pending
    pending = (async () => {
      await initialize()
      if (['preview', 'unconfigured'].includes(snapshot.status.state)) return
      lastAttempt = now()
      publish({ ...snapshot, busy: true })
      save()
      try { publish({ ...snapshot, status: await gateway.check(automatic) }) }
      catch { publish({ ...snapshot, status: { ...snapshot.status, state: 'failed' } }) }
      finally { lastAttempt = now(); publish({ ...snapshot, busy: false }); save() }
    })().finally(() => { pending = undefined })
    return pending
  }
  async function automaticCheck(startup = false) {
    await initialize()
    try {
      const saved = JSON.parse(storage()?.getItem(storageKey) ?? '{}')
      if (typeof saved.automatic === 'boolean' && saved.automatic !== snapshot.automatic) publish({ ...snapshot, automatic: saved.automatic })
      if (Number.isFinite(saved.lastAttempt) && saved.lastAttempt >= 0 && saved.lastAttempt <= now()) lastAttempt = Math.max(lastAttempt, saved.lastAttempt)
    } catch {}
    if (!snapshot.automatic || ['preview', 'unconfigured'].includes(snapshot.status.state)) return updateCheckInterval
    if (startup || !lastAttempt || now() - lastAttempt >= updateCheckInterval) await check(true)
    return Math.max(1, updateCheckInterval - (now() - lastAttempt))
  }
  return {
    getSnapshot: () => snapshot,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
    initialize, check, automaticCheck,
    setAutomatic(automatic: boolean) {
      publish({ ...snapshot, automatic }); save()
      if (automatic) void automaticCheck()
    },
  }
}
