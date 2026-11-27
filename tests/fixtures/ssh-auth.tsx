import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { flushSync } from 'react-dom'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import SessionConnectionPanel from '../../frontend/src/features/connections/SessionConnectionPanel'
import type { LiveSSH } from '../../frontend/src/features/connections/useSSHSessions'
import type { SSHTransport } from '../../frontend/src/shared/bridge/ssh'
import { initialHosts } from '../../frontend/src/features/workspace/mockGateway'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'

Object.assign(globalThis, { mygo: {} })
const counters = { connected: 0, disconnected: 0 }
const transport: SSHTransport = { async *read() {}, send: () => false, sendBinary: () => false, resize: () => {}, close: () => {}, confirm: async () => true }
const connected = (): LiveSSH => ({ attempt: 1, keyId: '', transport, phase: 'connected', message: '', code: '' })
let setPhase: (phase: LiveSSH['phase']) => void = () => {}
function Fixture() {
  const [live, setLive] = useState<LiveSSH>(connected)
  setPhase = phase => flushSync(() => setLive({ ...connected(), phase, challenge: phase === 'hostkey' ? { id: 'test-only', address: '127.0.0.1:22', algorithm: 'ssh-ed25519', fingerprint: 'SHA256:test-only' } : undefined }))
  return <Theme theme={workbenchTheme} mode="light"><Stack width={320} height="100dvh">
    <Text>Isolated auth regression — no SSH service</Text>
    <SessionConnectionPanel host={initialHosts[0]!} live={live} active keys={[]} keyError="" onChooseKey={async () => null}
      onConnect={() => { counters.connected++; setLive({ ...connected(), phase: 'connecting' }) }}
      onDisconnect={() => { counters.disconnected++; setPhase('disconnected') }} />
  </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture />)
Object.assign(window, { async runAuthRegression() {
  const result: Record<string, boolean> = {}
  for (const phase of ['connected', 'connecting', 'hostkey'] as const) {
    setPhase(phase)
    const before = counters.connected
    const button = [...document.querySelectorAll<HTMLButtonElement>('#session-auth-form button')].find(button => button.textContent === (phase === 'connected' ? '断开连接' : '取消连接'))!
    button.click()
    await new Promise(resolve => setTimeout(resolve, 20))
    result[`${phase}DoesNotSubmit`] = counters.connected === before
  }
  return { ...result, disconnectCalls: counters.disconnected, accidentalConnectCalls: counters.connected }
} })
