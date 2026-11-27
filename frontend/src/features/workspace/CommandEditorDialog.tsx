import { lazy, Suspense, useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { Selector } from '@astryxdesign/core/Selector'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { localizeMessage, tr } from '../../shared/i18n/messages'
import { getLanguage, useLocale } from '../../shared/i18n/locale'
import EditorDialog from '../../shared/ui/EditorDialog'
import OsIconPicker from '../connections/OsIconPicker'
import DraftRevisionNotice from './DraftRevisionNotice'
import { needsDraftReview, reviewDraft } from './draftRevision'
import type { WorkspaceChange } from './savedWorkspace'
import {
  commandGroup,
  commandIcon,
  commandUngrouped,
  normalizeCommandText,
  validateQuickCommand,
  validCommand,
  type QuickCommand,
} from './shortcuts'

const CommandCodeEditor = lazy(() => import('./CommandCodeEditor'))

export default function CommandEditorDialog({
  isOpen,
  command,
  existing, initialRevision, revision, latest,
  groups,
  editable = true,
  onApply,
  onClose,
  onAfterClose,
  onSaved,
}: {
  isOpen: boolean
  command: QuickCommand | null
  initialRevision?: number
  revision?: number
  latest?: QuickCommand
  existing: boolean
  groups: readonly string[]
  editable?: boolean
  onApply: (change: WorkspaceChange, expectedRevision?: number) => Promise<boolean>
  onClose: () => void
  onAfterClose?: () => void
  onSaved?: (command: QuickCommand) => void
}) {
  useLocale()
  const [draft, setDraft] = useState<QuickCommand | null>(() => command ? { ...command, group: commandGroup(command) } : null)
  const [base, setBase] = useState({ record: command, revision: initialRevision })
  const stale = needsDraftReview(base.revision, revision, existing, !!latest)
  const editing = existing || base.revision !== undefined
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const pending = useRef(false)
  const form = useRef<HTMLFormElement>(null)
  const composing = useRef(false)
  const orderedGroups = useMemo(
    () => [commandUngrouped, ...groups.filter(group => group !== commandUngrouped)],
    [groups, getLanguage()],
  )

  useLayoutEffect(() => {
    if (!isOpen || !command) return
    setBase({ record: command, revision: initialRevision })
    setDraft({ ...command, group: commandGroup(command) })
    setError('')
  }, [isOpen, command, initialRevision])

  const close = () => {
    if (pending.current) return
    onClose()
  }

  async function save(event?: FormEvent) {
    event?.preventDefault()
    if (stale || !draft || composing.current || pending.current || !editable) return
    const next = { ...draft, name: draft.name.trim(), group: commandGroup(draft), command: normalizeCommandText(draft.command) }
    const validation = validateQuickCommand(next) || (!orderedGroups.includes(next.group) ? tr("请选择当前命令分组。") : '')
    if (validation) {
      setError(validation)
      requestAnimationFrame(() => form.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus())
      return
    }
    pending.current = true
    setSaving(true)
    try {
      if (!await onApply({ kind: 'saveCommand', command: next }, base.revision)) {
        setError(tr("命令未保存，编辑内容已保留；请重试。"))
        return
      }
      onSaved?.(next)
      onClose()
    } catch {
      setError(tr("保存失败，编辑内容已保留，请重试。"))
    } finally {
      pending.current = false
      setSaving(false)
    }
  }

  if (!draft) return null

  return <EditorDialog isOpen={isOpen} busy={saving} onClose={close} onAfterClose={onAfterClose}
    startContent={<OsIconPicker systemOnly value={commandIcon(draft)} disabled={saving || !editable}
      onChange={icon => setDraft(previous => previous ? { ...previous, icon } : previous)}/>}
    action={editing ? 'edit' : 'add'} title={editing ? tr("编辑快捷命令") : tr("新建快捷命令")} closeLabel={tr("关闭命令编辑")}
    formProps={{
      ref: form,
      'aria-label': tr("快捷命令编辑"),
      onSubmit: event => { void save(event) },
      onCompositionStart: () => { composing.current = true },
      onCompositionEnd: () => { composing.current = false },
    }}
    actions={<Button label={tr("保存命令")} type="submit" variant="primary" size="md" isDisabled={stale || !editable || saving} isLoading={saving} />}>
    <FormLayout defaultOptionality="required"><Stack gap={3}>
      <Stack direction="horizontal" gap={3} wrap="wrap">
        <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 7)' }}>
          <TextInput id="command-name" label={tr("命令名称")} value={draft.name}
            onChange={name => { setDraft(previous => previous ? { ...previous, name } : previous); setError('') }}
            isRequired hasAutoFocus isDisabled={saving} autoComplete="off" htmlName="command-name" size="md" placeholder={tr("例如：容器状态")}
            status={error && (!draft.name.trim() || [...draft.name.trim()].length > 40) ? { type: 'error', message: localizeMessage(error) } : undefined} />
        </StackItem>
        <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 6)' }}>
          <Selector id="command-group" label={tr("分组")} isRequired size="md" width="100%" value={commandGroup(draft)}
            options={orderedGroups.map(name => ({ value: name, label: name === commandUngrouped ? tr("不指定分组") : name }))}
            onChange={value => { setDraft(previous => previous ? { ...previous, group: value } : previous); setError('') }}
            isDisabled={saving} hasSearch searchPlaceholder={tr("搜索命令分组…")} />
        </StackItem>
      </Stack>
      <Stack gap={1}>
        <Text id="command-code-label" type="label">{tr("命令内容")}</Text>
        <Suspense fallback={<Stack height="clamp(calc(var(--spacing-10) * 4), 28dvh, calc(var(--spacing-10) * 6))" padding={3}><Text type="supporting" color="secondary">{tr("正在准备编辑器…")}</Text></Stack>}>
          <CommandCodeEditor key={draft.id} value={draft.command} disabled={saving} invalid={!!error && !validCommand(draft.command)}
            onChange={commandText => { setDraft(current => current ? { ...current, command: commandText } : null); setError('') }}
            onSave={() => form.current?.requestSubmit()} />
        </Suspense>
      </Stack>
    </Stack></FormLayout>
    <Text id="command-editor-help" type="supporting" color="secondary">{tr("末尾留空行会立即执行，否则填入终端，等待你回车。")}</Text>
    {stale && <DraftRevisionNotice busy={saving || !editable} onReview={latest && base.record ? () => {
      setDraft(reviewDraft(base.record!, draft, latest))
      setBase({ record: latest, revision })
      setError('')
    } : undefined} />}
    {!stale && error && <Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
  </EditorDialog>
}
