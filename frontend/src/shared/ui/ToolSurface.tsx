import { useRef, type ReactNode } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { usePanelReveal } from './usePanelReveal'

export default function ToolSurface({ id, active, children, scrollable = false, mainView = false, triggerId }: {
  id: string; active: boolean; children: ReactNode; scrollable?: boolean; mainView?: boolean; triggerId?: string
}) {
  const surface = useRef<HTMLElement>(null)
  usePanelReveal(surface, active)
  return <Stack ref={surface} role={triggerId || mainView ? 'region' : 'tabpanel'} id={mainView ? `panel-${id}` : `tool-content-${id}`} aria-labelledby={triggerId ?? (mainView ? `tab-${id}` : `tool-tab-${id}`)} hidden={!active}
    style={{ height: '100%', minHeight: 0, minWidth: 0, display: active ? 'flex' : 'none', overflow: scrollable ? 'auto' : 'hidden', background: 'var(--color-background-body)', scrollbarGutter: scrollable ? 'stable' : undefined }}>
    {children}
  </Stack>
}
