import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import type {EditorView} from '@codemirror/view'
import {undo,redo,undoDepth,redoDepth,selectAll} from '@codemirror/commands'
import {readClipboardText,writeClipboardText} from '../bridge/application'
import {registerTextContextActions} from './textContextActions'

export function registerCodeEditorContextActions(view:EditorView){
  return registerTextContextActions(view.dom,onError=>{
    const state=view.state,selection=state.sliceDoc(state.selection.main.from,state.selection.main.to)
    const writable=()=>view.dom.isConnected&&!view.state.readOnly&&view.state.doc===state.doc
    const guarded=(action:()=>Promise<void>)=>{void action().catch(()=>onError(tr("剪贴板操作失败，请重试。")))}
    const replace=(text:string)=>{if(writable()){view.dispatch({...state.replaceSelection(text),userEvent:'input.paste',scrollIntoView:true});view.focus()}}
    return [
      {label:tr("撤销"),isDisabled:state.readOnly||undoDepth(state)===0,onClick:()=>{if(writable()){undo(view);view.focus()}}},
      {label:tr("重做"),isDisabled:state.readOnly||redoDepth(state)===0,onClick:()=>{if(writable()){redo(view);view.focus()}}},
      {type:'divider'},
      {label:tr("剪切"),isDisabled:state.readOnly||!selection,onClick:()=>guarded(async()=>{if(writable()){await writeClipboardText(selection);replace('')}})},
      {label:tr("复制"),isDisabled:!selection,onClick:()=>guarded(()=>writeClipboardText(selection))},
      {label:tr("粘贴"),isDisabled:state.readOnly,onClick:()=>guarded(async()=>{const text=await readClipboardText();if(text.length>1048576){onError(tr("粘贴内容超过 1 MiB，请分段粘贴。"));return}replace(text)})},
      {label:tr("删除"),isDisabled:state.readOnly||!selection,onClick:()=>replace('')},
      {type:'divider'},
      {label:tr("全选"),onClick:()=>{if(view.dom.isConnected){selectAll(view);view.focus()}}},
    ]
  })
}
