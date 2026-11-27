import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useState } from 'react'
import { Dialog } from '../../shared/ui/AppDialog'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { XMarkIcon } from '@heroicons/react/24/outline'
import type { LiveSSH } from './useSSHSessions'
import type { HostProfile } from '../workspace/mockGateway'
import ActionDialogHeader from '../../shared/ui/ActionDialogHeader'
import { forwardKinds, forwardSummary } from './networkConfig'

export default function SessionForwardsDialog({ open, host, live, onClose }: { open:boolean;host:HostProfile;live?:LiveSSH;onClose:()=>void }) {
  useLocale()

  const [busy,setBusy]=useState<Record<string,boolean>>({})
  const [error,setError]=useState('')
  useEffect(()=>{setBusy({});setError('')},[live?.attempt,host.id])
  const connected=live?.phase==='connected'
  const act=async(id:string,start:boolean)=>{if(busy[id]||!live)return;setBusy(previous=>({...previous,[id]:true}));setError('');try{if(!await live.transport.setForward(id,start))setError(tr("此会话已结束或规则状态已变更，请重新连接。"))}finally{setBusy(previous=>({...previous,[id]:false}))}}
  const rules=host.forwards??[]
  return <Dialog isOpen={open} purpose="form" padding={0} width="calc(var(--spacing-10) * 18)" maxHeight="85dvh" onOpenChange={next=>{if(!next)onClose()}}>
    <Stack gap={0} style={{maxHeight:'85dvh',minHeight:0}}>
      <Stack padding={4} style={{flexShrink:0}}><ActionDialogHeader action="edit" title={tr("端口转发")} subtitle={host.name} endContent={<IconButton label={tr("关闭端口转发")} type="button" variant="ghost" size="md" icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={onClose}/>}/></Stack>
      <Stack padding={4} paddingBlockStart={0} gap={2} style={{overflow:'auto',minHeight:0}}>
        {!rules.length&&<Text type="supporting" color="secondary">{tr("尚未配置转发规则，可在编辑连接的“端口转发”页添加。")}</Text>}
        {rules.map(rule=>{const status=live?.forwards?.[rule.id];const running=connected&&(status?.state==='running'||status?.state==='starting');return <Stack key={rule.id} direction="horizontal" align="center" gap={3} paddingBlock={2} style={{borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
          <StackItem size="fill"><Stack gap={1}><Text type="label">{rule.name} · {forwardKinds[rule.kind]}</Text><Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{forwardSummary(rule)}</Text><Text type="supporting" role="status">{connected?status?.message??tr("等待启动"): tr("会话未连接，监听已停止")}</Text></Stack></StackItem>
          <Button label={running?tr("停止"):status?.state==='failed'?tr("重试"):tr("启动")} type="button" variant="secondary" size="md" isLoading={busy[rule.id]} isDisabled={!connected||busy[rule.id]} onClick={()=>{void act(rule.id,!running)}}/>
        </Stack>})}
        {error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
        {rules.length>0&&<Text type="supporting" color="secondary">{tr("这里只调整本次会话的运行状态；断开连接会关闭全部监听。")}</Text>}
      </Stack>
    </Stack>
  </Dialog>
}
