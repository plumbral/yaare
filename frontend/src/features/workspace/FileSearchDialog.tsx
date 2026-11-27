import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useCallback,useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput'
import {Icon} from '@astryxdesign/core/Icon'
import {MagnifyingGlassIcon,FolderIcon,DocumentTextIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import WindowedList from '../../shared/ui/WindowedList'
import {fileGateway,type FileLocation,type SearchHit} from '../../shared/bridge/files'

export default function FileSearchDialog({isOpen,location,hidden,onClose,onReveal,gateway=fileGateway}:{isOpen:boolean;location:FileLocation;hidden:boolean;onClose:()=>void;onReveal:(hit:SearchHit)=>Promise<void>;gateway?:typeof fileGateway}){
  useLocale()

 const [query,setQuery]=useState(''),[range,setRange]=useState(location.path),[hits,setHits]=useState<SearchHit[]>([]),[busy,setBusy]=useState(false),[opening,setOpening]=useState(false),[error,setError]=useState(''),[status,setStatus]=useState(tr("输入文件名关键词，然后开始搜索。"))
 const [recursive,setRecursive]=useState(true),[includeHidden,setIncludeHidden]=useState(hidden)
 const cancel=useRef<(()=>void)|null>(null),generation=useRef(0),mounted=useRef(true),working=useRef(false)
 const stop=()=>{generation.current++;cancel.current?.();cancel.current=null;working.current=false;setBusy(false)}
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;generation.current++;cancel.current?.()}},[])
 useEffect(()=>{if(!isOpen){stop();return};setRange(location.path);setHits([]);setError('');setStatus(tr("仅搜索指定目录内的文件名，不读取文件内容。"))},[isOpen,location])
 function search(){if(working.current){stop();setStatus(tr("搜索已停止，已找到的结果保留。"));return};if(!query.trim()||Array.from(query.trim()).length>128||/[\\/\x00]/.test(query)){setError(tr("请输入 1–128 字的文件名关键词，不包含路径分隔符。"));return};if(!range.startsWith('/')){setError(tr("请输入以 / 开头的搜索目录。"));return}
  const version=++generation.current;working.current=true;setBusy(true);setHits([]);setError('');setStatus(tr("正在搜索…"))
  cancel.current=gateway.search({...location,path:range},query.trim(),includeHidden,recursive,event=>{if(version!==generation.current||!mounted.current)return;setHits(previous=>[...previous,...event.hits].slice(0,1000));const reason=event.reason==='result-limit'?tr("结果达到上限，请缩小目录范围。"):event.reason==='scan-limit'?tr("扫描达到 50,000 项，请缩小目录范围。"):event.reason?tr("部分目录因层级或队列上限未扫描。"):'';setStatus(tr("{p0} · 已检查 {p1} 项{p2}{p3}", (event.done?tr("搜索结束"):tr("正在搜索")), event.scanned, (event.skipped?tr(" · 跳过 {p0} 项", event.skipped):''), (reason?' · '+reason:'')))},cause=>{if(version!==generation.current||!mounted.current)return;setError(cause instanceof Error?cause.message:tr("搜索未完成，已找到的结果保留。"))},()=>{if(version===generation.current&&mounted.current){working.current=false;setBusy(false);cancel.current=null}})
 }
 const row=useCallback((hit:SearchHit)=>({label:hit.name,description:hit.path,startContent:<Icon icon={hit.directory?FolderIcon:DocumentTextIcon} size="sm"/>,isDisabled:busy||opening,onClick:()=>{if(busy||opening)return;setOpening(true);setError('');void onReveal(hit).then(onClose).catch(cause=>setError(cause instanceof Error?cause.message:tr("未能定位结果，请重试。"))).finally(()=>{if(mounted.current)setOpening(false)})}}),[busy,opening,onReveal,onClose])
 return <EditorDialog isOpen={isOpen} title={location.side==='remote'?tr("搜索远程文件名"):tr("搜索本地文件名")} action="edit" startContent={<Icon icon={MagnifyingGlassIcon} size="md"/>} closeLabel={tr("关闭文件搜索")} cancelLabel={tr("关闭")} busy={opening} onClose={onClose} hint={tr("仅在本次指定目录内搜索；最多 30 秒、1,000 个结果。")} formProps={{'aria-label':tr("递归文件搜索"),onSubmit:e=>{e.preventDefault();search()}}} actions={<Button label={busy?tr("停止搜索"):tr("开始搜索")} variant={busy?'secondary':'primary'} size="md" type="submit" isDisabled={opening||!gateway.available()}/> }>
  <Stack direction="horizontal" gap={3} wrap="wrap">
   <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}>
    <TextInput label={tr("搜索范围")} value={range} onChange={setRange} size="md" autoComplete="off" isDisabled={busy||opening}/>
   </StackItem>
   <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)',minWidth:0}}>
    <TextInput label={tr("文件名包含")} value={query} onChange={setQuery} size="md" autoComplete="off" isDisabled={busy||opening} hasClear hasAutoFocus/>
   </StackItem>
  </Stack>
  <Stack direction="horizontal" gap={4} wrap="wrap"><CheckboxInput label={tr("搜索子目录")} value={recursive} onChange={setRecursive} isDisabled={busy||opening}/><CheckboxInput label={tr("包含隐藏项")} value={includeHidden} onChange={setIncludeHidden} isDisabled={busy||opening}/></Stack>
  <Text type="supporting" color="secondary">{tr("不跟随链接目录。权限不足的子目录会跳过，并统计在结果中。")}</Text>
  <Stack style={{height:'min(calc(var(--spacing-10) * 7), 35dvh)',minHeight:'calc(var(--spacing-10) * 2.5)',minWidth:0,borderRadius:'var(--radius-element)',border:'var(--border-width) solid var(--color-border)',background:'var(--color-background-muted)',overflow:'hidden'}}>{hits.length?<WindowedList items={hits} itemKey={hit=>hit.path} row={row} label={tr("搜索结果")} resetKey={range+query} estimateSize={()=>64}/>:<Stack height="100%" align="center" justify="center" padding={4}><Text type="supporting" color="secondary">{busy?tr("正在寻找匹配项…"):tr("暂无搜索结果。")}</Text></Stack>}</Stack>
  {error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}<Text type="supporting" color="secondary" role="status">{status}{hits.length?tr(" · {p0} 个匹配", hits.length):''}</Text>
 </EditorDialog>
}
