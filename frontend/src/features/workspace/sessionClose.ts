import type { fileGateway, FileTask } from '../../shared/bridge/files'
import { tr } from '../../shared/i18n/messages'
import type { FileCloseGuard } from './fileCloseGuard'
import { transfersForClose } from './transferModel'

type Files = Pick<typeof fileGateway, 'available' | 'tasks' | 'control' | 'watch' | 'stopDirectorySync'>
export interface CloseParticipant {
  id: string
  name: string
  attempt: number
  guards: Map<string, FileCloseGuard>
  forwards: string[]
}
export interface ClosingPane {
  id: string
  name: string
  dirty: boolean
  path: string
  tasks: number
  forwards: number
  syncing: boolean
}
interface CloseReview {
  ids: string[]
  group: boolean
  panes: ClosingPane[]
  scope: Set<string>
  needsConfirmation: boolean
}
export interface SessionCloseState {
  review: CloseReview | null
  busy: boolean
  error: string
}

function assess(participants: CloseParticipant[], tasks: FileTask[], group: boolean): CloseReview {
  const scope = new Set<string>()
  const counted = new Set<string>()
  let needsConfirmation = group && participants.length > 1
  const panes = participants.map(({ id, name, guards, forwards, attempt }) => {
    const paths: string[] = []
    let dirty = false, syncing = false, taskCount = 0
    scope.add(JSON.stringify([id, 'session', attempt]))
    for (const forward of forwards) scope.add(JSON.stringify([id, 'forward', forward]))
    for (const task of transfersForClose(tasks, [id])) {
      scope.add(JSON.stringify([id, 'transfer', task.id]))
      if (!counted.has(task.id)) { counted.add(task.id); taskCount++ }
    }
    for (const [key, guard] of guards) {
      const state = guard.getCloseState()
      if (state.dirty) {
        dirty = true
        if (state.path) paths.push(state.path)
        scope.add(JSON.stringify([id, key, 'dirty', state.revision, state.path]))
      }
      if (state.busy) {
        needsConfirmation = true
        if (guard.cancelForClose) taskCount++
        scope.add(JSON.stringify([id, key, 'busy', state.revision]))
      }
      if (state.syncing) { syncing = true; scope.add(JSON.stringify([id, key, 'sync'])) }
    }
    if (dirty || syncing || taskCount || forwards.length) needsConfirmation = true
    return { id, name, dirty, syncing, tasks: taskCount, forwards: forwards.length,
      path: paths.join(' · ') || tr('未保存的工作区内容') }
  })
  return { ids: panes.map(pane => pane.id), group, panes, scope, needsConfirmation }
}

async function waitForCancellation(files: Files, ids: Set<string>, signal: AbortSignal, timeout: number) {
  let stop: (() => void) | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let aborted: (() => void) | undefined
  try {
    await new Promise<void>((resolve, reject) => {
      aborted = () => reject(signal.reason)
      signal.throwIfAborted()
      signal.addEventListener('abort', aborted, { once: true })
      timer = setTimeout(() => reject(Error(tr('传输任务正在取消，请稍后再次关闭；连接已保留。'))), timeout)
      stop = files.watch(tasks => {
        if (!tasks.some(task => ids.has(task.id) && task.state !== 'cancelled' && task.state !== 'completed')) resolve()
      }, () => reject(Error(tr('传输状态连接中断，SSH 连接已保留，请重试关闭。'))))
    })
  } finally {
    clearTimeout(timer)
    if (aborted) signal.removeEventListener('abort', aborted)
    stop?.()
  }
}

export function createSessionCloser(options: {
  files: Files
  participants(ids: string[]): CloseParticipant[]
  release(ids: string[]): void
  publish(state: SessionCloseState): void
  report(message: string): void
}, timeout = 6500) {
  const { files, participants, publish, report } = options
  let active: AbortController | undefined
  let review: CloseReview | null = null
  let busy = false

  let lastError = ''
  function show(next: CloseReview | null, error = '') {
    lastError = error
    review = next
    publish({ review, busy, error })
  }
  async function readTasks(signal: AbortSignal) {
    try {
      const tasks = files.available() ? (await files.tasks()).tasks : []
      signal.throwIfAborted()
      return tasks
    } catch {
      signal.throwIfAborted()
      throw Error(tr('传输状态读取失败，连接已保留，请稍后重试关闭。'))
    }
  }
  function failed(cause: unknown) {
    const message = cause instanceof Error ? cause.message : tr('关闭未完成，连接已保留。')
    show(review, message)
    report(message)
  }
  async function execute(approved: CloseReview, save: boolean, signal: AbortSignal) {
    busy = true
    show(review)
    function check(tasks: FileTask[]) {
      signal.throwIfAborted()
      const next = assess(participants(approved.ids), tasks, approved.group)
      if ([...next.scope].some(key => !approved.scope.has(key))) {
        show(next, tr('关闭范围已变化，请核对后再次确认。'))
        return false
      }
      return true
    }
    try {
      let tasks = await readTasks(signal)
      if (!check(tasks)) return
      const guards = participants(approved.ids).flatMap(pane =>
        Array.from(pane.guards.keys(), key => () => participants([pane.id])[0]?.guards.get(key)))
      for (const current of guards) {
        const guard = current()
        if (!guard) continue
        const state = guard.getCloseState()
        if (state.busy && (!guard.cancelForClose || (save && state.dirty))) {
          throw Error(tr('文件操作仍在进行，请完成后再关闭。'))
        }
      }
      if (save) {
        for (const current of guards) {
          if (!check(tasks)) return
          const guard = current()
          if (guard?.getCloseState().dirty && !await guard.saveForClose()) {
            signal.throwIfAborted()
            show(null)
            guard.reveal()
            throw Error(tr('文件未保存，请先处理编辑器中的错误或冲突。'))
          }
        }
      }
      for (const current of guards) {
        if (!check(tasks)) return
        const guard = current()
        if (!guard?.getCloseState().busy) continue
        await guard.cancelForClose?.()
        if (current()?.getCloseState().busy) throw Error(tr('文件操作仍在进行，请完成后再关闭。'))
      }
      if (!check(tasks)) return
      if (files.available()) {
        for (const id of approved.ids) {
          signal.throwIfAborted()
          await files.stopDirectorySync(id)
        }
        tasks = await readTasks(signal)
        if (!check(tasks)) return
        const cancellations = transfersForClose(tasks, approved.ids)
        for (const task of cancellations) {
          signal.throwIfAborted()
          if (task.state !== 'cancelling') await files.control(task.id, 'cancel')
        }
        if (cancellations.length) {
          await waitForCancellation(files, new Set(cancellations.map(t => t.id)), signal, timeout)
          tasks = await readTasks(signal)
          if (!check(tasks)) return
          if (transfersForClose(tasks, approved.ids).length) {
            show(assess(participants(approved.ids), tasks, approved.group), tr('关闭范围已变化，请核对后再次确认。'))
            return
          }
        }
      }
      if (!check(tasks)) return
      options.release(approved.ids)
      show(null)
    } catch (cause) {
      if (!signal.aborted) failed(cause)
    } finally {
      if (!signal.aborted) {
        busy = false
        publish({ review, busy, error: lastError })
      }
    }
  }
  async function request(ids: string[], group: boolean) {
    if (busy) return
    active?.abort()
    active = new AbortController()
    const { signal } = active
    try {
      const tasks = await readTasks(signal)
      if (signal.aborted) return
      const next = assess(participants([...new Set(ids)]), tasks, group)
      if (!next.ids.length) return
      if (next.needsConfirmation) show(next)
      else await execute(next, false, signal)
    } catch (cause) { if (!signal.aborted) failed(cause) }
  }
  function reset() {
    active?.abort()
    active = undefined
    review = null
    busy = false
  }
  return {
    request,
    confirm: (displayed: CloseReview, save: boolean) => !busy && review === displayed && active
      ? execute(displayed, save, active.signal) : Promise.resolve(),
    dismiss: () => { if (!busy) { reset(); show(null) } },
    reset,
  }
}
