export const compactRailWidth = 48
export function railHeaderPlacement(docked: boolean, expanded: boolean, footprint: string, visualWidth: string, captionStart: string) {
  const slotWidth = `max(0px, calc(${footprint} - ${captionStart}))`
  if (docked) {
    return {
      overlay: false,
      slotWidth,
      slotMinWidth: 'max-content' as const,
      surfaceWidth: '100%',
      surfaceMinWidth: 'max-content' as const,
    }
  }
  return {
    overlay: true,
    slotWidth,
    slotMinWidth: `${compactRailWidth}px`,
    surfaceWidth: `max(${compactRailWidth}px, calc(${visualWidth} - ${captionStart}))`,
    surfaceMinWidth: expanded ? 'max-content' as const : undefined,
  }
}
export function panelLayout(width: number, left: number, right: number, leftWanted: boolean, rightWanted: boolean, priority: 'start' | 'end', terminalMinimum = 320) {
  const budget = Math.max(0, width - terminalMinimum - 2)
  let showLeft = leftWanted && budget >= 200
  let showRight = rightWanted && budget >= 280 + compactRailWidth
  if (showLeft && showRight && budget < 480) {
    if (priority === 'start') showRight = false
    else showLeft = false
  }
  let leftMax = showLeft ? Math.min(360, budget - (showRight ? 280 : 0)) : 360
  const rightMax = showRight ? Math.min(480, budget - (showLeft ? Math.min(left, leftMax) : compactRailWidth)) : 480
  if (showLeft && showRight) leftMax = Math.min(leftMax, budget - Math.min(right, rightMax))
  return { leftCollapsed: !showLeft, rightCollapsed: !showRight, leftMax: Math.max(200, leftMax), rightMax: Math.max(280, rightMax) }
}
