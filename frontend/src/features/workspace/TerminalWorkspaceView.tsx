import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { memo,useCallback,useEffect,useLayoutEffect,useMemo,useRef,useState,type ReactNode } from 'react'
import { flushSync } from 'react-dom'
import { Stack,StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { Selector } from '@astryxdesign/core/Selector'
import OsIcon from '../../shared/icons/OsIcon'
import { PlusIcon, ServerStackIcon, CommandLineIcon, ViewColumnsIcon, Bars2Icon } from '@heroicons/react/24/outline'
import './workspaceTasks.css'
import { DropdownMenu,type DropdownMenuOption } from '@astryxdesign/core/DropdownMenu'
import { ArrowsPointingOutIcon,ArrowsPointingInIcon,EllipsisHorizontalIcon,XMarkIcon } from '@heroicons/react/24/outline'
import {presetLabels} from './SplitLayoutDialog'
import SplitDivider from './SplitDivider'
import { dropZone,type DockSide,type SplitPreset,canSplit,groupFor,layoutRects,leaves,maximumPanes,minimumSize,resizeSplit,type SplitAxis,type TerminalWorkspace,type PaneRect } from './terminalLayout'
import { shouldFocusPanePointer } from '../terminal/terminalFocus'
import type { HostProfile } from './mockGateway'

export interface WorkspaceGeometry {width:number;height:number;focusOnly:boolean;spaceLimited:boolean;panes:Map<string,PaneRect>}
export interface WorkspaceControls {quickSplit:()=>void;canQuickSplit:boolean;splitMenu:DropdownMenuOption[];focusOnly:boolean;maximized:boolean;spaceLimited:boolean;activeId:string;panes:{value:string;label:string}[];onActivate:(id:string,focus?:boolean)=>void;onRestore:()=>void}
export default function TerminalWorkspaceView({state,visible,roundLeadingCorners,hosts,renderPane,onActivate,onSplit,onAttach,onMove,onSwap,onDetach,onClose,onRatio,onArrange,onPreset,onDock,onRename,onQuickSplit,onMaximize,onGeometry,renderToolbar}:{
 state:TerminalWorkspace;visible:boolean;roundLeadingCorners?:boolean;hosts:HostProfile[];renderPane:(id:string,visible:boolean,focused:boolean,tabId:string)=>ReactNode
 onActivate:(id:string,focus?:boolean)=>void;onSplit:(axis:SplitAxis,id?:string)=>void;onAttach:(id:string,host:HostProfile,sameHost:boolean)=>void;onMove:(source:string,target:string)=>void;onSwap:(source:string,target:string)=>void;onDetach:(id:string)=>void;onClose:(id:string)=>void;onRatio:(id:string,ratio:number)=>void;onArrange:(columns:number)=>void;onMaximize:()=>void;onGeometry?:(geometry:WorkspaceGeometry)=>void
 onRename:(id:string)=>void;onPreset:(preset:SplitPreset)=>void;onDock:(source:string,target:string,side:DockSide,wholeTab:boolean)=>void;onQuickSplit:()=>void
 renderToolbar?:(controls:WorkspaceControls)=>ReactNode
}){
  useLocale()

 const root=useRef<HTMLElement>(null),[size,setSize]=useState({width:0,height:0}),[moveTarget,setMoveTarget]=useState('')
 const group=groupFor(state,state.activeId),groupRoot=group?.root
 const ids=useMemo(()=>groupRoot?leaves(groupRoot):[],[groupRoot]),multiple=ids.length>1
 const minimum=useMemo(()=>groupRoot?minimumSize(groupRoot):{width:0,height:0},[groupRoot])
 const narrow=multiple&&size.width>0&&size.height>0&&(size.width<minimum.width||size.height<minimum.height),focusOnly=!!group?.maximized||narrow
 const geometry=useMemo(()=>groupRoot?layoutRects(groupRoot,size.width,size.height):{panes:new Map<string,PaneRect>(),dividers:[]},[groupRoot,size.width,size.height])
 const previewNodes = useRef<HTMLElement[]>([])
 const clearPreview = useCallback(() => {
   for (const element of previewNodes.current) {
     for (const property of ['x', 'y', 'width', 'height']) element.style.removeProperty('--yaare-preview-' + property)
     element.style.removeProperty('--yaare-split-first')
   }
   previewNodes.current = []
 }, [])
 const previewRatio = useCallback((id: string, ratio?: number) => {
   if (ratio === undefined || !groupRoot || !root.current) { clearPreview(); return }
   const preview = layoutRects(resizeSplit(groupRoot, id, ratio), size.width, size.height)
   const dividers = new Map(preview.dividers.map(divider => [divider.id, divider]))
   const elements = Array.from(root.current.querySelectorAll<HTMLElement>('[data-terminal-pane], [data-split-divider]'))
   for (const element of elements) {
     const divider = dividers.get(element.dataset.splitDivider ?? '')
     const rect = divider?.rect ?? preview.panes.get(element.dataset.terminalPane ?? '')
     if (!rect) continue
     for (const [property, value] of Object.entries(rect)) element.style.setProperty('--yaare-preview-' + property, value + 'px')
     if (divider) {
       const length = (divider.axis === 'horizontal' ? rect.width : rect.height) - 1
       element.style.setProperty('--yaare-split-first', Math.round(length * divider.ratio) + 'px')
     }
   }
   previewNodes.current = elements
 }, [groupRoot, size.width, size.height, clearPreview])
 useLayoutEffect(clearPreview, [geometry, visible, focusOnly, clearPreview])
 const commitRatio = useCallback((id: string, ratio: number) => {
   if (geometry.dividers.find(divider => divider.id === id)?.ratio === ratio) clearPreview()
   onRatio(id, ratio)
 }, [geometry, clearPreview, onRatio])
 const dimensions=useRef(onGeometry);dimensions.current=onGeometry
 useLayoutEffect(()=>{
  const node=root.current;if(!node||!visible)return
  const update=(width:number,height:number)=>setSize(old=>Math.abs(old.width-width)<.5&&Math.abs(old.height-height)<.5?old:{width,height})
  const rect=node.getBoundingClientRect();update(rect.width,rect.height)
  let measuredWidth=rect.width,measuredHeight=rect.height
  const observer=new ResizeObserver(entries=>{
   const box=entries[0]?.contentRect
   if(!box||Math.abs(measuredWidth-box.width)<.5&&Math.abs(measuredHeight-box.height)<.5)return
   measuredWidth=box.width;measuredHeight=box.height
   flushSync(()=>update(box.width,box.height))
  })
  observer.observe(node);return()=>observer.disconnect()
 },[visible,focusOnly])
 useLayoutEffect(()=>{dimensions.current?.({...size,focusOnly,spaceLimited:narrow,panes:geometry.panes})},[size.width,size.height,group?.root,focusOnly,narrow])
 const actionsRef = useRef({ onActivate, onClose, onSplit, onDetach, onSwap, onDock, onRename, onMaximize })
 actionsRef.current = { onActivate, onClose, onSplit, onDetach, onSwap, onDock, onRename, onMaximize }
 const actions = useMemo(() => ({
   activate: (id: string, focus?: boolean) => actionsRef.current.onActivate(id, focus),
   close: (id: string) => actionsRef.current.onClose(id),
   split: (axis: SplitAxis, id: string) => actionsRef.current.onSplit(axis, id),
   detach: (id: string) => actionsRef.current.onDetach(id),
   swap: (source: string, target: string) => actionsRef.current.onSwap(source, target),
   rename:(id:string)=>actionsRef.current.onRename(id),
   dock:(source:string,target:string,side:DockSide,wholeTab:boolean)=>actionsRef.current.onDock(source,target,side,wholeTab),
   maximize: () => actionsRef.current.onMaximize(),
 }), [])
 const toggleSwap = useCallback((id: string) => setMoveTarget(previous => previous === id ? '' : id), [])
 const labelFor=(id:string)=>{const pane=state.sessions.find(s=>s.id===id);return pane?.pending?tr("新终端"):pane?.label??pane?.host.name??tr("终端")}
 const rect=geometry.panes.get(state.activeId),canAdd=ids.length<maximumPanes&&!state.sessions.find(s=>s.id===state.activeId)?.pending
 const presets=presetLabels()
 const splitMenu:DropdownMenuOption[]=[...(Object.keys(presets) as SplitPreset[]).map(p=>({label:presets[p],onClick:()=>onPreset(p)})),{type:'divider'},{label:tr("向右拆分"),icon:<Icon icon={ViewColumnsIcon}/>,isDisabled:!canAdd||focusOnly||!canSplit(rect,'horizontal'),onClick:()=>onSplit('horizontal')},{label:tr("向下拆分"),icon:<Icon icon={Bars2Icon}/>,isDisabled:!canAdd||focusOnly||!canSplit(rect,'vertical'),onClick:()=>onSplit('vertical')},{type:'divider'},...[1,2,3,4].filter(n=>n<=ids.length).map(n=>({label:n===1?tr("纵向排列"):tr("{p0} 列排列", n),onClick:()=>onArrange(n)})),{label:tr("均分当前布局"),onClick:()=>onArrange(ids.length<=3?ids.length:Math.ceil(ids.length/2))}]
 return <Stack id="terminal-workspace" hidden={!visible} inert={!visible} height="100%" style={{display:visible?'flex':'none',minHeight:0,minWidth:0}}>
   {group&&(renderToolbar?renderToolbar({quickSplit:onQuickSplit,canQuickSplit:canAdd&&!focusOnly&&canSplit(rect,'horizontal'),splitMenu,focusOnly,maximized:!!group.maximized,spaceLimited:narrow,activeId:state.activeId,panes:ids.map((id,i)=>({value:id,label:(i+1)+' / '+ids.length+' · '+labelFor(id)})),onActivate,onRestore:onMaximize}):<Stack direction="horizontal" align="center" gap={2} paddingInline={2} style={{flexShrink:0,minHeight:'var(--size-element-md)',borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
     <StackItem size="fill" style={{minWidth:0}}>{focusOnly?<Selector label={tr("当前窗格")} isLabelHidden size="md" width="100%" variant="ghost" value={state.activeId} options={ids.map((id,i)=>({value:id,label:(i+1)+' / '+ids.length+' · '+labelFor(id)}))} onChange={onActivate}/>:<Text type="supporting" color="secondary">{ids.length===1?tr("终端"):tr("{p0} 个窗格 · 当前 {p1}", ids.length, (ids.indexOf(state.activeId)+1))}</Text>}</StackItem>
     {narrow&&<Text type="supporting" color="secondary">{tr("空间不足，已聚焦")}</Text>}
     {group.maximized&&<Button label={tr("还原布局")} size="md" variant="ghost" onClick={onMaximize}/>}
     <DropdownMenu alignment="end" presentation="adaptive" renderTrigger={props=><Button {...props} id="split-menu" label={tr("分屏")} size="md" variant="ghost" tooltip={ids.length>=8?tr("每个标签最多 8 个窗格"):tr("拆分与排列终端")}/>} items={splitMenu}/>
   </Stack>)}
   <Stack ref={root} id={group?'panel-terminal-'+group.id:undefined} role="tabpanel" aria-labelledby={group?'tab-terminal-'+group.id:undefined}
     style={{position:'relative',flex:1,minHeight:0,minWidth:0,overflow:'hidden',
       borderStartStartRadius:roundLeadingCorners?'var(--radius-element)':undefined,borderEndStartRadius:roundLeadingCorners?'var(--radius-element)':undefined}}>
     {state.sessions.map(session=>{
       const owner=groupFor(state,session.id),position=focusOnly?{x:0,y:0,width:size.width,height:size.height}:geometry.panes.get(session.id),shown=visible&&owner?.id===group?.id&&(!focusOnly||state.activeId===session.id),focused=state.activeId===session.id
       return <PaneFrame key={session.id} id={session.id} host={session.pending?undefined:session.host} name={session.pending?tr("新终端"):session.label?session.label+' · '+session.host.name:session.host.name} number={owner?leaves(owner.root).indexOf(session.id)+1:1} shown={shown} focused={focused} multiple={multiple} position={position} status={session.pending?tr("待连接"):session.connection?.status??'idle'} onActivate={actions.activate} onClose={actions.close} onMaximize={actions.maximize} maximized={!!group?.maximized} canMaximize={!narrow||!!group?.maximized} onDetach={actions.detach} onSwap={actions.swap} onDock={actions.dock} onRename={actions.rename} canSplitRight={canAdd&&!focusOnly&&canSplit(geometry.panes.get(session.id),'horizontal')&&!session.pending}
         canSplitDown={canAdd&&!focusOnly&&canSplit(geometry.panes.get(session.id),'vertical')&&!session.pending}
         canDetach={multiple&&!session.pending} canToggleMaximize={multiple&&(!narrow||!!group?.maximized)}
         onSplit={actions.split} onToggleSwap={toggleSwap}>
         {session.pending?<PendingPane focused={shown&&focused} host={session.host} hosts={hosts} sessions={state.sessions.filter(s=>!s.pending)} onAttach={(host,same)=>onAttach(session.id,host,same)} onMove={id=>onMove(id,session.id)} onCancel={()=>onClose(session.id)}/>
           :renderPane(session.id,shown,focused,'terminal-'+(owner?.id??session.id))}
         {moveTarget===session.id&&<Stack padding={2} gap={1}><Text type="supporting">{tr("选择交换位置的窗格")}</Text><Stack direction="horizontal" wrap="wrap" gap={1}>{ids.filter(id=>id!==session.id).map(id=><Button key={id} label={labelFor(id)} size="md" variant="secondary" onClick={()=>{onSwap(session.id,id);setMoveTarget('')}}/>)}<Button label={tr("取消交换")} size="md" variant="ghost" onClick={()=>setMoveTarget('')}/></Stack></Stack>}
       </PaneFrame>
     })}
     {visible&&!focusOnly&&geometry.dividers.map(divider=><SplitDivider key={group!.id+divider.id} divider={divider} onChange={commitRatio} onPreview={previewRatio}/>)}
   </Stack>
 </Stack>
}

type PaneFrameProps = {
 host?:HostProfile; id: string; name: string; number: number; shown: boolean; focused: boolean; multiple: boolean
 position?: PaneRect; status: string; maximized: boolean; canMaximize: boolean
 canSplitRight: boolean; canSplitDown: boolean; canDetach: boolean; canToggleMaximize: boolean
 onActivate: (id: string, focus?: boolean) => void; onClose: (id: string) => void
 onDock:(source:string,target:string,side:DockSide,wholeTab:boolean)=>void;onRename:(id:string)=>void
 onMaximize: () => void; onDetach: (id: string) => void; onSwap: (source: string, target: string) => void
 onSplit: (axis: SplitAxis, id: string) => void; onToggleSwap: (id: string) => void; children: ReactNode
}

const PaneFrame = memo(function PaneFrame({ shown, multiple, position, onSwap, onDock, children, ...header }: PaneFrameProps) {
  useLocale()
  const { id, name, number, focused, onActivate } = header
 const [zone,setZone]=useState<DockSide|'center'>()
 useEffect(()=>{
   if(!zone)return
   const clear=()=>setZone(undefined)
   window.addEventListener('dragend',clear)
   window.addEventListener('drop',clear)
   return ()=>{window.removeEventListener('dragend',clear);window.removeEventListener('drop',clear)}
 },[!!zone])
 const targetZone=(e:React.DragEvent<HTMLElement>,whole:boolean)=>{
   const r=e.currentTarget.getBoundingClientRect()
   return dropZone((e.clientX-r.left)/r.width,(e.clientY-r.top)/r.height,whole)
 }
 return <Stack id={'pane-'+id} data-terminal-pane={id} data-split-active={shown && multiple && focused ? '' : undefined} role="group" aria-label={tr("终端窗格 {p0} · {p1}", number, name)} aria-current={focused?'true':undefined} hidden={!shown} inert={!shown} onPointerDownCapture={event=>{if(!focused)onActivate(id,shouldFocusPanePointer(event.target))}} onFocusCapture={()=>{if(!focused)onActivate(id,false)}}
   onDragOver={e=>{
     const whole=e.dataTransfer.types.includes('application/x-yaare-tab')
     if(!whole&&!e.dataTransfer.types.includes('application/x-yaare-pane'))return
     e.preventDefault();e.dataTransfer.dropEffect='move';setZone(targetZone(e,whole))
   }} onDragLeave={e=>{if(!(e.relatedTarget instanceof Node)||!e.currentTarget.contains(e.relatedTarget))setZone(undefined)}}
   onDrop={e=>{
     const tab=e.dataTransfer.getData('application/x-yaare-tab'),source=tab||e.dataTransfer.getData('application/x-yaare-pane')
     if(!source)return
     e.preventDefault();const side=targetZone(e,!!tab);setZone(undefined)
     if(side==='center')onSwap(source,id);else onDock(source,id,side,!!tab)
   }}
   style={{position:'absolute',left:`var(--yaare-preview-x, ${position?.x??0}px)`,top:`var(--yaare-preview-y, ${position?.y??0}px)`,width:`var(--yaare-preview-width, ${position?.width??0}px)`,height:`var(--yaare-preview-height, ${position?.height??0}px)`,display:shown?'flex':'none',minWidth:0,minHeight:0,overflow:'hidden',zIndex:focused?1:0}}>
   {multiple && <PaneHeading {...header}/>}
   {children}
   {shown&&zone&&<Stack aria-hidden="true" align="center" justify="center" data-pane-drop={zone}><Text type="label">{zone==='center'?tr('松手交换位置'):zone==='left'?tr('松手放在左侧'):zone==='right'?tr('松手放在右侧'):zone==='top'?tr('松手放在上方'):tr('松手放在下方')}</Text></Stack>}
 </Stack>
})

const PaneHeading = memo(function PaneHeading({
  id, host, name, number, focused, status, maximized, canMaximize, canSplitRight, canSplitDown,
  canDetach, canToggleMaximize, onActivate, onClose, onMaximize, onDetach, onSplit, onToggleSwap,onRename,
}: Omit<PaneFrameProps, 'shown' | 'multiple' | 'position' | 'onSwap' | 'onDock' | 'children'>) {
  useLocale()
  const [hovered, setHovered] = useState(false)
  const menu: DropdownMenuOption[] = [
    { label: tr("向右拆分"), isDisabled: !canSplitRight, onClick: () => onSplit('horizontal', id) },
    { label: tr("向下拆分"), isDisabled: !canSplitDown, onClick: () => onSplit('vertical', id) },
    { label: maximized ? tr("还原布局") : tr("放大窗格"), isDisabled: !canToggleMaximize, onClick: () => { onActivate(id); onMaximize() } },
    { label: tr("重命名…"), onClick:()=>onRename(id) },
    { label: tr("移到独立工作区"), isDisabled: !canDetach, onClick: () => onDetach(id) },
    { label: tr("与其他窗格交换"), onClick: () => onToggleSwap(id) },
    { label: tr("关闭窗格"), onClick: () => onClose(id) },
  ]
  return <Stack direction="horizontal" align="center" gap={1} paddingInline={2} onPointerEnter={()=>setHovered(true)} onPointerLeave={()=>setHovered(false)} style={{flexShrink:0,minHeight:'var(--size-element-md)',borderBlockEnd:'var(--border-width) solid var(--color-border)',background:focused?'var(--color-accent-muted)':'transparent'}}>
     <StackItem size="fill" draggable onDragStart={event=>{event.dataTransfer.setData('application/x-yaare-pane',id);event.dataTransfer.effectAllowed='move'}} onDoubleClick={()=>{onActivate(id);if(canMaximize)onMaximize()}} style={{minWidth:0,cursor:'grab'}}><Stack direction="horizontal" align="center" gap={2}>{host&&<OsIcon name={host.icon}/>}<Text type="supporting" weight={focused?'semibold':'normal'} maxLines={1}>{number} · {name}{['connecting','reconnecting'].includes(status)?tr(" · 连接中"):''}</Text>{host&&<Text type="supporting" color="secondary" maxLines={1}>{host.username}@{host.address}</Text>}</Stack></StackItem>
     <Stack direction="horizontal" align="center" gap={1} style={{visibility:focused||hovered?'visible':'hidden'}}>
       <IconButton tabIndex={focused?0:-1} label={tr("{p0}窗格 {p1}", (maximized?tr("还原"):tr("放大")), number)} isDisabled={!canMaximize} tooltip={!canMaximize?tr("扩大窗口后自动还原布局"):maximized?tr("还原布局"):tr("放大窗格")} size="md" variant="ghost" icon={<Icon icon={maximized?ArrowsPointingInIcon:ArrowsPointingOutIcon} size="sm"/>} onClick={()=>{onActivate(id);onMaximize()}}/>
       <DropdownMenu alignment="end" renderTrigger={props=><IconButton {...props} tabIndex={focused?0:-1} label={tr("窗格 {p0} 操作", number)} size="md" variant="ghost" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={menu}/>
       <IconButton tabIndex={focused?0:-1} label={tr("关闭窗格 {p0} · {p1}", number, name)} tooltip={tr("关闭此窗格")} size="md" variant="ghost" icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={()=>onClose(id)}/>
     </Stack>
   </Stack>
})

function PendingPane({host,hosts,sessions,onAttach,onMove,onCancel,focused}:{focused:boolean;host:HostProfile;hosts:HostProfile[];sessions:TerminalWorkspace['sessions'];onAttach:(host:HostProfile,same:boolean)=>void;onMove:(id:string)=>void;onCancel:()=>void}){
  useLocale()

 const primary=useRef<HTMLButtonElement>(null)
 useLayoutEffect(()=>{if(focused)primary.current?.focus({preventScroll:true})},[focused])
 const other=tr("选择其他主机…"), move=tr("移入已打开的终端…")
 const hostOptions=useMemo(()=>hosts.map(h=>({value:h.id,label:h.name,description:h.username+'@'+h.address,icon:<OsIcon name={h.icon}/>})),[hosts])
 return <Stack padding={3} style={{flex:1,minHeight:0,overflow:'auto'}} onKeyDown={event=>{
   if(event.key==='Escape'&&!event.defaultPrevented&&!event.nativeEvent.isComposing&&!(event.target as Element).closest('[role="listbox"],[aria-expanded="true"]')){event.stopPropagation();onCancel()}
 }}>
   <Stack data-pane-chooser="" aria-label={tr("连接新终端")} role="group">
     {host.id&&<Button ref={primary} variant="secondary" label={tr("连接 {p0}",host.name)} aria-label={tr("连接 {p0}",host.name)}
       width="100%" style={{height:'auto',padding:'var(--spacing-3)',justifyContent:'flex-start'}} onClick={()=>onAttach(host,true)}>
       <Stack direction="horizontal" width="100%" align="center" gap={3}>
         <OsIcon name={host.icon} size="lg"/>
         <Stack align="start" style={{flex:1,minWidth:0,textAlign:'start'}}>
           <Text type="label" maxLines={1}>{host.name}</Text>
           <Text type="supporting" color="secondary">{tr("当前主机 · {p0}",host.username)}</Text>
         </Stack>
         <Stack padding={1} style={{borderRadius:'var(--radius-inner)',background:'var(--color-accent)',color:'var(--color-on-accent)'}}><Icon icon={PlusIcon} size="sm"/></Stack>
       </Stack>
     </Button>}
     <Selector ref={host.id?undefined:primary} label={other} placeholder={other} isLabelHidden width="100%" size="md" variant="ghost" hasSearch startIcon={<Icon icon={ServerStackIcon}/>}
       options={hostOptions} onChange={id=>{const next=hosts.find(h=>h.id===id);if(next)onAttach(next,false)}}/>
     <Selector label={move} placeholder={move} isLabelHidden width="100%" size="md" variant="ghost" hasSearch startIcon={<Icon icon={CommandLineIcon}/>}
       isDisabled={!sessions.length} options={sessions.map((s,i)=>({value:s.id,label:s.host.name+' · '+(i+1),description:s.host.username+'@'+s.host.address,icon:<OsIcon name={s.host.icon}/>}))} onChange={onMove}/>
   </Stack>
 </Stack>
}
