import { useCallback, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { flushSync } from 'react-dom'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { PencilSquareIcon } from '@heroicons/react/24/outline'
import type { ListItemProps } from '@astryxdesign/core/List'
import WindowedList from '../../frontend/src/shared/ui/WindowedList'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'

type Item = { id: string; name: string }
const rowCount = new URLSearchParams(location.search).get('rows') === '10000' ? 10000 : 450
const data = Array.from({ length: rowCount }, (_, index) => ({ id: `probe-${index}`, name: `Host ${index}` }))
const probe = { calls: 0, selected: '', edited: '', replace: () => {}, updateParent: () => {}, reset: () => {} }
function Fixture() {
  const [items, setItems] = useState(data)
  const [revision, setRevision] = useState(0)
  const [reset, setReset] = useState(0)
  const renderRow = useCallback((item: Item): ListItemProps => {
    probe.calls++
    return { id: item.id, label: item.name, description: 'Regression fixture',
      onClick: () => { probe.selected = item.name },
      endContent: <IconButton label={`Edit ${item.name}`} size="sm" variant="ghost" icon={<Icon icon={PencilSquareIcon} size="sm" />}
        onClick={event => { event.stopPropagation(); probe.edited = item.name }} /> }
  }, [])
  probe.replace = () => setItems(items => items.map((item, index) => index === 0 ? { ...item, name: 'Updated host' } : item))
  probe.updateParent = () => setRevision(value => value + 1)
  probe.reset = () => setReset(value => value + 1)
  return <Theme theme={workbenchTheme} mode="light"><Stack height="100dvh" data-revision={revision}>
    <WindowedList items={items} itemKey={item => item.id} row={renderRow} estimateSize={() => 48} label="Virtual list probe" resetKey={String(reset)} />
  </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture />)
Object.assign(window, { virtualListProbe: probe, runVirtualListProbe() {
  const list = document.querySelector<HTMLElement>('[aria-label="Virtual list probe"]')!
  const move = (offset: number) => flushSync(() => { list.scrollTop = offset; list.dispatchEvent(new Event('scroll')) })
  move(0)
  probe.calls = 0
  const before = performance.now()
  let maxMounted = 0
  for (let step = 1; step <= 48; step++) {
    move((step <= 24 ? step : 48 - step) * 12)
    maxMounted = Math.max(maxMounted, list.querySelectorAll('li[data-index]').length)
  }
  const elapsedMs = performance.now() - before
  const scrollRowCalls = probe.calls
  probe.calls = 0
  flushSync(probe.updateParent)
  const parentRowCalls = probe.calls
  probe.calls = 0
  flushSync(probe.replace)
  const updateRowCalls = probe.calls
  const updated = document.getElementById('probe-0')?.textContent?.includes('Updated host')
  const first = document.getElementById('probe-0')!
  first.querySelector('button')!.click()
  first.querySelector<HTMLButtonElement>('button[aria-label="Edit Updated host"]')!.click()
  const callbacksCurrent = probe.selected === 'Updated host' && probe.edited === 'Updated host'
  const geometry = [...list.querySelectorAll<HTMLElement>('li[data-index]')].map(row => ({ index: Number(row.dataset.index), top: row.getBoundingClientRect().top, height: row.getBoundingClientRect().height }))
  const gaps = geometry.slice(1).some((row, index) => row.index === geometry[index]!.index + 1 && Math.abs(row.top - geometry[index]!.top - geometry[index]!.height) > 1)
  const passed = scrollRowCalls < 48 && parentRowCalls === 0 && updateRowCalls === 1 && updated && callbacksCurrent && !gaps && maxMounted < 50
  return { passed, rows: data.length, steps: 48, scrollRowCalls, parentRowCalls, updateRowCalls, updated, callbacksCurrent, gaps, maxMounted, elapsedMs: Math.round(elapsedMs * 10) / 10 }
} })
