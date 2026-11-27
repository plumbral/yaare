import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { Fragment, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Layout, LayoutHeader, LayoutContent } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Heading, Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { Tooltip } from '@astryxdesign/core/Tooltip'
import { List, ListItem, type ListItemProps } from '@astryxdesign/core/List'
import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'
import { Bars3Icon, CheckIcon, PencilSquareIcon, TrashIcon, XMarkIcon, FolderPlusIcon } from '@heroicons/react/24/outline'
import {childGroupName,groupLeaf,groupParent,groupLabel,isComposingEnter} from '../connections/groupCreation'
import { moveHostIDs } from '../connections/hostOrdering'
import { commandUngrouped } from './shortcuts'
import { findShortcut, shortcutAria, shortcutText, useShortcutOverrides } from './shortcutReference'
import { validHostGroup } from './savedWorkspace'
import type { WorkspaceChange } from './savedWorkspace'
import { useGroupOrderDrag } from './useGroupOrderDrag'

export default function GroupManagementPanel({
  groups,
  open,
  busy,
  onApply,
  onDelete,
  onClose,
  createRequest,
  revision,
  kind = 'command',
}: {
  groups: string[]
  open: boolean
  busy: boolean
  onApply: (change: WorkspaceChange, expectedRevision?: number) => Promise<boolean>
  onDelete: (name: string) => void
  onClose: () => void
  kind?: 'command' | 'host'
  createRequest?:{version:number;parent?:string}
  revision?: number
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()
  const orderKeys = [shortcutText('move-up',shortcuts),shortcutText('move-down',shortcuts)].filter(Boolean).join(' / ')

  const scope = kind === 'command' ? tr("命令") : tr("主机")
  const inputId = `new-${kind}-group`
  const errorId = `${kind}-group-error`
  const orderedGroups = useMemo(() => groups.filter(group => group !== commandUngrouped), [groups])
  const [name, setName] = useState('')
  const [original, setOriginal] = useState<string | null>(null)
  const [renamed, setRenamed] = useState('')
  const [error, setError] = useState<{ field: 'new' | 'rename' | 'child'; text: string } | null>(null)
  const [childParent,setChildParent]=useState<string|null>(null)
  const [childName,setChildName]=useState('')
  const childInput=useRef<HTMLInputElement>(null)
  const composing=useRef(false)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [orderError, setOrderError] = useState('')
  const pending = useRef(false)
  const newInput = useRef<HTMLInputElement>(null)
  const renameInput = useRef<HTMLInputElement>(null)
  const viewport = useRef<HTMLDivElement>(null)
  const disabled = busy || saving
  const canReorder = kind === 'host' && open && !disabled && !original && !childParent && orderedGroups.length > 1
  const drag = useGroupOrderDrag(viewport, orderedGroups, canReorder, (name, to) => void moveGroup(name, to))
  useEffect(() => {
    if (open) (original ? renameInput.current : newInput.current)?.focus({ preventScroll: true })
  }, [open])
  const handledCreation=useRef<number|undefined>(undefined)
  useEffect(()=>{
    if(!open||!createRequest||handledCreation.current===createRequest.version)return
    handledCreation.current=createRequest.version
    setOriginal(null);setError(null)
    setChildParent(createRequest.parent??null);setChildName('')
    if(!createRequest.parent)setName('')
    requestAnimationFrame(()=>(createRequest.parent?childInput.current:newInput.current)?.focus({preventScroll:true}))
  },[open,createRequest])

  async function moveGroup(group: string, to: number) {
    if (!canReorder || pending.current || orderedGroups.indexOf(group) === to) return
    const moved = moveHostIDs(orderedGroups, group, to)
    if (moved.every((name, index) => name === orderedGroups[index])) return
    let index = 0
    const order = groups.map(name => name === commandUngrouped ? name : moved[index++]!)
    pending.current = true
    setOrderError('')
    try {
      if (await onApply({ kind: 'reorderGroups', groups: order }, revision)) {
        setMessage(tr("已调整 {p0} 的位置", groupLabel(group)))
      } else {
        setOrderError(tr("分组顺序未保存，请重试。"))
      }
    } catch {
      setOrderError(tr("分组顺序未保存，请重试。"))
    } finally {
      pending.current = false
      requestAnimationFrame(() => {
        const handle = document.getElementById(`host-group-order-${group}`)
        const focused = document.activeElement
        if (handle?.getClientRects().length && (focused === document.body || focused === handle)) {
          handle.scrollIntoView({ block: 'nearest', inline: 'nearest' })
          handle.focus({ preventScroll: true })
        }
      })
    }
  }

  async function save(event: FormEvent, field: 'new' | 'rename' | 'child') {
    event.preventDefault()
    if (disabled || pending.current || composing.current) return
    const leaf = (field === 'new' ? name : field==='child'?childName:renamed).trim()
    let next=leaf
    const input = field === 'new' ? newInput.current : field==='child'?childInput.current:renameInput.current
    const fail = (text: string) => {
      setError({ field, text })
      input?.focus()
    }
    if(kind==='host'){
      try {next=childGroupName(field==='child'?childParent??'':field==='rename'?groupParent(original??''):'',leaf,field==='rename'?groups.filter(g=>g!==original):groups)}
      catch(e){fail(e instanceof Error?e.message:tr('分组未保存，输入已保留，请重试。'));return}
    }
    if (
      kind === 'host'
        ? !validHostGroup(next)
        : !next || [...next].length > 32 || /[\p{Cc}\u2028\u2029]/u.test(next)
    ) {
      fail(tr("请输入 1–32 个字符的分组名称。"))
      return
    }
    if (groups.includes(next) && (field === 'new' || next !== original)) {
      fail(tr("此分组已存在，请换一个名称。"))
      return
    }
    pending.current = true
    setSaving(true)
    setError(null)
    try {
      const change: WorkspaceChange =
        field !== 'rename'
          ? { kind: kind === 'command' ? 'addCommandGroup' : 'addGroup', name: next }
          : {
              kind: kind === 'command' ? 'renameCommandGroup' : 'renameGroup',
              name: original!,
              newName: next,
            }
      if (!(await onApply(change))) {
        fail(tr("分组未保存，输入已保留，请重试。"))
        return
      }
      if(field==='child'){
        setChildParent(null);setChildName('')
        requestAnimationFrame(()=>document.getElementById(`${kind}-group-edit-${next}`)?.focus({preventScroll:true}))
      }else if (field === 'new') {
        setName('')
        newInput.current?.focus()
      } else {
        setOriginal(null)
        setRenamed('')
        requestAnimationFrame(() =>
          document.getElementById(`${kind}-group-edit-${next}`)?.focus({ preventScroll: true }),
        )
      }
      setMessage(field !== 'rename' ? tr("已添加分组「{p0}」", groupLabel(next)) : tr("已修改分组为「{p0}」", groupLabel(next)))
    } catch {
      fail(tr("保存失败，输入已保留，请重试。"))
    } finally {
      pending.current = false
      setSaving(false)
    }
  }

  const cancelRename = () => {
    const old = original
    setOriginal(null)
    setRenamed('')
    setError(null)
    requestAnimationFrame(() =>
      document.getElementById(`${kind}-group-edit-${old}`)?.focus({ preventScroll: true }),
    )
  }
  const groupRow = (group: string): ListItemProps => ({
    label:
      original === group ? (
        <form
          aria-label={tr("修改{p0}分组 {p1}", scope, group)}
          autoComplete="off"
          noValidate
          onSubmit={(event) => void save(event, 'rename')}
        >
          <FormLayout defaultOptionality="required">
            <Stack direction="horizontal" align="center" gap={1}>
              <StackItem size="fill" style={{ minWidth: 0 }}>
                <TextInput
                  ref={renameInput}
                  label={tr("分组名称")}
                  isLabelHidden
                  hasAutoFocus
                  value={renamed}
                  onChange={(value) => {
                    setRenamed(value)
                    setError(null)
                  }}
                  size="md"
                  isRequired
                  isDisabled={disabled}
                  autoComplete="off"
                  htmlName={`rename-${kind}-group`}
                  aria-invalid={error?.field === 'rename' || undefined}
                  aria-describedby={error?.field === 'rename' ? errorId : undefined}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && (composing.current||isComposingEnter(event.nativeEvent))) event.preventDefault()
                  }}
                />
              </StackItem>
              <IconButton
                label={tr("保存分组名称 {p0}", group)}
                tooltip={tr("保存名称")}
                type="submit"
                variant="ghost"
                size="md"
                isDisabled={disabled}
                icon={<Icon icon={CheckIcon} size="md" />}
              />
              <IconButton
                label={tr("取消修改分组")}
                tooltip={tr("取消修改")}
                variant="ghost"
                size="md"
                isDisabled={disabled}
                icon={<Icon icon={XMarkIcon} size="md" />}
                onClick={cancelRename}
              />
            </Stack>
          </FormLayout>
        </form>
      ) : kind === 'host' ? (
        <Tooltip content={groupLabel(group)} hasHoverIndication={false} touchTrigger="none">
          <Text type="label" maxLines={1}>{groupLabel(group)}</Text>
        </Tooltip>
      ) : group,
    endContent:
      original === group ? undefined : (
        <Stack direction="horizontal" align="center" gap={1}>
          {kind === 'host' && <IconButton id={`host-group-order-${group}`} label={tr("调整分组顺序 {p0}", groupLabel(group))}
            tooltip={orderKeys?tr('{p0} · {p1}',tr('拖动排序'),orderKeys):tr('拖动排序')} aria-keyshortcuts={[shortcutAria('move-up',shortcuts),shortcutAria('move-down',shortcuts)].filter(Boolean).join(' ')||undefined}
            variant="ghost" size="md" isDisabled={!canReorder} icon={<Icon icon={Bars3Icon} size="sm" />}
            style={{ cursor: canReorder ? 'grab' : undefined, touchAction: 'none', userSelect: 'none' }}
            onPointerDown={event => drag.grab(event, group)} onPointerMove={drag.move} onPointerUp={drag.drop}
            onPointerCancel={drag.cancelPointer} onLostPointerCapture={drag.cancelPointer}
            onKeyDown={event => {
              const action = canReorder && findShortcut(event.nativeEvent,['move-up','move-down'])
              if (!action) return
              event.preventDefault()
              event.stopPropagation()
              if(!event.repeat)void moveGroup(group, orderedGroups.indexOf(group) + (action === 'move-up' ? -1 : 1))
            }} />}
          {kind==='host'&&<IconButton id={`host-group-child-${group}`} label={tr('在「{p0}」下创建子分组',groupLabel(group))} tooltip={tr('创建子分组')} type="button" variant="ghost" size="md" isDisabled={disabled||group.split('/').length>=8} icon={<Icon icon={FolderPlusIcon} size="sm"/>} onClick={()=>{setOriginal(null);setChildParent(group);setChildName('');setError(null)}}/>}
          <IconButton
            id={`${kind}-group-edit-${group}`}
            label={tr("修改分组 {p0}", group)}
            tooltip={tr("修改名称")}
            variant="ghost"
            size="md"
            isDisabled={disabled}
            icon={<Icon icon={PencilSquareIcon} size="sm" />}
            onClick={() => {
              setOriginal(group)
              setRenamed(kind==='host'?groupLeaf(group):group)
              setChildParent(null)
              setError(null)
            }}
          />
          <IconButton
            label={tr("删除分组 {p0}", group)}
            tooltip={tr("删除分组")}
            variant="ghost"
            size="md"
            isDisabled={disabled}
            icon={<Icon icon={TrashIcon} size="sm" />}
            onClick={() => onDelete(group)}
          />
        </Stack>
      ),
  })
  const childEditor = (g: string) => <ListItem key={'child-editor:'+g} label={<form aria-label={tr('在「{p0}」下创建子分组',groupLabel(g))} autoComplete="off" noValidate onSubmit={event=>void save(event,'child')} onKeyDown={event=>{if(event.key==='Escape'&&!composing.current&&!isComposingEnter(event.nativeEvent)){event.preventDefault();event.stopPropagation();setChildParent(null);setChildName('');setError(null);document.getElementById('host-group-child-'+g)?.focus({preventScroll:true})}}}>
           <Stack direction="horizontal" gap={1} align="center">
            <StackItem size="fill" style={{minWidth:0}}><TextInput ref={childInput} label={tr('子分组名称')} isLabelHidden hasAutoFocus placeholder={tr('子分组名称')} size="md" width="100%" value={childName} onChange={value=>{setChildName(value);setError(null)}} isDisabled={disabled} autoComplete="off" hasSpellCheck={false} aria-invalid={error?.field==='child'||undefined} aria-describedby={error?.field==='child'?errorId:undefined} onKeyDown={event=>{if(event.key==='Enter'&&(composing.current||isComposingEnter(event.nativeEvent)))event.preventDefault()}}/></StackItem>
            <IconButton label={tr('创建子分组')} tooltip={tr('创建子分组')} type="submit" variant="ghost" size="md" isDisabled={disabled} icon={<Icon icon={CheckIcon} size="sm"/>}/>
            <IconButton label={tr('取消创建子分组')} tooltip={tr('取消')} type="button" variant="ghost" size="md" isDisabled={disabled} icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={()=>{setChildParent(null);setError(null);document.getElementById('host-group-child-'+g)?.focus({preventScroll:true})}}/>
           </Stack>
          </form>} />
  return (
    <Stack
      height="100%"
      style={{ minWidth: 0, minHeight: 0 }}
      onCompositionStart={()=>{composing.current=true}}
      onCompositionEnd={()=>{composing.current=false}}
      onKeyDown={(event) => {
        if (
          event.key === 'Escape' &&
          !event.defaultPrevented &&
          !event.nativeEvent.isComposing &&
          !disabled
        ) {
          event.preventDefault()
          if(drag.cancel())return
          if(childParent){const parent=childParent;setChildParent(null);setError(null);document.getElementById('host-group-child-'+parent)?.focus({preventScroll:true})}
          else if (original) cancelRename()
          else onClose()
        }
      }}
    >
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={3} paddingBlockEnd={2}>
            <Stack gap={3}>
              <Stack direction="horizontal" align="center" justify="between" gap={2}>
                <Heading level={4} accessibilityLevel={2} weight="semibold">
                  {tr("分组管理")}</Heading>
                <IconButton
                  label={tr("关闭{p0}分组", scope)}
                  tooltip={tr("收起分组管理 · Esc")}
                  variant="ghost"
                  size="md"
                  icon={<Icon icon={XMarkIcon} size="md" />}
                  onClick={onClose}
                />
              </Stack>
              <form
                aria-label={tr("添加{p0}分组", scope)}
                autoComplete="off"
                noValidate
                onSubmit={(event) => void save(event, 'new')}
              >
                <FormLayout defaultOptionality="required">
                  <Stack gap={1}>
                    <Stack direction="horizontal" align="center" gap={2}>
                      <StackItem size="fill" style={{ minWidth: 0 }}>
                        <TextInput
                          ref={newInput}
                          id={inputId}
                          label={tr("新分组名称")}
                          isLabelHidden
                          value={name}
                          onChange={(value) => {
                            setName(value)
                            setError(null)
                          }}
                          placeholder={tr("新分组名称")}
                          size="md"
                          autoComplete="off"
                          htmlName={inputId}
                          isRequired
                          isDisabled={disabled}
                          aria-invalid={error?.field === 'new' || undefined}
                          aria-describedby={error?.field === 'new' ? errorId : undefined}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter' && (composing.current||isComposingEnter(event.nativeEvent))) event.preventDefault()
                          }}
                        />
                      </StackItem>
                      <Button
                        label={tr("添加")}
                        type="submit"
                        variant="secondary"
                        size="md"
                        isDisabled={disabled}
                        isLoading={saving && !original}
                      />
                    </Stack>
                  </Stack>
                </FormLayout>
              </form>
            </Stack>
          </LayoutHeader>
        }
        content={
          <LayoutContent
            ref={viewport}
            padding={3}
            isScrollable
            style={{ paddingBlockStart: 0, minHeight: 0, minWidth: 0, overscrollBehavior: 'contain' }}
          >
            {kind==='host'&&!orderedGroups.length?<Text type="supporting" color="secondary">{tr("还没有分组，可在上方添加。")}</Text>:(
              <List aria-label={tr("{p0}分组管理列表", scope)} density="compact" edge="outset">
                {orderedGroups.map((group) => (
                  <Fragment key={group}>
                    <ListItem {...groupRow(group)} itemKey={group} data-group-order-row={kind === 'host' ? '' : undefined}
                      style={{ position: 'relative' }} />
                    {kind === 'host' && childParent === group && childEditor(group)}
                  </Fragment>
                ))}
              </List>
            )}
            {kind === 'host' && <Stack ref={drag.marker} aria-hidden="true" style={{ display: 'none', position: 'fixed', zIndex: 3,
              pointerEvents: 'none', borderBlockStart: 'var(--border-width) solid var(--color-text-primary)' }} />}
            {orderError && <Text type="supporting" role="alert">{orderError}</Text>}
            {error && (
              <Text id={errorId} type="supporting" role="alert">
                {error.text}
              </Text>
            )}
            <VisuallyHidden as="p" role="status">
              {message}
            </VisuallyHidden>
          </LayoutContent>
        }
      />
    </Stack>
  )
}
