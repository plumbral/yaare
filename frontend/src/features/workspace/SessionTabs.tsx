import { Stack } from '@astryxdesign/core/Stack'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { CommandLineIcon, ServerStackIcon, KeyIcon } from '@heroicons/react/24/outline'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { managementPages, shortcutAria, shortcutLabel, shortcutTooltip, useShortcutOverrides, type ManagementPage } from './shortcutReference'

export type WorkspaceView = 'hosts' | 'identities' | 'commands' | 'settings' | 'terminal'
const toolIcons = { hosts: ServerStackIcon, identities: KeyIcon, commands: CommandLineIcon }

export function WorkspaceNavigation({ view, compact, showShortcuts = false, onView }: {
  view: WorkspaceView; compact: boolean; showShortcuts?: boolean; onView: (view: ManagementPage) => void
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()
  const index = managementPages.findIndex(page => page === view)
  const offset = `calc(${Math.max(index, 0)} * (var(--size-element-md) + var(--spacing-1)))`
  return <Stack as="nav" aria-label={tr("工作区导航")} direction={compact?'vertical':'horizontal'} gap={1} align="center"
    style={{position:'relative',width:'max-content',flexShrink:0}}>
    <Stack aria-hidden="true" data-management-indicator="" style={{opacity:index < 0 ? 0 : 1,
      transform:compact ? `translateY(${offset})` : `translateX(${offset})`}}/>
    {managementPages.map(page=><IconButton key={page} id={`tab-${page}`} label={shortcutLabel(page)} tooltip={showShortcuts ? shortcutTooltip(shortcutLabel(page),page,shortcuts) : shortcutLabel(page)} size="md"
      variant="ghost" style={{position:'relative'}} aria-current={view===page?'page':undefined} aria-controls={`panel-${page}`}
      aria-keyshortcuts={showShortcuts ? shortcutAria(page,shortcuts) : undefined} icon={<Icon icon={toolIcons[page]} size="sm"/>} onClick={()=>onView(page)}/>)}
  </Stack>
}
