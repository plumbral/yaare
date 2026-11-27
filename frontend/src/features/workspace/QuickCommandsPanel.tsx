import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {writeClipboardText} from '../../shared/bridge/application'
import { lazy, Suspense, useCallback, useDeferredValue, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Layout, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Selector } from '@astryxdesign/core/Selector'
import ManagementToolbar, {managementSearchStyle} from '../../shared/ui/ManagementToolbar'
import GroupFilters from '../../shared/ui/GroupFilters'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { Dialog } from '../../shared/ui/AppDialog'
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'
import type { ListItemProps } from '@astryxdesign/core/List'
import { PencilSquareIcon, PlusIcon, FolderIcon, DocumentDuplicateIcon, EllipsisHorizontalIcon, XMarkIcon } from '@heroicons/react/24/outline'
import ActionDialogHeader from '../../shared/ui/ActionDialogHeader'
import WindowedList from '../../shared/ui/WindowedList'
import ListSortControl, { sortMenuItems } from '../../shared/ui/ListSortControl'
import { nextListSort, readListSort, useLocalListView } from '../../shared/ui/listSort'
import { commandGroup, commandIcon, commandSortKeys, commandUngrouped, quickCommandExecutes, sortCommandRows, type CommandSortKey, type QuickCommand } from './shortcuts'
import OsIcon from '../../shared/icons/OsIcon'
import type { WorkspaceChange } from './savedWorkspace'
import GroupManagementPanel from './GroupManagementPanel'
import CommandEditorDialog from './CommandEditorDialog'
import AuxiliaryPanel from '../../shared/ui/AuxiliaryPanel'

const ConfirmDelete = lazy(() => import('../../shared/ui/ConfirmDelete'))
type Target = { id: string; name: string }
type Entry = { item: QuickCommand; preview: string; search: string }
const nameWidth = 'calc(var(--spacing-10) * 5)'
const groupWidth = 'calc(var(--spacing-10) * 3)'
const autoRunWidth = 'calc(var(--spacing-10) * 2.75)'
const commandSortLabel = (key: CommandSortKey) => key === 'name' ? tr("名称") : tr("分组")
const readCommandSort = (value: unknown) => readListSort(value, commandSortKeys)
const displayedGroup = (item: QuickCommand) => commandGroup(item) === commandUngrouped ? '—' : commandGroup(item)
const commandAutoRun = (item: QuickCommand) => quickCommandExecutes(item.command) ? tr("是") : tr("否")

export default function QuickCommandsPanel({ commands, revision = 0, groups, onApply, targets, activeTarget, onSend, editable = true, active = true, caption }: {
  commands: QuickCommand[]; revision?: number; groups: string[]; onApply: (change: WorkspaceChange, expectedRevision?: number) => Promise<boolean>
  targets: Target[]; activeTarget?: string; onSend: (command: QuickCommand, targetId: string) => string | undefined
  editable?: boolean; active?: boolean; caption?:HTMLElement|null
}) {
  const language = useLocale()

  const [query, setQuery] = useState('')
  const [selectedGroup, setGroup] = useState('all')
  const [sort, setSort] = useLocalListView('yaare.command-list-sort.v1', readCommandSort)
  const collator = useMemo(() => new Intl.Collator(language, { numeric: true, sensitivity: 'base' }), [language])
  const deferredQuery = useDeferredValue(query.trim().toLowerCase())
  const listPane = useRef<HTMLElement>(null)
  const [wide, setWide] = useState(false)
  useLayoutEffect(() => {
    const element = listPane.current
    if (!element) return
    const measure = (width: number) => { if (width > 0) setWide(width >= 850) }
    measure(element.getBoundingClientRect().width)
    const observer = new ResizeObserver(entries => { if (entries[0]) measure(entries[0].contentRect.width) })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const [draft, setDraft] = useState<QuickCommand | null>(null)
  const [draftRevision, setDraftRevision] = useState<number>()
  const [editOpen, setEditOpen] = useState(false)
  const pendingSavedRow = useRef<string | undefined>(undefined)
  const [savedRow, setSavedRow] = useState<{key: string}>()
  const [message, setMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const pending = useRef(false)
  const [groupsOpen, setGroupsOpen] = useState(false)
  const closeGroups = () => { document.getElementById('command-manage-groups')?.focus({ preventScroll: true }); setGroupsOpen(false) }
  const [deleting, setDeleting] = useState<{ kind: 'command' | 'group'; name: string; id?: string } | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const [sendCommand, setSendCommand] = useState<QuickCommand | null>(null)
  const [sendOpen, setSendOpen] = useState(false)
  const [sendTarget, setSendTarget] = useState('')
  const [sendError, setSendError] = useState('')
  const namedGroups = useMemo(() => groups.filter(name => name !== commandUngrouped), [groups])
  const group = namedGroups.includes(selectedGroup.slice(6)) ? selectedGroup : 'all'
  const entries = useMemo<Entry[]>(() => commands.map(item => {
    const lines = item.command.split('\n')
    return { item, preview: lines.find(line => line.trim()) || '', search: `${item.name}\n${item.command}\n${commandGroup(item)}`.toLowerCase() }
  }), [commands, getLanguage()])
  const filtered = useMemo(() => sortCommandRows(entries.filter(e =>
    (group === 'all' || commandGroup(e.item) === group.slice(6)) && e.search.includes(deferredQuery)), e => e.item, sort, collator),
    [entries, group, deferredQuery, sort, collator])
  useEffect(() => { if (selectedGroup !== group) setGroup(group) }, [selectedGroup, group])
  useEffect(() => { if (!active) { setEditOpen(false); setGroupsOpen(false); setSendOpen(false); setDeleteOpen(false) } }, [active])
  const beginEdit = useCallback((item?: QuickCommand) => {
    const original = item && commands.find(command => command.id === item.id)
    if (item && !original) return
    setDraftRevision(original ? revision : undefined)
    const next = original ? { ...original, group: commandGroup(original) } : { id: crypto.randomUUID(), name: '', command: '', group: group === 'all' ? commandUngrouped : group.slice(6) }
    setDraft(next); setEditOpen(true)
  }, [commands, group, revision])
  const copy = useCallback(async (item: QuickCommand) => {
    try { await writeClipboardText(item.command); setMessage(tr("已复制「{p0}」", item.name)) }
    catch { setMessage(tr("复制未完成，请打开编辑器选中内容后复制。")) }
  }, [])
  const requestDelete = (value: NonNullable<typeof deleting>) => {
    document.getElementById(value.kind === 'group' ? 'new-command-group' : 'command-search')?.focus({ preventScroll: true })
    setDeleting(value); setDeleteError(''); setDeleteOpen(true)
  }
  const askSend = useCallback((item: QuickCommand) => {
    setSendCommand({ ...item }); setSendTarget(targets.some(t => t.id === activeTarget) ? activeTarget! : targets[0]?.id || '')
    setSendError(''); setSendOpen(true)
  }, [targets, activeTarget])
  const renderRow = useCallback((entry: Entry): ListItemProps => ({
    id: `command-row-${entry.item.id}`,
    label: wide ? <Stack direction="horizontal" align="center" gap={4} width="100%">
      <Text type="label" maxLines={1} style={{ width: nameWidth, flexShrink: 0 }}>{entry.item.name}</Text>
      <StackItem size="fill" style={{ minWidth: 0 }}><Text type="supporting" maxLines={1} color="secondary" style={{ fontFamily: 'var(--font-family-code)' }}>{entry.preview}</Text></StackItem>
    </Stack> : <Stack style={{ minWidth: 0 }}>
      <Text type="label" maxLines={1}>{entry.item.name}</Text>
      <Text type="supporting" color="secondary" maxLines={1} style={{fontFamily:'var(--font-family-code)'}}>{entry.preview}</Text>
      <Stack direction="horizontal" align="center" gap={2} wrap="wrap" style={{minWidth:0}}>
        <Text type="supporting" color="secondary" maxLines={1}>{displayedGroup(entry.item)}</Text>
        <Text type="supporting" color="secondary">{tr("自动执行：{p0}", commandAutoRun(entry.item))}</Text>
      </Stack>
    </Stack>,
    startContent: <OsIcon name={commandIcon(entry.item)} />,
    onClick: () => beginEdit(entry.item),
    endContent: <Stack direction="horizontal" gap={1} align="center">
      {wide && <>
        <Text type="supporting" color="secondary" maxLines={1} style={{ width: groupWidth }}>{displayedGroup(entry.item)}</Text>
        <Text type="supporting" color="secondary" style={{ width: autoRunWidth }}>{commandAutoRun(entry.item)}</Text>
      </>}
      <IconButton label={tr("复制命令 {p0}", entry.item.name)} tooltip={tr("复制命令")} variant="ghost" size="sm" icon={<Icon icon={DocumentDuplicateIcon} size="sm" />} onClick={event => { event.stopPropagation(); void copy(entry.item) }} />
      <IconButton label={tr("编辑命令 {p0}", entry.item.name)} tooltip={tr("编辑命令")} variant="ghost" size="sm" isDisabled={!editable} icon={<Icon icon={PencilSquareIcon} size="sm" />} onClick={event => { event.stopPropagation(); beginEdit(entry.item) }} />
      <Stack onClick={event => event.stopPropagation()}><DropdownMenu presentation="popover" alignment="end" menuWidth="calc(var(--spacing-10) * 5)"
        renderTrigger={props => <IconButton {...props} label={tr("命令操作 {p0}", entry.item.name)} tooltip={tr("更多操作")} variant="ghost" size="sm" icon={<Icon icon={EllipsisHorizontalIcon} size="sm" />} />}
        items={[{ label: tr("发送到会话…"), isDisabled: targets.length === 0, onClick: () => askSend(entry.item) }, { type: 'divider' },
          { label: tr("删除命令"), variant: 'destructive', isDisabled: !editable, onClick: () => requestDelete({ kind: 'command', name: entry.item.name, id: entry.item.id }) }]} /></Stack>
    </Stack>,
  }), [wide, beginEdit, copy, askSend, targets.length, editable, language])
  const sortHeader = (key: CommandSortKey) => <ListSortControl label={commandSortLabel(key)}
    direction={sort?.key === key ? sort.direction : undefined} onClick={() => setSort(current => nextListSort(current, key))} />
  const heading: ListItemProps | undefined = wide ? {
    startContent: <Stack aria-hidden="true" width="var(--spacing-4)"/>,
    label: <Stack direction="horizontal" align="center" gap={4} width="100%">
      <Stack width={nameWidth} style={{flexShrink:0}}>{sortHeader('name')}</Stack>
      <StackItem size="fill"><Text type="supporting" color="secondary">{tr("命令预览")}</Text></StackItem>
    </Stack>,
    endContent: <Stack direction="horizontal" align="center" gap={1}>
      <Stack width={groupWidth}>{sortHeader('group')}</Stack>
      <Text type="supporting" color="secondary" style={{width:autoRunWidth}}>{tr("自动执行")}</Text>
      <Stack align="end" width="calc(var(--size-element-sm) * 3 + var(--spacing-1) * 2)"><Text type="supporting" color="secondary">{tr("操作")}</Text></Stack>
    </Stack>,
    style: {minHeight:'var(--size-element-md)'},
  } : undefined
  return <Stack height="100%" style={{ minHeight: 0, minWidth: 0 }}>
    <Layout height="fill" content={<LayoutContent padding={0} isScrollable={false} style={{ minWidth: 0, minHeight: 0, display: 'flex' }}><StackItem ref={listPane} size="fill" style={{ minWidth: 0, minHeight: 0 }}>
    <Layout height="fill" header={<ManagementToolbar title={tr("快捷命令")} caption={caption}
      search={<TextInput id="command-search" label={tr("搜索快捷命令")} isLabelHidden type="search" value={query} onChange={setQuery} placeholder={tr("搜索名称或命令…")}
        startIcon="search" size="md" hasClear width="100%" containerStyle={managementSearchStyle} autoComplete="off" htmlName="command-query" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none"/>}
      navigation={<GroupFilters id="command-group-filters" label={tr("命令分组筛选")} value={group} onChange={setGroup}
        options={[{value:'all',label:tr("全部")},...namedGroups.map(name=>({value:'group:'+name,label:name}))]}/> }>
      {compact=><>{!wide && <DropdownMenu presentation="popover" alignment="end"
        items={sortMenuItems(commandSortKeys, commandSortLabel, sort, setSort, tr("原顺序"))}
        renderTrigger={props => <Button {...props} label={tr("排序")} variant="ghost" size="md" />} />}
      {compact?<DropdownMenu presentation="popover" alignment="end" items={[
        {label:tr("管理命令分组"),isDisabled:!editable,onClick:()=>{if(groupsOpen)closeGroups();else setGroupsOpen(true)}},
      ]} renderTrigger={props=><IconButton {...props} id="command-manage-groups" label={tr("更多")} tooltip={tr("更多")}
        size="md" variant="ghost" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>}/>
      :<IconButton id="command-manage-groups" label={tr("管理命令分组")} tooltip={tr("管理命令分组")} aria-expanded={groupsOpen} aria-controls="command-groups-panel"
        variant={groupsOpen?'secondary':'ghost'} size="md" isDisabled={!editable} icon={<Icon icon={FolderIcon} size="sm"/>} onClick={()=>{if(groupsOpen)closeGroups();else setGroupsOpen(true)}}/>}
      <Button id="quick-command-add" label={tr("新建命令")} tooltip={tr("新建命令")} variant="secondary" size="md" isIconOnly={compact} isDisabled={!editable} icon={<Icon icon={PlusIcon} size="sm"/>} onClick={()=>beginEdit()}/></>}
    </ManagementToolbar>} content={<LayoutContent padding={0} isScrollable={false} style={{ minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {filtered.length ? <WindowedList savedRow={active ? savedRow : undefined} onSavedRowShown={()=>setSavedRow(undefined)} header={heading} headerInteractive items={filtered} itemKey={entry => entry.item.id} row={renderRow}
        estimateSize={() => wide ? 48 : 68} measurementKey={String(wide)} label={tr("快捷命令管理列表")} resetKey={`${group}:${deferredQuery}`} /> :
        <Stack align="center" justify="center" gap={3} width="100%" height="100%" padding={6} style={{ textAlign: 'center' }}>
          <Text type="label">{commands.length ? tr("没有匹配的命令") : tr("还没有快捷命令")}</Text>
          <Text type="supporting" color="secondary">{commands.length ? tr("尝试其他关键词或分组。") : tr("保存常用命令，在不同主机间复用。")}</Text>
          {commands.length > 0 && <Button label={tr("清除筛选")} size="sm" variant="secondary" onClick={() => { setQuery(''); setGroup('all') }} />}
        </Stack>}
    </LayoutContent>} footer={message?<LayoutFooter padding={3}><Text type="supporting" color="secondary" role="status">{localizeMessage(message)}</Text></LayoutFooter>:undefined} />
    </StackItem></LayoutContent>} end={<AuxiliaryPanel id="command-groups-panel" label={tr("命令分组管理")} open={groupsOpen}>
      <GroupManagementPanel groups={groups} open={groupsOpen} busy={!editable} onApply={onApply} onDelete={name => requestDelete({ kind: 'group', name })} onClose={closeGroups} />
    </AuxiliaryPanel>} />

    <CommandEditorDialog isOpen={editOpen} command={draft}
      existing={draftRevision !== undefined} initialRevision={draftRevision} revision={revision}
      latest={editOpen && draft ? commands.find(item => item.id === draft.id) : undefined} groups={groups} editable={editable}
      onApply={onApply} onClose={() => setEditOpen(false)}
      onSaved={saved => {
        pendingSavedRow.current = saved.id
        if (!`${saved.name}\n${saved.command}\n${commandGroup(saved)}`.toLowerCase().includes(query.trim().toLowerCase())) setQuery('')
        if (group !== 'all' && group.slice(6) !== commandGroup(saved)) setGroup('all')
        setMessage(tr("已保存「{p0}」", saved.name))
      }} onAfterClose={() => {
        const key = pendingSavedRow.current
        pendingSavedRow.current = undefined
        if (key && active) setSavedRow({ key })
      }} />

    {deleting && <Suspense fallback={null}><ConfirmDelete isOpen={deleteOpen} title={deleting.kind === 'command' ? tr("删除命令「{p0}」？", deleting.name) : tr("删除命令分组「{p0}」？", deleting.name)}
      description={deleting.kind === 'command' ? tr("仅删除保存的快捷命令，不影响已发送到服务器的内容。") : tr("该分组下的命令会保留，并取消分组归属。")} busy={saving || !editable} error={localizeMessage(deleteError)}
      onClose={() => setDeleteOpen(false)} onConfirm={async () => { if (!editable || pending.current) return; pending.current = true; setSaving(true); try {
        if (await onApply(deleting.kind === 'command' ? { kind: 'deleteCommand', id: deleting.id! } : { kind: 'deleteCommandGroup', name: deleting.name })) { setDeleteOpen(false); setMessage(tr("已删除，其他内容保持原状。")) }
        else setDeleteError(tr("删除未完成，请重试。"))
      } catch { setDeleteError(tr("删除失败，原内容已保留。")) } finally { pending.current = false; setSaving(false) } }} /></Suspense>}

    {sendCommand && <Dialog isOpen={sendOpen} onOpenChange={open => { if (!open) setSendOpen(false) }} purpose="form" padding={0} width="calc(var(--spacing-10) * 18)" maxHeight="85dvh">
      <Stack padding={4} gap={4} style={{ minHeight: 0, maxHeight: '85dvh' }}><ActionDialogHeader action="edit" title={tr("发送「{p0}」", sendCommand.name)} endContent={<IconButton label={tr("关闭命令发送")} variant="ghost" size="sm" icon={<Icon icon={XMarkIcon} size="sm" />} onClick={() => setSendOpen(false)} />} />
        <Selector label={tr("目标会话")} value={sendTarget} onChange={value => { setSendTarget(value); setSendError('') }} placeholder={tr("请选择已连接的会话")} size="sm" options={targets.map(t => ({ value: t.id, label: t.name }))} />
        <Stack style={{ minHeight: 0, overflow: 'auto' }}><CodeBlock code={sendCommand.command} language="bash" hasLineNumbers={sendCommand.command.split('\n').length >= 5} hasCopyButton={false} width="100%" maxHeight="35dvh" /></Stack>
        {sendError && <Text role="alert" type="supporting">{localizeMessage(sendError)}</Text>}
        <Stack direction="horizontal" justify="end" gap={2}><Button label={tr("取消发送")} variant="ghost" size="sm" onClick={() => setSendOpen(false)} />
          <Button label={quickCommandExecutes(sendCommand.command) ? tr("发送并执行") : tr("填入终端")} variant="primary" size="sm" isDisabled={!targets.some(t => t.id === sendTarget)} onClick={() => {
            const failure = onSend(sendCommand, sendTarget)
            if (failure) setSendError(failure)
            else setSendOpen(false)
          }} />
        </Stack>
      </Stack>
    </Dialog>}
  </Stack>
}
