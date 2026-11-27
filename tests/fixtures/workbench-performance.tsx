import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import HostSidebar from '../../frontend/src/features/connections/HostSidebar'
import TerminalPane from '../../frontend/src/features/terminal/TerminalPane'
import { initialHosts } from '../../frontend/src/features/workspace/mockGateway'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'
import type { TerminalTransport } from '../../frontend/src/features/terminal/remoteOutput'

const hosts = Array.from({ length: 10000 }, (_, index) => ({
  ...initialHosts[0]!, id: `stress-${index}`, name: `Test host ${index}`, group: `Group ${Math.floor(index / 1000)}`,
}))
const frame = () => new Promise<number>(resolve => requestAnimationFrame(resolve))
const metrics = { ready: false, received: 0, completed: false, errors: 0, rows: 10000, elapsed: 0, frames: 0, sent: [] as string[] }
const encoder = new TextEncoder()
let releaseStart: () => void = () => {}
const startGate = new Promise<void>(resolve => { releaseStart = resolve })
const transport: TerminalTransport = {
  async *read(signal) {
    await startGate
    if (signal.aborted) return
    const start = performance.now()
    for (let offset = 0; offset < 100000 && !signal.aborted; offset += 256) {
      const count = Math.min(256, 100000 - offset)
      yield encoder.encode(Array.from({ length: count }, (_, line) => `remote-line-${offset + line}\r\n`).join(''))
      metrics.received += count
    }
    metrics.elapsed = performance.now() - start; metrics.completed = true
  },
  send: data => { metrics.sent.push(data); return true }, resize: () => {},
}
function Fixture() {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState('stress-0')
  return <Theme theme={workbenchTheme} mode="dark"><Stack height="100dvh" style={{ background: 'var(--color-background-body)' }}>
    <Text>Isolated performance fixture — synthetic data only</Text>
    <StackItem size="fill" style={{ display: 'flex', minHeight: 0 }}>
      <Stack width={320} style={{ minHeight: 0, flexShrink: 0 }}>
        <HostSidebar hosts={hosts} query={query} onQuery={setQuery} selectedId={selected} onOpen={host => setSelected(host.id)} onEdit={() => {}} />
      </Stack>
      <TerminalPane id="stress" host={initialHosts[0]!} active mode="dark" fontSize={14} scrollback={1000} cursorBlink={false}
        transport={transport} onReady={() => { metrics.ready = true }} onError={() => { metrics.errors++ }} />
    </StackItem>
  </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture />)
Object.assign(window, {
  stressMetrics: metrics,
  async runStress() {
    while (!metrics.ready) await frame()
    releaseStart()
    const times: number[] = []; let last = await frame(); let maxMounted = 0
    const list = document.querySelector<HTMLElement>('[aria-label="服务器配置"]')!
    let longTasks = 0; let observer: PerformanceObserver | undefined
    if (PerformanceObserver.supportedEntryTypes.includes('longtask')) { observer = new PerformanceObserver(entries => { longTasks += entries.getEntries().length }); observer.observe({ type: 'longtask' }) }
    const start = performance.now()
    for (let step = 0; step < 240; step++) {
      list.scrollTop = step < 120 ? step * 180 : (240 - step) * 180
      const now = await frame(); times.push(now - last); last = now
      metrics.frames = step + 1
      maxMounted = Math.max(maxMounted, document.querySelectorAll('[aria-label="服务器配置"] li').length)
    }
    const elapsed = performance.now() - start
    observer?.disconnect()
    const ordered = [...times].sort((a, b) => a - b)
    const result = { ...metrics, visibility: document.visibilityState, elapsedScroll: elapsed, maxMounted,
      p95Frame: ordered[Math.floor(ordered.length * .95)], maxFrame: Math.max(...times), framesOver33ms: times.filter(ms => ms > 33.4).length, longTasks }
    Object.assign(window, { stressResult: result })
    return result
  },
})

