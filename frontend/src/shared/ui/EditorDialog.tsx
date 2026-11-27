import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { useRef, type ComponentPropsWithRef, type ReactNode } from 'react'
import { Dialog } from './AppDialog'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { XMarkIcon } from '@heroicons/react/24/outline'
import ActionDialogHeader from './ActionDialogHeader'
import { matchesShortcut, shortcutText, useShortcutOverrides } from '../../features/workspace/shortcutReference'

export default function EditorDialog({ isOpen = true, busy = false, title, action, subtitle, startContent,
  closeLabel = tr("关闭编辑"), closeId, onClose, onAfterClose, formProps, children, actions, navigation, hint, cancelLabel=tr("取消"),headerActions, saveHint, width = 'calc(var(--spacing-10) * 18)' }: {
  isOpen?: boolean; busy?: boolean; title: string; action: 'add' | 'edit' | 'delete'; subtitle?: ReactNode
  startContent?: ReactNode; closeLabel?: string; closeId?: string; onClose: () => void; onAfterClose?: () => void
  formProps: ComponentPropsWithRef<'form'>; children: ReactNode; actions: ReactNode; hint?: string
  navigation?:ReactNode
  cancelLabel?:string
  headerActions?:ReactNode
  saveHint?: string
  width?: string
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()
  const saveKeys = shortcutText('save', shortcuts)
  const defaultHint = saveKeys ? tr('{p0} {p1} · Esc 关闭', saveKeys, saveHint ?? tr('保存')) : tr('Esc 关闭')

  const composing = useRef(false)
  const requestClose = () => { if (isOpen && !busy) onClose() }
  const closeControl=<IconButton id={closeId} label={closeLabel} tooltip={tr("关闭 · Esc")} size="md" variant="ghost" isDisabled={busy} icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={requestClose}/>
  return <Dialog isOpen={isOpen} onAfterClose={onAfterClose} purpose={busy ? 'required' : 'form'} padding={0}
    width={width} maxHeight="90dvh" onOpenChange={open => { if (!open) requestClose() }}>
    <form {...formProps} autoComplete="off" noValidate style={{ display: 'flex', flexDirection: 'column', minHeight: 0, maxHeight: '90dvh' }}
      onCompositionStart={event => { composing.current = true; formProps.onCompositionStart?.(event) }}
      onCompositionEnd={event => { composing.current = false; formProps.onCompositionEnd?.(event) }}
      onKeyDown={event => {
        if (event.nativeEvent.isComposing || composing.current) return
        formProps.onKeyDown?.(event)
        if (matchesShortcut(event.nativeEvent, 'save')) {
          event.preventDefault(); if (!busy && !event.repeat) event.currentTarget.requestSubmit()
        }
      }}>
      <Stack padding={4} paddingBlockEnd={2} style={{ flexShrink: 0 }}>
        <ActionDialogHeader action={action} title={title} subtitle={subtitle} startContent={startContent}
          endContent={headerActions?<Stack direction="horizontal" gap={1} align="center">{headerActions}{closeControl}</Stack>:closeControl} />
      </Stack>
      {navigation&&<Stack padding={4} paddingBlockStart={0} paddingBlockEnd={0} style={{flexShrink:0}}>{navigation}</Stack>}
      <Stack padding={4} paddingBlockStart={2} gap={3} style={{ overflow: 'auto', overscrollBehavior: 'contain', minHeight: 0 }}>
        {children}
      </Stack>
      <Stack padding={4} direction="horizontal" align="center" justify="between" gap={3} wrap="wrap"
        style={{ borderBlockStart: 'var(--border-width) solid var(--color-border)', flexShrink: 0 }}>
        <StackItem size="fill" style={{ minWidth: 'calc(var(--spacing-10) * 3)' }}><Text type="supporting" color="secondary">{hint ?? defaultHint}</Text></StackItem>
        <Stack direction="horizontal" align="center" justify="end" gap={2} wrap="wrap" style={{marginInlineStart:'auto'}}>
          <Button label={cancelLabel} type="button" variant="ghost" size="md" isDisabled={busy} onClick={requestClose} />
          {actions}
        </Stack>
      </Stack>
    </form>
  </Dialog>
}
