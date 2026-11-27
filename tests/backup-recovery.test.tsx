import { afterEach, expect, spyOn, test } from 'bun:test'
import { act } from 'react'
import { document, dom, installWorkbenchDom } from './fixtures/workbench-dom'
import type { RecoveredWorkspace } from '../frontend/src/shared/generated/mygo'

installWorkbenchDom()
const originalMode = process.env.MODE
afterEach(() => {
  if (originalMode === undefined) delete process.env.MODE
  else process.env.MODE = originalMode
})

function button(label: string) {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(node => node.textContent?.trim() === label)
}

async function enterPassword() {
  const input = document.querySelector<HTMLInputElement>('input[name="backup-password"]')!
  const set = Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    set.call(input, 'disposable-fixture')
    input.dispatchEvent(new dom.Event('input', { bubbles: true }))
  })
}

async function openImport(mode: string) {
  process.env.MODE = mode
  const { createRoot } = await import('react-dom/client')
  const { default: Panel } = await import('../frontend/src/features/workspace/BackupSettingsPanel')
  const root = createRoot(document.querySelector('main')!)
  await act(async () => root.render(<Panel/>))
  await act(async () => button('导入配置…')!.click())
  return () => act(async () => root.unmount())
}

const selection = { id: 'selected-by-picker', name: 'recovery.yaare', size: 1024 }
test('independent recovery retains the original workspace while the picker is busy or cancelled', async () => {
  let finish: (result: RecoveredWorkspace) => void = () => {}
  const rpc = spyOn(globalThis.mygo!, 'callWith').mockImplementation(async (method, args) => {
    if (method === 'BackupService.ChooseImport') return selection
    if (method === 'BackupService.Discard') return
    if (method === 'BackupService.RestoreNewWorkspace') {
      expect(args).toEqual([selection.id, 'disposable-fixture'])
      return new Promise<RecoveredWorkspace>(resolve => { finish = resolve })
    }
    throw Error('Unexpected RPC ' + method)
  })
  const close = await openImport('production')
  try {
    const opener = button('导入配置…')!
    let restoredFocus = false
    opener.addEventListener('focus', () => { restoredFocus = true })
    const restore = button('恢复到新工作区')!
    expect(restore.getAttribute('aria-disabled')).toBe('true')
    await enterPassword()
    await act(async () => restore.click())
    expect(restore.getAttribute('aria-disabled')).toBe('true')
    await act(async () => { restore.click(); finish({ directory: '', launched: false }) })
    expect(document.querySelector('dialog[open]')?.textContent).toContain(selection.name)
    expect(restore.getAttribute('aria-disabled')).toBe('true')
    await enterPassword()
    await act(async () => restore.click())
    await act(async () => finish({ directory: '/fixture/Yaare-Recovery-test', launched: true }))
    expect(document.querySelector('main')?.textContent).toContain('/fixture/Yaare-Recovery-test')
    expect(restoredFocus).toBe(true)
    expect(rpc.mock.calls.map(([method]) => method)).toEqual([
      'BackupService.ChooseImport', 'BackupService.RestoreNewWorkspace', 'BackupService.RestoreNewWorkspace',
    ])
  } finally { await close(); rpc.mockRestore() }
})

test('Docker retains the regular import preview and hides opening a desktop workspace', async () => {
  const rpc = spyOn(globalThis.mygo!, 'callWith').mockImplementation(async method => {
    if (method === 'BackupService.ChooseImport') return selection
    if (method === 'BackupService.Discard') return
    throw Error('Unexpected RPC ' + method)
  })
  const close = await openImport('browser')
  try {
    expect(button('解锁并检查')).toBeDefined()
    expect(button('恢复到新工作区')).toBeUndefined()
  } finally { await close(); rpc.mockRestore() }
})
