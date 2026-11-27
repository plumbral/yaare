import { useMemo } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { List, ListItem } from '@astryxdesign/core/List'
import { Popover } from '@astryxdesign/core/Popover'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { createReorderable } from '@astryxdesign/core'
import { EyeIcon, EyeSlashIcon, ViewColumnsIcon } from '@heroicons/react/24/outline'
import { tr } from '../../shared/i18n/messages'
import type { HostColumn, HostSortKey, HostView } from './hostColumns'
import './hostColumnPicker.css'

export function hostColumnLabel(key: HostSortKey) {
  switch (key) {
    case 'name': return tr("主机")
    case 'group': return tr("分组")
    case 'address': return tr("地址")
    case 'port': return tr("端口")
    case 'username': return tr("用户名")
    case 'system': return tr("操作系统")
    case 'status': return tr("当前状态")
    case 'lastUsed': return tr("最近连接")
    case 'authentication': return tr("认证方式")
    case 'notes': return tr("备注")
  }
}

export default function HostColumnPicker({ columns, open, setOpen, onVisibility, onMove, onReset }: {
  columns: HostView['columns']
  open: boolean
  setOpen: (open: boolean) => void
  onVisibility: (key: HostColumn, visible: boolean) => void
  onMove: (key: HostColumn, to: number) => void
  onReset: () => void
}) {
  const label = tr("显示列")
  const reorder = useMemo(() => createReorderable<HostColumn>({ onReorder: ({ key, to }) => onMove(key, to) }), [onMove])
  return <Popover label={label} isOpen={open} onOpenChange={setOpen} alignment="end" isModal={false}
    width="calc(var(--spacing-10) * 5.75)" padding={2} closeButtonLabel={tr("关闭")}
    content={<Stack gap={1}>
      <Stack direction="horizontal" align="center" justify="between" gap={1}
        style={{ paddingInlineStart: 'var(--spacing-2)', borderBlockEnd: 'var(--border-width) solid var(--color-border)' }}>
        <Text type="label">{label}</Text>
        <Button label={tr("恢复默认")} variant="ghost" size="sm" onClick={onReset} />
      </Stack>
      <Stack isScrollable style={{ maxHeight: 'min(55dvh, calc(var(--spacing-10) * 9))', scrollbarWidth: 'thin',
        scrollbarColor: 'var(--color-text-secondary) transparent' }}>
        <List aria-label={label} data-host-column-options="" density="compact" edge="flush" reorderable={reorder}
          style={{ display: 'flex', flexDirection: 'column', rowGap: 'var(--spacing-0-5)' }}>
          {columns.map(({ key, visible }) => {
            const name = hostColumnLabel(key)
            return <ListItem key={key} itemKey={key} aria-label={name} data-yaare-compound-action=""
              style={{ paddingBlock: 0, paddingInlineEnd: 'var(--spacing-2)',
                backgroundColor: visible ? 'var(--color-overlay-hover)' : undefined }}
              label={<Button data-host-column-visibility="" label={name} aria-pressed={visible} variant="ghost" size="sm" width="100%"
                icon={<Icon icon={visible ? EyeIcon : EyeSlashIcon} size="xsm" />}
                style={{ minWidth: 0, justifyContent: 'flex-start', paddingInline: 'var(--spacing-2)',
                  color: visible ? 'var(--color-text-primary)' : 'var(--color-text-secondary)' }}
                onClick={() => onVisibility(key, !visible)} />} />
          })}
        </List>
      </Stack>
    </Stack>}>
    <IconButton id="host-column-picker" label={label} tooltip={label} variant={open ? 'secondary' : 'ghost'} size="sm"
      icon={<Icon icon={ViewColumnsIcon} size="sm" />} />
  </Popover>
}
