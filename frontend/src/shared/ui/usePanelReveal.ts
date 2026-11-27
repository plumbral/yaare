import { useLayoutEffect, type RefObject } from 'react'

export function usePanelReveal(ref: RefObject<HTMLElement | null>, active: boolean, identity?: string) {
  useLayoutEffect(() => {
    const element = ref.current
    if (!active || !element?.animate || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const style = getComputedStyle(element)
    const animation = element.animate([
      { opacity: 0.78 }, { opacity: 1 },
    ], { duration: 165, easing: style.getPropertyValue('--ease-entrance').trim() || 'ease-out' })
    return () => animation.cancel()
  }, [active, identity, ref])
}
