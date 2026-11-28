import {useLayoutEffect,useRef,useState} from 'react'
import {Button} from '@astryxdesign/core/Button'
import {Icon} from '@astryxdesign/core/Icon'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Stack} from '@astryxdesign/core/Stack'
import {ChevronDownIcon,ChevronRightIcon,CommandLineIcon,DocumentIcon,FolderIcon,PlusIcon,XMarkIcon} from '@heroicons/react/24/outline'
import type {SplitNode} from './terminalLayout'
import {compactRailWidth} from './panelLayout'
import OsIcon,{type OsIconName} from '../../shared/icons/OsIcon'
import HorizontalScrollArea from '../../shared/ui/HorizontalScrollArea'
import {tr} from '../../shared/i18n/messages'
import {shortcutAria,shortcutTooltip,useShortcutOverrides} from './shortcutReference'

export interface ConnectionTab {
  id:string; label:string; detail?:string; dirty?:boolean; terminalPaneId?:string
  layout?:SplitNode;groupId?:string; groupName?:string; icon?:OsIconName; count?:number; pinned?:boolean; disconnected?:boolean; streaming?:boolean
  onSelect:()=>void; onClose?:()=>void; onNewTerminal?:()=>void
}
function layoutGlyph(node:SplitNode):React.ReactNode {
  if(node.kind==='leaf')return null
  return <Stack aria-hidden direction={node.axis==='horizontal'?'horizontal':'vertical'} style={{width:'100%',height:'100%'}}>
    <Stack style={{flex:node.ratio,minWidth:0,minHeight:0}}>{layoutGlyph(node.first)}</Stack>
    <Stack style={{flex:1-node.ratio,minWidth:0,minHeight:0,[node.axis==='horizontal'?'borderInlineStart':'borderBlockStart']:'var(--border-width) solid currentColor'}}>{layoutGlyph(node.second)}</Stack>
  </Stack>
}
export default function ConnectionTabs({tabs,selected,onNewTerminal,showShortcuts,top,compact,railWidth,marked,onMark,collapsed,onCollapse,onOrder,onGroup,onCombine}:{
  tabs:ConnectionTab[];selected:string;onNewTerminal?:()=>void;showShortcuts:boolean;top:boolean;compact:boolean;railWidth:number
  onOrder:(pane:string,target:string,after:boolean)=>void;onGroup:(pane:string,groupId:string)=>void;onCombine:()=>void
  marked:string[];onMark:(ids:string[])=>void;collapsed:string[];onCollapse:(id:string)=>void
}){
  const shortcuts=useShortcutOverrides(),anchor=useRef(''),[focused,setFocused]=useState('')
  const pinColumns=railWidth<216?3:railWidth<252?4:5
  const sections=new Map<string,ConnectionTab[]>()
  for(const tab of tabs){
    const key=tab.pinned?'pins':tab.groupId??tab.id
    const section=sections.get(key)??[]
    section.push(tab);sections.set(key,section)
  }
  const orderedSections=[...sections].sort(([a],[b])=>a==='pins'?-1:b==='pins'?1:0)
  const expandedRail=!top&&!compact
  const showGlobalAdd=!expandedRail||!tabs.some(tab=>!tab.pinned&&tab.onNewTerminal)||tabs.some(tab=>tab.id===selected&&tab.pinned)
  const grouped=(id:string,items:ConnectionTab[])=>id!=='pins'&&items.length>1&&!!items[0]?.groupName
  const visible=orderedSections.flatMap(([id,items])=>grouped(id,items)&&collapsed.includes(id)?[]:items)
  const markedCount=tabs.filter(t=>marked.includes(t.id)).length
  const entry=[focused,selected].find(id=>visible.some(tab=>tab.id===id))??visible[0]?.id
  const reveal=(id:string)=>document.getElementById('tab-'+id)?.scrollIntoView({block:'nearest',inline:'nearest'})
  useLayoutEffect(()=>{if(selected&&!top)reveal(selected)},[selected,top,compact])
  const select=(tab:ConnectionTab,range:boolean,toggle:boolean)=>{
    if(tab.terminalPaneId&&(range||toggle)){
      const terminals=visible.filter(t=>t.terminalPaneId).map(t=>t.id)
      const start=terminals.indexOf(anchor.current||selected),end=terminals.indexOf(tab.id)
      if(range&&start>=0){onMark(terminals.slice(Math.min(start,end),Math.max(start,end)+1));return}
      const current=marked.length?marked:terminals.includes(selected)?[selected]:[]
      onMark(current.includes(tab.id)?current.filter(id=>id!==tab.id):[...current,tab.id])
      anchor.current=tab.id;return
    }
    anchor.current=tab.id;onMark([]);tab.onSelect()
  }
  const renderAdd=(id:string,tab:ConnectionTab|undefined)=>{
    if(!tab?.onNewTerminal)return null
    return <IconButton data-new-terminal-for={id} size="sm" variant="ghost"
      label={tr('在「{p0}」中新建终端',tab.groupName||tab.label)} tooltip={tr('新建终端')}
      icon={<Icon icon={PlusIcon} size="sm"/>} onClick={()=>{
        if(collapsed.includes(id))onCollapse(id)
        tab.onNewTerminal?.()
      }}/>
  }
  const renderTab=(tab:ConnectionTab,addToSection?:string)=>{
    const active=selected===tab.id,chosen=marked.includes(tab.id),tile=compact||!!tab.pinned
    const name=tab.label+(tab.count&&tab.count>1?' · '+tab.count:'')+(tab.dirty?' ●':'')
    return <Stack key={tab.id} direction="horizontal" align="center" data-yaare-compound-action="" data-terminal-context-pane={tab.terminalPaneId}
      data-picked={chosen||undefined} style={{minWidth:0,flexShrink:0,borderRadius:'var(--radius-element)',background:active?'var(--color-neutral)':undefined,
        outline:chosen?'var(--border-width) solid var(--color-accent)':undefined}}>
      <Button id={'tab-'+tab.id} role="tab" aria-selected={active} aria-controls={active?'panel-'+tab.id:undefined}
        data-session-tab={tab.id} draggable={!!tab.terminalPaneId} onDragStart={e=>{if(tab.terminalPaneId){e.dataTransfer.setData('application/x-yaare-tab',tab.terminalPaneId);e.dataTransfer.effectAllowed='move'}}}
        onDragOver={e=>{if(tab.terminalPaneId&&e.dataTransfer.types.includes('application/x-yaare-tab'))e.preventDefault()}}
        onDrop={e=>{
          const pane=e.dataTransfer.getData('application/x-yaare-tab')
          if(!pane||!tab.terminalPaneId)return
          e.preventDefault()
          const rect=e.currentTarget.getBoundingClientRect()
          onOrder(pane,tab.terminalPaneId,top?e.clientX>rect.left+rect.width/2:e.clientY>rect.top+rect.height/2)
        }}
        label={name} aria-description={[chosen?tr('已选择'):undefined,tab.detail,tab.disconnected?tr('已断开'):undefined].filter(Boolean).join(' · ')}
        tooltip={top?[name,tab.detail,tab.disconnected?tr('已断开'):undefined].filter(Boolean).join(' · '):tile?name:undefined} tabIndex={entry===tab.id?0:-1}
        isIconOnly={tile} variant="ghost" size="sm" icon={tab.layout?.kind==='split'?<Stack aria-hidden style={{width:'var(--spacing-4)',height:'var(--spacing-3)',border:'var(--border-width) solid currentColor',borderRadius:'var(--radius-inner)',overflow:'hidden'}}>{layoutGlyph(tab.layout)}</Stack>:tab.icon?<OsIcon name={tab.icon}/>:<Icon icon={tab.terminalPaneId?CommandLineIcon:tab.id.startsWith('files-')?FolderIcon:DocumentIcon} size="sm"/>}
        aria-keyshortcuts="Delete Shift+F10" style={{minWidth:0,flex:top?undefined:1,justifyContent:tile?'center':'flex-start',maxWidth:top?'calc(var(--spacing-10) * 6)':undefined,
        color:tab.disconnected?'var(--color-text-secondary)':undefined,backgroundColor:tab.streaming&&!active?'var(--color-background-body)':undefined}}
        onFocus={()=>{setFocused(tab.id);if(!top)reveal(tab.id)}} onClick={e=>select(tab,e.shiftKey,e.ctrlKey||e.metaKey)}
        onMouseDown={e=>{if(e.button===1)e.preventDefault()}} onAuxClick={e=>{if(e.button===1){e.preventDefault();tab.onClose?.()}}}
        onKeyDown={e=>{
          if(e.nativeEvent.isComposing||e.altKey)return
          if(e.key==='Escape'){onMark([]);return}
          if(e.key==='Delete'){e.preventDefault();if(!e.repeat)tab.onClose?.();return}
          const i=visible.indexOf(tab),pinGrid=tab.pinned&&!compact&&!top
          const step=e.key==='ArrowRight'&&(top||pinGrid)?1:e.key==='ArrowLeft'&&(top||pinGrid)?-1:
            e.key==='ArrowDown'&&!top?(pinGrid?pinColumns:1):e.key==='ArrowUp'&&!top?(pinGrid?-pinColumns:-1):0
          const next=e.key==='Home'?visible[0]:e.key==='End'?visible.at(-1):step?visible[Math.max(0,Math.min(visible.length-1,i+step))]:undefined
          if(next){e.preventDefault();document.getElementById('tab-'+next.id)?.focus();if(e.shiftKey)select(next,true,false)}
        }}/>
      {!tile&&tab.onClose&&<IconButton data-tab-close="" size="sm" variant="ghost" tabIndex={active?0:-1} label={tr('关闭 {p0}',tab.label)} icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={tab.onClose}/>}
      {addToSection&&renderAdd(addToSection,tab)}
    </Stack>
  }
  const tabList=<Stack role="tablist" aria-label={tr('会话标签')} aria-orientation={top?'horizontal':'vertical'} direction={top?'horizontal':'vertical'} gap={1} style={{flexShrink:0}}>
      {orderedSections.map(([id,items])=>{
        const group=grouped(id,items),closed=group&&collapsed.includes(id)
        return <Stack key={id} direction={top?'horizontal':'vertical'} gap={1} style={{flexShrink:0}}>
          {group&&<Stack direction="horizontal" align="center" style={{minWidth:0}}><Button label={items[0]!.groupName!} tooltip={expandedRail?undefined:items[0]!.groupName} aria-expanded={!closed} isIconOnly={compact} size="sm" variant={closed&&items.some(t=>t.id===selected)?'secondary':'ghost'}
            onDragOver={e=>{if(e.dataTransfer.types.includes('application/x-yaare-tab'))e.preventDefault()}}
            onDrop={e=>{const pane=e.dataTransfer.getData('application/x-yaare-tab');if(pane){e.preventDefault();onGroup(pane,id)}}}
            icon={<Icon icon={closed?ChevronRightIcon:ChevronDownIcon} size="sm"/>} style={{flex:expandedRail?1:undefined,minWidth:0,justifyContent:compact?'center':'flex-start'}} onClick={()=>onCollapse(id)}/>
            {expandedRail&&renderAdd(id,items.find(tab=>tab.id===selected&&tab.onNewTerminal)??items.find(tab=>tab.onNewTerminal))}
          </Stack>}
          {!closed&&<Stack direction={top?'horizontal':'vertical'} gap={1} style={id==='pins'&&!compact&&!top?{display:'grid',gridTemplateColumns:'repeat('+pinColumns+',minmax(0,1fr))'}:undefined}>
            {items.map(tab=>renderTab(tab,expandedRail&&!group&&id!=='pins'?id:undefined))}
          </Stack>}
        </Stack>
      })}
    </Stack>
  return <Stack id="session-tab-strip" data-caption-interactive={top?'':undefined} direction={top?'horizontal':'vertical'} align={top?'center':undefined} gap={1}
    padding={top?0:1.5}
    style={{minWidth:0,minHeight:0,...(top?{}:{overflowX:'hidden',overflowY:'auto',scrollPaddingBlock:'var(--spacing-1-5)',scrollbarWidth:compact?'none':'thin',
      paddingInline:compact?`calc((${compactRailWidth}px - var(--size-element-sm)) / 2)`:undefined})}}
    onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget))setFocused('')}}>
    {top?<HorizontalScrollArea id="horizontal-session-tabs" label={tr('会话标签')} value={selected} hideScrollbar>{tabList}</HorizontalScrollArea>:tabList}
    {markedCount>1&&<Button size="sm" variant="secondary" label={tr('组合为分屏')+' · '+markedCount} isIconOnly={compact} icon={<Icon icon={CommandLineIcon} size="sm"/>} onClick={onCombine}/>}
    {onNewTerminal&&showGlobalAdd&&<Button isIconOnly id="connection-new-terminal" label={tr('新建终端')} tooltip={showShortcuts?shortcutTooltip(tr('新建终端'),'new-tab',shortcuts):tr('新建终端')}
      aria-keyshortcuts={shortcutAria('new-tab',shortcuts)} size="sm" variant="ghost" style={{flexShrink:0,alignSelf:'center'}} icon={<Icon icon={PlusIcon} size="sm"/>} onClick={onNewTerminal}/>}
  </Stack>
}
