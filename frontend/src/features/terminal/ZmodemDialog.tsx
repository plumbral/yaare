import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {ProgressBar} from '@astryxdesign/core/ProgressBar'
import EditorDialog from '../../shared/ui/EditorDialog'
import type {ZmodemEngine,ZmodemView} from './zmodemEngine'

function bytes(value:number){if(value<1024)return value+' B';if(value<1048576)return(value/1024).toFixed(1)+' KiB';return(value/1048576).toFixed(1)+' MiB'}
export default function ZmodemDialog({engine,view,serverName}:{engine:ZmodemEngine;view:ZmodemView;serverName:string}){
  useLocale()

 const [choosing,setChoosing]=useState(false),[cancelling,setCancelling]=useState(false)
 const active=engine.active,offer=view.phase==='offer',conflict=view.phase==='conflict',sending=view.phase==='choose'
 async function close(){if(cancelling)return;setCancelling(true);try{if(engine.active)await engine.cancel();engine.hide()}finally{setCancelling(false)}}
 async function choose(){setChoosing(true);try{await engine.chooseFolder()}finally{setChoosing(false)}}
 const action=()=>{if(sending)void engine.upload();else if(offer&&view.folder)void engine.receive()}
 return <EditorDialog isOpen={view.open} title={view.mode==='send'?tr("ZMODEM 上传"):tr("ZMODEM 接收")} subtitle={serverName} action="edit" closeLabel={tr("关闭 ZMODEM 传输")} cancelLabel={active?tr("取消传输"):tr("关闭")} onClose={()=>void close()} hint={active?tr("本批结束前暂停终端输入；取消保留已完成文件。"):tr("在终端运行 rz / sz 可再次传输。")} formProps={{onSubmit:event=>{event.preventDefault();action()}}} actions={<>
  {offer&&<Button label={tr("接收本批文件")} type="submit" variant="primary" size="md" isDisabled={!view.folder||choosing||cancelling}/>}
  {sending&&<Button label={tr("选择文件并上传")} type="submit" variant="primary" size="md" isDisabled={cancelling}/>}
  {conflict&&<><Button label={tr("保留两个")} type="button" variant="secondary" size="md" isDisabled={cancelling} onClick={()=>void engine.receive('keepBoth')}/><Button label={tr("覆盖此文件")} type="button" variant="primary" size="md" isDisabled={cancelling} onClick={()=>void engine.receive('overwrite')}/></>}
 </>}>
  {view.name&&<Stack gap={1}><Text type="label" style={{overflowWrap:'anywhere'}}>{view.name}</Text><Text type="supporting" color="secondary">{bytes(view.size)}{conflict?tr(" · 已有文件 {p0}", bytes(view.existingSize)):''}</Text></Stack>}
  {view.mode==='receive'&&active&&<Stack direction="horizontal" align="center" gap={2} wrap="wrap"><StackItem size="fill" style={{minWidth:0}}><Text type="supporting" style={{overflowWrap:'anywhere'}}>{view.folder?tr("接收到 {p0}", view.folder):tr("请选择本批文件的接收目录")}</Text></StackItem><Button label={view.folder?tr("更换目录"):tr("选择目录")} size="md" variant="secondary" isLoading={choosing} isDisabled={!offer||cancelling} onClick={()=>void choose()}/></Stack>}
  {view.phase==='transferring'&&<ProgressBar label={view.mode==='send'?tr("正在上传"):tr("正在接收")} value={view.done} max={Math.max(view.size,1)} hasValueLabel formatValueLabel={()=>bytes(view.done)+' / '+bytes(view.size)}/>}
  <Text type="supporting" role="status">{localizeMessage(view.message)}</Text>
  {(offer||conflict)&&<Stack direction="horizontal" justify="end"><Button label={tr("跳过此文件")} size="md" variant="ghost" isDisabled={choosing||cancelling} onClick={()=>void engine.skip()}/></Stack>}
  {view.error&&<Text type="supporting" role="alert" style={{overflowWrap:'anywhere'}}>{localizeMessage(view.error)}</Text>}
 </EditorDialog>
}
