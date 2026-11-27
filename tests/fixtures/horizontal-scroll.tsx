import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import HorizontalScrollArea from '../../frontend/src/shared/ui/HorizontalScrollArea'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'

function Fixture() {
  const [value, setValue] = useState('0')
  return <Theme theme={workbenchTheme} mode="light"><Stack width="calc(var(--spacing-10) * 8)" gap={4}>
    <HorizontalScrollArea id="overflow" label="测试分组" value={value}><SegmentedControl label="测试分组筛选" value={value} onChange={setValue}>
      {Array.from({ length: 15 }, (_, index) => <SegmentedControlItem key={index} value={String(index)} label={`测试分组 ${index + 1}`} />)}
    </SegmentedControl></HorizontalScrollArea>
    <HorizontalScrollArea id="fitting" label="短分组" value="0"><SegmentedControl label="短分组筛选" value="0" onChange={() => {}}>
      <SegmentedControlItem value="0" label="全部" />
    </SegmentedControl></HorizontalScrollArea>
  </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture />)
Object.assign(window, { async runHorizontalScrollRegression() {
  const wait = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  await wait()
  const v = document.getElementById('overflow-viewport')!, root = document.getElementById('overflow')!
  const short = document.getElementById('fitting-viewport')!
  const wheel = (element: HTMLElement, options: WheelEventInit) => {
    const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ...options })
    element.dispatchEvent(event); return event.defaultPrevented
  }
  const result: Record<string, boolean> = {}
  v.scrollLeft = 0
  result.wheelMovesHorizontally = wheel(v, { deltaY: 90 }) && v.scrollLeft > 0
  const before = v.scrollLeft
  result.trackpadHorizontalPreserved = !wheel(v, { deltaX: 80 }) && v.scrollLeft === before
  result.zoomPreserved = !wheel(v, { deltaY: 40, ctrlKey: true }) && v.scrollLeft === before
  result.shiftPreserved = !wheel(v, { deltaY: 40, shiftKey: true }) && v.scrollLeft === before
  v.scrollLeft = 0
  result.startEdgeChains = !wheel(v, { deltaY: -30 }) && v.scrollLeft === 0
  v.scrollLeft = v.scrollWidth
  const end = v.scrollLeft
  result.endEdgeChains = !wheel(v, { deltaY: 30 }) && v.scrollLeft === end
  result.fittingDoesNotTrapWheel = !wheel(short, { deltaY: 40 }) && short.scrollLeft === 0
  v.scrollLeft = 0
  result.lineUnits = wheel(v, { deltaY: 3, deltaMode: WheelEvent.DOM_DELTA_LINE }) && v.scrollLeft > 3
  v.scrollLeft = 0
  result.pageUnits = wheel(v, { deltaY: 1, deltaMode: WheelEvent.DOM_DELTA_PAGE }) && Math.abs(v.scrollLeft - v.clientWidth) < 1
  result.customThumbVisible = getComputedStyle(v).scrollbarWidth === 'none' && !!root.querySelector('[data-orientation="horizontal"]')
  result.fittingHidesTrack = !document.querySelector('#fitting [data-orientation="horizontal"]')
  const source = v.querySelector<HTMLElement>('[role="radiogroup"]')!
  const radios = source.querySelectorAll<HTMLButtonElement>('[role="radio"]')
  radios[14]!.click(); await wait()
  const last = radios[14]!.getBoundingClientRect(), viewport = v.getBoundingClientRect()
  result.selectionRevealed = last.left >= viewport.left - 1 && last.right <= viewport.right + 1
  const previousWidth = v.clientWidth
  root.style.width = '50%'; await wait()
  result.resizeUpdatesThumb = v.clientWidth < previousWidth && !!root.querySelector('[data-orientation="horizontal"]')
  root.style.width = ''
  v.scrollLeft = 0; v.style.direction = 'rtl'; await wait()
  result.rtlWheel = wheel(v, { deltaY: 40 }) && v.scrollLeft < 0
  v.style.direction = ''; await wait()
  return result
} })
