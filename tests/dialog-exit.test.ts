import { expect, mock, test } from 'bun:test'
import { startDialogExit } from '../frontend/src/shared/ui/dialogExit'

function mockDialog({ reducedMotion = false, open = true, canAnimate = true, canAnimateBackdrop = true, rejectsAnimation = false } = {}) {
  const motion = Object.assign(new EventTarget(), { matches: reducedMotion })
  const surfaceDone = Promise.withResolvers<void>()
  const cancelSurface = mock(() => surfaceDone.reject(new DOMException('Cancelled', 'AbortError')))
  const cancelBackdrop = mock(() => {})
  const animate = mock((_frames: Keyframe[], timing: KeyframeAnimationOptions) => {
    if (rejectsAnimation) throw new DOMException('Unsupported animation', 'NotSupportedError')
    if (!timing.pseudoElement) return { finished: surfaceDone.promise, cancel: cancelSurface }
    if (!canAnimateBackdrop) throw new DOMException('Unsupported pseudo-element', 'NotSupportedError')
    return { cancel: cancelBackdrop }
  })
  const dialog = Object.assign(new EventTarget(), {
    open, animate: canAnimate ? animate : undefined,
    ownerDocument: { defaultView: { matchMedia: () => motion, getComputedStyle: () => ({ opacity: '0.65' }) } },
  }) as unknown as HTMLDialogElement
  return { dialog, motion, surfaceDone, animate, cancelSurface, cancelBackdrop }
}

test('keeps the dialog modal and blocks repeat input until the fade ends', async () => {
  const { dialog, surfaceDone, animate, cancelBackdrop } = mockDialog()
  const close = mock(() => { dialog.open = false })
  const cleanup = startDialogExit(dialog, close)
  expect(dialog.open).toBe(true)
  expect(close).not.toHaveBeenCalled()
  expect(animate.mock.calls).toEqual([
    [[{ opacity: '0.65' }, { opacity: 0 }], { duration: 120, easing: 'ease-out', fill: 'forwards' }],
    [[{ opacity: 1 }, { opacity: 0 }], { duration: 120, easing: 'ease-out', fill: 'forwards', pseudoElement: '::backdrop' }],
  ])
  for (const type of ['click', 'keydown', 'submit', 'contextmenu']) {
    expect(dialog.dispatchEvent(new Event(type, { cancelable: true }))).toBe(false)
  }
  surfaceDone.resolve()
  await surfaceDone.promise
  expect(close).toHaveBeenCalledTimes(1)
  expect(dialog.open).toBe(false)
  cleanup?.()
  expect(cancelBackdrop).toHaveBeenCalledTimes(1)
  expect(dialog.dispatchEvent(new Event('click', { cancelable: true }))).toBe(true)
})

test('closes immediately if the webview rejects the surface animation', () => {
  const { dialog, animate } = mockDialog({ rejectsAnimation: true })
  const close = mock(() => {})
  expect(startDialogExit(dialog, close)).toBeUndefined()
  expect(close).toHaveBeenCalledTimes(1)
  expect(animate).toHaveBeenCalledTimes(1)
})

test('cancels a pending exit and restores input on rapid reopen', async () => {
  const { dialog, motion, cancelSurface, cancelBackdrop } = mockDialog()
  const close = mock(() => {})
  const cleanup = startDialogExit(dialog, close)
  cleanup?.()
  motion.dispatchEvent(Object.assign(new Event('change'), { matches: true }))
  await Promise.resolve()
  expect(close).not.toHaveBeenCalled()
  expect(dialog.open).toBe(true)
  expect(dialog.dispatchEvent(new Event('keydown', { cancelable: true }))).toBe(true)
  expect(cancelSurface).toHaveBeenCalledTimes(1)
  expect(cancelBackdrop).toHaveBeenCalledTimes(1)
})

test('skips the fade for reduced motion, closed dialogs, or missing animation support', () => {
  for (const options of [{ reducedMotion: true }, { open: false }, { canAnimate: false }]) {
    const { dialog, animate } = mockDialog(options)
    const close = mock(() => {})
    expect(startDialogExit(dialog, close)).toBeUndefined()
    expect(close).toHaveBeenCalledTimes(1)
    expect(animate).not.toHaveBeenCalled()
  }
})

test('finishes a pending exit if reduced motion is enabled mid-fade', async () => {
  const { dialog, motion, surfaceDone } = mockDialog()
  const close = mock(() => {})
  const cleanup = startDialogExit(dialog, close)
  motion.dispatchEvent(Object.assign(new Event('change'), { matches: true }))
  expect(close).toHaveBeenCalledTimes(1)
  surfaceDone.resolve()
  await surfaceDone.promise
  expect(close).toHaveBeenCalledTimes(1)
  cleanup?.()
})

test('still closes if the webview cannot animate the backdrop', async () => {
  const { dialog, surfaceDone } = mockDialog({ canAnimateBackdrop: false })
  const close = mock(() => {})
  const cleanup = startDialogExit(dialog, close)
  expect(close).not.toHaveBeenCalled()
  surfaceDone.resolve()
  await surfaceDone.promise
  expect(close).toHaveBeenCalledTimes(1)
  cleanup?.()
})

test('an interrupted surface animation does not strand the modal', async () => {
  const { dialog, cancelSurface } = mockDialog()
  const close = mock(() => {})
  const cleanup = startDialogExit(dialog, close)
  cancelSurface()
  await Promise.resolve()
  expect(close).toHaveBeenCalledTimes(1)
  cleanup?.()
})
