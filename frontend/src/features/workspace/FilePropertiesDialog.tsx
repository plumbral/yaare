import { useEffect, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import {
  ArchiveBoxIcon,
  CodeBracketIcon,
  DocumentDuplicateIcon,
  DocumentIcon,
  DocumentTextIcon,
  FilmIcon,
  FolderIcon,
  LinkIcon,
  MusicalNoteIcon,
  PhotoIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { writeClipboardText } from '../../shared/bridge/application'
import { useLocale } from '../../shared/i18n/locale'
import { tr } from '../../shared/i18n/messages'
import ActionDialogHeader from '../../shared/ui/ActionDialogHeader'
import { Dialog } from '../../shared/ui/AppDialog'
import { fileEntryExtension, fileEntryIconCategory, type FileNode } from './fileTreeModel'
import { transferSize } from './transferModel'

function propertyIcon(node: FileNode | null) {
  if (!node) return DocumentTextIcon
  switch (fileEntryIconCategory(node.entry)) {
    case 'link': return LinkIcon
    case 'folder': return FolderIcon
    case 'archive': return ArchiveBoxIcon
    case 'image': return PhotoIcon
    case 'video': return FilmIcon
    case 'audio': return MusicalNoteIcon
    case 'code': return CodeBracketIcon
    case 'text': return DocumentTextIcon
    default: return DocumentIcon
  }
}

function propertyTypeLabel(node: FileNode): string {
  if (node.entry.link) return tr("符号链接")
  if (node.entry.directory) return tr("文件夹")
  const ext = fileEntryExtension(node.entry)
  return ext ? `${ext.toUpperCase()} · ${tr("文件")}` : tr("文件")
}

export default function FilePropertiesDialog({
  isOpen,
  node,
  onClose,
  onAfterClose,
}: {
  isOpen?: boolean
  node: FileNode | null
  onClose: () => void
  onAfterClose?: () => void
}) {
  const language = useLocale()
  const [notice, setNotice] = useState('')
  const open = isOpen ?? !!node

  useEffect(() => {
    setNotice('')
  }, [node?.path, open])

  const copy = async (value: string, message: string) => {
    try {
      await writeClipboardText(value)
      setNotice(message)
    } catch {
      setNotice(tr("复制失败，请重试。"))
    }
  }

  const formattedDate = node && node.entry.modified > 0
    ? new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(node.entry.modified))
    : '—'
  const formattedSize = !node || node.entry.directory
    ? '—'
    : node.entry.size >= 1024
      ? `${transferSize(node.entry.size)} (${node.entry.size.toLocaleString(language)} B)`
      : transferSize(node.entry.size)
  const permissionsText = node ? [node.entry.permissions, node.entry.mode].filter(Boolean).join(' · ') || '—' : '—'

  const rows: { key: string; label: string; value: string; mono?: boolean }[] = node
    ? [
        { key: 'type', label: tr("类型"), value: propertyTypeLabel(node) },
        { key: 'size', label: tr("大小"), value: formattedSize },
        { key: 'modified', label: tr("修改时间"), value: formattedDate },
        { key: 'permissions', label: tr("权限"), value: permissionsText, mono: true },
        ...(node.entry.ownerUid != null
          ? [{ key: 'owner', label: tr("所有者 UID"), value: String(node.entry.ownerUid), mono: true }]
          : []),
      ]
    : []

  const HeaderIcon = propertyIcon(node)

  return (
    <Dialog
      isOpen={open}
      onOpenChange={next => { if (!next) onClose() }}
      onAfterClose={onAfterClose}
      purpose="info"
      width="min(calc(var(--spacing-10) * 11.5), 92vw)"
      maxHeight="85dvh"
      padding={0}
    >
      <Stack style={{ minHeight: 0, maxHeight: '85dvh' }}>
        <Stack padding={4} paddingBlockEnd={2} style={{ flexShrink: 0 }}>
          <ActionDialogHeader
            title={tr("文件属性")}
            action="edit"
            startContent={<Icon icon={HeaderIcon} size="md" />}
            endContent={
              <IconButton
                label={tr("关闭文件属性")}
                tooltip={tr("关闭 · Esc")}
                size="md"
                variant="ghost"
                icon={<Icon icon={XMarkIcon} size="sm" />}
                onClick={onClose}
              />
            }
          />
        </Stack>

        {node && (
          <Stack paddingInline={4} paddingBlockStart={2} paddingBlockEnd={4} gap={3} style={{ overflow: 'auto', overscrollBehavior: 'contain', minHeight: 0 }}>
            <Stack
              padding={3}
              gap={1}
              style={{
                borderRadius: 'var(--radius-element)',
                background: 'var(--color-background-muted)',
                border: 'var(--border-width) solid var(--color-border)',
              }}
            >
              <Text type="label" weight="semibold" style={{ overflowWrap: 'anywhere', userSelect: 'text' }}>
                {node.entry.name}
              </Text>
              <Stack direction="horizontal" align="center" justify="between" gap={2} style={{ minWidth: 0 }}>
                <StackItem size="fill" style={{ minWidth: 0 }}>
                  <Text
                    type="supporting"
                    color="secondary"
                    style={{ fontFamily: 'var(--font-family-code)', overflowWrap: 'anywhere', userSelect: 'text' }}
                  >
                    {node.path}
                  </Text>
                </StackItem>
                <IconButton
                  label={tr("复制路径")}
                  tooltip={tr("复制路径")}
                  size="sm"
                  variant="ghost"
                  icon={<Icon icon={DocumentDuplicateIcon} size="sm" />}
                  onClick={() => void copy(node.path, tr("已复制路径"))}
                />
              </Stack>
            </Stack>

            <Stack
              style={{
                borderRadius: 'var(--radius-element)',
                border: 'var(--border-width) solid var(--color-border)',
                overflow: 'hidden',
              }}
            >
              {rows.map((row, index) => (
                <Stack
                  key={row.key}
                  direction="horizontal"
                  align="center"
                  gap={3}
                  paddingInline={3}
                  paddingBlock={2}
                  style={{
                    borderBlockStart: index > 0 ? 'var(--border-width) solid var(--color-border)' : undefined,
                    minHeight: 'var(--size-element-md)',
                  }}
                >
                  <Text
                    type="supporting"
                    color="secondary"
                    style={{ width: 'calc(var(--spacing-10) * 2.3)', flexShrink: 0 }}
                  >
                    {row.label}
                  </Text>
                  <StackItem size="fill" style={{ minWidth: 0 }}>
                    <Text
                      type="supporting"
                      style={{
                        fontVariantNumeric: 'tabular-nums',
                        fontFamily: row.mono ? 'var(--font-family-code)' : undefined,
                        overflowWrap: 'anywhere',
                        userSelect: 'text',
                      }}
                    >
                      {row.value}
                    </Text>
                  </StackItem>
                </Stack>
              ))}
            </Stack>
          </Stack>
        )}

        <Stack
          paddingInline={4}
          paddingBlock={3}
          direction="horizontal"
          align="center"
          justify="between"
          gap={2}
          style={{
            borderBlockStart: 'var(--border-width) solid var(--color-border)',
            flexShrink: 0,
          }}
        >
          <StackItem size="fill" style={{ minWidth: 0 }}>
            <Text type="supporting" color="secondary" role="status">
              {notice}
            </Text>
          </StackItem>
          <Stack direction="horizontal" align="center" gap={2} style={{ flexShrink: 0 }}>
            {node && (
              <Button
                label={tr("复制文件名")}
                variant="ghost"
                size="md"
                onClick={() => void copy(node.entry.name, tr("已复制文件名"))}
              />
            )}
            <Button label={tr("关闭")} variant="secondary" size="md" onClick={onClose} />
          </Stack>
        </Stack>
      </Stack>
    </Dialog>
  )
}
