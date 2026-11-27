import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useId,useRef,useState} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Selector} from '@astryxdesign/core/Selector'
import {TextInput} from '@astryxdesign/core/TextInput'
import EditorDialog from '../../shared/ui/EditorDialog'
import WindowedList from '../../shared/ui/WindowedList'
import {fileGateway,type FileLocation,type FileConnection} from '../../shared/bridge/files'
export type RemoteCopySource={source:FileLocation;names:string[]}
export default function RemoteCopyDialog({isOpen,sources,onClose,onQueued,gateway=fileGateway,sessionLabel}:{isOpen:boolean;sources:RemoteCopySource[];onClose:()=>void;onQueued:()=>void;gateway?:typeof fileGateway;sessionLabel?:(id:string)=>string}){
  useLocale()

 const [connections,setConnections]=useState<FileConnection[]>([]),[target,setTarget]=useState(''),[path,setPath]=useState('/'),[busy,setBusy]=useState(false),[loading,setLoading]=useState(false),[error,setError]=useState(''),[queued,setQueued]=useState(0),[attempted,setAttempted]=useState(false)
 const lock=useRef(false),seq=useRef(0),errorId=useId(),origin=sources[0]?.source.sessionId
 useEffect(()=>{if(!isOpen)return;setError('');setQueued(0);setAttempted(false);setLoading(true);const id=++seq.current;void gateway.fileConnections().then(next=>{if(id!==seq.current)return;const allowed=next.filter(item=>item.id!==origin);setConnections(allowed);setTarget(allowed[0]?.id??'')},cause=>{if(id===seq.current)setError(cause instanceof Error?cause.message:tr("连接列表未读取，请重新打开。"))}).finally(()=>{if(id===seq.current)setLoading(false)});return()=>{seq.current++}},[isOpen,origin,gateway])
 useEffect(()=>{if(error)requestAnimationFrame(()=>document.getElementById(errorId)?.focus())},[error,errorId])
 const rows=sources.flatMap(group=>group.names.map(name=>({key:group.source.path+'/'+name,name,parent:group.source.path})))
 async function submit(){if(lock.current||loading||attempted)return;const connection=connections.find(item=>item.id===target);if(!connection){setError(tr("请先连接目标服务器，再重新打开这里。"));return};if(!path.startsWith('/')||path.includes('\0')){setError(tr("请输入目标服务器的绝对目录路径。"));return};lock.current=true;setBusy(true);setError('');setAttempted(true);let count=0
  try{for(const group of sources){await gateway.transfer({...group,destination:{side:'remote',sessionId:connection.id,attempt:connection.attempt,token:'',path},policy:'ask'});count++;setQueued(count)};onClose();onQueued()}catch(cause){setError(tr("{p0}{p1} 请先查看任务，避免重复提交。", (count?tr("已确认加入 {p0} 组。", count):''), (cause instanceof Error?cause.message:tr("传输请求未确认。"))))}finally{lock.current=false;setBusy(false)}
 }
 return <EditorDialog isOpen={isOpen} width="min(calc(var(--spacing-10) * 14), 92vw)" title={tr("复制到另一台服务器")} action="add" closeLabel={tr("关闭远程复制")} onClose={onClose} busy={busy} subtitle={tr("通过本机中转，使用两端现有的 SSH 连接；冲突时在任务页确认。")} hint={tr("只复制所选内容，不删除来源。")} formProps={{onSubmit:event=>{event.preventDefault();void submit()}}} actions={attempted&&!busy?<Button label={tr("查看任务")} type="button" variant="primary" size="md" onClick={event=>{event.preventDefault();onClose();onQueued()}}/>:<Button label={tr("加入传输队列")} type="submit" variant="primary" size="md" isDisabled={loading||!connections.length||!rows.length||busy} isLoading={busy}/> }>
  <Selector label={tr("目标服务器会话")} size="md" value={target} onChange={setTarget} options={connections.map(connection=>({value:connection.id,label:sessionLabel?.(connection.id)||connection.name||tr("SSH 会话")}))} placeholder={loading?tr("正在读取已连接会话…"):tr("先连接另一台服务器")} isDisabled={loading||busy||attempted}/>
  <TextInput label={tr("目标目录")} size="md" value={path} onChange={setPath} autoComplete="off" isDisabled={busy||attempted}/>
  <Stack gap={2} style={{minWidth:0}}><Text type="label">{tr("所选 ")}{rows.length} {tr(" 项")}</Text><Stack style={{height:'min(calc(var(--size-element-md) * '+Math.min(Math.max(rows.length,1),4)+' * 1.5), 28dvh)',minWidth:0,borderRadius:'var(--radius-element)',border:'var(--border-width) solid var(--color-border)',background:'var(--color-background-muted)',overflow:'hidden'}}><WindowedList label={tr("待远程复制内容")} items={rows} itemKey={item=>item.key} estimateSize={()=>48} row={item=>({label:item.name,description:item.parent})}/></Stack></Stack>
  {!loading&&!connections.length&&<Text type="supporting" role="status">{tr("先连接目标服务器，再回来选择。各端会独立验证服务器身份。")}</Text>}
  {queued>0&&<Text type="supporting" role="status">{tr("已加入 ")}{queued} {tr(" 组，任务页可查看进度和取消。")}</Text>}{error&&<Text id={errorId} tabIndex={-1} role="alert" type="supporting">{localizeMessage(error)}</Text>}
 </EditorDialog>
}
