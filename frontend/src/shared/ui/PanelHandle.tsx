import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { ResizeHandle, type ResizableRegion } from '@astryxdesign/core/Resizable'
import { Stack } from '@astryxdesign/core/Stack'

export default function PanelHandle({ region, side, onResizeIntent }: { region: ResizableRegion; side: 'start' | 'end'; onResizeIntent: (pointer: boolean) => void }) {
  useLocale()

  const left = side === 'start'
  return <Stack height="100%" width={left?0:'var(--border-width)'} style={{ position: 'relative', flexShrink: 0, zIndex: 3 }}>
    <ResizeHandle direction="horizontal" hasDivider={!left} isReversed={!left} resizable={region}
      onPointerDownCapture={event => { if (event.button === 0) onResizeIntent(true) }} onKeyDownCapture={event => { if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) onResizeIntent(false) }}
      label={tr("调整{p0}宽度", left ? tr("会话标签") : tr("文件管理"))}><></></ResizeHandle>
  </Stack>
}
