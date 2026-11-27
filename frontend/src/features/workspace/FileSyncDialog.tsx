import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {lazy,Suspense,useEffect,useId,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput'
import {TabList,Tab} from '@astryxdesign/core/TabList'
import EditorDialog from '../../shared/ui/EditorDialog'
import WindowedList from '../../shared/ui/WindowedList'
import {fileGateway,type FileLocation,type LocalFolder,type BilateralAction,type DirectorySyncView,type ComparedText} from '../../shared/bridge/files'
const RemoteCodeEditor=lazy(()=>import('./RemoteCodeEditor'))
const FileDiffEditor=lazy(()=>import('./FileDiffEditor'))
const names:Record<string,string>={get upload() { return tr("上传") },get download() { return tr("下载") },get 'delete-local'() { return tr("删除本地") },get 'delete-remote'() { return tr("删除远程") },get conflict() { return tr("需要处理") },get retained() { return tr("保留") }}
export default function FileSyncDialog({isOpen,remote,local,onClose,onLocal,onRunning,onFileGuard,onProtection,onReveal,serverName=tr("远程服务器"),gateway=fileGateway}:{isOpen:boolean;remote:FileLocation;local:LocalFolder|null;onClose:()=>void;onLocal:(folder:LocalFolder)=>void;onRunning:(running:boolean)=>void;serverName?:string;gateway?:typeof fileGateway;onFileGuard?:import('./fileCloseGuard').RegisterFileGuard;onProtection?:(key:string,value:boolean)=>void;onReveal?:()=>void}){
  useLocale()

 const [root,setRoot]=useState(local),[left,setLeft]=useState('/'),[right,setRight]=useState(remote.path),[deletes,setDeletes]=useState(false),[preview,setPreview]=useState<BilateralAction[]|null>(null),[view,setView]=useState<DirectorySyncView|null>(null),[running,setRunning]=useState(false),[busy,setBusy]=useState(false),[selected,setSelected]=useState(''),[error,setError]=useState(''),[status,setStatus]=useState(tr("先扫描预览，再确认开启；首次不会将单端独有文件判为删除。"))
 const [merge,setMerge]=useState<{path:string;revision:number;text:ComparedText}|null>(null),[draft,setDraftValue]=useState(''),[mergeTab,setMergeTab]=useState('result')
 const draftRevision=useRef(0)
 function setDraft(value:string){draftRevision.current++;setDraftValue(value)}
 const control=useRef<(()=>void)|null>(null),held=useRef(''),sequence=useRef(0),alive=useRef(true),runningNow=useRef(false),errorId=useId(),mergeId=useId()
 runningNow.current=running
 function release(){if(held.current)void gateway.forgetComparison(held.current).catch(()=>{});held.current=''}
 function stop(){sequence.current++;control.current?.();control.current=null;setRunning(false);onRunning(false);setStatus(tr("自动同步已停止；重新开启前请再次扫描。"));setPreview(null);setView(null);release()}
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;sequence.current++;control.current?.();release();onRunning(false)}},[])
 useEffect(()=>{if(!runningNow.current){setRoot(local);setRight(remote.path)}},[isOpen])
 useEffect(()=>()=>{sequence.current++;control.current?.();control.current=null;setRunning(false);onRunning(false);release()},[remote.sessionId,remote.attempt])
 useEffect(()=>{if(error&&isOpen)requestAnimationFrame(()=>document.getElementById(errorId)?.focus())},[error,isOpen,errorId])
 function invalidate(){release();setPreview(null);setView(null);setError('');setStatus(tr("范围已变化，请重新扫描。"))}
 async function choose(){setBusy(true);setError('');try{const chosen=await gateway.selectLocal();if(chosen){invalidate();setRoot(chosen);setLeft('/');onLocal(chosen)}}catch(cause){setError(problem(cause))}finally{setBusy(false)}}
 function scan(){if(!root){setError(tr("请先选择本地目录。"));return};if(!left.startsWith('/')||!right.startsWith('/')){setError(tr("请输入以 / 开头的路径。"));return};invalidate();setBusy(true);setStatus(tr("正在扫描完整子目录…"));const seq=++sequence.current;let resultSeen=false
  control.current=gateway.compare({left:{side:'local',token:root.token,path:left,sessionId:'',attempt:0},right:{...remote,path:right},recursive:true},event=>{if(seq!==sequence.current)return;setStatus(tr("已检查 {p0} 项", event.scanned));if(event.result){resultSeen=true;held.current=event.result.id;void gateway.previewDirectorySync(event.result.id,deletes).then(actions=>{if(seq===sequence.current){setPreview(actions);setStatus(tr("扫描完成。确认后自动复制安全变化；冲突文件等待处理。"))}},cause=>{if(seq===sequence.current)setError(problem(cause))}).finally(()=>{if(seq===sequence.current)setBusy(false)})}},cause=>{if(seq===sequence.current){setError(problem(cause));setBusy(false)}},()=>{if(seq===sequence.current&&!resultSeen)setBusy(false)})
 }
 function start(){if(!held.current||busy||running)return;setError('');setRunning(true);onRunning(true);const seq=++sequence.current
  control.current?.();control.current=gateway.directorySync({comparisonId:held.current,deleteEnabled:deletes,confirmed:true},next=>{if(alive.current&&seq===sequence.current){setView(next);setStatus(next.message)}},cause=>{if(alive.current&&seq===sequence.current){setError(problem(cause));setRunning(false);onRunning(false);setPreview(null);release()}},()=>{if(alive.current&&seq===sequence.current){setRunning(false);onRunning(false)}})
 }
 const guardKey='sync:'+mergeId,dirty=!!merge&&draft!==merge.text.left.content
 const latestGuard=useRef({getCloseState:()=>({dirty:false,busy:false,syncing:false,revision:0}),saveForClose:async()=>false,reveal:()=>{}})
 latestGuard.current={getCloseState:()=>({dirty,busy:!!merge&&working,syncing:running,revision:draftRevision.current}),saveForClose:()=>resolve('merge'),reveal:()=>onReveal?.()}
 useEffect(()=>{onFileGuard?.(remote.sessionId,guardKey,{getCloseState:()=>latestGuard.current.getCloseState(),saveForClose:()=>latestGuard.current.saveForClose(),reveal:()=>latestGuard.current.reveal()});return()=>onFileGuard?.(remote.sessionId,guardKey,null)},[remote.sessionId,guardKey,onFileGuard])
 useEffect(()=>{onProtection?.(guardKey,dirty||!!merge&&busy);return()=>onProtection?.(guardKey,false)},[guardKey,dirty,merge,busy,onProtection])
 useEffect(()=>{if(!dirty)return;const protect=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue=''};window.addEventListener('beforeunload',protect);return()=>window.removeEventListener('beforeunload',protect)},[dirty])
 const actions=view?.actions??preview??[],item=actions.find(a=>a.path===selected),working=busy||running&&['scanning','copying'].includes(view?.state??'scanning')
 async function resolve(choice:'local'|'remote'|'merge'):Promise<boolean>{if(!view||!item||working)return false;setBusy(true);setError('');try{await gateway.resolveDirectorySync({id:view.id,revision:merge?.revision??view.revision,path:item.path,choice,content:choice==='merge'?draft:'',leftVersion:merge?.text.left.version??'',rightVersion:merge?.text.right.version??''});setMerge(null);setSelected('');setStatus(tr("已提交处理，正在重新检查两端。"));return true}catch(cause){setError(problem(cause));return false}finally{setBusy(false)}}
 async function readMerge(){if(!view||!item||working)return;setBusy(true);setError('');try{const text=await gateway.syncConflictText(view.id,item.path,view.revision);setMerge({path:item.path,revision:view.revision,text});setDraft(text.left.content);setMergeTab('result')}catch(cause){setError(problem(cause))}finally{setBusy(false)}}
 return <EditorDialog isOpen={isOpen} title={merge?tr("合并同步冲突"):tr("自动双向同步")} action="edit" closeLabel={tr("关闭自动同步设置")} cancelLabel={running?tr("收起"):tr("关闭")} onClose={()=>{setMerge(null);onClose()}} busy={busy} subtitle={merge?merge.path:tr("对指定的本地与远程目录自动同步；关闭此设置窗口后继续，SSH 断开或应用退出后停止。")} hint={merge?tr("合并结果以 UTF-8 / LF 保存到两端；请先核对差异。"):tr("修改先校验再提交；不会自动恢复上次的同步。")} formProps={{onSubmit:event=>{event.preventDefault();if(merge)void resolve('merge');else if(!running){if(preview)start();else scan()}}}} actions={merge?<><Button label={tr("返回冲突列表")} type="button" variant="secondary" size="md" isDisabled={busy} onClick={event=>{event.preventDefault();setMerge(null)}}/><Button label={tr("保存合并到两端")} type="submit" variant="primary" size="md" isDisabled={working}/></>:running?<Button label={tr("停止自动同步")} type="button" variant="secondary" size="md" onClick={event=>{event.preventDefault();stop()}}/>:<Button label={preview?tr("确认并开启"):tr("扫描预览")} type="submit" size="md" variant="primary" isDisabled={busy||!gateway.available()} isLoading={busy}/> }>
  {merge?<>
   <TabList role="tablist" aria-label={tr("合并视图")} size="md" value={mergeTab} onChange={setMergeTab}>{[['result',tr("合并结果")],['diff',tr("两端差异")]].map(([value,label])=><Tab key={value} id={mergeId+'-'+value} value={value} label={label} panelId={mergeId+'-content'}/>)}</TabList>
   <Stack id={mergeId+'-content'} role="tabpanel" aria-labelledby={mergeId+'-'+mergeTab} style={{height:'min(calc(var(--spacing-10) * 8), 40dvh)',minHeight:'calc(var(--spacing-10) * 4)',minWidth:0}}><Suspense fallback={<Text role="status">{tr("正在准备编辑器…")}</Text>}>{mergeTab==='diff'?<FileDiffEditor left={merge.text.left.content} right={merge.text.right.content} path={merge.path}/>:<RemoteCodeEditor value={draft} path={merge.path} label={tr("同步合并结果")} readOnly={busy} onChange={setDraft} onSave={()=>void resolve('merge')}/>}</Suspense></Stack>
   <Stack direction="horizontal" gap={2} wrap="wrap"><Button label={tr("以本地文本为起点")} variant="ghost" size="md" isDisabled={busy} onClick={()=>setDraft(merge.text.left.content)}/><Button label={tr("以远程文本为起点")} variant="ghost" size="md" isDisabled={busy} onClick={()=>setDraft(merge.text.right.content)}/></Stack>
  </>:<>
   <Stack direction="horizontal" align="center" justify="between" gap={2} wrap="wrap" padding={3} style={{borderRadius:'var(--radius-element)',background:'var(--color-background-muted)',border:'var(--border-width) solid var(--color-border)'}}><StackItem size="fill" style={{minWidth:'calc(var(--spacing-10) * 4)'}}><Text type="label">{tr("本地目录：")}{root?.name??tr("尚未选择")}</Text></StackItem><Button label={tr("选择本地目录")} type="button" size="md" variant="secondary" isDisabled={busy||running} onClick={()=>void choose()}/></Stack>
   <Stack direction="horizontal" gap={3} wrap="wrap">
    <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}><TextInput label={tr("本地路径（相对授权目录）")} value={left} onChange={value=>{setLeft(value);invalidate()}} size="md" isDisabled={busy} isReadOnly={running}/></StackItem>
    <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}><TextInput label={tr("远程目录 · {p0}", serverName)} value={right} onChange={value=>{setRight(value);invalidate()}} size="md" isDisabled={busy} isReadOnly={running}/></StackItem>
   </Stack>
   <CheckboxInput label={tr("同步删除")} description={tr("开启后，一端删除已同步项目会删除另一端对应项目。两端同时修改时等待处理。")} value={deletes} onChange={value=>{setDeletes(value);invalidate()}} isDisabled={busy} isReadOnly={running}/>
   {!running&&preview&&<Button label={tr("重新扫描")} variant="ghost" size="md" isDisabled={busy} onClick={scan}/>}
   {(running||preview)&&<Stack style={{height:actions.length?'min(calc(var(--size-element-md) * '+Math.min(actions.length,4)+' * 2 + var(--spacing-4)), 30dvh)':'calc(var(--spacing-10) * 2.2)',minHeight:'calc(var(--spacing-10) * 2.2)',minWidth:0,borderRadius:'var(--radius-element)',border:'var(--border-width) solid var(--color-border)',background:'var(--color-background-muted)',overflow:'hidden'}}>{actions.length?<WindowedList items={actions} itemKey={item=>item.path} label={running?tr("自动同步待处理项"):tr("自动同步预览")} estimateSize={()=>64} row={entry=>({label:entry.path,description:names[entry.kind]+(entry.message?' · '+entry.message:''),isSelected:selected===entry.path,isDisabled:working,onClick:()=>{setSelected(entry.path);setError('')}})}/>:<Stack height="100%" align="center" justify="center" padding={4}><Text type="supporting" color="secondary" role="status">{tr("两端已一致，没有待处理项。")}</Text></Stack>}</Stack>}
   {running&&item?.kind==='conflict'&&!item.directory&&<Stack direction="horizontal" gap={2} wrap="wrap"><Button label={tr("采用本地")} variant="secondary" size="md" isDisabled={working} onClick={()=>void resolve('local')}/><Button label={tr("采用远程")} variant="secondary" size="md" isDisabled={working} onClick={()=>void resolve('remote')}/><Button label={tr("合并文本…")} variant="secondary" size="md" isDisabled={working} onClick={()=>void readMerge()}/></Stack>}
  </>}
  {error&&<Text id={errorId} role="alert" tabIndex={-1} type="supporting">{localizeMessage(error)}</Text>}<Text type="supporting" role="status" color="secondary">{status}</Text>
 </EditorDialog>
}
function problem(cause:unknown){return cause instanceof Error?cause.message:tr("操作未完成，请重新检查并重试。")}
