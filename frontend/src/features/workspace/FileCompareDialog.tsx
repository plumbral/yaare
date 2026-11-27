import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useCallback,useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Icon} from '@astryxdesign/core/Icon'
import {TextInput} from '@astryxdesign/core/TextInput'
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput'
import {ArrowsRightLeftIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import WindowedList from '../../shared/ui/WindowedList'
import {fileGateway,type FileLocation,type LocalFolder,type ComparisonView,type CompareEntry,type SyncIntent} from '../../shared/bridge/files'
import {selectTree} from './fileTreeModel'
import {transferSize as fileSize} from './transferModel'
const statuses:Record<string,string>={get 'same'() { return tr("内容相同") },get 'different'() { return tr("内容不同") },get 'left-only'() { return tr("仅本地") },get 'right-only'() { return tr("仅远程") },get 'directory'() { return tr("目录") },get 'unverified'() { return tr("无法确定") },get 'unsupported'() { return tr("不支持") },get 'name-conflict'() { return tr("名称冲突") },get 'type-conflict'() { return tr("类型冲突") }}
export default function FileCompareDialog({isOpen,remote,local,onClose,onText,onQueued,onLocal,gateway=fileGateway}:{isOpen:boolean;remote:FileLocation;local:LocalFolder|null;onClose:()=>void;onText:(id:string,relative:string)=>void;onQueued:()=>void;onLocal:(folder:LocalFolder)=>void;gateway?:typeof fileGateway}){
  useLocale()

 const [root,setRoot]=useState(local),[left,setLeft]=useState('/'),[right,setRight]=useState(remote.path),[recursive,setRecursive]=useState(false),[result,setResult]=useState<ComparisonView|null>(null),[selected,setSelected]=useState<Set<string>>(new Set()),[anchor,setAnchor]=useState(''),[busy,setBusy]=useState(false),[committing,setCommitting]=useState(false),[error,setError]=useState(''),[status,setStatus]=useState(tr("比较只读取文件，不修改任一端。")),[intent,setIntent]=useState<SyncIntent|null>(null)
 const sequence=useRef(0),cancel=useRef<(()=>void)|null>(null),alive=useRef(true),working=useRef(false),held=useRef(''),retain=useRef(false),intentNow=useRef<SyncIntent|null>(null);intentNow.current=intent
 const stop=()=>{sequence.current++;cancel.current?.();cancel.current=null;working.current=false;setBusy(false)}
 const release=()=>{if(held.current&&!retain.current)void gateway.forgetComparison(held.current).catch(()=>{});held.current='';if(intentNow.current)void gateway.discardSync(intentNow.current.id).catch(()=>{})}
 const invalidate=()=>{release();setResult(null);setSelected(new Set());setIntent(null);setStatus(tr("范围已变化，请重新比较。"))}
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;sequence.current++;cancel.current?.();release()}},[])
 useEffect(()=>{if(!isOpen){stop();release();return};retain.current=false;setRoot(local);setLeft('/');setRight(remote.path);setRecursive(false);setResult(null);setIntent(null);setSelected(new Set());setError('');setStatus(tr("比较只读取文件，不修改任一端。"))},[isOpen,remote])
 async function choose(){if(working.current)return;setError('');try{const folder=await gateway.selectLocal();if(folder){setRoot(folder);onLocal(folder);setResult(null);setLeft('/')}}catch(cause){setError(cause instanceof Error?cause.message:tr("本地目录未选择。"))}}
 function compare(){if(working.current){stop();setStatus(tr("比较已停止；未完成的快照不能用于同步。"));return};if(!root){setError(tr("请先选择并授权本地目录。"));return};if(!left.startsWith('/')||!right.startsWith('/')){setError(tr("请输入以 / 开头的本地相对根路径及远程绝对路径。"));return};release();retain.current=false;setResult(null);setSelected(new Set());setIntent(null);setError('');setStatus(tr("正在只读比较…"));working.current=true;setBusy(true);const id=++sequence.current
  cancel.current=gateway.compare({left:{...remote,side:'local',token:root.token,path:left},right:{...remote,path:right},recursive},event=>{if(!alive.current||id!==sequence.current)return;setStatus(tr("已检查 {p0} 项 · 内容读取 {p1}", event.scanned, fileSize(event.bytes)));if(event.result){held.current=event.result.id;setResult(event.result);setStatus(event.result.comparison.complete?tr("比较完成；请选择需要复制或查看的项。"):event.result.comparison.message)}},cause=>{if(alive.current&&id===sequence.current)setError(cause instanceof Error?cause.message:tr("比较未完成，请重试。"))},()=>{if(alive.current&&id===sequence.current){working.current=false;setBusy(false);cancel.current=null}})
 }
 const rows=result?.comparison.entries??[],ids=rows.map(e=>e.relative)
 const row=useCallback((entry:CompareEntry)=>({label:entry.relative==='.'?tr("当前文件"):entry.relative,description:entry.message||(tr("本地 {p0} · 远程 {p1}", (entry.leftExists?entry.directory?tr("目录"):fileSize(entry.leftSize):tr("缺失")), (entry.rightExists?entry.directory?tr("目录"):fileSize(entry.rightSize):tr("缺失")))),endContent:<Text type="supporting" color="secondary">{statuses[entry.status]??entry.status}</Text>,isSelected:selected.has(entry.relative),isDisabled:busy||committing,onClick:(event:React.MouseEvent<Element>)=>{const next=selectTree(ids,selected,entry.relative,anchor,event.ctrlKey||event.metaKey,event.shiftKey);if(next.limited){setError(tr("一次最多选择 256 项。"));return};setSelected(next.selected);setAnchor(next.anchor)}}),[rows,selected,anchor,busy,committing])
 async function preview(direction:string){if(!result||working.current)return;working.current=true;setCommitting(true);setError('');try{const next=await gateway.previewSync(result.id,direction,[...selected]);if(alive.current)setIntent(next)}catch(cause){setError(cause instanceof Error?cause.message:tr("复制预览未生成。"))}finally{working.current=false;if(alive.current)setCommitting(false)}}
 async function execute(){if(!intent||working.current)return;working.current=true;setCommitting(true);setError('');try{await gateway.executeSync(intent.id);setIntent(null);onQueued();onClose()}catch(cause){setIntent(null);setError(cause instanceof Error?cause.message:tr("文件在预览后可能变化，请重新比较。"))}finally{working.current=false;if(alive.current)setCommitting(false)}}
 const single=selected.size===1?rows.find(e=>selected.has(e.relative)):undefined
 return <EditorDialog isOpen={isOpen} title={tr("比较本地与远程文件")} action="edit" startContent={<Icon icon={ArrowsRightLeftIcon} size="md"/>} closeLabel={tr("关闭文件比较")} cancelLabel={tr("关闭")} busy={committing} onClose={onClose} hint={intent?tr("只复制确认清单；仅在目标端的文件保留，不执行删除。"):tr("单击选择，Ctrl / Shift 多选；比较结果保留 5 分钟。")} formProps={{'aria-label':tr("文件比较"),onSubmit:e=>{e.preventDefault();if(intent)void execute();else compare()}}}
 actions={intent?<><Button label={tr("返回比较结果")} size="md" variant="secondary" isDisabled={committing} onClick={()=>{void gateway.discardSync(intent.id).catch(()=>{});setIntent(null)}}/><Button label={tr("确认执行复制")} size="md" variant="primary" type="submit" isLoading={committing}/></>:<Button label={busy?tr("停止比较"):tr("开始比较")} size="md" variant={busy?'secondary':'primary'} type="submit" isDisabled={committing||!gateway.available()}/> }>
  {!intent&&<>
   <Stack direction="horizontal" align="center" justify="between" gap={2} wrap="wrap" padding={3} style={{borderRadius:'var(--radius-element)',background:'var(--color-background-muted)',border:'var(--border-width) solid var(--color-border)'}}>
    <StackItem size="fill" style={{minWidth:'calc(var(--spacing-10) * 4)'}}><Text type="label">{tr("本地授权目录：")}{root?.name??tr("尚未选择")}</Text></StackItem>
    <Button label={root?tr("更换本地目录"):tr("选择本地目录")} size="md" variant="secondary" isDisabled={busy||committing} onClick={()=>{void choose()}}/>
   </Stack>
   <Stack direction="horizontal" gap={3} wrap="wrap">
    <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}>
     <TextInput label={tr("本地路径（相对授权目录）")} value={left} onChange={value=>{setLeft(value);invalidate()}} autoComplete="off" size="md" isDisabled={busy||committing}/>
    </StackItem>
    <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}>
     <TextInput label={tr("远程路径")} value={right} onChange={value=>{setRight(value);invalidate()}} autoComplete="off" size="md" isDisabled={busy||committing}/>
    </StackItem>
   </Stack>
   <Stack direction="horizontal" align="center" justify="between" gap={2} wrap="wrap">
    <CheckboxInput label={tr("比较子目录")} value={recursive} onChange={value=>{setRecursive(value);invalidate()}} isDisabled={busy||committing}/>
    <Stack direction="horizontal" gap={2} wrap="wrap">
     <Button label={tr("查看文本差异")} size="md" variant="ghost" isDisabled={!single||single.directory||busy||committing} onClick={()=>{if(single&&result){retain.current=true;onText(result.id,single.relative);onClose()}}}/>
     <Button label={tr("预览上传所选")} size="md" variant="secondary" isDisabled={!selected.size||busy||committing||!result?.comparison.complete} onClick={()=>{void preview('upload')}}/>
     <Button label={tr("预览下载所选")} size="md" variant="secondary" isDisabled={!selected.size||busy||committing||!result?.comparison.complete} onClick={()=>{void preview('download')}}/>
    </Stack>
   </Stack>
  </>}
  {intent&&(
   <Stack padding={3} gap={1} style={{borderRadius:'var(--radius-element)',background:'var(--color-background-muted)',border:'var(--border-width) solid var(--color-border)'}}>
    <Text type="label">{intent.direction==='upload'?tr("本地 → 远程"):tr("远程 → 本地")} {tr(" · 将复制 ")}{intent.items.length} {tr(" 项")}{intent.retained?tr("；保留 {p0} 个仅在目标端的项", intent.retained):''}</Text>
    <Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{tr("本地 ")}{root?.name} · {left} {tr(" ↔ 远程 ")}{right}</Text>
   </Stack>
  )}
  <Stack style={{height:intent?`min(calc(var(--size-element-md) * ${Math.max(1,Math.min(intent.items.length,5))} * 2 + var(--spacing-8)), 35dvh)`:'min(calc(var(--spacing-10) * 7), 35dvh)',minHeight:'calc(var(--spacing-10) * 2.5)',minWidth:0,borderRadius:'var(--radius-element)',border:'var(--border-width) solid var(--color-border)',background:'var(--color-background-muted)',overflow:'hidden'}}>{intent?<WindowedList items={intent.items} itemKey={item=>item.path} estimateSize={()=>48} label={tr("待复制清单")} row={item=>({label:item.path,description:(item.action==='overwrite'?tr("覆盖已有文件"):tr("创建{p0}", (item.directory?tr("目录"):tr("文件"))))+(item.directory?'':' · '+fileSize(item.size))})}/>:rows.length?<WindowedList items={rows} itemKey={entry=>entry.relative} row={row} estimateSize={()=>64} label={tr("文件比较结果")} resetKey={result?.id}/>:<Stack height="100%" align="center" justify="center" padding={4}><Text type="supporting" color="secondary">{busy?tr("正在扫描指定范围…"):tr("暂无比较结果。相同大小和时间仍会核对内容摘要。")}</Text></Stack>}</Stack>
  {error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}<Text type="supporting" role="status" color="secondary">{status}</Text>
 </EditorDialog>
}
