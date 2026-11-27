import { useEffect, useRef, type PointerEvent, type RefObject } from 'react'

type Drag = {
  pointerId: number; name: string; from: number; to: number
  handle: HTMLButtonElement; row: HTMLElement; others: HTMLElement[]
  startY: number; y: number; scrollTop: number; started: boolean
  frame: number; time: number
}

export function useGroupOrderDrag(viewport: RefObject<HTMLDivElement | null>, groups: readonly string[], enabled: boolean,
  commit: (name: string, to: number) => void) {
  const drag = useRef<Drag | null>(null)
  const marker = useRef<HTMLElement>(null)

  function finish() {
    const active = drag.current
    if (!active) return false
    drag.current = null
    cancelAnimationFrame(active.frame)
    if (marker.current) marker.current.style.display = 'none'
    active.row.style.removeProperty('translate')
    active.row.style.removeProperty('z-index')
    active.row.style.removeProperty('background-color')
    active.handle.style.cursor = 'grab'
    if (active.handle.hasPointerCapture(active.pointerId)) active.handle.releasePointerCapture(active.pointerId)
    return true
  }

  useEffect(() => {
    if (!enabled) return
    window.addEventListener('blur', finish)
    return () => { window.removeEventListener('blur', finish); finish() }
  }, [groups, enabled])

  function place(active: Drag, scroll: HTMLElement) {
    const bounds = scroll.getBoundingClientRect()
    const y = Math.max(bounds.top, Math.min(active.y, bounds.bottom))
    const next = active.others.findIndex(row => {
      const rect = row.getBoundingClientRect()
      return y < rect.top + rect.height / 2
    })
    active.to = next < 0 ? active.others.length : next
    active.row.style.translate = `0 ${active.y - active.startY + scroll.scrollTop - active.scrollTop}px`

    const line = marker.current
    if (!line) return
    const after = active.others[active.to]?.getBoundingClientRect()
    const before = active.others[active.to - 1]?.getBoundingClientRect()
    const dropY = after && before ? (before.bottom + after.top) / 2 : after?.top ?? before?.bottom
    const visible = active.to !== active.from && dropY !== undefined && dropY >= bounds.top && dropY <= bounds.bottom
    line.style.display = visible ? 'block' : 'none'
    if (visible) {
      line.style.top = `${dropY}px`
      line.style.left = `${active.row.getBoundingClientRect().left}px`
      line.style.width = `${active.row.offsetWidth}px`
    }
  }

  function tick(now: number) {
    const active = drag.current, scroll = viewport.current
    if (!active || !scroll) return
    const bounds = scroll.getBoundingClientRect()
    const edge = Math.min(active.row.offsetHeight, bounds.height / 3)
    const speed = active.y < bounds.top + edge ? -Math.min(1, (bounds.top + edge - active.y) / edge)
      : active.y > bounds.bottom - edge ? Math.min(1, (active.y - bounds.bottom + edge) / edge) : 0
    if (speed) scroll.scrollTop += speed * Math.min(32, now - active.time || 16) * 0.6
    active.time = now
    place(active, scroll)
    active.frame = requestAnimationFrame(tick)
  }

  function grab(event: PointerEvent<HTMLButtonElement>, name: string) {
    if (!enabled || !event.isPrimary || event.button !== 0 || drag.current) return
    const handle = event.currentTarget, scroll = viewport.current
    const row = handle.closest<HTMLElement>('[data-group-order-row]')
    const from = groups.indexOf(name)
    if (!row || !scroll || from < 0) return
    const others = [...scroll.querySelectorAll<HTMLElement>('[data-group-order-row]')].filter(other => other !== row)
    handle.setPointerCapture(event.pointerId)
    drag.current = { pointerId: event.pointerId, name, from, to: from, handle, row, others,
      startY: event.clientY, y: event.clientY, scrollTop: scroll.scrollTop, started: false, frame: 0, time: 0 }
  }

  function move(event: PointerEvent<HTMLButtonElement>) {
    const active = drag.current
    if (!active || active.pointerId !== event.pointerId) return
    active.y = event.clientY
    if (active.started || Math.abs(active.y - active.startY) < 5) return
    active.started = true
    active.row.style.zIndex = '1'
    active.row.style.backgroundColor = 'var(--color-background-body)'
    active.handle.style.cursor = 'grabbing'
    active.frame = requestAnimationFrame(tick)
  }

  function drop(event: PointerEvent<HTMLButtonElement>) {
    const active = drag.current, scroll = viewport.current
    if (!active || active.pointerId !== event.pointerId) return
    if (active.started && scroll) { active.y = event.clientY; place(active, scroll) }
    const bounds = scroll?.getBoundingClientRect()
    finish()
    if (active.started && active.to !== active.from && bounds && event.clientX >= bounds.left && event.clientX <= bounds.right)
      commit(active.name, active.to)
  }

  function cancelPointer(event: PointerEvent<HTMLButtonElement>) {
    if (drag.current?.pointerId === event.pointerId) finish()
  }

  return { marker, grab, move, drop, cancelPointer, cancel: finish }
}
