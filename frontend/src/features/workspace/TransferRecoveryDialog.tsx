import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useId,useRef,useState} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Selector} from '@astryxdesign/core/Selector'
import {DropdownMenu} from '@astryxdesign/core/DropdownMenu'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {EllipsisHorizontalIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import {fileGateway,type TransferRecoveryView,type LocalFolder} from '../../shared/bridge/files'
import {transferSize} from './transferModel'
import {isMyGo,onFileDrop} from 'mygo-runtime'

export default function TransferRecoveryDialog({isOpen,onClose,onAfterClose,gateway=fileGateway,onChooseLocal}:{isOpen:boolean;onClose:()=>void;onAfterClose?:()=>void;gateway?:typeof fileGateway;onChooseLocal:()=>Promise<LocalFolder|null>}){
  useLocale()

 const [view,setView]=useState<TransferRecoveryView|null>(null),[selected,setSelected]=useState(''),[source,setSource]=useState(''),[destination,setDestination]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(false)
 const sequence=useRef(0),lock=useRef(false)
 const errorId=useId()
 const dropped=useRef<{id:string;x:number;y:number;at:number}|null>(null),[dropMessage,setDropMessage]=useState('')
 useEffect(()=>{if(!isOpen||!isMyGo())return;return onFileDrop(event=>{const target=dropped.current;dropped.current=null;if(!target||Date.now()-target.at>5000||Math.abs(target.x-event.x)>2||Math.abs(target.y-event.y)>2||!event.paths[0]?.startsWith('yaare-drop:'))return;setBusy(true);setError('');void gateway.authorizeRecoveredDrop(target.id,event.paths[0]).then(()=>setDropMessage(tr("原文件已重新授权，可以校验并继续。")),cause=>setError(cause instanceof Error?cause.message:tr("原文件未能重新授权，请重试。"))).finally(()=>setBusy(false))})},[isOpen,gateway])
 useEffect(()=>{if(error)requestAnimationFrame(()=>document.getElementById(errorId)?.focus())},[error,errorId])
 const choose=(id:string,next=view)=>{dropped.current=null;setDropMessage('');setSelected(id);const item=next?.items.find(item=>item.id===id);setSource(item?.sourceConnections[0]??'');setDestination(item?.destinationConnections[0]??'');setError('')}
 async function reload(){const request=++sequence.current;setBusy(true);setError('');try{const next=await gateway.recoverableTransfers();if(request!==sequence.current)return;setView(next);choose(next.items.some(item=>item.id===selected)?selected:next.items[0]?.id??'',next)}catch(cause){if(request===sequence.current)setError(cause instanceof Error?cause.message:tr("中断任务未能读取，请重试。"))}finally{if(request===sequence.current)setBusy(false)}}
 useEffect(()=>{if(isOpen)void reload();return()=>{sequence.current++;dropped.current=null}},[isOpen,gateway])
 const item=view?.items.find(item=>item.id===selected)
 const sourceRemote=item?.direction.startsWith('remote'),destinationRemote=item?.direction.endsWith('remote')
 async function restore(){if(lock.current||busy||!item)return;if(sourceRemote&&!source||destinationRemote&&!destination){setError(tr("请先连接原服务器，再刷新并选择对应的 SSH 会话。"));return};lock.current=true;setBusy(true);setError('');try{await gateway.restoreTransfer({id:item.id,sourceSessionId:source,destinationSessionId:destination});onClose()}catch(cause){setError(cause instanceof Error?cause.message:tr("恢复未确认，请刷新任务状态后重试。"))}finally{lock.current=false;setBusy(false)}}
 async function discard(){if(!item||busy)return;setBusy(true);setError('');try{await gateway.discardRecoveredTransfer(item.id);await reload()}catch(cause){setError(cause instanceof Error?cause.message:tr("恢复记录未移除，请重试。"))}finally{setBusy(false)}}
 async function chooseSources(){if(!item||busy)return;setBusy(true);setError('');setDropMessage('');try{if(await gateway.chooseRecoveredSources(item.id))setDropMessage(tr("原来源已重新授权，可以校验并继续。"))}catch(cause){setError(cause instanceof Error?cause.message:tr("原来源未能重新授权，请重试。"))}finally{setBusy(false)}}
 return <EditorDialog isOpen={isOpen} onClose={onClose} onAfterClose={onAfterClose} busy={busy} title={tr("恢复中断任务")} closeLabel={tr("关闭任务恢复")} subtitle={tr("重新选择原本地目录并连接原服务器；继续前校验已传内容。")} saveHint={tr('校验并继续')} action="edit"
  formProps={{onSubmit:event=>{event.preventDefault();void restore()}}} headerActions={item?<DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} label={tr("恢复记录操作")} size="md" variant="ghost" isDisabled={busy} icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={[{label:tr("移除恢复记录"),variant:'destructive',isDisabled:busy,onClick:()=>void discard()}]}/>:undefined} actions={<Button label={tr("校验并继续")} type="submit" size="md" variant="primary" isDisabled={busy||!item} isLoading={busy}/> }>
  <Stack direction="horizontal" gap={2} wrap="wrap"><Button label={tr("刷新恢复记录")} type="button" variant="ghost" size="md" isDisabled={busy} onClick={()=>void reload()}/>{item?.direction.includes('local')&&!item.dropSource&&<Button label={tr("选择原本地目录")} type="button" variant="secondary" size="md" isDisabled={busy} onClick={async()=>{setBusy(true);setError('');try{await onChooseLocal()}catch(cause){setError(cause instanceof Error?cause.message:tr("本地目录未选择。"))}finally{setBusy(false)}}}/>}</Stack>
  {view?.items.length?<Selector label={tr("中断任务")} size="md" value={selected} hasSearch options={view.items.map(item=>({value:item.id,label:item.name,description:item.source+' → '+item.destination}))} onChange={choose} isDisabled={busy}/>:<Text type="supporting" role="status">{busy?tr("正在读取中断任务…"):tr("没有可恢复的中断任务。")}</Text>}
  {item&&<Stack gap={3}>
   <Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{item.source} → {item.destination} {tr(" · 已传 ")}{transferSize(item.done)} / {transferSize(item.total)}</Text>
   {item.protocol==='scp'&&<Text type="supporting" color="secondary">{tr("SCP 会清理原任务未提交的临时文件，从当前文件头重传；已完成的文件不重放。")}</Text>}
   {item.dropSource&&<Stack padding={3} gap={2} role="group" aria-label={tr("重新授权拖入来源")} style={{border:'var(--border-width) dashed var(--color-border-emphasized)',borderRadius:'var(--radius-element)'}} onDragOver={event=>{if(!busy&&event.dataTransfer.types.includes('Files')){event.preventDefault();event.dataTransfer.dropEffect='copy'}}} onDropCapture={event=>{if(busy||!event.dataTransfer.types.includes('Files'))return;event.preventDefault();event.stopPropagation();setDropMessage('');dropped.current={id:item.id,x:event.clientX,y:event.clientY,at:Date.now()}}}><Text type="supporting" role="status">{dropMessage||tr("选择原来的全部文件和文件夹，或重新拖到这里；随后校验并继续。")}</Text><Stack direction="horizontal"><Button label={tr("选择原来源…")} type="button" variant="secondary" size="md" isDisabled={busy} onClick={()=>void chooseSources()}/></Stack></Stack>}
   {sourceRemote&&<Selector label={tr("源服务器会话")} value={source} size="md" options={view!.connections.filter(connection=>item.sourceConnections.includes(connection.id)).map(connection=>({value:connection.id,label:connection.name}))} placeholder={tr("连接原服务器后刷新")} onChange={setSource} isDisabled={busy}/>}
   {destinationRemote&&<Selector label={tr("目标服务器会话")} value={destination} size="md" options={view!.connections.filter(connection=>item.destinationConnections.includes(connection.id)).map(connection=>({value:connection.id,label:connection.name}))} placeholder={tr("连接原服务器后刷新")} onChange={setDestination} isDisabled={busy}/>}
   <Text type="supporting" color="secondary">{tr("不会自动重传；已有目标变化时停止并提示。移除记录保留原文件及未提交的临时文件。")}</Text>
  </Stack>}
  {view?.message&&<Text type="supporting" role="status">{localizeMessage(view.message)}</Text>}{error&&<Text id={errorId} tabIndex={-1} role="alert" type="supporting">{localizeMessage(error)}</Text>}
 </EditorDialog>
}
