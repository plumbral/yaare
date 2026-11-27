export function rowMotion(element: HTMLElement, offset: number, entering = false) {
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) return
  const tokens = getComputedStyle(element)
  const time = tokens.getPropertyValue('--duration-fast-max').trim()
  const duration = (Number.parseFloat(time) * (time.endsWith('ms') ? 1 : 1000)) || 165
  const easing = tokens.getPropertyValue('--ease-standard').trim() || 'ease-out'
  return element.animate(entering ? [{ opacity: .55 }, { opacity: 1 }]
    : [{ translate: `0 ${offset}px` }, { translate: '0 0' }], { duration, easing })
}
