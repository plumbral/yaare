type FontWheelEvent = Pick<WheelEvent, 'ctrlKey' | 'deltaY' | 'preventDefault' | 'stopPropagation'>

export function handleTerminalFontWheel(event: FontWheelEvent, overTerminal: boolean, onStep: (step: number) => void) {
  if (!event.ctrlKey) return

  event.preventDefault()
  event.stopPropagation()
  if (overTerminal && event.deltaY !== 0) onStep(event.deltaY < 0 ? 1 : -1)
}
