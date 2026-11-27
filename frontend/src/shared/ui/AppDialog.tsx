import {useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode} from 'react'
import {Dialog as AstryxDialog,type DialogProps} from '@astryxdesign/core/Dialog'
import {useMergedRefs} from '@astryxdesign/core/hooks'
import AppContextMenu from './AppContextMenu'
import {startDialogExit} from './dialogExit'
import './listFocus.css'

let pointerOpener: HTMLElement | null = null
let pointerClickTime = 0
let lastInputWasPointer = false
if (typeof document !== 'undefined') {
  document.addEventListener('pointerdown', () => {lastInputWasPointer = true}, true)
  document.addEventListener('click', event => {
    const focused = document.activeElement
    pointerOpener = event.detail > 0 && focused instanceof HTMLElement &&
      event.composedPath().includes(focused) ? focused : null
    pointerClickTime = performance.now()
  }, true)
  document.addEventListener('keydown', () => {
    pointerOpener = null
    lastInputWasPointer = false
  }, true)
}

export function DialogPresence<Snapshot>({value,children}: {
  value: Snapshot | null
  children: (snapshot: Snapshot, isOpen: boolean, onAfterClose: () => void) => ReactNode
}) {
  const [lastValue, setLastValue] = useState(() => value)
  if (value !== null && value !== lastValue) setLastValue(() => value)
  const release = useCallback(() => setLastValue(null), [])
  const snapshot = value ?? lastValue
  return snapshot === null ? null : children(snapshot, value !== null, release)
}

export function Dialog({children,isOpen,isInline,onOpenChange,onAfterClose,ref,...props}:DialogProps & {onAfterClose?: () => void}){
  const dialog = useRef<HTMLDialogElement>(null)
  const mergedRef = useMergedRefs(ref, dialog)
  const [requestedOpen, setRequestedOpen] = useState(isOpen)
  const [exiting, setExiting] = useState(false)
  if (requestedOpen !== isOpen) {
    setRequestedOpen(isOpen)
    setExiting(!isOpen && !isInline)
  }
  const visible = isOpen || (exiting && !isInline)
  const wasVisible = useRef(false)

  useLayoutEffect(() => {
    if (exiting && !isOpen && !isInline) return startDialogExit(dialog.current, () => setExiting(false))
  }, [exiting, isOpen, isInline])

  useEffect(() => {
    const closed = wasVisible.current && !visible
    wasVisible.current = visible
    if (closed) onAfterClose?.()
  }, [visible, onAfterClose])

  useLayoutEffect(() => {
    if (!visible || isInline || performance.now() - pointerClickTime > 1000) return
    const opener = pointerOpener
    if (!opener) return
    return () => {
      requestAnimationFrame(() => {
        if (!lastInputWasPointer || opener.ownerDocument.activeElement !== opener || !opener.matches(':focus-visible')) return
        opener.blur()
        opener.focus({ preventScroll: true, focusVisible: false } as FocusOptions)
        if (!opener.matches(':focus-visible')) return
        const cleanup = new AbortController()
        const showFocus = () => {
          opener.removeAttribute('data-quiet-dialog-focus')
          cleanup.abort()
        }
        opener.setAttribute('data-quiet-dialog-focus', '')
        opener.addEventListener('blur', showFocus, { signal: cleanup.signal })
        opener.addEventListener('keydown', showFocus, { signal: cleanup.signal })
      })
    }
  }, [visible, isInline])
  return <AstryxDialog {...props} ref={mergedRef} isOpen={visible} isInline={isInline} onOpenChange={open => { if (isOpen) onOpenChange(open) }}>
    <AppContextMenu>{children}</AppContextMenu>
  </AstryxDialog>
}
