import {useEffect,useRef} from 'react'
import type {PointerEvent} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {useMediaQuery} from '@astryxdesign/core/hooks'
import {Bars3Icon,ArrowUpIcon,ArrowDownIcon,ArrowUpTrayIcon,ArrowDownTrayIcon} from '@heroicons/react/24/outline'
import {tr} from '../../shared/i18n/messages'
import {findShortcut, shortcutAria, useShortcutOverrides} from '../workspace/shortcutReference'
import {PinOutlineIcon,PinFilledIcon} from '../../shared/icons/PinIcon'
import type {HostProfile} from '../workspace/mockGateway'
import type {HostMove} from './hostOrdering'

export const hostActionTrayWidth=(canOrder:boolean)=>`calc((var(--size-element-md) + var(--spacing-1)) * ${canOrder?5:1})`
export default function HostOrderControl({host,index,count,allowed,editable,open,onOpen,onMove,onPin,onGrab,suppressClick}:{
 host:HostProfile;index:number;count:number;allowed:boolean;editable:boolean;open:boolean
 onOpen:(id:string|null)=>void;onMove:(host:HostProfile,to:HostMove)=>void
 onPin:(host:HostProfile)=>Promise<boolean>
 onGrab:(event:PointerEvent<HTMLButtonElement>,host:HostProfile)=>void;suppressClick:()=>boolean
}){
 const root=useRef<HTMLElement|null>(null),handle=useRef<HTMLButtonElement|null>(null)
 const shortcuts=useShortcutOverrides()
 const timer=useRef<ReturnType<typeof setTimeout>|undefined>(undefined),locked=useRef(false)
 const reduced=useMediaQuery('(prefers-reduced-motion: reduce)'),expanded=open&&editable
 const cancel=()=>clearTimeout(timer.current)
 const close=()=>{cancel();locked.current=false;onOpen(null)}
 useEffect(()=>()=>clearTimeout(timer.current),[])
 useEffect(()=>{if(!open){locked.current=false;clearTimeout(timer.current)}},[open])
 useEffect(()=>{
  if(!expanded)return
  const outside=(event:globalThis.PointerEvent)=>{if(!root.current?.contains(event.target as Node))close()}
  document.addEventListener('pointerdown',outside,true)
  return()=>document.removeEventListener('pointerdown',outside,true)
 },[expanded,onOpen])
 const leave=()=>{cancel();if(!locked.current&&!root.current?.contains(document.activeElement))timer.current=setTimeout(()=>onOpen(null),160)}
 const movements:[HostMove,string,typeof ArrowUpIcon][]=[['first',host.pinned?tr('移到置顶区首位'):tr('移到普通区首位'),ArrowUpTrayIcon],['up',tr('上移'),ArrowUpIcon],['down',tr('下移'),ArrowDownIcon],['last',host.pinned?tr('移到置顶区末位'):tr('移到普通区末位'),ArrowDownTrayIcon]]
 return <Stack ref={root} direction="horizontal" align="center" gap={0} style={{flexShrink:0,position:'relative'}} data-host-order-control="" data-expanded={expanded}
   onClick={event=>event.stopPropagation()} onClickCapture={event=>{if(suppressClick()){event.preventDefault();event.stopPropagation()}}}
   onPointerEnter={()=>{cancel();if(editable&&!open)timer.current=setTimeout(()=>onOpen(host.id),180)}} onPointerLeave={leave}
   onFocusCapture={()=>cancel()} onBlurCapture={event=>{if(!event.currentTarget.contains(event.relatedTarget as Node|null))close()}}
   onKeyDown={event=>{if(event.key==='Escape'&&expanded){event.preventDefault();event.stopPropagation();handle.current?.focus({preventScroll:true});close()}}}>
  <Stack id={'host-order-tray-'+host.id} role="group" aria-label={tr('调整主机顺序 {p0}',host.name)} aria-hidden={!expanded} inert={!expanded}
    width={expanded?hostActionTrayWidth(allowed):0} style={{position:'absolute',insetInlineEnd:'100%',overflow:'hidden',opacity:expanded?1:0,transition:reduced?'none':'width var(--duration-fast-max) var(--ease-standard), opacity var(--duration-fast-max) var(--ease-standard)'}}>
   <Stack direction="horizontal" align="center" gap={1} style={{width:'max-content',paddingInlineEnd:'var(--spacing-1)'}}>
    {allowed&&movements.map(([to,label,icon])=><IconButton key={to} id={'host-move-'+to+'-'+host.id} label={label} icon={<Icon icon={icon} size="sm"/>} variant="ghost" size="md"
      isDisabled={!editable||(to==='first'||to==='up'?index<=0:index>=count-1)} onClick={()=>onMove(host,to)}/>)}
    <IconButton id={'manager-pin-'+host.id} label={host.pinned?tr('取消置顶'):tr('置顶')}
      icon={<Icon icon={host.pinned?PinFilledIcon:PinOutlineIcon} size="sm"/>}
      variant="ghost" size="md" isDisabled={!editable} clickAction={async()=>{await onPin(host)}}/>
   </Stack>
  </Stack>
  <IconButton ref={handle} id={'manager-order-'+host.id} label={tr('调整主机顺序 {p0}',host.name)} aria-expanded={expanded} aria-controls={'host-order-tray-'+host.id}
    aria-keyshortcuts={allowed?([shortcutAria('move-up',shortcuts),shortcutAria('move-down',shortcuts)].filter(Boolean).join(' ')||undefined):undefined}
    icon={<Icon data-host-secondary-icon="" icon={Bars3Icon} size="sm"/>} variant={host.pinned?'secondary':'ghost'} size="md" isDisabled={!editable}
    style={{cursor:allowed?'grab':undefined,touchAction:'none',userSelect:'none'}}
    onClick={()=>{cancel();if(!expanded||!locked.current){locked.current=true;onOpen(host.id)}else close()}}
    onFocus={event=>{if(editable&&!open&&event.currentTarget.matches(':focus-visible')){locked.current=true;onOpen(host.id)}}}
    onPointerDown={event=>{if(allowed&&editable){cancel();onGrab(event,host)}}}
    onKeyDown={event=>{const action=allowed&&editable&&findShortcut(event.nativeEvent,['move-up','move-down']);if(action){event.preventDefault();event.stopPropagation();if(!event.repeat)onMove(host,action==='move-up'?'up':'down')}}}/>
 </Stack>
}
