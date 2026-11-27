import { useNotice } from './useNotice'
import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import {useCallback,useRef,useState,type ReactNode} from 'react'
import {ContextMenu,type ContextMenuOption} from '@astryxdesign/core/ContextMenu'
import {writeClipboardText} from '../bridge/application'
import {textContextActions} from './textContextActions'

interface AppContextMenuProps {
  children: ReactNode
  actions?: ContextMenuOption[] | ((target: Element) => ContextMenuOption[])
  actionsForTarget?: (target: Element) => ContextMenuOption[] | undefined
  label?: string
  fill?: boolean
  onOpenChange?: (open: boolean) => void
}

export default function AppContextMenu({children,actions=[],actionsForTarget,label,fill=false,onOpenChange}: AppContextMenuProps){
  useLocale()

  const [items,setItems]=useState<ContextMenuOption[]>([]),[editing,setEditing]=useState(false)
  const notice=useNotice()
  const setError=(message:string)=>notice(message,'error')
  const returnTo=useRef<HTMLElement|null>(null)
  const trigger=useCallback((node:HTMLElement|null)=>{if(node)Object.assign(node.style,{display:'flex',flexDirection:'column',flex:'1 1 auto',minWidth:'0',minHeight:'0',height:fill?'100%':'',userSelect:'text'})},[fill])
  return <ContextMenu ref={trigger} data-app-context-scope="" label={editing?tr("文本编辑"):label??tr("应用操作")} presentation="popover" items={items}
    onContextMenuCapture={event=>{
      if(!(event.target instanceof Element))return
      if(event.target.closest('[data-app-context-scope]')!==event.currentTarget)return
      if(event.target.closest('[role="menu"]')){event.preventDefault();event.stopPropagation();return}
      const targetActions=actionsForTarget?.(event.target)
      const active=document.activeElement instanceof HTMLElement?document.activeElement:null
      returnTo.current=targetActions?event.target.closest<HTMLElement>('[role="radio"],[role="tab"]')??active:active
      const textItems=textContextActions(event.target,setError)
      const selected=textItems||targetActions?'':window.getSelection()?.toString()??''
      const defaultActions = typeof actions === 'function' ? actions(event.target) : actions
      setEditing(!!textItems)
      setItems(textItems??[
        ...(selected?[{label:tr("复制"),onClick:()=>{void writeClipboardText(selected).catch(()=>setError(tr("复制失败，请重试。")))}},{type:'divider' as const}]:[]),
        ...(targetActions??(defaultActions.length?defaultActions:[{label:tr("复制"),isDisabled:!selected}])),
      ])
    }}
    onContextMenu={event=>event.stopPropagation()}
    onOpenChange={open=>{onOpenChange?.(open);if(!open){setItems([]);queueMicrotask(()=>{
      const active=document.activeElement
      if(!active||active===document.body||active.closest('[role="menu"]'))returnTo.current?.isConnected&&returnTo.current.focus({preventScroll:true})
      returnTo.current=null
    })}}}>
    {children}
  </ContextMenu>
}
