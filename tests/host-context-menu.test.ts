import { expect, test } from 'bun:test'
import { parseHTML } from 'linkedom'
import { hostCopyActions, hostRowContextActions } from '../frontend/src/features/connections/hostContextMenu'
import { initialHosts } from '../frontend/src/features/workspace/mockGateway'
import { tr } from '../frontend/src/shared/i18n/messages'

const hosts = [
  { ...initialHosts[0]!, id: 'selected', name: 'First', address: 'first.example.test' },
  { ...initialHosts[0]!, id: 'clicked', name: '東京 · Logs', address: '2001:db8::8', port: 2202 },
]
const globalActions = { newConnection: { label: 'New connection' }, settings: { label: 'Settings' } }

async function withClipboard(writeText: (value: string) => Promise<void>, run: () => Promise<void>) {
  const previous = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  try { await run() } finally {
    if (previous) Object.defineProperty(navigator, 'clipboard', previous)
    else Reflect.deleteProperty(navigator, 'clipboard')
  }
}

test('a nested right-click target copies the addressed row even when another row is selected', async () => {
  const { document } = parseHTML('<ul><li data-host-context-id="selected" aria-selected="true"></li><li data-host-context-id="clicked"><strong>label</strong></li></ul>')
  const written: string[] = []
  const notices: string[] = []
  const actions = hostRowContextActions(document.querySelector('strong')!, hosts, globalActions, host => hostCopyActions(host, (_, kind) => notices.push(kind)))
  expect(actions.map(item => 'label' in item ? item.label : item.type)).toEqual([
    tr('复制主机地址'), tr('复制主机名称'), 'divider', 'New connection',
  ])
  expect(written).toBeEmpty()
  await withClipboard(async value => { written.push(value) }, async () => {
    for (const action of actions.slice(0, 2)) {
      if (!('onClick' in action) || !action.onClick) throw new Error('Missing copy action')
      await action.onClick()
    }
  })
  expect(written).toEqual(['2001:db8::8', '東京 · Logs'])
  expect(notices).toEqual(['success', 'success'])
  expect(document.querySelector('[aria-selected="true"]')?.getAttribute('data-host-context-id')).toBe('selected')
})

test('blank space, groups, other lists and stale rows fall through to their own menus', () => {
  const { document } = parseHTML('<main><button data-group-filter-value="group:ops"></button><li data-row-key="clicked"></li><li data-host-context-id="deleted"></li></main>')
  for (const element of document.querySelectorAll('main,button,li')) {
    expect(hostRowContextActions(element, hosts, globalActions, () => [])).toEqual([globalActions.newConnection, globalActions.settings])
  }
})

test('clipboard rejection shows an error and a missing address disables only that action', async () => {
  const notices: string[] = []
  const actions = hostCopyActions({ ...hosts[0]!, address: '' }, (message, kind) => notices.push(kind, message))
  expect(actions[0]!.isDisabled).toBe(true)
  await withClipboard(async () => { throw new Error('Clipboard denied') }, async () => {
    await actions[1]!.onClick()
  })
  expect(notices).toEqual(['error', tr('复制失败，请重试。')])
})
