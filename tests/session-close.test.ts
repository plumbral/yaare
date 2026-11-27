import { expect, test } from 'bun:test'
import { createSessionCloser, type CloseParticipant, type SessionCloseState } from '../frontend/src/features/workspace/sessionClose'
import type { FileTask } from '../frontend/src/shared/bridge/files'

function fixture() {
  const events: string[] = []
  const panes: CloseParticipant[] = ['A', 'B', 'C'].map(id => ({ id, name: id, attempt: 1, guards: new Map(), forwards: [] }))
  const relay = { id: 'relay', sessionId: 'A', sourceSession: 'A', destinationSession: 'B', state: 'running' } as FileTask
  const model = { tasks: [relay], finishCancel: true, failRead: false, afterStop: () => {}, stoppedWatch: 0 }
  let state: SessionCloseState = { review: null, error: '', busy: false }
  const closer = createSessionCloser({
    files: {
      available: () => true,
      tasks: async () => {
        if (model.failRead) throw Error('offline')
        return { tasks: [...model.tasks], concurrency: 2 }
      },
      stopDirectorySync: async id => { events.push('stop ' + id); model.afterStop() },
      control: async (id, action) => {
        events.push(action + ' ' + id)
        if (model.finishCancel) model.tasks = model.tasks.filter(t => t.id !== id)
      },
      watch: onTasks => { onTasks(model.tasks); return () => { model.stoppedWatch++ } },
    },
    participants: ids => panes.filter(p => ids.includes(p.id)),
    release: ids => events.push('close ' + ids.join(',')),
    publish: next => { state = next },
    report: () => {},
  }, 15)
  return { model, events, panes, closer, state: () => state, confirm: (save = false) => closer.confirm(state.review!, save) }
}

test('both relay endpoints require confirmation, dismiss leaves the transfer alone, unrelated pane closes', async () => {
  const f = fixture()
  for (const id of ['A', 'B']) {
    await f.closer.request([id], false)
    expect(f.state().review?.panes[0]?.tasks).toBe(1)
    f.closer.dismiss()
  }
  expect(f.events).toEqual([])
  await f.closer.request(['C'], false)
  expect(f.events).toEqual(['stop C', 'close C'])
  expect(f.model.tasks).toHaveLength(1)
})

test('closing a group counts and cancels a relay once, resists a double submit and disposes its watch', async () => {
  const f = fixture()
  await f.closer.request(['A', 'B'], true)
  expect(f.state().review?.panes.map(p => p.tasks)).toEqual([1, 0])
  await Promise.all([f.confirm(), f.confirm()])
  expect(f.events).toEqual(['stop A', 'stop B', 'cancel relay', 'close A,B'])
  expect(f.model.stoppedWatch).toBe(1)
})

test('new or newly edited documents, forwards and tasks need a fresh decision', async () => {
  const f = fixture()
  const b = f.panes[1]!
  await f.closer.request(['B'], false)
  const oldReview = f.state().review!
  let revision = 1
  b.guards.set('document', { getCloseState: () => ({ dirty: true, busy: false, revision }), reveal() {}, saveForClose: async () => true })
  await f.confirm()
  expect(f.state().review?.panes[0]?.dirty).toBe(true)
  expect(f.state().error).not.toBe('')
  await f.closer.confirm(oldReview, false)
  revision++
  b.forwards.push('forward')
  await f.confirm()
  expect(f.state().review?.panes[0]?.forwards).toBe(1)
  expect(f.events).toEqual([])
  f.model.afterStop = () => { f.model.tasks.push({ ...f.model.tasks[0]!, id: 'new' }) }
  await f.confirm()
  expect(f.events).toEqual(['stop B'])
  expect(f.state().review?.panes[0]?.tasks).toBe(2)
})

test('save failure reveals the editor and releases no shell or transfer', async () => {
  const f = fixture()
  f.panes[1]!.guards.set('document', {
    getCloseState: () => ({ dirty: true, busy: false, path: '/a' }),
    reveal: () => f.events.push('reveal'), saveForClose: async () => { f.events.push('save'); return false },
  })
  await f.closer.request(['B'], false)
  await f.confirm(true)
  expect(f.events).toEqual(['save', 'reveal'])
  expect(f.state().review).toBeNull()
  expect(f.state().busy).toBe(false)
})

test('read failure and cancellation timeout keep SSH alive, then retry uses the remaining work', async () => {
  const f = fixture()
  f.model.failRead = true
  await f.closer.request(['B'], false)
  expect(f.events).toEqual([])
  f.model.failRead = false
  f.model.finishCancel = false
  await f.closer.request(['B'], false)
  await f.confirm()
  expect(f.events).toEqual(['stop B', 'cancel relay'])
  expect(f.state().error).not.toBe('')
  expect(f.model.stoppedWatch).toBe(1)
  f.model.tasks = []
  await f.confirm()
  expect(f.events).toEqual(['stop B', 'cancel relay', 'stop B', 'close B'])
})

test('stopping an idle directory sync requires explicit confirmation even with an empty transfer queue', async () => {
  const f = fixture()
  f.model.tasks = []
  f.panes[0]!.guards.set('sync', { getCloseState: () => ({ dirty: false, busy: false, syncing: true }), saveForClose: async () => true, reveal() {} })
  await f.closer.request(['A'], false)
  expect(f.state().review?.panes[0]?.syncing).toBe(true)
  expect(f.events).toEqual([])
})

test('save and close finishes document saves before cancelling other work on that pane', async () => {
  const f = fixture()
  let importing = true, dirty = true, allowSave = false
  const guards = f.panes[1]!.guards
  guards.set('import', {
    getCloseState: () => ({ busy: importing, dirty: false }), reveal() {}, saveForClose: async () => true,
    cancelForClose: async () => { importing = false; f.events.push('cancel import') },
  })
  guards.set('document', {
    getCloseState: () => ({ busy: false, dirty }), reveal: () => f.events.push('reveal'),
    saveForClose: async () => { f.events.push('save'); if (allowSave) dirty = false; return allowSave },
  })
  await f.closer.request(['B'], false)
  await f.confirm(true)
  expect(f.events).toEqual(['save', 'reveal'])
  expect(importing).toBe(true)
  allowSave = true
  await f.closer.request(['B'], false)
  await f.confirm(true)
  expect(f.events.slice(2)).toEqual(['save', 'cancel import', 'stop B', 'cancel relay', 'close B'])
})

test('a guard still running after its cancellation and an in-flight transfer both keep the shell alive', async () => {
  const f = fixture()
  const guards = f.panes[1]!.guards
  guards.set('import', {
    getCloseState: () => ({ busy: true, dirty: false }), reveal() {}, saveForClose: async () => true,
    cancelForClose: async () => { f.events.push('cancel import') },
  })
  await f.closer.request(['B'], false)
  await f.confirm()
  expect(f.events).toEqual(['cancel import'])
  expect(f.state().error).not.toBe('')
  guards.clear()
  f.model.tasks[0] = { ...f.model.tasks[0]!, state: 'cancelling' }
  await f.confirm()
  expect(f.events).toEqual(['cancel import', 'stop B'])
  expect(f.state().error).not.toBe('')
  expect(f.model.stoppedWatch).toBe(1)
})

test('unmount aborts an outstanding watch and ignores its late completion', async () => {
  const f = fixture()
  f.model.finishCancel = false
  await f.closer.request(['B'], false)
  const pending = f.confirm()
  while (!f.events.includes('cancel relay')) await Promise.resolve()
  await Promise.resolve()
  f.closer.reset()
  await pending
  expect(f.model.stoppedWatch).toBe(1)
  expect(f.events).not.toContain('close B')
})
