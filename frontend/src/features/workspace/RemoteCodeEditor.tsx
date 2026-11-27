import { useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { StackItem } from '@astryxdesign/core/Stack'
import { Annotation, EditorState, Compartment, Transaction } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, drawSelection, highlightActiveLine, highlightSpecialChars } from '@codemirror/view'
import { matchesShortcut } from './shortcutReference'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { getSearchQuery, openSearchPanel, search, searchKeymap } from '@codemirror/search'
import { StreamLanguage, syntaxHighlighting, bracketMatching, indentOnInput,type StreamParser } from '@codemirror/language'
import { shell } from '@codemirror/legacy-modes/mode/shell'
import { javascript,json,typescript } from '@codemirror/legacy-modes/mode/javascript'
import { python } from '@codemirror/legacy-modes/mode/python'
import { yaml } from '@codemirror/legacy-modes/mode/yaml'
import { toml } from '@codemirror/legacy-modes/mode/toml'
import { properties } from '@codemirror/legacy-modes/mode/properties'
import { nginx } from '@codemirror/legacy-modes/mode/nginx'
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile'
import { css } from '@codemirror/legacy-modes/mode/css'
import { xml,html } from '@codemirror/legacy-modes/mode/xml'
import { go } from '@codemirror/legacy-modes/mode/go'
import { standardSQL } from '@codemirror/legacy-modes/mode/sql'
import { powerShell } from '@codemirror/legacy-modes/mode/powershell'
import { colors, editorTheme } from '../../shared/ui/codeEditorTheme'
import { tags } from '@lezer/highlight'
import {registerCodeEditorContextActions} from '../../shared/ui/codeEditorContextActions'
import { textLanguage } from './remoteText'
import EditorSearchPanel from './EditorSearchPanel'

const parsers={shell,javascript,json,typescript,python,yaml,toml,properties,nginx,dockerfile:dockerFile,css,xml,html,go,sql:standardSQL,powershell:powerShell}
const replaceFromServer=Annotation.define<boolean>()
export function remoteSyntax(path:string){const parser:StreamParser<unknown>|undefined=parsers[textLanguage(path) as keyof typeof parsers];return parser?[StreamLanguage.define({...parser,tokenTable:{...parser.tokenTable,property:tags.propertyName}})]:[]}
export default function RemoteCodeEditor({value,path,label,readOnly=false,onChange,onSave,active=true,findRequest=0,searchTarget,onSearchOpenChange}:{
  value:string;path:string;label:string;readOnly?:boolean;onChange?:(value:string)=>void;onSave?:()=>void;active?:boolean;findRequest?:number
  searchTarget?:HTMLElement|null;onSearchOpenChange?:(open:boolean)=>void
}) {
  const mount=useRef<HTMLElement>(null),view=useRef<EditorView|null>(null)
  const [searchHost,setSearchHost]=useState<{dom:HTMLElement;view:EditorView}|null>(null), [,refreshSearch]=useState(0)
  const latest=useRef({onChange,onSave,readOnly,active,onSearchOpenChange});latest.current={onChange,onSave,readOnly,active,onSearchOpenChange}
  const externalSearch=searchTarget!==undefined
  const settings=useRef(new Compartment()),language=useRef(new Compartment()),id=useId()
  const attributes=()=>[EditorState.readOnly.of(latest.current.readOnly),EditorView.editable.of(!latest.current.readOnly),EditorView.contentAttributes.of({id,'aria-label':label,spellcheck:'false',autocorrect:'off',autocapitalize:'off',autocomplete:'off',tabindex:'0'})]
  const syntax=()=>remoteSyntax(path)
  useEffect(()=>{
    if(!mount.current)return
    const instance=new EditorView({parent:mount.current,state:EditorState.create({doc:value,extensions:[lineNumbers(),highlightSpecialChars(),history({minDepth:20}),drawSelection(),highlightActiveLine(),syntaxHighlighting(colors),bracketMatching(),indentOnInput(),editorTheme,EditorView.lineWrapping,
      search({top:true,createPanel(view){
        const dom=view.dom.ownerDocument.createElement('div')
        if(externalSearch)dom.style.display='none'
        return {dom,top:true,mount:()=>{setSearchHost({dom,view});latest.current.onSearchOpenChange?.(true)},
          destroy:()=>{setSearchHost(current=>current?.dom===dom?null:current);latest.current.onSearchOpenChange?.(false)},
          update:update=>{if(update.docChanged||update.state.readOnly!==update.startState.readOnly||getSearchQuery(update.state)!==getSearchQuery(update.startState))refreshSearch(value=>value+1)}}
      }}),
      externalSearch?EditorView.theme({'.cm-panels-top':{display:'none'}}):[],
      EditorView.domEventHandlers({keydown(event, view) { if(!latest.current.onSave||!matchesShortcut(event,'save'))return false; if(!event.repeat&&!view.composing&&latest.current.active&&!latest.current.readOnly)latest.current.onSave(); return true }}),
      keymap.of([...defaultKeymap,...historyKeymap,...searchKeymap]),settings.current.of(attributes()),language.current.of(syntax()),EditorView.updateListener.of(update=>{if(update.docChanged&&!update.transactions.some(transaction=>transaction.annotation(replaceFromServer)))latest.current.onChange?.(update.state.doc.toString())})]})})
    view.current=instance
    const unregisterContext=registerCodeEditorContextActions(instance)
    return()=>{unregisterContext();instance.destroy();view.current=null}
  },[])
  useEffect(()=>{
    const instance=view.current
    if(!instance)return
    const before=instance.state.doc.toString()
    if(before===value)return
    let from=0,to=before.length,end=value.length
    while(from<to&&from<end&&before[from]===value[from])from++
    while(to>from&&end>from&&before[to-1]===value[end-1]){to--;end--}
    const changes=instance.state.changes({from,to,insert:value.slice(from,end)})
    instance.dispatch({changes,effects:instance.scrollSnapshot().map(changes),
      annotations:[replaceFromServer.of(true),Transaction.addToHistory.of(false)]})
  },[value])
  useEffect(()=>{view.current?.dispatch({effects:settings.current.reconfigure(attributes())})},[readOnly,label])
  useEffect(()=>{view.current?.dispatch({effects:language.current.reconfigure(syntax())})},[path])
  useEffect(()=>{if(active){view.current?.requestMeasure();if(!readOnly)view.current?.focus()}},[active])
  useEffect(()=>{
    if(!findRequest||!view.current)return
    openSearchPanel(view.current)
    const input=searchTarget?.querySelector<HTMLInputElement>('input[main-field]')
    input?.focus();input?.select()
  },[findRequest,searchTarget])
  const target=externalSearch?searchTarget:searchHost?.dom
  return <>
    <StackItem ref={mount} size="fill" style={{height:'100%',minHeight:0,minWidth:0,overflow:'hidden',borderRadius:'var(--radius-element)'}}/>
    {searchHost&&target&&createPortal(<EditorSearchPanel view={searchHost.view} inline={externalSearch}/>,target)}
  </>
}
