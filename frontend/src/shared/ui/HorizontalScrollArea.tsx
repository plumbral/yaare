import { useLayoutEffect, useRef, type CSSProperties, type ReactNode } from 'react'
import { ScrollArea } from '@base-ui/react/scroll-area'
import { Stack } from '@astryxdesign/core/Stack'
import { useMediaQuery } from '@astryxdesign/core/hooks'
import useHorizontalWheel from './useHorizontalWheel'
import './horizontalScrollFade.css'

function HorizontalScrollBar({ compact = false }: { compact?: boolean }) {
  const highContrast = useMediaQuery('(prefers-contrast: more), (forced-colors: active)')
  return <ScrollArea.Scrollbar data-caption-interactive="" orientation="horizontal" aria-hidden="true" render={<Stack direction="horizontal" align="center" />}
    style={state => ({ height: 'var(--spacing-3)', cursor: 'pointer',
      ...(compact ? { position: 'absolute', insetInline: 0, bottom: 'calc(var(--spacing-2) * -1)' } : {}),
      '--strip-thumb-opacity': highContrast || state.hovering || state.scrolling ? 1 : 0.65,
    }) as CSSProperties}>
    <ScrollArea.Thumb render={<Stack justify="center" />} style={{ height: 'var(--spacing-3)', cursor: 'grab' }}>
      <Stack aria-hidden="true" height="var(--spacing-0-5)" style={{ flexShrink: 0, pointerEvents: 'none',
        borderRadius: 'var(--radius-full)', color: highContrast ? 'var(--color-text-primary)' : 'var(--color-text-secondary)',
        backgroundColor: 'currentColor', border: 'var(--border-width) solid currentColor', opacity: 'var(--strip-thumb-opacity)',
        transition: 'opacity var(--duration-fast) var(--ease-standard)' }} />
    </ScrollArea.Thumb>
  </ScrollArea.Scrollbar>
}

export default function HorizontalScrollArea({ id, label, value, children, compact = false, hideScrollbar = false }: {
  id: string; label: string; value: string; children: ReactNode; compact?: boolean; hideScrollbar?: boolean
}) {
  const viewport = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLDivElement>(null)
  useHorizontalWheel(viewport, root)
  const reveal = (element: HTMLElement) => {
    const strip = viewport.current
    if (!strip) return
    const item = element.getBoundingClientRect(), bounds = strip.getBoundingClientRect()
    const padding = hideScrollbar ? parseFloat(getComputedStyle(strip).scrollPaddingLeft) || 0 : 0
    const left = bounds.left + padding, right = bounds.right - padding
    if (item.left < left) strip.scrollLeft += item.left - left
    else if (item.right > right) strip.scrollLeft += item.right - right
  }
  useLayoutEffect(() => {
    const selected = viewport.current?.querySelector<HTMLElement>('[aria-checked="true"], [aria-selected="true"]')
    if (selected) reveal(selected)
  }, [value])
  return <ScrollArea.Root ref={root} id={id} render={<Stack />} style={state => ({
    position: 'relative', display: 'block', minWidth: 0, width: '100%',
    paddingBlockEnd: !hideScrollbar && !compact && state.hasOverflowX ? 'var(--spacing-3)' : 0,
  })}>
    <ScrollArea.Viewport ref={viewport} id={`${id}-viewport`} role="group" aria-label={label} render={<Stack />}
      data-horizontal-scroll-fade={hideScrollbar ? '' : undefined}
      style={{ display: 'block', maxWidth: '100%', overflowX: 'auto', overflowY: 'hidden', scrollbarWidth: 'none', borderRadius: 'var(--radius-element)' }}
      onFocusCapture={event => { const item = (event.target as HTMLElement).closest<HTMLElement>('button, [role="radio"], [role="tab"]'); if (item) reveal(item) }}>
      <ScrollArea.Content render={<Stack />} style={{ width: 'max-content', minWidth: '100%' }}>
        <Stack width="max-content" padding={1.5}>{children}</Stack>
      </ScrollArea.Content>
    </ScrollArea.Viewport>
    {!hideScrollbar && <HorizontalScrollBar compact={compact} />}
  </ScrollArea.Root>
}
