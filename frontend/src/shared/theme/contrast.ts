export function luminance(hex: string): number {
  const rgb = hex.replace('#', '').match(/../g)!.slice(0, 3).map(c => parseInt(c, 16) / 255)
  return rgb.map(c => c <= .04045 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4).reduce((sum, c, i) => sum + c * [.2126, .7152, .0722][i]!, 0)
}
export function contrastRatio(a: string, b: string): number {
  const x = luminance(a), y = luminance(b)
  return (Math.max(x, y) + .05) / (Math.min(x, y) + .05)
}
export function readableColor(foreground: string, background: string, minimum = 4.5): string {
  if (contrastRatio(foreground, background) >= minimum) return foreground
  const target = luminance(background) > .179 ? 0 : 255
  const rgb = foreground.slice(1).match(/../g)!.map(c => parseInt(c, 16))
  for (let step = 1; step <= 255; step++) {
    const candidate = '#' + rgb.map(c => Math.round(c + (target - c) * step / 255).toString(16).padStart(2, '0')).join('')
    if (contrastRatio(candidate, background) >= minimum) return candidate
  }
  return target === 0 ? '#000000' : '#ffffff'
}
