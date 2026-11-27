import { expect, mock, test } from 'bun:test'
import { act } from 'react'
import type { ComponentProps } from 'react'
import type EditorDialog from '../frontend/src/shared/ui/EditorDialog'
import type { HostProfile } from '../frontend/src/features/workspace/mockGateway'
import { document, dom, host, installWorkbenchDom } from './fixtures/workbench-dom'

installWorkbenchDom()

mock.module('../frontend/src/shared/ui/EditorDialog', () => ({
  default: ({ formProps, children, navigation, actions }: ComponentProps<typeof EditorDialog>) =>
    <form {...formProps}>{navigation}{children}{actions}</form>,
}))

async function typeField(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  const nativeSetter = Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    nativeSetter.call(input, value)
    input.dispatchEvent(new dom.Event('input', { bubbles: true }))
  })
}

for (const proxy of [false, true]) {
  test(`after save fails the editor focuses and requires only the cleared ${proxy ? 'proxy' : 'login'} password`, async () => {
    const { createRoot } = await import('react-dom/client')
    const { default: ConnectionEditor } = await import('../frontend/src/features/connections/ConnectionEditor')
    const saved: HostProfile = { ...host, authentication: 'password', useAgent: false,
      proxy: proxy ? { kind:'socks5', address:'proxy.example.test', port:1080, username:'tester', useAuthentication:true } : undefined }
    const root = createRoot(document.querySelector('main')!)
    const fieldName = proxy ? 'proxy-password' : 'ssh-secret'
    const label = proxy ? '代理密码' : '登录密码'
    const accepted: boolean[] = []
    let focusEvents = 0
    const submit = () => act(async () => {
      document.querySelector('form')!.dispatchEvent(new dom.Event('submit', { bubbles: true, cancelable: true }))
    })
    try {
      await act(async () => root.render(<ConnectionEditor host={saved} hosts={[saved]} groups={['未分组']}
        revision={2} initialRevision={2} keys={[]} keyError="" choosingKey={false} onChooseKey={async () => null}
        storageError="The configuration is busy" onClose={() => {}} onSave={async (candidate, _, credential) => {
          expect(candidate.name).toBe('New display name')
          accepted.push((proxy ? credential?.proxyPassword : credential?.password) === 'test-only-value')
          return accepted.length > 1
        }}/>))
      const field = document.querySelector<HTMLInputElement>(`[name="${fieldName}"]`)!
      field.addEventListener('focus', () => { focusEvents++ })
      await typeField('name', 'New display name')
      await typeField(fieldName, 'test-only-value')
      await submit()
      expect(accepted).toEqual([true])
      expect(field.value).toBe('')
      expect(document.querySelector('main')!.textContent).toContain('The configuration is busy')
      expect(document.querySelector('main')!.textContent).toContain('请重新输入：'+label)
      expect(document.querySelector('#connection-panel-'+(proxy?'network':'basic'))?.hasAttribute('inert')).toBe(false)
      expect(focusEvents).toBeGreaterThan(0)
      await submit()
      expect(accepted).toHaveLength(1)
      await typeField(fieldName, 'test-only-value')
      await submit()
      expect(accepted).toEqual([true,true])
      expect(document.querySelector('main')!.textContent).not.toContain('请重新输入')
    } finally { await act(async () => root.unmount()) }
  })
}
