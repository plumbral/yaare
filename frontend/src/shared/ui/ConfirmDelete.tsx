import { localizeMessage } from '../i18n/messages'
import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { Dialog } from './AppDialog'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import ActionDialogHeader from './ActionDialogHeader'
import type {ReactNode} from 'react'

export default function ConfirmDelete({ title, subject, description, busy, error, onConfirm, onClose, isOpen = true }: {
  title: string; description: string; busy: boolean; error: string; onConfirm: () => void; onClose: () => void
  isOpen?: boolean
  subject?:ReactNode
}) {
  useLocale()

  return <Dialog isOpen={isOpen} onOpenChange={open => { if (!open && !busy) onClose() }} purpose={busy ? 'required' : 'form'} padding={4} width="calc(var(--spacing-10) * 10)">
    <Stack gap={4}>
      <ActionDialogHeader action="delete" title={title} />
      {subject}
      <Text type="body">{description}</Text>
      {error && <Text role="alert" type="supporting">{localizeMessage(error)}</Text>}
      <Stack direction="horizontal" justify="end" gap={2}>
        <Button label={tr("取消")} type="button" size="sm" variant="secondary" isDisabled={busy} onClick={onClose} />
        <Button label={tr("确认删除")} type="button" size="sm" isLoading={busy} isDisabled={busy} onClick={event => { event.preventDefault(); onConfirm() }} />
      </Stack>
    </Stack>
  </Dialog>
}
