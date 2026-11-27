import { useRef, type ReactNode } from 'react'
import { LayoutPanel } from '@astryxdesign/core/Layout'
import { Stack } from '@astryxdesign/core/Stack'
import { usePanelReveal } from './usePanelReveal'

export default function AuxiliaryPanel({ id, label, open, children }: { id: string; label: string; open: boolean; children: ReactNode }) {
  const content = useRef<HTMLElement>(null)
  usePanelReveal(content, open)
  return <LayoutPanel id={id} label={label} role="region" padding={0} isScrollable={false} hasDivider={open}
    width={open ? 'min(40%, calc(var(--spacing-10) * 8))' : '0%'} aria-hidden={!open} inert={!open}
    style={{ minWidth: 0, overflow: 'clip', visibility: open ? 'visible' : 'hidden' }}>
    <Stack ref={content} height="100%" style={{ minWidth: 0, minHeight: 0 }}>{children}</Stack>
  </LayoutPanel>
}
