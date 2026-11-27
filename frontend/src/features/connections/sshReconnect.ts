export type ReconnectWait = 'waiting' | 'offline'

const retryable = new Set([
  'CONNECTION_LOST', 'CONNECT_TIMEOUT', 'CONNECTION_TIMEOUT', 'CONNECTION_REFUSED',
  'NETWORK_UNREACHABLE', 'DNS_FAILED', 'PROXY_DNS_FAILED', 'PROXY_CONNECTION_FAILED',
  'PROXY_TARGET_UNREACHABLE', 'PROXY_TARGET_REFUSED', 'PROXY_TIMEOUT', 'IO_FAILED',
])

export const canReconnect = (code: string) => retryable.has(code)

const stableConnectionMs = 30_000
const maxAutomaticRetries = 5

export function reconnectAttempt(retries: number, connectedMs: number | undefined): number | undefined {
  const used = connectedMs !== undefined && connectedMs >= stableConnectionMs ? 0 : retries
  return used < maxAutomaticRetries ? used : undefined
}

export function reconnectDelay(attempts: number, random = Math.random()) {
  return Math.round(Math.min(60, 2 ** Math.max(0, attempts)) * 1000 * (1 - random * 0.15))
}

export function createReconnectQueue({ blocked, changed, setTimer = setTimeout, clearTimer = clearTimeout }: {
  blocked: () => Exclude<ReconnectWait, 'waiting'> | undefined
  changed: (id: string, wait?: ReconnectWait) => void
  setTimer?: (run: () => void, delay: number) => ReturnType<typeof setTimeout>
  clearTimer?: (timer: ReturnType<typeof setTimeout>) => void
}) {
  type Job = { run: () => void; timer?: ReturnType<typeof setTimeout>; wait?: ReconnectWait }
  const jobs = new Map<string, Job>()

  const stop = (id: string) => {
    const job = jobs.get(id)
    if (!job) return
    jobs.delete(id)
    if (job.timer !== undefined) clearTimer(job.timer)
    changed(id)
  }
  const arm = (id: string, job: Job, delay: number) => {
    if (job.timer !== undefined) clearTimer(job.timer)
    job.timer = undefined
    const reason = blocked()
    const wait = reason ?? 'waiting'
    if (job.wait !== wait) {
      job.wait = wait
      changed(id, wait)
    }
    if (reason) return
    job.timer = setTimer(() => {
      if (jobs.get(id) !== job) return
      job.timer = undefined
      if (blocked()) { arm(id, job, 0); return }
      stop(id)
      job.run()
    }, delay)
  }

  return {
    schedule(id: string, attempts: number, run: () => void) {
      stop(id)
      const job: Job = { run }
      jobs.set(id, job)
      arm(id, job, reconnectDelay(attempts))
    },
    refresh() {
      const wait = blocked() ?? 'waiting'
      for (const [id, job] of jobs) {
        if (wait !== job.wait) arm(id, job, Math.round(Math.random() * 750))
      }
    },
    tryNow(id: string) {
      const job = jobs.get(id)
      if (job) arm(id, job, 0)
    },
    stop,
    clear() { for (const id of jobs.keys()) stop(id) },
  }
}
