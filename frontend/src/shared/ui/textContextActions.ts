import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import type {ContextMenuOption} from '@astryxdesign/core/ContextMenu'
import {readClipboardText,writeClipboardText} from '../bridge/application'

const editors=new WeakMap<HTMLElement,(onError:(message:string)=>void)=>ContextMenuOption[]>()
export function registerTextContextActions(element:HTMLElement,actions:(onError:(message:string)=>void)=>ContextMenuOption[]){
  editors.set(element,actions)
  return ()=>{editors.delete(element)}
}
export function textContextActions(target:Element,onError:(message:string)=>void):ContextMenuOption[]|null{
  for(let node:Element|null=target;node;node=node.parentElement){const actions=editors.get(node as HTMLElement);if(actions)return actions(onError)}
  const input=target.closest('input,textarea')
  if(!(input instanceof HTMLInputElement||input instanceof HTMLTextAreaElement))return null
  if(input instanceof HTMLInputElement&&!['text','search','password','email','tel','url','number'].includes(input.type))return null
  const start=input.selectionStart,end=input.selectionEnd
  const writable=()=>input.isConnected&&!input.disabled&&!input.readOnly&&!input.closest('[inert]')
  const secret=input instanceof HTMLInputElement&&input.type==='password'
  const selection=!secret&&start!==null&&end!==null?input.value.slice(start,end):''
  const restore=()=>{input.focus({preventScroll:true});if(start!==null&&end!==null)input.setSelectionRange(start,end)}
  const edit=(command:'insertText'|'delete',text='')=>{
    if(!writable())return
    restore()
    if(!document.execCommand(command,false,text))onError(tr("无法编辑此输入框，请使用键盘快捷键。"))
  }
  const guarded=(action:()=>Promise<void>)=>{void action().catch(()=>onError(tr("剪贴板操作失败，请重试。")))}
  const clipboardEdit=async(action:(apply:(command:'insertText'|'delete',text?:string)=>void)=>Promise<void>)=>{
    restore()
    let changed=false
    const invalidate=()=>{changed=true}
    input.addEventListener('input',invalidate);input.addEventListener('blur',invalidate)
    try{await action((command,text)=>{if(!changed&&document.activeElement===input)edit(command,text)})}
    finally{input.removeEventListener('input',invalidate);input.removeEventListener('blur',invalidate)}
  }
  return [
    {label:tr("剪切"),isDisabled:!selection||!writable(),onClick:()=>guarded(()=>clipboardEdit(async apply=>{if(writable()){await writeClipboardText(selection);apply('delete')}}))},
    {label:tr("复制"),isDisabled:!selection,onClick:()=>guarded(()=>writeClipboardText(selection))},
    {label:tr("粘贴"),isDisabled:!writable(),onClick:()=>guarded(()=>clipboardEdit(async apply=>{const text=await readClipboardText();if(text.length>1048576){onError(tr("粘贴内容超过 1 MiB，请分段粘贴。"));return}apply('insertText',text)}))},
    {label:tr("删除"),isDisabled:start===null||start===end||!writable(),onClick:()=>edit('delete')},
    {type:'divider'},
    {label:tr("全选"),isDisabled:input.disabled,onClick:()=>{if(input.isConnected){input.focus({preventScroll:true});input.select()}}},
  ]
}
