import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { shortcutAria, useShortcutOverrides } from '../workspace/shortcutReference'
import { memo, useCallback, useDeferredValue, useMemo } from 'react'
import { Layout, LayoutHeader, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Stack,StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { PencilSquareIcon } from '@heroicons/react/24/outline'
import type { ListItemProps } from '@astryxdesign/core/List'
import type { HostProfile } from '../workspace/mockGateway'
import { desktopAvailable } from '../../shared/bridge/application'
import OsIcon from '../../shared/icons/OsIcon'
import {PinFilledIcon} from '../../shared/icons/PinIcon'
import {filterHostList, sidebarHostRows, type SidebarHostRow} from './hostOrdering'
import WindowedList from '../../shared/ui/WindowedList'
import PanelCollapseButton from '../../shared/ui/PanelCollapseButton'

type HostRow = SidebarHostRow
const isHost = (row: HostRow) => !('count' in row)
export default memo(function HostSidebar({ hosts, pinOrder = [], selectedId, query, onQuery, onOpen, onEdit,onCollapse }: {
  hosts: HostProfile[];pinOrder?:readonly string[]; selectedId: string; query: string; onQuery: (value: string) => void
  onOpen: (host: HostProfile) => void; onEdit: (host: HostProfile, trigger: HTMLElement) => void
  onCollapse?:()=>void
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()

  const deferredQuery = useDeferredValue(query)
  const filtered = useMemo(() => filterHostList(hosts,deferredQuery,'all','all',pinOrder), [hosts,deferredQuery,pinOrder])
  const rows = useMemo(() => sidebarHostRows(filtered,pinOrder), [filtered,pinOrder])
  const renderRow = useCallback((host: HostRow): ListItemProps => 'count' in host ? {
    startContent:host.pinned?<Icon icon={PinFilledIcon} size="sm"/>:undefined,
    label: <Text type="supporting" color="secondary">{host.pinned?tr("已置顶"):host.group}</Text>, endContent: <Text type="supporting" color="secondary">{host.count}</Text>,
  } : {
    id: `host-${host.id}`, label: host.name, description: `${host.username}@${host.address}${host.port === 22 ? '' : `:${host.port}`}`,
    startContent: <OsIcon name={host.icon} />,
    endContent: <IconButton label={tr("编辑 {p0}", host.name)} tooltip={tr("编辑 {p0}", host.name)} variant="ghost" size="sm"
      icon={<Icon icon={PencilSquareIcon} size="sm" />} onClick={event => { event.stopPropagation(); onEdit(host, event.currentTarget) }} />,
    isSelected: host.id === selectedId, onClick: () => onOpen(host),
  }, [selectedId, onOpen, onEdit, getLanguage()])
  return <Layout height="fill" header={<LayoutHeader padding={0}>
    <Stack gap={2} paddingInline={3} paddingBlock={2}>
      <Stack direction="horizontal" align="center" gap={2}>
        <Text type="label">{tr("服务器")}</Text><StackItem size="fill"><Text type="supporting" color="secondary">{query ? `${filtered.length} / ${hosts.length}` : hosts.length}</Text></StackItem>
        {onCollapse&&<PanelCollapseButton side="start" onCollapse={onCollapse}/>}
      </Stack>
      <search aria-label={tr("搜索服务器")}>
        <TextInput id="host-search" label={tr("搜索服务器")} isLabelHidden type="search" size="md" value={query}
          onChange={onQuery} placeholder={tr("搜索主机或分组")} startIcon="search" hasClear hasSpellCheck={false}
          hasAutoCorrect={false} autoComplete="off" aria-keyshortcuts={shortcutAria('host-search',shortcuts)}
          onKeyDown={event => { if (event.key === 'Escape' && !event.nativeEvent.isComposing) { event.preventDefault(); onQuery('') } }} />
      </search>
      {!filtered.length && <Text type="supporting" color="secondary">{tr("没有匹配的服务器")}</Text>}
    </Stack>
  </LayoutHeader>} content={<LayoutContent padding={0} isScrollable={false} style={{ display: 'flex', minHeight: 0 }}>
    <WindowedList animateReorder items={rows} itemKey={row => row.id} label={tr("服务器配置")} selectedKey={selectedId} resetKey={deferredQuery}
      estimateSize={row => 'count' in row ? 30 : 48} row={renderRow} isItemFocusable={isHost} />
  </LayoutContent>} footer={<LayoutFooter padding={3}>
    <Text type="supporting" color="secondary">{desktopAvailable() ? tr("连接配置保存在本机") : tr("浏览器演示，刷新后重置")}</Text>
  </LayoutFooter>} />
})

