import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { flushSync } from 'react-dom'
import { EditorView } from '@codemirror/view'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import ConnectionEditor from '../../frontend/src/features/connections/ConnectionEditor'
import QuickCommandsPanel from '../../frontend/src/features/workspace/QuickCommandsPanel'
import { initialGroups, initialHosts, type HostProfile } from '../../frontend/src/features/workspace/mockGateway'
import type { Credentials } from '../../frontend/src/features/connections/useSSHSessions'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'

let finish: (success: boolean) => void = () => {}
let calls = 0; let closes = 0
let transientCredentialReceived = false; let profileHasNoSecrets = true
Object.assign(globalThis, { mygo: {} })
let switchView: (view: 'connection' | 'commands') => void = () => {}
function Fixture() {
  const [connectionOpen, setConnectionOpen] = useState(true)
  const [view, setView] = useState<'connection' | 'commands'>('connection')
  switchView = next => flushSync(() => setView(next))
  const pending = (host?: HostProfile, credentials?: Credentials) => {
    calls++
    if (host) profileHasNoSecrets = !['password', 'passphrase', 'privateKey'].some(key => key in host)
    if (credentials) transientCredentialReceived = credentials.password === 'fixture-only-password'
    return new Promise<boolean>(resolve => { finish = resolve })
  }
  return <Theme theme={workbenchTheme} mode="light"><Stack width="calc(var(--spacing-10) * 8)" height="100dvh">
    <Text>隔离回归检查 · 不访问桌面配置</Text>
    {view === 'connection' ? <ConnectionEditor isOpen={connectionOpen} host={initialHosts[0]} groups={initialGroups} onSave={pending} onClose={() => { closes++; setConnectionOpen(false) }} keys={[]} choosingKey={false} keyError="" onChooseKey={async () => null} />
      : <QuickCommandsPanel commands={[]} groups={['未分组']} onApply={() => pending()} targets={[]} onSend={() => false} />}
  </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture />)
const wait = () => new Promise(resolve => setTimeout(resolve, 30))
const button = (label: string) => [...document.querySelectorAll<HTMLButtonElement>('button')].find(b => b.getAttribute('aria-label') === label || b.textContent === label)!
const disabled = (label: string) => button(label).disabled || button(label).getAttribute('aria-disabled') === 'true'
const setInput = (name: string, value: string) => {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
Object.assign(window, { async runConfigRegression() {
  const result: Record<string, boolean> = {}
  switchView('connection'); await wait()
  const form = document.getElementById('connection-editor') as HTMLFormElement
  form.requestSubmit(); await wait()
  result.pendingDisablesSave = disabled('保存')
  form.requestSubmit(); await wait(); result.noDuplicateSubmit = calls === 1
  finish(false); await wait()
  result.failedHostKeepsDraft = document.querySelector<HTMLInputElement>('input[name="name"]')?.value === initialHosts[0]!.name && !!document.querySelector('[role="alert"]') && closes === 0 && !disabled('保存')
  setInput('ssh-secret', 'fixture-only-password'); await wait()
  const secretInput = document.querySelector<HTMLInputElement>('input[name="ssh-secret"]')!
  form.requestSubmit(button('保存并连接')); await wait()
  result.connectUsesTemporaryCredential = transientCredentialReceived && profileHasNoSecrets
  result.secretClearedOnSubmit = secretInput.isConnected && secretInput.value === ''
  finish(false); await wait()
  setInput('name', '修改尚未保存'); await wait(); button('关闭连接编辑').click(); await wait()
  result.hostClosesWithoutConfirmation = closes === 1 && !document.querySelector('dialog[open]') && !button('放弃修改')
  switchView('commands'); await wait(); button('新建命令').click()
  for (let attempt = 0; attempt < 100 && !document.getElementById('command-code-input'); attempt++) await wait()
  setInput('command-name', 'keep draft'); await wait()
  const editor = EditorView.findFromDOM(document.getElementById('command-code-input')!)!
  const commandSource = '  printf ok\n\n\tprintf done\n'
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: commandSource } }); await wait()
  document.querySelector<HTMLFormElement>('form')!.requestSubmit(); await wait(); finish(false); await wait()
  result.failedCommandKeepsDraft = editor.state.doc.toString() === commandSource && !!document.querySelector('[role="alert"]')
  document.querySelector('dialog[open]')!.dispatchEvent(new Event('cancel', { cancelable: true })); await wait()
  result.commandClosesWithEscape = !document.querySelector('dialog[open]') && !button('放弃修改')
  return result
} })
