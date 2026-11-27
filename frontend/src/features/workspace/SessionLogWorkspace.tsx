import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {lazy,Suspense,useEffect,useRef,useState,type ComponentProps} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text,Heading} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {Selector} from '@astryxdesign/core/Selector'
import {DateTimeInput} from '@astryxdesign/core/DateTimeInput'
import {logGateway,type SessionLog,type LogPage} from '../../shared/bridge/sessionLogs'
const CodeEditor=lazy(()=>import('./RemoteCodeEditor'))
const empty:LogPage={lines:[],next:0,more:false}
const stamp=(value:number)=>new Date(value).toLocaleString(getLanguage())
export default function SessionLogWorkspace({sessionId,attempt,connected,active,current,onClose,gateway=logGateway}:{
 sessionId:string;attempt:number;connected:boolean;active:boolean;current?:SessionLog|null;onClose:()=>void;gateway?:typeof logGateway
}){
  useLocale()

 const [logs,setLogs]=useState<SessionLog[]>([]),[selected,setSelected]=useState(''),[query,setQuery]=useState(''),[after,setAfter]=useState<ComponentProps<typeof DateTimeInput>['value']>()
 const [page,setPage]=useState<LogPage>(empty),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState(''),[deleting,setDeleting]=useState(false)
 const working=useRef(false),sequence=useRef(0),mounted=useRef(true),selection=useRef(selected),applied=useRef({text:'',after:0});selection.current=selected
 const currentLog=logs.find(log=>log.sessionId===sessionId&&log.attempt===attempt&&log.state==='recording')
 const chosen=logs.find(log=>log.id===selected)
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;sequence.current++}},[])
 async function action(run:()=>Promise<void>){if(working.current)return;working.current=true;setBusy(true);setError('');setNotice('');try{await run()}catch(cause){if(mounted.current)setError(cause instanceof Error?cause.message:tr("日志操作未完成，请重试。"))}finally{working.current=false;if(mounted.current)setBusy(false)}}
 async function reload(preferred=selection.current){const next=await gateway.list();if(!mounted.current)return;setLogs(next);setSelected(next.some(log=>log.id===preferred)?preferred:next.find(log=>log.sessionId===sessionId&&log.attempt===attempt)?.id??next[0]?.id??'')}
 const reloadRef=useRef(reload);reloadRef.current=reload
 useEffect(()=>{if(active&&gateway.available())void action(()=>reloadRef.current())},[active,current?.id,current?.state,attempt,connected])
 async function read(cursor=0){if(!selected)return;if(cursor===0){const when=after?new Date(after).getTime():0;if(!Number.isFinite(when))throw Error(tr("请选择有效的开始时间。"));applied.current={text:query,after:when}};const filter=applied.current;const token=++sequence.current;const result=await gateway.read(selected,filter.text,filter.after,cursor);if(mounted.current&&token===sequence.current)setPage(result)}
 const readRef=useRef(read);readRef.current=read
 useEffect(()=>{sequence.current++;setPage(empty);setDeleting(false);setError('');if(selected)void readRef.current().catch(cause=>{if(mounted.current)setError(cause instanceof Error?cause.message:tr("日志读取未完成，请重试。"))})},[selected])
 const text=page.lines.map(line=>`[${stamp(line.time)}] ${line.text}`).join('')
 return <Stack role="region" aria-label={tr("会话日志")} hidden={!active} inert={!active} style={{height:'100%',minHeight:0,minWidth:0,overflow:'auto',display:active?'flex':'none'}}>
  <Stack padding={3} gap={3} style={{flexShrink:0,borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
   <Stack direction="horizontal" align="center" gap={2} wrap="wrap">
    <StackItem size="fill"><Heading level={4} accessibilityLevel={2}>{tr("会话日志")}</Heading></StackItem>
    <Button label={tr("返回终端")} variant="ghost" size="md" onClick={onClose}/>
    <Button label={currentLog?tr("停止记录本窗格"):tr("开始记录本窗格")} variant={currentLog?'secondary':'primary'} size="md" isDisabled={!gateway.available()||!currentLog&&!connected} isLoading={busy} onClick={()=>{void action(async()=>{const info=currentLog?await gateway.stop(sessionId,attempt):await gateway.start(sessionId,attempt);await reload(info.id);if(selection.current===info.id)await read();setNotice(info.state==='recording'?tr("已开始记录本窗格输出"):tr("本窗格录制已结束"))})}}/>
   </Stack>
   <Text type="supporting" color="secondary">{tr("从开启时记录服务器输出；服务器回显也会保留。每份最多 20 MiB，总量 200 MiB，满后停止记录。")}</Text>
   <Stack direction="horizontal" align="end" gap={2} wrap="wrap">
    <StackItem size="fill" style={{minWidth:0}}><Selector label={tr("选择记录")} value={selected} placeholder={tr("尚无录制记录")} options={logs.map(log=>({value:log.id,label:`${log.name||tr("会话")} · ${stamp(log.started)}${log.state==='recording'?tr(" · 录制中"):''}`}))} onChange={value=>setSelected(value)} isDisabled={busy||!logs.length} size="md" width="100%"/></StackItem>
    <Button label={tr("刷新")} variant="secondary" size="md" isDisabled={!gateway.available()||busy} onClick={()=>{void action(async()=>{await reload();await read()})}}/>
    <Button label={tr("导出 TXT")} variant="secondary" size="md" isDisabled={!chosen||chosen.state==='recording'||busy} onClick={()=>{if(chosen)void action(async()=>{if(await gateway.export(chosen.id))setNotice(tr("日志已导出"))})}}/>
    <Button label={tr("删除记录")} variant="ghost" size="md" isDisabled={!chosen||chosen.state==='recording'||busy} onClick={()=>setDeleting(true)}/>
   </Stack>
   <form aria-label={tr("搜索会话日志")} autoComplete="off" onSubmit={event=>{event.preventDefault();void action(()=>read())}}>
    <Stack direction="horizontal" align="end" gap={2} wrap="wrap">
     <StackItem size="fill" style={{minWidth:0}}><TextInput label={tr("搜索内容")} value={query} onChange={setQuery} autoComplete="off" hasSpellCheck={false} size="md"/></StackItem>
     <DateTimeInput label={tr("开始时间")} timeLabel={tr("开始时间（时分）")} value={after} onChange={setAfter} hasClear size="md" width="calc(var(--spacing-10) * 11)"/>
     <Button label={tr("查找")} type="submit" size="md" variant="secondary" isDisabled={!selected||busy}/>
    </Stack>
   </form>
   {chosen&&<Text type="supporting" role="status">{chosen.state==='recording'?tr("正在记录，刷新查看最新内容"):chosen.state==='limited'?tr("记录已停止，内容可能不完整"):tr("记录已结束")} · {Math.ceil(chosen.bytes/1024)} KiB{chosen.message?' · '+chosen.message:''}</Text>}
   {deleting&&<Stack gap={2}><Text type="supporting" role="alert">{tr("删除这份日志后无法恢复，服务器文件不受影响。")}</Text><Stack direction="horizontal" gap={2}><Button label={tr("保留记录")} variant="ghost" onClick={()=>setDeleting(false)}/><Button label={tr("确认删除日志")} variant="destructive" isDisabled={busy} onClick={()=>{if(chosen)void action(async()=>{await gateway.delete(chosen.id);setDeleting(false);await reload('');setNotice(tr("日志已删除"))})}}/></Stack></Stack>}
  </Stack>
  <StackItem size="fill" style={{minHeight:'calc(var(--spacing-10) * 3)',minWidth:0,overflow:'hidden'}}><Stack padding={3} style={{height:'100%',minHeight:0}}>
   {selected?<Suspense fallback={<Text type="supporting">{tr("正在打开日志…")}</Text>}><CodeEditor path="session.txt" label={tr("日志内容（只读）")} value={text} readOnly active={active}/></Suspense>:<Text type="supporting">{gateway.available()?tr("尚无日志，连接后可开始记录本窗格。"):tr("请在桌面程序中使用会话日志。")}</Text>}
  </Stack></StackItem>
  <Stack padding={3} gap={2} style={{flexShrink:0,borderBlockStart:'var(--border-width) solid var(--color-border)'}}>
   {error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}<Text type="supporting" role="status">{localizeMessage(notice)}</Text>
   <Stack direction="horizontal" align="center" justify="between" gap={2} wrap="wrap"><Text type="supporting" color="secondary">{tr("本页 ")}{page.lines.length} {tr(" 条 · 时间为本机接收时间")}</Text><Button label={tr("下一页")} variant="secondary" size="md" isDisabled={!page.more||busy} onClick={()=>{void action(()=>read(page.next))}}/></Stack>
  </Stack>
 </Stack>
}
