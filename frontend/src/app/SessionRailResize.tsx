import { useLayoutEffect, useRef, type RefObject } from 'react'
import { useResizable } from '@astryxdesign/core/Resizable'
import PanelHandle from '../shared/ui/PanelHandle'

export default function SessionRailResize({ surface, size, maxSize, onCommit }: {
  surface: RefObject<HTMLElement | null>; size: number; maxSize: number; onCommit: (size: number) => void
}) {
  const gesture = useRef<'pointer' | 'keyboard' | null>(null)
  const draft = useRef(size)
  const frame = useRef(0)
  const region = useResizable({ defaultSize: size, minSize: 200, maxSize, onSizeChange: next => {
    if (!gesture.current) return
    draft.current = next
    if (gesture.current === 'keyboard') onCommit(next)
    else if (!frame.current) frame.current = requestAnimationFrame(() => {
      frame.current = 0
      surface.current?.style.setProperty('--yaare-live-rail-width', draft.current + 'px')
    })
  } })
  useLayoutEffect(() => {
    if (!gesture.current && region.size !== size) region.resize(size)
    if (!gesture.current) surface.current?.style.removeProperty('--yaare-live-rail-width')
  }, [region, size, surface])
  useLayoutEffect(() => {
    const element = surface.current
    const end = () => {
      if (gesture.current === 'pointer') onCommit(draft.current)
      gesture.current = null
      cancelAnimationFrame(frame.current)
      frame.current = 0
    }
    window.addEventListener('pointerup', end)
    window.addEventListener('pointercancel', end)
    window.addEventListener('keyup', end)
    window.addEventListener('blur', end)
    return () => {
      window.removeEventListener('pointerup', end)
      window.removeEventListener('pointercancel', end)
      window.removeEventListener('keyup', end)
      window.removeEventListener('blur', end)
      cancelAnimationFrame(frame.current)
      element?.style.removeProperty('--yaare-live-rail-width')
    }
  }, [onCommit, surface])
  return <PanelHandle region={region} side="start" onResizeIntent={pointer => {
    if (!gesture.current) draft.current = region.size
    gesture.current = pointer ? 'pointer' : 'keyboard'
  }} />
}
