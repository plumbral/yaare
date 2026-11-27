const exitDuration = 120
const exitInput = ['pointerdown', 'pointerup', 'click', 'auxclick', 'contextmenu', 'keydown', 'keyup', 'beforeinput', 'submit', 'drop']

export function startDialogExit(dialog: HTMLDialogElement | null, onFinish: () => void) {
  const view = dialog?.ownerDocument.defaultView
  const motion = view?.matchMedia?.('(prefers-reduced-motion: reduce)')
  if (!dialog?.open || !view || motion?.matches || typeof dialog.animate !== 'function') {
    onFinish()
    return
  }

  const timing = { duration: exitDuration, easing: 'ease-out', fill: 'forwards' } as const
  const frames = [{ opacity: view.getComputedStyle(dialog).opacity }, { opacity: 0 }]
  let surface: Animation
  try {
    surface = dialog.animate(frames, timing)
  } catch {
    onFinish()
    return
  }
  let backdrop: Animation | undefined
  try {
    backdrop = dialog.animate([{ opacity: 1 }, { opacity: 0 }], { ...timing, pseudoElement: '::backdrop' })
  } catch {}

  const blockInput = (event: Event) => { event.preventDefault(); event.stopImmediatePropagation() }
  for (const type of exitInput) dialog.addEventListener(type, blockInput, true)
  let pending = true
  const finish = () => {
    if (!pending) return
    pending = false
    onFinish()
  }
  const updateMotion = (event: MediaQueryListEvent) => { if (event.matches) finish() }
  motion?.addEventListener('change', updateMotion)
  void surface.finished.then(finish, finish)

  return () => {
    pending = false
    motion?.removeEventListener('change', updateMotion)
    for (const type of exitInput) dialog.removeEventListener(type, blockInput, true)
    surface.cancel()
    backdrop?.cancel()
  }
}
