import { useLayoutEffect, useRef } from 'react'
import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { Dialog } from '../../shared/ui/AppDialog'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import ActionDialogHeader from '../../shared/ui/ActionDialogHeader'

import type { ClosingPane } from './sessionClose'
export default function CloseTerminalDialog({isOpen,panes,group,busy,error,onCancel,onClose,onDetach,onAfterClose,onDetachFocus}:{isOpen:boolean;panes:ClosingPane[];group:boolean;busy:boolean;error:string;onCancel:()=>void;onClose:(save:boolean)=>void;onDetach?:()=>void;onAfterClose:()=>void;onDetachFocus?:()=>void}){
  useLocale()

 const detached = useRef(false)
 useLayoutEffect(() => { if (isOpen) detached.current = false }, [isOpen])
 const dirty=panes.some(p=>p.dirty)
 return <Dialog isOpen={isOpen} onAfterClose={() => { if (detached.current) onDetachFocus?.(); onAfterClose() }} purpose={busy?'required':'form'} padding={4} width="calc(var(--spacing-10) * 14)" maxHeight="90dvh" onOpenChange={open=>{if(!open&&!busy)onCancel()}}>
   <Stack gap={4} style={{minHeight:0,maxHeight:'80dvh'}}><ActionDialogHeader action="delete" title={group?tr("关闭这组终端？"):tr("关闭此窗格？")} subtitle={tr("将关闭 {p0} 个窗格，并结束对应连接。", panes.length)}/>
     <Stack gap={3} style={{overflow:'auto',minHeight:0}}>{panes.map(p=><Stack key={p.id} gap={1}><Text type="label">{p.name}</Text>{p.dirty&&<Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{tr("未保存的内容：{p0}", p.path)}</Text>}{p.tasks>0&&<Text type="supporting" color="secondary">{tr("将取消的传输任务数：{p0}。已完成的文件会保留。", p.tasks)}</Text>}{p.syncing&&<Text type="supporting" color="secondary">{tr('停止自动同步')}</Text>}{p.forwards>0&&<Text type="supporting" color="secondary">{tr("将停止的端口转发数：{p0}。", p.forwards)}</Text>}</Stack>)}</Stack>
     {error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
     <Stack direction="horizontal" justify="end" gap={2} wrap="wrap"><Button data-autofocus label={tr("取消")} size="md" variant="ghost" isDisabled={busy} onClick={onCancel}/>{onDetach&&<Button label={tr("移到独立工作区，保留连接")} size="md" variant="secondary" isDisabled={busy} onClick={()=>{detached.current=true;onDetach()}}/>}{panes.some(p=>p.dirty)&&<Button label={tr("保存并关闭")} size="md" variant="secondary" isDisabled={busy} onClick={()=>onClose(true)}/>}<Button label={dirty?tr("丢弃修改并结束会话"):tr("结束会话并关闭")} size="md" variant="destructive" isLoading={busy} isDisabled={busy} onClick={()=>onClose(false)}/></Stack>
   </Stack>
 </Dialog>
}
