import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { memo, useEffect, useLayoutEffect, useRef } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { ResizeHandle, useResizable } from '@astryxdesign/core/Resizable'
import type { DividerRect } from './terminalLayout'

type Props = {
  divider: DividerRect
  onChange: (id: string, ratio: number) => void
  onPreview: (id: string, ratio?: number) => void
}

export default memo(function SplitDivider({ divider, onChange, onPreview }: Props) {
  useLocale()
  const ref = useRef<HTMLElement>(null)
  const pointer = useRef<number | null>(null)
  const keyboard = useRef(false)
  const next = useRef<number | undefined>(undefined)
  const frame = useRef(0)
  const latest = useRef({ onChange, onPreview })
  latest.current = { onChange, onPreview }
  const horizontal = divider.axis === 'horizontal'
  const length = Math.max(1, (horizontal ? divider.rect.width : divider.rect.height) - 1)
  const first = Math.round(length * divider.ratio)
  const region = useResizable({
    direction: divider.axis, containerRef: ref, defaultSize: first,
    minSize: divider.firstMinimum, maxSize: Math.max(divider.firstMinimum, length - divider.secondMinimum),
    onSizeChange(size) {
      if (pointer.current === null && !keyboard.current) return
      const preview = pointer.current !== null
      next.current = size / length
      if (frame.current) return
      frame.current = requestAnimationFrame(() => {
        frame.current = 0
        if (next.current === undefined) return
        if (preview) latest.current.onPreview(divider.id, next.current)
        else {
          latest.current.onChange(divider.id, next.current)
          next.current = undefined
        }
      })
    },
  })
  useLayoutEffect(() => {
    if (pointer.current === null && !keyboard.current && Math.abs(region.size - first) > .5) region.resize(first)
  }, [first])

  const resetHandle = useRef(() => {})
  resetHandle.current = () => region.resize(first)
  useEffect(() => {
    const finish = (commit: boolean) => {
      if (pointer.current === null) return
      pointer.current = null
      cancelAnimationFrame(frame.current)
      frame.current = 0
      const ratio = next.current
      next.current = undefined
      if (commit && ratio !== undefined) latest.current.onChange(divider.id, ratio)
      else {
        latest.current.onPreview(divider.id)
        resetHandle.current()
      }
    }
    const up = (event: PointerEvent) => { if (event.pointerId === pointer.current) finish(true) }
    const cancel = (event: PointerEvent) => { if (event.pointerId === pointer.current) finish(false) }
    const blur = () => { keyboard.current = false; finish(false) }
    const keyUp = () => { keyboard.current = false }
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('keyup', keyUp)
    window.addEventListener('blur', blur)
    return () => {
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('keyup', keyUp)
      window.removeEventListener('blur', blur)
      if (pointer.current !== null) latest.current.onPreview(divider.id)
      cancelAnimationFrame(frame.current)
    }
  }, [divider.id])

  const size = `var(--yaare-split-first, ${first}px)`
  return <Stack ref={ref} data-split-divider={divider.id} direction={horizontal ? 'horizontal' : 'vertical'} style={{
    position: 'absolute', left: `var(--yaare-preview-x, ${divider.rect.x}px)`, top: `var(--yaare-preview-y, ${divider.rect.y}px)`,
    width: `var(--yaare-preview-width, ${divider.rect.width}px)`, height: `var(--yaare-preview-height, ${divider.rect.height}px)`,
    pointerEvents: 'none', zIndex: 2,
  }}>
    <Stack aria-hidden="true" style={{ width: horizontal ? size : '100%', height: horizontal ? '100%' : size, flexShrink: 0 }}/>
    <Stack style={{ width: horizontal ? 'var(--border-width)' : '100%', height: horizontal ? '100%' : 'var(--border-width)', pointerEvents: 'auto', flexShrink: 0 }}
      onDoubleClickCapture={event => {
        event.preventDefault()
        event.stopPropagation()
        latest.current.onChange(divider.id, .5)
      }}>
      <ResizeHandle direction={divider.axis} resizable={region} hasDivider isAlwaysVisible={false} pillPlacement="center"
        label={horizontal ? tr("调整终端左右比例") : tr("调整终端上下比例")}
        onPointerDownCapture={event => {
          if (event.button !== 0) return
          pointer.current = event.pointerId
          next.current = undefined
          event.currentTarget.focus({ preventScroll: true })
        }}
        onKeyDownCapture={event => {
          if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) keyboard.current = true
        }}/>
    </Stack>
  </Stack>
})
