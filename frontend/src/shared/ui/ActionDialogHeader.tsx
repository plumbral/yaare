import type { ReactNode } from 'react'
import { DialogHeader } from '@astryxdesign/core/Dialog'
import { Icon } from '@astryxdesign/core/Icon'
import { PlusIcon, PencilSquareIcon, TrashIcon } from '@heroicons/react/24/outline'
import './actionDialogHeader.css'

const icons = { add: PlusIcon, edit: PencilSquareIcon, delete: TrashIcon }

export default function ActionDialogHeader({ action, title, subtitle, startContent, endContent }: {
  action: keyof typeof icons; title: string; subtitle?: ReactNode; startContent?: ReactNode; endContent?: ReactNode
}) {
  return <DialogHeader data-yaare-action-header="" title={title} subtitle={subtitle} hasDivider={false}
    startContent={startContent ?? <Icon icon={icons[action]} size="md" />} endContent={endContent} />
}
