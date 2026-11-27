type FocusTarget = { focus: () => boolean }

function originDismissed(origin: Element | null) {
  if (!origin) return false
  if ('isConnected' in origin && !origin.isConnected) return true
  if (typeof origin.closest === 'function') {
    try {
      if (origin.closest('[hidden], [inert], dialog:not([open]), [popover]:not(:popover-open)')) return true
    } catch {
      if (origin.closest('[hidden], [inert], dialog:not([open])')) return true
    }
  }
  return false
}

export function createTerminalFocus(
  controllerFor: (id: string) => FocusTarget | undefined,
  currentFocus: () => Element | null,
  schedule: (callback: () => void) => number,
  cancel: (frame: number) => void,
) {
  let visible = ''
  let requested = ''
  let pending: { id: string; origin: Element | null; settled: boolean } | undefined
  let frame: number | undefined

  function flush() {
    frame = undefined
    if (!pending) return
    if (pending.id !== visible) {
      pending = undefined
      return
    }
    const focused = currentFocus()
    if (focused && focused !== pending.origin) {
      if (!pending.settled && originDismissed(pending.origin)) {
        pending.origin = focused
      } else {
        pending = undefined
        return
      }
    }
    pending.settled = true
    if (controllerFor(pending.id)?.focus()) pending = undefined
  }

  function enqueue() {
    if (frame === undefined) frame = schedule(flush)
  }

  return {
    request(id: string) {
      if (!id) return
      requested = id
      pending = { id, origin: currentFocus(), settled: false }
      enqueue()
    },
    wasRequested(id: string) { return requested === id },
    setVisible(id: string) {
      visible = id
      if (pending?.id === id) enqueue()
      else pending = undefined
    },
    ready(id: string) { if (pending?.id === id) enqueue() },
    cancel() {
      if (frame !== undefined) cancel(frame)
      frame = undefined
      pending = undefined
    },
  }
}

export function shouldFocusPanePointer(target: EventTarget | null) {
  if (!target || typeof (target as Element).closest !== 'function') return true
  return !(target as Element).closest('button, a[href], input, select, [role="button"], [role="menuitem"], [role="tab"], textarea:not(.xterm-helper-textarea)')
}

export function focusTerminalInput(input: HTMLTextAreaElement | undefined, focus: () => void) {
  if (!input || input.closest('[hidden], [inert]') || input.ownerDocument.querySelector('dialog[open]')) return false
  if (input.ownerDocument.activeElement !== input) focus()
  return input.ownerDocument.activeElement === input
}
