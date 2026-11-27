import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { matchesShortcut } from './shortcutReference'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {lazy,Suspense,useEffect,useId,useRef,useState} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Icon} from '@astryxdesign/core/Icon'
import {CheckboxList,CheckboxListItem} from '@astryxdesign/core/CheckboxList'
import {SegmentedControl,SegmentedControlItem} from '@astryxdesign/core/SegmentedControl'
import {List,ListItem} from '@astryxdesign/core/List'
import {CommandLineIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import {broadcastGateway} from '../../shared/bridge/ssh'
import type {BroadcastResult} from '../../shared/generated/mygo'
import {prepareBroadcast,BroadcastDraftError,type BroadcastOption} from './broadcastModel'
const CodeEditor=lazy(()=>import('./CommandCodeEditor'))
type Preview=ReturnType<typeof prepareBroadcast>&{id:string}
export default function BroadcastDialog({isOpen,options,onClose,gateway=broadcastGateway}:{isOpen:boolean;options:BroadcastOption[];onClose:()=>void;gateway?:typeof broadcastGateway}){
  useLocale()

 const [selected,setSelected]=useState<string[]>([]),[command,setCommand]=useState(''),[execute,setExecute]=useState(true),[phase,setPhase]=useState<'edit'|'preview'|'result'>('edit'),[preview,setPreview]=useState<Preview|null>(null),[result,setResult]=useState<BroadcastResult|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const working=useRef(false),alive=useRef(true),id=useId()
 const [errorField,setErrorField]=useState<'targets'|'command'|null>(null)
 useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[])
 function reset(){setSelected([]);setCommand('');setExecute(true);setPhase('edit');setPreview(null);setResult(null);setError('');setErrorField(null)}
 useEffect(()=>{if(isOpen){reset();return};const clear=()=>{setSelected([]);setCommand('');setPreview(null)},dialog=document.getElementById(id+'-input')?.closest('dialog');if(!dialog?.open){clear();return};dialog.addEventListener('close',clear,{once:true});return()=>dialog.removeEventListener('close',clear)},[isOpen])
 function prepare(){if(working.current)return;try{const next=prepareBroadcast(options,selected,command,execute);setPreview({...next,id:crypto.randomUUID()});setError('');setErrorField(null);setPhase('preview')}catch(cause){const field=cause instanceof BroadcastDraftError?cause.field:null;setErrorField(field);setError(cause instanceof Error?cause.message:tr("广播内容无效。"));requestAnimationFrame(()=>{if(field==='targets')document.getElementById(id+'-targets')?.querySelector<HTMLElement>('input')?.focus();else document.getElementById(id+'-input')?.focus()})}}
 async function send(){if(!preview||working.current)return;working.current=true;setBusy(true);setError('');try{const next=await gateway.send({id:preview.id,command:preview.command,execute:preview.execute,targets:preview.targets.map(t=>({sessionId:t.sessionId,attempt:t.attempt}))});if(alive.current){setResult(next);setPhase('result')}}catch(cause){if(alive.current){setError(tr("发送结果未确认，请先检查各终端，不要重复提交。{p0}", (cause instanceof Error?' '+cause.message:'')));setPhase('result')}}finally{working.current=false;if(alive.current){setBusy(false);setCommand('');setPreview(current=>current?{...current,command:''}:null)}}}
 async function refresh(){if(!preview||working.current)return;working.current=true;setBusy(true);try{const next=await gateway.status(preview.id);if(alive.current){setResult(next);setError('')}}catch(cause){if(alive.current)setError(cause instanceof Error?cause.message:tr("发送状态未能读取，请检查终端。"))}finally{working.current=false;if(alive.current)setBusy(false)}}
 return <EditorDialog isOpen={isOpen} title={tr("命令广播")} action="edit" startContent={<Icon icon={CommandLineIcon} size="md"/>} closeLabel={tr("关闭命令广播")} cancelLabel={tr("关闭")} busy={busy} onClose={onClose} hint={phase==='edit'?tr("命令仅用于本次广播，不保存。"):phase==='preview'?tr("确认各目标都处于空白 Shell 提示符。"):tr("已发送表示写入终端，不代表命令运行成功。")}
  formProps={{'aria-label':tr("命令广播"),onKeyDown:event=>{if(phase!=='edit'&&matchesShortcut(event.nativeEvent,'save'))event.preventDefault()},onSubmit:event=>{event.preventDefault();if(phase==='edit')prepare();else if(phase==='preview')void send()}}}
  actions={phase==='edit'?<Button label={tr("预览广播")} size="md" variant="primary" type="submit" isDisabled={!gateway.available()||!options.length}/>:phase==='preview'?<><Button label={tr("返回修改")} size="md" variant="secondary" isDisabled={busy} onClick={()=>setPhase('edit')}/><Button label={tr("确认发送到 {p0} 个窗格", (preview?.targets.length??0))} type="submit" size="md" variant="primary" isLoading={busy}/></>:<>{result?.pending&&<Button label={tr("刷新发送状态")} size="md" variant="secondary" isLoading={busy} onClick={()=>{void refresh()}}/>}<Button label={tr("新一次广播")} size="md" variant="primary" isDisabled={busy||!result||result.pending} onClick={reset}/></>}
 >
  {phase==='edit'?<><Stack id={id+'-targets'}><CheckboxList label={tr("目标窗格（当前标签）")} value={selected} onChange={values=>{setSelected(values);setError('');setErrorField(null)}} density="compact" hasDividers status={error&&errorField==='targets'?{type:'error',message:localizeMessage(error)}:undefined}>
   {options.map(option=><CheckboxListItem key={option.sessionId} value={option.sessionId} label={option.label} description={option.description}/>)}
  </CheckboxList></Stack>{!options.length&&<Text type="supporting">{tr("当前标签没有已连接的窗格。")}</Text>}
  <SegmentedControl label={tr("发送方式")} size="md" layout="hug" value={execute?'execute':'fill'} onChange={value=>{setExecute(value==='execute');setError('')}}><SegmentedControlItem label={tr("发送回车")} value="execute"/><SegmentedControlItem label={tr("仅填入")} value="fill"/></SegmentedControl></>:<>
   <Text type="label">{phase==='preview'?(preview?.execute?tr("发送回车"):tr("仅填入，不发送回车")):tr("发送结果")}</Text>
   <List density="compact" edge="flush" aria-label={phase==='preview'?tr("已确认的目标窗格"):tr("各窗格发送结果")}>{preview?.targets.map(target=>{const item=result?.items.find(item=>item.sessionId===target.sessionId&&item.attempt===target.attempt);return <ListItem key={target.sessionId} label={target.label} description={phase==='result'?(item?.message??tr("发送结果未确认，请检查终端。")):target.description} endContent={phase==='result'?<Text type="supporting">{item?.state==='sent'?tr("已发送"):item?.state==='not-sent'?tr("未发送"):tr("未确认")}</Text>:undefined}/>})}</List>
  </>}
  {phase!=='result'&&<><label id={id+'-label'} htmlFor={id+'-input'}><Text type="label">{tr("广播命令")}</Text></label><Suspense fallback={<Text type="supporting">{tr("正在打开命令编辑器…")}</Text>}><CodeEditor fieldId={id+'-input'} labelledBy={id+'-label'} describedBy={id+'-help'} value={command} onChange={value=>{setCommand(value);setError('');setErrorField(null)}} onSave={prepare} disabled={busy||phase==='preview'} invalid={!!error&&errorField==='command'}/></Suspense>
   <Text id={id+'-help'} type="supporting" color="secondary">{tr("请先确认所有目标处于空白 Shell 提示符；当前运行的程序也会收到输入。多行命令只支持发送回车。")}</Text></>}
  {error&&(phase!=='edit'||errorField!=='targets')&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
 </EditorDialog>
}
