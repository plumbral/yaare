import { useEffect, type RefObject } from 'react'

export function horizontalWheelDelta({ deltaX, deltaY, shiftKey }: Pick<WheelEvent, 'deltaX' | 'deltaY' | 'shiftKey'>, forwarded: boolean) {
  const vertical = Math.abs(deltaY) > Math.abs(deltaX)
  if (forwarded) return shiftKey ? deltaX || deltaY : vertical ? 0 : deltaX
  return !shiftKey && vertical ? deltaY : 0
}

export default function useHorizontalWheel(viewport: RefObject<HTMLElement | null>, target = viewport,
  { enabled = true, forwardFromTarget = false }: { enabled?: boolean; forwardFromTarget?: boolean } = {}) {
  useEffect(() => {
    const element = viewport.current, eventTarget = target.current
    if (!enabled || !element || !eventTarget) return
    const onWheel = (event: WheelEvent) => {
      if (event.defaultPrevented || !event.cancelable || event.ctrlKey || event.metaKey) return
      const mode = event.deltaMode
      const forwarded = forwardFromTarget && !element.contains(event.target as Node)
      const delta = horizontalWheelDelta(event, forwarded)
      if (!delta) return
      const max = element.scrollWidth - element.clientWidth
      if (max <= 1) return
      const style = getComputedStyle(element)
      const direction = style.direction === 'rtl' ? -1 : 1
      const logicalDelta = forwarded && event.deltaX ? delta * direction : delta
      const unit = mode === WheelEvent.DOM_DELTA_LINE ? parseFloat(style.lineHeight) || parseFloat(style.fontSize) || 1
        : mode === WheelEvent.DOM_DELTA_PAGE ? element.clientWidth : 1
      const position = element.scrollLeft * direction
      const next = Math.max(0, Math.min(max, position + logicalDelta * unit)) * direction
      if (element.scrollLeft === next) return
      element.scrollLeft = next
      event.preventDefault()
    }
    eventTarget.addEventListener('wheel', onWheel, { passive: false })
    return () => eventTarget.removeEventListener('wheel', onWheel)
  }, [viewport, target, enabled, forwardFromTarget])
}
