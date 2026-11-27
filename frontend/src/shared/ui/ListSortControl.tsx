import { Button } from '@astryxdesign/core/Button'
import type { DropdownMenuOption } from '@astryxdesign/core/DropdownMenu'
import { Icon } from '@astryxdesign/core/Icon'
import { Text } from '@astryxdesign/core/Text'
import { ArrowDownIcon, ArrowUpIcon } from '@heroicons/react/24/outline'
import { tr } from '../i18n/messages'
import type { ListSort } from './listSort'

export default function ListSortControl({ label, direction, onClick }: {
  label: string
  direction?: 'ascending' | 'descending'
  onClick?: () => void
}) {
  if (!onClick) return <Text type="supporting" color="secondary" maxLines={1}>{label}</Text>
  const status = direction === 'ascending' ? tr("{p0}：升序", label) : direction === 'descending' ? tr("{p0}：降序", label) : undefined
  return <Button label={label} aria-label={status ?? label} size="sm" variant="ghost" onClick={onClick}
    endContent={direction && <Icon icon={direction === 'ascending' ? ArrowUpIcon : ArrowDownIcon} size="xsm" />}
    style={{ paddingInline: 0, maxWidth: '100%', alignSelf: 'flex-start', minWidth: 0 }} />
}

export function sortMenuItems<Key extends string>(choices: readonly Key[], labelFor: (key: Key) => string,
  current: ListSort<Key>, set: (sort: ListSort<Key>) => void, resetLabel: string): DropdownMenuOption[] {
  const mark = <Text type="supporting" color="secondary">{tr("当前")}</Text>
  return [
    { label: resetLabel, onClick: () => set(null), endContent: !current ? mark : undefined },
    { type: 'divider' },
    ...choices.map(key => {
      const label = labelFor(key)
      return { id: key, label, items: [
        { label: tr("{p0}：升序", label), onClick: () => set({ key, direction: 'ascending' }),
          endContent: current?.key === key && current.direction === 'ascending' ? mark : undefined },
        { label: tr("{p0}：降序", label), onClick: () => set({ key, direction: 'descending' }),
          endContent: current?.key === key && current.direction === 'descending' ? mark : undefined },
      ] }
    }),
  ]
}
