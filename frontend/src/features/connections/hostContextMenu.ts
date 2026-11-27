import type { ContextMenuOption } from '@astryxdesign/core/ContextMenu'
import { writeClipboardText } from '../../shared/bridge/application'
import { tr } from '../../shared/i18n/messages'
import type { HostProfile } from '../workspace/mockGateway'

type Notice = (message: string, kind: 'success' | 'error') => void

export function hostCopyActions({ address, name }: HostProfile, notice: Notice) {
  async function copy(value: string, confirmation: string) {
    try {
      await writeClipboardText(value)
      notice(confirmation, 'success')
    } catch {
      notice(tr('复制失败，请重试。'), 'error')
    }
  }
  return [
    { label: tr('复制主机地址'), isDisabled: !address, onClick: () => copy(address, tr('已复制地址')) },
    { label: tr('复制主机名称'), onClick: () => copy(name, tr('已复制名称')) },
  ]
}

export function hostRowContextActions(
  target: Element,
  hosts: readonly HostProfile[],
  globalActions: { newConnection: ContextMenuOption; settings: ContextMenuOption },
  actionsForHost: (host: HostProfile) => ContextMenuOption[],
): ContextMenuOption[] {
  const row = target.closest<HTMLElement>('[data-host-context-id]')
  const host = row && hosts.find(item => item.id === row.dataset.hostContextId)
  if (!host) return [globalActions.newConnection, globalActions.settings]
  return [...actionsForHost(host), { type: 'divider' }, globalActions.newConnection]
}
