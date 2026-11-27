import { useRef, type KeyboardEvent } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Button } from '@astryxdesign/core/Button'
import HorizontalScrollArea from './HorizontalScrollArea'
import AppContextMenu from './AppContextMenu'
import type {ContextMenuOption} from '@astryxdesign/core/ContextMenu'

export default function GroupFilters({ id, label, value, options, onChange, contextActions }: {
  id: string; label: string; value: string
  options: readonly { value: string; label: string }[]; onChange: (value: string) => void
  contextActions?:(value?:string)=>ContextMenuOption[]
}) {
  const group = useRef<HTMLElement>(null)
  const handleKey = (event: KeyboardEvent, index: number) => {
    if (event.nativeEvent.isComposing || event.altKey || event.ctrlKey || event.metaKey) return
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1
      : event.key === 'ArrowRight' ? (index + 1) % options.length
      : event.key === 'ArrowLeft' ? (index - 1 + options.length) % options.length : -1
    if (next < 0) return
    event.preventDefault()
    onChange(options[next]!.value)
    group.current?.querySelectorAll<HTMLElement>('[role="radio"]')[next]?.focus({ preventScroll: true })
  }
  const selected = options.findIndex(option => option.value === value)
  const content=<HorizontalScrollArea id={id} label={label} value={value} hideScrollbar>
    <Stack ref={group} data-filter-strip="" role="radiogroup" aria-label={label} direction="horizontal"
      style={{ display: 'grid', gridAutoColumns: 'max-content', columnGap: 'var(--spacing-1)' }}>
      {options.map((option, index) => <Button key={option.value} label={option.label} variant="ghost" size="md"
        data-group-filter-value={option.value}
        role="radio" aria-checked={index === selected} tabIndex={index === selected ? 0 : -1}
        style={{ gridColumn: index + 1, gridRow: 1, color: index === selected ? 'var(--color-text-primary)' : 'var(--color-text-secondary)' }}
        onKeyDown={event => handleKey(event, index)} onClick={() => onChange(option.value)}/>) }
      {selected >= 0 && <Stack aria-hidden="true" data-filter-underline="" style={{ gridColumn: selected + 1, gridRow: 1, alignSelf: 'end',
        height: 'calc(var(--border-width) * 2)', borderBlockEnd: 'calc(var(--border-width) * 2) solid var(--color-text-primary)',
        borderRadius: 'var(--radius-inner)', pointerEvents: 'none' }}/>} 
    </Stack>
  </HorizontalScrollArea>
  return contextActions?<AppContextMenu label={label} actionsForTarget={target=>contextActions(target.closest<HTMLElement>('[data-group-filter-value]')?.dataset.groupFilterValue)}>{content}</AppContextMenu>:content
}
