import {saveBrowserDownload} from '../../shared/bridge/browserFiles'
import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { lazy,Suspense,useEffect, useId,useRef, useState } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { ProgressBar } from '@astryxdesign/core/ProgressBar'
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu'
import { ArrowUpIcon, ArrowDownIcon, ArrowsRightLeftIcon, ChevronRightIcon, ChevronDownIcon, ExclamationCircleIcon, EllipsisHorizontalIcon } from '@heroicons/react/24/outline'
import { fileGateway, type FileTask,type LocalFolder } from '../../shared/bridge/files'
import { canPause,canContinue,canRetry,remainingTime,transferSize,transferLabels,phaseLabels } from './transferModel'

import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'
import './workspaceTasks.css'

import { DialogPresence } from '../../shared/ui/AppDialog'
const TransferRecoveryDialog=lazy(()=>import('./TransferRecoveryDialog'))
export default function TransferTasks({tasks,gateway=fileGateway,onError,onChooseLocal,session,onCollapse}:{session?:{id:string;host:{name:string}};onCollapse?:()=>void;tasks:FileTask[];gateway?:typeof fileGateway;onError:(message:string)=>void;onChooseLocal?:()=>Promise<LocalFolder|null>}){
  useLocale()

  const id=useId(),batchId=id+'-batch',[recoveryOpen,setRecoveryOpen]=useState(false)
  const [expanded,setExpanded]=useState('')
  const guard=useRef(false),[busy,setBusy]=useState(false),[announcement,setAnnouncement]=useState('')
  const previous=useRef<Map<string,string>|null>(null)
  useEffect(()=>{const changes=tasks.filter(t=>['completed','failed'].includes(t.state)&&previous.current&&previous.current.get(t.id)!==t.state);previous.current=new Map(tasks.map(t=>[t.id,t.state]));if(changes.length)setAnnouncement(changes.slice(0,2).map(t=>t.name+' '+transferLabels[t.state]).join('，'))},[tasks])
  async function act(work:()=>Promise<unknown>,message:string){if(guard.current)return;const focused=document.activeElement;guard.current=true;setBusy(true);try{await work();setAnnouncement(message)}catch(cause){onError(cause instanceof Error?cause.message:tr("任务操作未完成，请重试。"))}finally{guard.current=false;setBusy(false);requestAnimationFrame(()=>{if(focused&&!focused.isConnected&&document.activeElement===document.body)document.getElementById(batchId)?.focus({preventScroll:true})})}}
  const control=(task:FileTask,action:string)=>void act(()=>gateway.control(task.id,action),tr("任务操作已提交。"))
  const batch=(action:string,filter:(t:FileTask)=>boolean)=>void act(()=>gateway.controlMany(tasks.filter(filter).map(t=>t.id),action),tr("批量操作已提交。"))
  const canRecover = !!onChooseLocal && gateway.canRecoverTransfers()
  const running=tasks.filter(canPause).length,paused=tasks.filter(canContinue).length
  const endpoint = (side: string, label?: string, sessionId?: string) =>
    side === 'local' ? tr("本地") : label || (sessionId && sessionId === session?.id ? session.host.name : tr("远程"))
  const resolve = (task: FileTask, action: string) => {
    const { conflictId } = task
    if (conflictId) void act(() => gateway.resolveConflict(task.id, conflictId, action), tr("任务操作已提交。"))
  }
  return <Stack id={id+'-list'} data-transfer-task-list width="100%">
    <Stack direction="horizontal" align="center" justify="between" gap={1} paddingBlock={1}>
      <Text type="label">{tr("传输任务")} · {tasks.length}</Text>
      <Stack direction="horizontal" gap={1} align="center">
      <DropdownMenu alignment="end" presentation="adaptive" renderTrigger={props=><IconButton {...props} id={batchId} label={tr("批量操作")} tooltip={tr("批量操作")} size="md" variant="ghost" isDisabled={busy} icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={[
        ...(canRecover?[{label:tr("恢复中断任务…"),onClick:()=>setRecoveryOpen(true)}]:[]),
        {label:tr("全部暂停"),isDisabled:!running,onClick:()=>batch('pause',canPause)},
        {label:tr("全部继续"),isDisabled:!paused,onClick:()=>batch('resume',canContinue)},
        {label:tr("重试失败任务"),isDisabled:!tasks.some(t=>t.state==='failed'),onClick:()=>batch('retry',t=>t.state==='failed')},
        {type:'divider'},
        {label:tr("清理已完成"),isDisabled:!tasks.some(t=>t.state==='completed'),onClick:()=>void act(()=>gateway.clearCompleted(),tr("已清理完成的任务。"))},
      ]}/>
      {onCollapse&&<IconButton label={tr("收起")} variant="ghost" size="sm" icon={<Icon icon={ChevronDownIcon}/>} onClick={onCollapse}/>}
      </Stack>
    </Stack>
    {!tasks.length&&<Text type="supporting" color="secondary">{tr("暂无传输任务。选择文件后可上传或下载。")}</Text>}
    {!!tasks.length&&<Stack data-task-columns="" data-task-head="" aria-hidden="true">
      {[tr("名称"),tr("方向"),tr("目标"),tr("进度"),tr("状态"),tr("操作")].map((title,i)=><Text key={i} type="supporting" color="secondary">{title}</Text>)}
    </Stack>}
    {tasks.map(task=>{
      const active=task.state==='running', open=expanded===task.id
      const [from,to]=task.direction.split('-')
      const source = endpoint(from, task.sourceLabel, task.sourceSession)
      const destination = endpoint(to, task.destinationLabel, task.destinationSession)
      const route = source + ' → ' + destination
      const label=active?phaseLabels[task.phase]||tr("传输中"):transferLabels[task.state]||task.state
      const progress=active&&task.total>0&&task.phase==='transferring'
      const action=canPause(task)?'pause':canRetry(task)?'retry':canContinue(task)?'resume':undefined
      const toggle=()=>setExpanded(open?'':task.id)
      return <Stack key={task.id} style={{borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
        <Stack data-task-columns="">
          <Stack direction="horizontal" align="center" gap={2}><Icon size="sm" icon={task.direction==='local-remote'?ArrowUpIcon:task.direction==='remote-local'?ArrowDownIcon:ArrowsRightLeftIcon}/><Text type="label" maxLines={1}>{task.name}</Text></Stack>
          <Text type="supporting" maxLines={1}>{route}</Text>
          <Text type="supporting" maxLines={1}>{task.state === 'conflict' ? task.conflictTarget || task.destinationPath : task.destinationPath || '—'}</Text>
          <Stack direction="horizontal" align="center" gap={2}>{progress&&<>
            <Stack style={{flex:1,minWidth:0}}><ProgressBar label={tr("传输 {p0}",task.name)} isLabelHidden value={task.done} max={task.total} variant="neutral"/></Stack>
            <Text type="supporting">{Math.min(100,Math.floor(task.done*100/task.total))}%</Text></>}</Stack>
          <Stack direction="horizontal" align="center" gap={1} style={{color:task.state==='failed'?'var(--color-warning)':undefined}}>
            {task.state==='failed'&&<Icon icon={ExclamationCircleIcon} size="sm" color="inherit"/>}<Text type="supporting" maxLines={1} style={{color:'inherit'}}>{label}</Text>
          </Stack>
          <Stack direction="horizontal" align="center" justify="end" gap={1}>
            {import.meta.env.MODE==='browser'&&task.state==='completed'&&task.direction==='remote-local'&&(task.files>0||task.skipped===0)&&<Button label={tr("保存到设备")} size="md" variant="secondary" isDisabled={busy} onClick={()=>void act(()=>saveBrowserDownload(task.id),tr("已交给浏览器保存；可在浏览器下载记录中查看。"))}/>}
            {action&&<Button label={action==='pause'?tr("暂停"):action==='resume'?tr("继续"):tr("重试")} size="sm" variant={action==='retry'?'secondary':'ghost'} isDisabled={busy} onClick={()=>control(task,action)}/>}
            <IconButton label={tr("任务详情 · {p0}",task.name)} aria-expanded={open} aria-controls={open?id+task.id:undefined} size="sm" variant="ghost" icon={<Icon icon={open?ChevronDownIcon:ChevronRightIcon} size="sm"/>} onClick={toggle}/>
          </Stack>
        </Stack>
        {open&&<Stack id={id+task.id} padding={2} gap={1} style={{background:'var(--color-background-surface)'}}>
          {task.state === 'conflict' ? <Stack gap={1} style={{overflowWrap:'anywhere'}}>
            <Text type="supporting">{tr('源文件：{p0} · {p1}', source, task.conflictSource || task.current || task.name)}</Text>
            <Text type="label">{tr('目标位置：{p0} · {p1}', destination, task.conflictTarget || task.destinationPath || '—')}</Text>
          </Stack> : <Text type="supporting">{route} · {task.destinationPath||'—'}</Text>}
      <Stack direction="horizontal" gap={1} justify="between" wrap="wrap"><Text type="supporting" color="secondary">{transferSize(task.done)} / {transferSize(task.total)}{task.skipped?tr(" · 跳过 {p0} 项", task.skipped):''}</Text>{active&&task.speed>0&&<Text type="supporting" color="secondary">{transferSize(task.speed)}/s · {remainingTime(task.etaSeconds)}</Text>}</Stack>
      {task.current&&task.current!==task.name&&!['completed','cancelled'].includes(task.state)&&<Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{task.current}</Text>}
      {task.message&&['failed','conflict','cancelled'].includes(task.state)&&<Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{localizeMessage(task.message)}</Text>}
      {task.protocol==='scp'&&<Text type="supporting" color="secondary">{tr("SCP 数据传输；中断后从当前文件头重传，已完成的文件不重放。")}</Text>}
      {task.recoveryMessage&&<Text type="supporting" color="secondary">{task.recoveryMessage}</Text>}
      {task.state==='conflict'&&<Stack direction="horizontal" gap={1} wrap="wrap"><Button label={task.conflictDirectory?tr("合并目录"):tr("覆盖")} size="md" variant="secondary" isDisabled={busy || !task.conflictId} onClick={()=>resolve(task,'overwrite')}/><Button label={tr("跳过")} size="md" variant="ghost" isDisabled={busy || !task.conflictId} onClick={()=>resolve(task,'skip')}/><Button label={tr("保留两者")} size="md" variant="secondary" isDisabled={busy || !task.conflictId} onClick={()=>resolve(task,'keepBoth')}/></Stack>}
      <Stack direction="horizontal" gap={1}>
        {action!=='resume'&&canContinue(task)&&<Button label={task.protocol==='scp'?tr("从当前文件头重传"):tr("校验并继续")} size="sm" variant="ghost" isDisabled={busy} onClick={()=>control(task,'resume')}/>}
        {!['completed','cancelled','cancelling'].includes(task.state)&&<Button label={tr("取消任务")} size="sm" variant="ghost" isDisabled={busy} onClick={()=>control(task,'cancel')}/>}
      </Stack></Stack>}
    </Stack>})}
    <VisuallyHidden role="status">{announcement}</VisuallyHidden>
    {canRecover && <DialogPresence value={recoveryOpen ? onChooseLocal : null}>
      {(chooseLocal, isOpen, onAfterClose) => <Suspense fallback={null}><TransferRecoveryDialog isOpen={isOpen}
        onClose={() => setRecoveryOpen(false)} onAfterClose={onAfterClose} gateway={gateway} onChooseLocal={chooseLocal}/></Suspense>}
    </DialogPresence>}
  </Stack>
}
