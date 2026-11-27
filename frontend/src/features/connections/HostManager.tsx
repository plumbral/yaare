import { localizeMessage } from '../../shared/i18n/messages'
import './hostRowActions.css'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { shortcutAria, shortcutTooltip, useShortcutOverrides } from '../workspace/shortcutReference'
import { memo, lazy, Suspense, createRef, useCallback, useDeferredValue, useEffect,useLayoutEffect,useMemo, useRef, useState, type ReactNode } from 'react'
import { ScrollArea } from '@base-ui/react/scroll-area'
import { Layout, LayoutContent } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import ManagementToolbar, {managementSearchStyle} from '../../shared/ui/ManagementToolbar'
import GroupFilters from '../../shared/ui/GroupFilters'
import type {ContextMenuOption} from '@astryxdesign/core/ContextMenu'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu'
import {Tooltip} from '@astryxdesign/core/Tooltip'
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput'
import type { ListItemProps } from '@astryxdesign/core/List'
import { PencilSquareIcon, EllipsisHorizontalIcon,CheckCircleIcon,FolderIcon,PlusIcon } from '@heroicons/react/24/outline'
import OsIcon from '../../shared/icons/OsIcon'
import {VisuallyHidden} from '@astryxdesign/core/VisuallyHidden'
import {createHostSearchIndex, filterHostList, moveHostIDs, type HostMove, type HostScope} from './hostOrdering'
import WindowedList from '../../shared/ui/WindowedList'
import useHorizontalWheel from '../../shared/ui/useHorizontalWheel'
import '../../shared/ui/horizontalScrollFade.css'
import ListSortControl, { sortMenuItems } from '../../shared/ui/ListSortControl'
import { nextListSort, useLocalListView, type ListSort } from '../../shared/ui/listSort'
import { usePanelReveal } from '../../shared/ui/usePanelReveal'
import type { HostProfile } from '../workspace/mockGateway'
import type { ServerSystem } from '../../shared/generated/mygo'
import type {WorkspaceChange} from '../workspace/savedWorkspace'
import type {Identity} from './useIdentities'
import {patchBatchHosts,toggleHostSelection} from './batchHosts'

import HostOrderControl,{hostActionTrayWidth} from './HostOrderControl'
import useHostReorderDrag from './useHostReorderDrag'
import HostColumnPicker, { hostColumnLabel } from './HostColumnPicker'
import { hostColumnWidths, hostSubtitle, hostWithColumnMoved, hostWithColumnVisibility, normalizeHostView, sortHostRows, type HostColumn, type HostSortKey } from './hostColumns'

const BatchHostEditor=lazy(()=>import('./BatchHostEditor'))

const hostKey = (host: HostProfile) => host.id
const actionsWidth=(wide:boolean)=>`calc(var(--spacing-10) * ${wide?5:4})`
function GroupText({group}:{group:string}){
  useLocale()

  const assigned=!!group&&group!=='未分组',text=assigned?group.split('/').join(' / '):'—'
  return <Tooltip content={text} hasHoverIndication={false} touchTrigger="none" isEnabled={assigned}>
    <Text type="supporting" color="secondary" maxLines={1} aria-label={assigned?tr("分组：{p0}", text):tr("未指定分组")}>{text}</Text>
  </Tooltip>
}

const HostActions=memo(function HostActions({host,wide,opened,editable,onOpen,onEdit,menuItems,ordering}:{
  ordering:ReactNode;host:HostProfile;wide:boolean;opened:boolean;editable:boolean;onOpen:(host:HostProfile)=>void
  onEdit:(host:HostProfile,trigger:HTMLElement)=>void;menuItems:(host:HostProfile)=>ContextMenuOption[]
}){
  useLocale()

  return <Stack data-host-actions="" direction="horizontal" gap={1} align="center" justify="end" width={actionsWidth(wide)} style={{flexShrink:0}}>
    {ordering}
    {wide&&
      <IconButton id={`manager-edit-${host.id}`} label={tr("编辑主机 {p0}", host.name)} tooltip={tr("编辑 {p0}", host.name)} variant="ghost" size="md" isDisabled={!editable}
        icon={<Icon data-host-secondary-icon="" icon={PencilSquareIcon} size="sm"/>} onClick={event=>{event.stopPropagation();onEdit(host,event.currentTarget)}}/>
    }
    <Stack onClick={event=>event.stopPropagation()}>
      <DropdownMenu presentation="popover" alignment="end" menuWidth="calc(var(--spacing-10) * 5)"
        renderTrigger={props=><IconButton {...props} id={'manager-more-'+host.id} label={tr("主机操作 {p0}", host.name)} tooltip={tr("更多操作")} size="md" variant="ghost"
          icon={<Icon data-host-secondary-icon="" icon={EllipsisHorizontalIcon} size="sm"/>}/>}
        items={menuItems(host)}/>
    </Stack>
    <Button id={`manager-open-${host.id}`} label={opened?tr("进入会话"):tr("连接")} aria-label={opened?tr("进入会话 {p0}",host.name):tr("连接 {p0}",host.name)}
      tooltip={opened?tr("进入会话"):tr("连接服务器")} variant="secondary" size="md"
      onClick={event=>{event.stopPropagation();onOpen(host)}}/>
  </Stack>
})

const noPinnedHosts: readonly string[] = []

export default memo(function HostManager({ active, caption, hosts, detectedSystems, pinOrder = noPinnedHosts, groups, connectedHostIds, directHostIds, selectedId, savedRow, onSavedRowShown, searchRequest, resetQuery, group, onGroup, onOpen, onEdit, onCreate, groupContextActions, onManageGroups, groupsOpen, auxiliary, menuItems, onSelectHost, onPin, onImport, onRestoreDeleted, onQuickConnect, onApply, revision, identities, storageError, editable, storageReady }: {
  active: boolean; hosts: HostProfile[];pinOrder?:readonly string[]; connectedHostIds: ReadonlySet<string>; directHostIds: ReadonlySet<string>; selectedId: string; searchRequest: number; resetQuery: number; group: string
  savedRow?: {key: string}; onSavedRowShown?: () => void
  detectedSystems?: Record<string, ServerSystem>
  groups: string[]; editable: boolean; caption?:HTMLElement|null; onManageGroups:()=>void; groupsOpen:boolean; onImport: () => void; onQuickConnect:()=>void
  onRestoreDeleted?: () => void
  identities:Identity[];revision:number;storageError:string;onApply:(change:WorkspaceChange,expectedRevision?:number)=>Promise<boolean>
  auxiliary?: ReactNode
  storageReady: boolean
  menuItems: (host: HostProfile) => ContextMenuOption[]; onSelectHost: (id: string) => void
  onPin: (host: HostProfile) => Promise<boolean>
  onGroup: (value: string) => void
  onOpen: (host: HostProfile) => void; onEdit: (host: HostProfile, trigger: HTMLElement) => void
  onCreate?:(trigger:HTMLElement)=>void
  groupContextActions?:(name?:string)=>ContextMenuOption[]
}) {
  const language = useLocale()
  const shortcuts = useShortcutOverrides()
  const panel = useRef<HTMLElement>(null)
  const listPane=useRef<HTMLElement>(null)
  const columnScroll = useRef<HTMLDivElement>(null)
  const [view, setView] = useLocalListView('yaare.host-list.v1', normalizeHostView)
  const [columnsOpen, setColumnsOpen] = useState(false)
  const columns = useMemo(() => view.columns.filter(column => column.visible).map(column => column.key), [view.columns])
  const setColumnVisibility = useCallback((key: HostColumn, visible: boolean) =>
    setView(current => hostWithColumnVisibility(current, key, visible)), [setView])
  const moveColumn = useCallback((key: HostColumn, to: number) =>
    setView(current => hostWithColumnMoved(current, key, to)), [setView])
  const setSort = (sort: ListSort<HostSortKey>) => setView(current => ({ ...current, sort }))
  const [wide,setWide]=useState(false)
  const [rowGap,setRowGap]=useState(0)
  useLayoutEffect(()=>{
    const element=listPane.current
    if(!element)return
    setRowGap(Number.parseFloat(getComputedStyle(element).rowGap)||0)
    const measure=(width:number)=>{if(width>0)setWide(width>=760)}
    measure(element.getBoundingClientRect().width)
    const observer=new ResizeObserver(entries=>{if(entries[0])measure(entries[0].contentRect.width)})
    observer.observe(element)
    return()=>observer.disconnect()
  },[])
  const realGroups=useMemo(()=>groups.filter(name=>name!=='未分组'),[groups, getLanguage()])
  useEffect(()=>{if(group!=='all'&&!realGroups.includes(group.slice(6)))onGroup('all')},[group,realGroups,onGroup])
  usePanelReveal(panel, active)
  const [query, setQuery] = useState('')
  useLayoutEffect(() => { setQuery('') }, [resetQuery])
  useLayoutEffect(() => {
    if (!searchRequest) return
    const input = document.getElementById('manager-host-search') as HTMLInputElement | null
    input?.focus()
    input?.select()
  }, [searchRequest])
  const deferredQuery = useDeferredValue(query)
  const [scope,setScope]=useState<HostScope>('all')
  useLayoutEffect(() => {
    if (!savedRow || !active) return
    const host = hosts.find(item => item.id === savedRow.key)
    if (!host) { onSavedRowShown?.(); return }
    if (!filterHostList([host], query).length) setQuery('')
    if (!filterHostList([host], '', group).length) onGroup('all')
    if (!filterHostList([host], '', 'all', scope).length) setScope('all')
  }, [savedRow, active, hosts, query, group, scope, onGroup, onSavedRowShown])
  const [pinNotice,setPinNotice]=useState('')
  const [multi,setMulti]=useState(false),[selection,setSelection]=useState<string[]>([]),[batch,setBatch]=useState<'edit'|'delete'|null>(null),[bulkNotice,setBulkNotice]=useState(''),[bulkBusy,setBulkBusy]=useState(false)
  const anchor=useRef(''),shift=useRef(false),bulkLock=useRef(false)
  useEffect(()=>{setSelection([]);anchor.current=''},[query,group,scope,multi])
  useEffect(()=>{if(!bulkNotice)return;const timer=setTimeout(()=>setBulkNotice(''),7000);return()=>clearTimeout(timer)},[bulkNotice])
  const collator = useMemo(() => new Intl.Collator(language, { numeric: true, sensitivity: 'base' }), [language])
  const dateFormat = useMemo(() => new Intl.DateTimeFormat(language, { dateStyle: 'short', timeStyle: 'short' }), [language])
  const hostValue = useCallback((host: HostProfile, key: HostSortKey) => {
    switch (key) {
      case 'name': return host.name
      case 'group': return host.group === '未分组' ? undefined : host.group || undefined
      case 'system': return detectedSystems?.[host.id]?.label || host.system || undefined
      case 'status': return connectedHostIds.has(host.id) ? 1 : 0
      case 'lastUsed': return host.lastUsed && host.lastUsed > 0 && host.lastUsed <= 8.64e15 ? host.lastUsed : undefined
      case 'authentication': return host.identityId ? tr("已保存的身份") :
        host.authentication === 'password' ? tr("密码") : host.authentication === 'key' ? tr("密钥") : undefined
      default: return host[key] || undefined
    }
  }, [detectedSystems, connectedHostIds, language])
  const hostCell = (host: HostProfile, key: HostColumn) => {
    const value = hostValue(host, key)
    if (value === undefined) return '—'
    if (key === 'status') return value === 1 ? tr("已连接") : tr("未连接")
    if (key === 'lastUsed') return dateFormat.format(Number(value))
    if (key === 'notes') return String(value).replace(/\s+/g, ' ').trim() || '—'
    return String(value)
  }
  const sort = scope === 'recent' ? null : view.sort
  const searchText = useMemo(createHostSearchIndex, [hosts])
  const filtered = useMemo(() => sortHostRows(filterHostList(hosts, deferredQuery, group, scope, pinOrder, searchText), sort, collator, hostValue),
    [hosts, deferredQuery, group, scope, pinOrder, searchText, sort, collator, hostValue])
  const hasHeaderPicker = wide && filtered.length > 0
  const scrollColumns = wide && columns.length > 0 && filtered.length > 0
  const syncColumnScroll = () => listPane.current?.style.setProperty('--host-column-offset', `${-(columnScroll.current?.scrollLeft ?? 0)}px`)
  useLayoutEffect(syncColumnScroll, [columns, scrollColumns])
  useHorizontalWheel(columnScroll, listPane, { enabled: scrollColumns, forwardFromTarget: true })
  const orderPeers=useMemo(()=>{const pinned=filtered.filter(h=>h.pinned).map(h=>h.id),normal=filtered.filter(h=>!h.pinned).map(h=>h.id);return{pinned,normal,positions:new Map([...pinned.map((id,index)=>[id,index] as const),...normal.map((id,index)=>[id,index] as const)])}},[filtered])
  const [orderOpen,setOrderOpen]=useState<string|null>(null),[revealRequest,setRevealRequest]=useState<{key:string;serial:number;focusId:string;fallbackId?:string}>()
  const moveLock=useRef(false),moveSerial=useRef(0)
  const canOrder=active&&!multi&&!query.trim()&&!sort&&scope!=='recent'&&storageReady
  useEffect(()=>{setOrderOpen(null);anchor.current=''},[query,group,scope,sort,multi,active])
  useEffect(() => { setColumnsOpen(false) }, [hasHeaderPicker, active])
  useEffect(()=>{if(orderOpen&&!filtered.some(h=>h.id===orderOpen))setOrderOpen(null)},[filtered,orderOpen])
  const moveHost=useCallback(async(host:HostProfile,to:HostMove|number)=>{
    if(!canOrder||!editable||moveLock.current)return false
    const ids=host.pinned?orderPeers.pinned:orderPeers.normal,next=moveHostIDs(ids,host.id,to)
    if(next.every((id,index)=>id===ids[index]))return false
    const handleId = 'manager-order-' + host.id
    const actionId = 'host-move-' + to + '-' + host.id
    const keepAction = document.activeElement?.id === actionId && document.activeElement.matches(':focus-visible')
    moveLock.current=true
    try{
      const ok=await onApply({kind:host.pinned?'reorderPinnedHosts':'reorderHosts',ids:next},revision)
      if (ok) {
        const restoreAction = keepAction && (document.activeElement === document.body || [actionId, handleId].includes(document.activeElement?.id ?? ''))
        if (restoreAction) setOrderOpen(host.id)
        setRevealRequest({key:host.id, serial:++moveSerial.current, focusId:restoreAction ? actionId : handleId, fallbackId:handleId})
        setPinNotice(tr('已调整 {p0} 的位置',host.name))
        await new Promise<void>(resolve=>requestAnimationFrame(()=>resolve()))
      }
      return ok
    }finally{moveLock.current=false}
  },[canOrder,editable,orderPeers,onApply,revision])
  const drag=useHostReorderDrag({list:listPane,items:filtered,allowed:canOrder&&editable,onDrop:moveHost,onStart:()=>setOrderOpen(null)})
  const firstOrdinary = scope !== 'recent' && filtered[0]?.pinned ? filtered.find(host=>!host.pinned)?.id : undefined
  const pinHost = useCallback(async(host:HostProfile)=>{
    const ok=await onPin(host)
    if(ok)setPinNotice(host.pinned?tr('已取消置顶主机 {p0}',host.name):tr('已置顶主机 {p0}',host.name))
    return ok
  },[onPin])
  const ordered=useMemo(()=>filtered.map(h=>h.id),[filtered, getLanguage()])
  const selectedSet=useMemo(()=>new Set(selection),[selection, getLanguage()])
  const selectedHosts=useMemo(()=>filtered.filter(h=>selectedSet.has(h.id)),[filtered,selectedSet, getLanguage()])
  useEffect(()=>{const available=new Set(ordered);setSelection(previous=>previous.every(id=>available.has(id))?previous:previous.filter(id=>available.has(id)))},[ordered])
  const select=useCallback((id:string,checked:boolean,range:boolean)=>{if(!editable)return;setSelection(previous=>toggleHostSelection(previous,ordered,id,checked,anchor.current,range));if(!range)anchor.current=id},[ordered,editable])
  async function pinBatch(pinned:boolean){
    if(bulkLock.current||!selectedHosts.length)return
    bulkLock.current=true;setBulkBusy(true)
    try{const ok=await onApply({kind:'saveHosts',hosts:patchBatchHosts(selectedHosts,{pinned})},revision);setBulkNotice(ok?pinned?tr("已置顶 {p0} 台主机。", selectedHosts.length):tr("已取消置顶 {p0} 台主机。", selectedHosts.length):tr("批量操作未完成，请检查配置提示。"))}
    finally{bulkLock.current=false;setBulkBusy(false)}
  }
  const columnPicker = <HostColumnPicker columns={view.columns} open={columnsOpen} setOpen={setColumnsOpen}
    onVisibility={setColumnVisibility} onMove={moveColumn} onReset={() => setView(normalizeHostView(null))} />
  const columnStyle = {
    width: columns.length > 1 || columns[0] !== 'group' ? '68%' : '40%',
    flex: '0 1 auto', minWidth: 0,
  }
  const tracks = {
    display: 'grid', alignItems: 'center', columnGap: 'var(--spacing-3)',
    gridTemplateColumns: columns.map(key => `minmax(calc(var(--spacing-10) * ${hostColumnWidths[key]}), 1fr)`).join(' '),
    width: columns.length ? `max(100%, calc(var(--spacing-10) * ${columns.reduce((sum, key) => sum + hostColumnWidths[key], 0)} + var(--spacing-3) * ${columns.length - 1}))` : '100%',
  }
  const headingControl = (key: HostSortKey) => <ListSortControl label={hostColumnLabel(key)}
    direction={sort?.key === key ? sort.direction : undefined}
    onClick={scope === 'recent' ? undefined : () => setView(current => ({ ...current, sort: nextListSort(current.sort, key) }))} />
  const heading: ListItemProps = {
    startContent:<Stack direction="horizontal" gap={2} align="center">{multi&&<CheckboxInput label={tr("全选当前筛选的 {p0} 台主机", filtered.length)} isLabelHidden size="md" value={selectedHosts.length?(selectedHosts.length===filtered.length?true:'indeterminate'):false} isDisabled={!editable||!filtered.length} onChange={checked=>{setSelection(checked?[...ordered]:[]);anchor.current=''}}/>}<Stack aria-hidden="true" style={{visibility:'hidden'}}><OsIcon/></Stack></Stack>,
    label:<Stack direction="horizontal" gap={4} align="center" width="100%" style={{minWidth:0}}>
      <StackItem size="fill" style={{minWidth:0}}>{headingControl('name')}</StackItem>
      {scrollColumns && <ScrollArea.Root render={<Stack />} style={{ ...columnStyle, display: 'block' }}>
        <ScrollArea.Viewport render={<Stack />} ref={columnScroll} onScroll={syncColumnScroll}
          data-host-column-scroll="" data-horizontal-scroll-fade=""
          role="group" aria-label={tr("显示列")} style={{ display: 'block', overflowX: 'auto', overflowY: 'hidden', scrollbarWidth: 'none' }}>
          <ScrollArea.Content render={<Stack />} style={tracks}>
            {columns.map(key => <Stack key={key} data-host-column-heading={key}
              data-host-group-heading={key === 'group' ? '' : undefined}>{headingControl(key)}</Stack>)}
          </ScrollArea.Content>
        </ScrollArea.Viewport>
      </ScrollArea.Root>}
    </Stack>,
    endContent:<Stack width={actionsWidth(wide)} direction="horizontal" align="center" justify="end" gap={1} style={{flexShrink:0}}>
      <Text type="supporting" color="secondary">{tr("操作")}</Text>
      {hasHeaderPicker && columnPicker}
    </Stack>,
    style:{minHeight:'var(--size-element-md)'},
  }
  const renderRow = useCallback((host: HostProfile): ListItemProps => {
    const control = multi ? createRef<HTMLInputElement>() : undefined
    const subtitle = hostSubtitle(host, String(hostValue(host, 'system') ?? ''), columns)
    const trayWidth = orderOpen === host.id ? hostActionTrayWidth(canOrder) : undefined
    return ({
    id: `manager-host-${host.id}`,
    'data-host-context-id': host.id,
    style:host.id===firstOrdinary?{borderBlockStart:'var(--border-width) solid var(--color-border)'}:undefined,
    label:<Stack direction="horizontal" gap={4} align="center" width="100%" style={{minWidth:0}}>
      <StackItem size="fill" style={{minWidth:0,paddingInlineEnd:(!wide || !columns.length) ? trayWidth : undefined}}><Stack gap={0}>
        <Text data-host-name="" type="label" weight="semibold" maxLines={1}>{host.name}</Text>
        {subtitle && <Text type="supporting" color="secondary" maxLines={1}>{subtitle}</Text>}
        {!wide && (columns.length === 1 && columns[0] === 'group' ? <GroupText group={host.group}/> : columns.length > 0 &&
          <Stack direction="horizontal" gap={2} wrap="wrap" style={{ minWidth: 0 }}>
            {columns.map(key => <Text key={key} type="supporting" color="secondary" maxLines={1} style={{ maxWidth: '100%' }}>
              {tr("{p0}：{p1}", hostColumnLabel(key), hostCell(host, key))}
            </Text>)}
          </Stack>)}
      </Stack></StackItem>
      {scrollColumns && <Stack data-host-column-window="" style={{ ...columnStyle, overflow: 'clip', clipPath: trayWidth ? `inset(0 ${trayWidth} 0 0)` : undefined }}>
        <Stack style={{ ...tracks, translate: 'var(--host-column-offset, 0px) 0' }}>
          {columns.map(key => {
            const value = hostCell(host, key)
            const text = <Text type="supporting" color="secondary" maxLines={1}>{value}</Text>
            return <Stack key={key} data-host-column={key} data-host-group={key === 'group' ? '' : undefined} style={{minWidth:0}}>
              {key === 'group' ? <GroupText group={host.group}/> : key === 'notes' && value !== '—' ?
                <Tooltip content={value} hasHoverIndication={false} touchTrigger="none">{text}</Tooltip> : text}
            </Stack>
          })}
        </Stack>
      </Stack>}
    </Stack>,
    startContent: <Stack direction="horizontal" gap={2} align="center">{multi&&<CheckboxInput ref={control} label={tr("选择主机 {p0}", host.name)} isLabelHidden size="md" value={selectedSet.has(host.id)} isDisabled={!editable} onChange={(checked,event)=>{select(host.id,checked,shift.current||Boolean((event.nativeEvent as MouseEvent).shiftKey));shift.current=false}}/>}<OsIcon name={host.icon}/></Stack>, isSelected: multi?selectedSet.has(host.id):host.id === selectedId,
    interactiveRef:control,
    onPointerDownCapture:multi?event=>{shift.current=event.shiftKey}:undefined,
    onKeyDownCapture:multi?event=>{shift.current=event.shiftKey}:undefined,
    onClick: multi ? undefined : event => {
      if (event.detail === 0 || event.detail === 2) onOpen(host)
      else onSelectHost(host.id)
    },
    endContent:multi?<Button label={directHostIds.has(host.id)?tr("进入会话"):tr("连接")} aria-label={directHostIds.has(host.id)?tr("进入会话 {p0}",host.name):tr("连接 {p0}",host.name)} variant="secondary" size="md" onClick={()=>onOpen(host)}/>:<HostActions host={host} wide={wide} opened={directHostIds.has(host.id)} editable={editable} onOpen={onOpen} onEdit={onEdit} menuItems={menuItems} ordering={<HostOrderControl host={host} index={orderPeers.positions.get(host.id)??-1} count={(host.pinned?orderPeers.pinned:orderPeers.normal).length}
      allowed={canOrder} editable={editable}
      open={orderOpen===host.id} onOpen={id=>setOrderOpen(current=>id??(current===host.id?null:current))} onMove={moveHost} onPin={pinHost} onGrab={drag.grab} suppressClick={drag.suppressClick}/>}/>,
  })}, [wide,selectedId,directHostIds,editable,onOpen,onEdit,menuItems,onSelectHost,pinHost,multi,selectedSet,select,firstOrdinary,
    orderPeers,canOrder,orderOpen,moveHost,drag.grab,drag.suppressClick,columns,scrollColumns,hostValue,dateFormat])
  return <Stack ref={panel} id="panel-hosts" role="region" aria-labelledby="tab-hosts" hidden={!active}
    height="100%" style={{ display: active ? 'flex' : 'none', minHeight: 0, minWidth: 0 }}>
    <Stack ref={drag.marker} aria-hidden="true" data-host-order-marker="" style={{display:'none',position:'fixed',height:0,borderBlockStart:'var(--spacing-0-5) solid var(--color-text-primary)',pointerEvents:'none',zIndex:4}}/>
    <VisuallyHidden as="div" role="status" aria-atomic="true">{localizeMessage(pinNotice)}</VisuallyHidden>
    <Layout height="fill" content={<LayoutContent padding={0} isScrollable={false} style={{minWidth:0,minHeight:0}}>
    <Layout height="fill" header={<ManagementToolbar title={tr("主机列表")} caption={caption}
      search={<Stack direction="horizontal" align="center" gap={1} style={managementSearchStyle}>
        <TextInput id="manager-host-search" label={tr("搜索主机")} isLabelHidden type="search" size="md" value={query}
          width="100%" containerStyle={{ minWidth: 0, flex: '1 1 0' }} onChange={setQuery} placeholder={tr("搜索主机…")} startIcon="search" hasClear htmlName="host-query"
          hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" autoComplete="off" aria-keyshortcuts={shortcutAria('host-search',shortcuts)}
          onKeyDown={event=>{if(event.key==='Escape'&&!event.nativeEvent.isComposing){event.preventDefault();setQuery('')}}}/>
        {!hasHeaderPicker && columnPicker}
      </Stack>}
      navigation={<GroupFilters id="host-group-filters" label={tr("分组筛选")} value={group} onChange={onGroup}
        contextActions={groupContextActions?value=>groupContextActions(value?.startsWith('group:')?value.slice(6):undefined):undefined}
        options={[{value:'all',label:tr("全部")},...realGroups.map(name=>({value:'group:'+name,label:name}))]}/>}
      status={(multi||bulkNotice)?<Stack gap={2}>
        {multi&&<Stack direction="horizontal" gap={2} align="center" wrap="wrap">
          <Text type="supporting" role="status">{tr('已选 {p0} 台主机', selectedHosts.length)}</Text>
          <Button label={tr("批量设置")} variant="secondary" size="md" isDisabled={!editable||!selectedHosts.length||bulkBusy} onClick={()=>setBatch('edit')}/>
          <DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} label={tr("批量操作")} tooltip={tr("批量操作")} variant="ghost" size="md" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>} isDisabled={!editable||!selectedHosts.length||bulkBusy}/>} items={[
            {label:tr("置顶所选"),onClick:()=>void pinBatch(true)},{label:tr("取消置顶所选"),onClick:()=>void pinBatch(false)},
            {type:'divider'},{label:tr("删除所选连接…"),variant:'destructive',onClick:()=>setBatch('delete')},
          ]}/>
        </Stack>}
        {bulkNotice&&<Text role="status" type="supporting">{localizeMessage(bulkNotice)}</Text>}

      </Stack>:undefined}>
      {compact=><>
      {!compact&&<IconButton id="manage-groups" label={tr("管理分组")} tooltip={tr("管理分组")} aria-expanded={groupsOpen} aria-controls="host-groups-panel"
        variant={groupsOpen?'secondary':'ghost'} size="md" icon={<Icon icon={FolderIcon} size="sm"/>} onClick={onManageGroups}/>}
      {!compact&&<IconButton id="host-multi-select" label={multi?tr("退出多选"):tr("多选")} tooltip={multi?tr("退出多选"):tr("多选")} aria-pressed={multi}
        size="md" variant={multi?'secondary':'ghost'} isDisabled={!editable||bulkBusy} icon={<Icon icon={CheckCircleIcon} size="sm"/>}
        onClick={()=>{setMulti(previous=>!previous);setBulkNotice('')}}/>}
      <DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} id="host-manager-more" label={tr("主机列表操作")}
        tooltip={scope==='all'?tr("更多"):scope==='pinned'?tr("当前：仅看置顶"):tr("当前：最近连接")} size="md" variant={scope==='all'?'ghost':'secondary'} icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={[
          {label:tr("全部主机"),endContent:scope==='all'?<Text type="supporting">{tr("当前")}</Text>:undefined,onClick:()=>setScope('all')},
          {label:tr("仅看置顶"),endContent:scope==='pinned'?<Text type="supporting">{tr("当前")}</Text>:undefined,onClick:()=>setScope('pinned')},
          {label:tr("最近连接"),endContent:scope==='recent'?<Text type="supporting">{tr("当前")}</Text>:undefined,onClick:()=>setScope('recent')},
          ...(!wide && scope !== 'recent' ? [{ label: tr("排序"), items: sortMenuItems(['name', ...columns], hostColumnLabel, sort, setSort, tr("手动顺序")) }] : []),
          ...(compact?[
            {label:tr("管理分组"),onClick:onManageGroups},
            {label:multi?tr("退出多选"):tr("多选"),isDisabled:!editable||bulkBusy,onClick:()=>{setMulti(previous=>!previous);setBulkNotice('')}},
          ]:[]),
          {type:'divider'},{label:tr("临时连接…"),isDisabled:!editable,onClick:onQuickConnect},{label:tr("导入 SSH Config…"),isDisabled:!editable,onClick:onImport},
          ...(onRestoreDeleted ? [{label:tr('恢复已删主机'),isDisabled:!editable,onClick:onRestoreDeleted}] : []),
        ]}/>
      <Button id="new-connection" label={tr('新建连接')} icon={<Icon icon={PlusIcon} size="sm"/>} type="button" variant="secondary" size="md"
        style={{flexShrink:0,minWidth:'max-content'}} isDisabled={!editable||!onCreate} aria-keyshortcuts={shortcutAria('new-connection',shortcuts)}
        tooltip={shortcutTooltip(tr('新建连接'),'new-connection',shortcuts)} onClick={event=>onCreate?.(event.currentTarget)}/>
      </>}
    </ManagementToolbar>} content={<LayoutContent padding={0} isScrollable={false} style={{ display: 'flex', minHeight: 0 }}>
      <Stack ref={listPane} data-host-list-layout={wide?'columns':'stacked'} height="100%" width="100%" style={{minWidth:0,minHeight:0,rowGap:'var(--spacing-1)'}}>
      {filtered.length ? <WindowedList savedRow={active ? savedRow : undefined} onSavedRowShown={onSavedRowShown} animateReorder={!sort} retainedKey={drag.key??orderOpen??undefined} revealRequest={revealRequest} items={filtered} itemKey={hostKey} label={tr("主机管理列表")} selectedKey={multi?undefined:selectedId} header={heading} headerInteractive rowGap={rowGap} isItemFocusable={multi?()=>true:undefined}
        resetKey={`${deferredQuery}:${group}:${scope}`} measurementKey={`${wide?'columns':'stacked'}:${multi}:${columns.join(',')}`} estimateSize={()=>wide?48:68}
        row={renderRow} /> : <Stack id="host-list-empty" padding={6} gap={3} align="center" justify="center" width="100%" height="100%" style={{ minWidth: 0, textAlign: 'center' }}>
        <Text type="label">{!storageReady ? tr("主机配置尚未载入") : hosts.length ? tr("没有匹配的主机") : tr("还没有主机配置")}</Text>
        <Text type="supporting" color="secondary">{!storageReady ? tr("请重新读取配置，或从备份恢复。") : hosts.length ? tr("尝试其他关键词或分组。") : tr("新建一条连接配置，开始管理你的主机。")}</Text>
        {hosts.length > 0 && <Button label={tr("清除筛选")} variant="secondary" size="sm" onClick={() => { setQuery(''); onGroup('all');setScope('all') }} />}
      </Stack>}
      </Stack>
    </LayoutContent>} />
    </LayoutContent>} end={auxiliary} />
    {batch&&<Suspense fallback={<Text role="status" type="supporting">{tr("正在打开批量设置…")}</Text>}><BatchHostEditor hosts={selectedHosts} allHosts={hosts} groups={groups} identities={identities} revision={revision} onApply={onApply} storageError={localizeMessage(storageError)} deleting={batch==='delete'} onClose={message=>{setBatch(null);if(message){setSelection([]);setBulkNotice(message)}}}/></Suspense>}
  </Stack>
})
