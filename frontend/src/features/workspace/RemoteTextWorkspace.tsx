import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { lazy,Suspense,useEffect,useRef,useState } from 'react'
import { Stack,StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { ChevronDownIcon, MagnifyingGlassIcon, XMarkIcon } from '@heroicons/react/24/outline'
import { DropdownMenu, type DropdownMenuOption } from '@astryxdesign/core/DropdownMenu'
import { fileGateway,type TextFile } from '../../shared/bridge/files'
import { writeClipboardText } from '../../shared/bridge/application'
import { detectFontPlatform } from '../../shared/fonts/platformFonts'
import { matchesShortcut } from './shortcutReference'
import { fileLocation,remoteTextError } from './remoteText'

const CodeEditor=lazy(()=>import('./RemoteCodeEditor'))
export type RemoteTextController={guardClose:(action:()=>void)=>void;getCloseState:()=>{dirty:boolean;busy:boolean;path:string;revision:number};saveForClose:()=>Promise<boolean>}
type Pending={message:string;label:string;run:()=>void;saveFirst:boolean}
export default function RemoteTextWorkspace({documentId,sessionId,attempt,connected,active,path,onClose,onReveal,onController,onProtection,onDirtyChange,onSaved,gateway=fileGateway}:{
  documentId:string;sessionId:string;attempt:number;connected:boolean;active:boolean;path:string;onClose:()=>void;onReveal:()=>void;onController:(id:string,controller:RemoteTextController|null)=>void;onProtection:(id:string,protectedValue:boolean)=>void;onDirtyChange:(id:string,dirty:boolean)=>void;onSaved:()=>void;gateway?:typeof fileGateway
}) {
  useLocale()

  const [file,setFile]=useState<TextFile|null>(null),[value,setValue]=useState('')
  const [opening,setOpening]=useState(false)
  const [statusOpen,setStatusOpen]=useState(false)
  const [busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState(''),[conflict,setConflict]=useState<TextFile|null>(null),[compare,setCompare]=useState(false),[pending,setPending]=useState<Pending|null>(null)
  const working=useRef(false),mounted=useRef(true),sequence=useRef(0),latest=useRef({guard:(action:()=>void)=>{action()},getCloseState:()=>({dirty:false,busy:false,path:'',revision:0}),saveForClose:async()=>false})
  const draftRevision=useRef(0), [findRequest,setFindRequest]=useState(0)
  const [searchTarget,setSearchTarget]=useState<HTMLElement|null>(null),[searchOpen,setSearchOpen]=useState(false)
  const mac=detectFontPlatform()==='macos'
  const dirty=!!file&&(value!==file.content)
  const change=(next:string)=>{draftRevision.current++;setValue(next);setNotice('')}
  function applyFile(next:TextFile){
    setFile(next);setValue(next.content);setConflict(null);setCompare(false);setPending(null);setStatusOpen(false)
  }
  function guard(run:()=>void,message=tr("当前文件有未保存的修改。"),label=tr("放弃修改并继续"),saveFirst=true){
    if(working.current){setError(tr("文件操作正在进行，请完成后再继续。"));onReveal();return}
    if(dirty){setPending({message,label,run,saveFirst});setStatusOpen(true);onReveal()}else run()
  }
  latest.current.guard=action=>guard(action,tr("关闭文件会丢弃当前未保存的修改。"),tr("放弃修改并关闭文件"))
  latest.current.getCloseState=()=>({dirty,busy:working.current,path:file?.path??path,revision:draftRevision.current})
  latest.current.saveForClose=()=>dirty?save():Promise.resolve(!working.current)
  useEffect(()=>{mounted.current=true;onController(documentId,{guardClose:action=>latest.current.guard(action),getCloseState:()=>latest.current.getCloseState(),saveForClose:()=>latest.current.saveForClose()});return()=>{mounted.current=false;sequence.current++;working.current=false;onController(documentId,null)}},[documentId,onController])
  const refreshState=useRef({file,dirty})
  refreshState.current={file,dirty}
  useEffect(()=>{
    if(!active||!connected||working.current||refreshState.current.dirty)return
    const {file:cached}=refreshState.current
    const revision=draftRevision.current, seq=sequence.current
    let cancelled=false
    if(!cached)setOpening(true)
    const isCurrent=()=>!cancelled&&mounted.current&&seq===sequence.current&&revision===draftRevision.current
    void gateway.readText(fileLocation(sessionId,attempt,path)).then(next=>{
      if(!isCurrent()||refreshState.current.dirty)return
      if(cached?.version!==next.version){applyFile(next);setNotice('')}
      setError('')
    }).catch(cause=>{
      if(isCurrent()&&!cached)setError(cause instanceof Error?cause.message:tr("远程文件读取失败。"))
    }).finally(()=>{if(!cancelled)setOpening(false)})
    return()=>{cancelled=true;setOpening(false)}
  },[active,connected,attempt,gateway,sessionId,path])
  useEffect(()=>{onProtection(documentId,dirty||busy)},[dirty,busy,onProtection,documentId])
  useEffect(()=>()=>onProtection(documentId,false),[onProtection,documentId])
  useEffect(()=>{onDirtyChange(documentId,dirty)},[documentId,dirty,onDirtyChange])
  useEffect(()=>{const before=(event:BeforeUnloadEvent)=>{if(dirty||working.current){event.preventDefault();event.returnValue=''}};if(dirty||busy)window.addEventListener('beforeunload',before);return()=>window.removeEventListener('beforeunload',before)},[dirty,busy])
  useEffect(()=>{
    if(!notice)return
    const timeout=window.setTimeout(()=>setNotice(''),2400)
    return()=>window.clearTimeout(timeout)
  },[notice])
  async function save(version=file?.version,then?:()=>void){
    if(!file||!version||working.current)return false
    if(!connected){setError(tr("连接已断开，编辑内容已保留。"));return false}
    const validation=remoteTextError(value);if(validation){setError(validation);return false}
    working.current=true;setBusy(true);setError('');setNotice('');const seq=++sequence.current
    const sourceEncoding=version===conflict?.version?conflict.encoding:file.encoding
    try{const result=await gateway.saveText({location:fileLocation(sessionId,attempt,file.path),encoding:sourceEncoding,version,content:value});if(!mounted.current||seq!==sequence.current)return false
      if(result.status==='conflict'){setConflict(result.file);setPending(null);setStatusOpen(false);return false}
      else{applyFile(result.file);setNotice(tr("已保存到服务器"));onSaved();working.current=false;then?.();return true}
    }catch(cause){if(mounted.current&&seq===sequence.current)setError(cause instanceof Error?cause.message:tr("保存未完成，编辑内容已保留。"));return false}
    finally{if(mounted.current&&seq===sequence.current){working.current=false;setBusy(false)}}
  }
  function review(action:Pending){setPending(action);setStatusOpen(true)}
  function useServer(){if(!conflict)return;review({message:tr("使用服务器版本会丢弃当前未保存的修改。"),label:tr("确认使用服务器版本"),saveFirst:false,run:()=>{applyFile(conflict);setNotice(tr("已载入服务器版本"))}})}
  function requestSave(){if(!dirty||working.current)return;if(conflict)review({message:tr("覆盖会替换已显示的服务器版本；若服务器再次变化，将重新提示冲突。"),label:tr("确认覆盖服务器版本"),saveFirst:false,run:()=>{setPending(null);void save(conflict.version)}});else void save()}
  const visiblePath=file?.path??path, nameStart=visiblePath.lastIndexOf('/')+1
  const displayDirectory=visiblePath.slice(0,nameStart).replaceAll('/',' / ').trim()
  const filename=visiblePath.slice(nameStart)
  function copyPath(full:boolean){
    void writeClipboardText(full?visiblePath:filename).then(()=>setNotice(full?tr("已复制路径"):tr("已复制文件名")))
      .catch(()=>setNotice(tr("复制失败，请重试。")))
  }
  const status=pending?.message || error || (conflict?tr("服务器文件已更改，本地编辑内容已保留。"):'') ||
    (!connected?tr("连接已断开，编辑内容已保留。"):'') || (opening?tr("正在读取远程文本…"):'') ||
    (busy?tr("正在保存…"):'') || notice || (dirty?tr("未保存"):'')
  const statusActions:DropdownMenuOption[]=pending?[
    {label:tr("继续编辑"),onClick:()=>setPending(null)},
    ...(pending.saveFirst?[{label:tr("保存后继续"),isDisabled:busy||!connected||!!conflict,onClick:()=>void save(file?.version,pending.run)}]:[]),
    {label:pending.label,variant:'destructive',isDisabled:busy,onClick:()=>{setPending(null);pending.run()}},
  ]:conflict?[
    {label:compare?tr("收起服务器版本"):tr("对比服务器版本"),onClick:()=>setCompare(!compare)},
    {label:tr("使用服务器版本"),isDisabled:busy,hasCloseOnSelect:false,onClick:useServer},
    {label:tr("覆盖服务器版本"),isDisabled:busy||!connected,hasCloseOnSelect:false,onClick:requestSave},
  ]:[]
  return <Stack role="region" aria-label={tr("远程文本编辑")} hidden={!active} inert={!active} style={{height:'100%',minHeight:0,minWidth:0,display:active?'flex':'none'}}
    onKeyDownCapture={event=>{
      if(!active||event.defaultPrevented||event.nativeEvent.isComposing||event.nativeEvent.keyCode===229)return
      const save=matchesShortcut(event.nativeEvent,'save')
      const find=!event.altKey&&!event.shiftKey&&event.key.toLowerCase()==='f'&&(mac?event.metaKey&&!event.ctrlKey:event.ctrlKey&&!event.metaKey)
      if(!save&&!find)return
      event.preventDefault();event.stopPropagation()
      if(!event.repeat){if(save)requestSave();else if(file)setFindRequest(previous=>previous+1)}
    }}>
    <Stack direction="horizontal" align="center" gap={1} paddingInline={3} paddingBlock={1}
      style={{flexShrink:0,minWidth:0,position:'relative',zIndex:1}}>
      <StackItem size="fill" style={{minWidth:0,overflow:'hidden'}}>
        <Stack direction="horizontal" align="center" gap={1} style={{minWidth:0,overflow:'hidden'}}>
          <Button label={displayDirectory||'/'} tooltip={tr("复制路径")} aria-label={tr("复制路径")} size="sm" variant="ghost"
            onClick={()=>copyPath(true)} style={{minWidth:'var(--spacing-5)',paddingInline:'var(--spacing-1)',fontWeight:'normal',color:'var(--color-text-secondary)'}}/>
          <Button label={filename} tooltip={tr("复制文件名")} aria-label={tr("复制文件名")} size="sm" variant="ghost"
            onClick={()=>copyPath(false)} style={{minWidth:0,flexShrink:0,maxWidth:'70%',paddingInline:'var(--spacing-2)',fontWeight:'normal',
              backgroundColor:'var(--color-neutral)'}}/>
        </Stack>
      </StackItem>
      {status&&<Stack style={{minWidth:0,maxWidth:searchOpen?'20%':'42%',flexShrink:1}} role={error||pending||conflict?'alert':'status'}>
        {statusActions.length?<DropdownMenu presentation="popover" alignment="end" items={statusActions} isMenuOpen={statusOpen} onOpenChange={setStatusOpen}
          renderTrigger={props=><Button {...props} label={localizeMessage(status)} tooltip={localizeMessage(status)} variant="ghost" size="sm"
            endContent={<Icon icon={ChevronDownIcon} size="sm"/>}/>}/>
          :<Text type="supporting" color="secondary" maxLines={1}
            style={{color:error?'var(--color-text-error)':undefined}}>{localizeMessage(status)}</Text>}
      </Stack>}
      <Stack ref={setSearchTarget} style={{minWidth:0,maxWidth:'min(70%, calc(var(--spacing-10) * 13))',display:searchOpen?'flex':'none'}}/>
      {!searchOpen&&<IconButton label={tr("查找")} tooltip={mac?'⌘F':'Ctrl+F'} size="sm" variant="ghost"
        icon={<Icon icon={MagnifyingGlassIcon} size="sm"/>} isDisabled={!file} onClick={()=>setFindRequest(previous=>previous+1)}/>
      }
      <IconButton label={tr("关闭文件")} tooltip={tr("关闭文件")} size="sm" variant="ghost"
        icon={<Icon icon={XMarkIcon} size="sm"/>} isDisabled={busy} onClick={()=>latest.current.guard(onClose)}/>
    </Stack>
    <StackItem size="fill" style={{minHeight:0,minWidth:0,overflow:'auto'}}><Stack direction="horizontal" gap={3} padding={3} wrap="wrap" style={{height:'100%',minHeight:0}}>
      <Stack gap={2} style={{flex:'1 1 calc(var(--spacing-10) * 8)',height:'100%',minHeight:'calc(var(--spacing-10) * 4)',minWidth:0}}>
        {compare&&<Text type="label">{tr("本地修改")}</Text>}
        {file&&<Suspense fallback={null}><CodeEditor path={file.path} label={tr("远程文件内容")} value={value} onChange={change}
          findRequest={findRequest} searchTarget={searchTarget} onSearchOpenChange={setSearchOpen} readOnly={busy} active={active}/></Suspense>}
      </Stack>
      {compare&&conflict&&<Stack gap={2} style={{flex:'1 1 calc(var(--spacing-10) * 8)',height:'100%',minHeight:'calc(var(--spacing-10) * 4)',minWidth:0}}>
        <Text type="label">{tr("服务器版本（只读）")}</Text><Suspense fallback={<Text type="supporting">{tr("正在打开服务器版本…")}</Text>}><CodeEditor path={conflict.path} label={tr("服务器文件版本")} value={conflict.content} readOnly active={active}/></Suspense>
      </Stack>}
    </Stack></StackItem>
  </Stack>
}
