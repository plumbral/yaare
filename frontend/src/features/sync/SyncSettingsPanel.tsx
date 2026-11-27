import SyncDevices from './SyncDevices'
import {useEffect,useRef,useState,useSyncExternalStore,type RefObject} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Heading,Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {Selector} from '@astryxdesign/core/Selector'
import {Table,TableHeader,TableBody,TableRow,TableHeaderCell,TableCell} from '@astryxdesign/core/Table'
import {SyncService,type SyncSaveRequest,type SyncBackup} from '../../shared/generated/mygo'
import {desktopAvailable} from '../../shared/bridge/application'
import {cloudStatus} from '../../shared/bridge/cloud'
import {tr,localizeMessage} from '../../shared/i18n/messages'
import {useLocale,getLanguage} from '../../shared/i18n/locale'
import EditorDialog from '../../shared/ui/EditorDialog'
import BackupSettingsPanel from '../workspace/BackupSettingsPanel'
type SyncPreview=Awaited<ReturnType<typeof SyncService.preview>>

const empty:SyncSaveRequest={version:0,target:{provider:'webdav',endpoint:'',username:'',bucket:'',region:'us-east-1',prefix:'Yaare'},credential:'',password:''}
export default function SyncSettingsPanel({historyRequest,handledHistory}:{historyRequest:number;handledHistory:RefObject<number>}){
 useLocale()
 const{value,error:statusError}=useSyncExternalStore(cloudStatus.subscribeSettings,cloudStatus.readSettings)
 const[draft,setDraft]=useState<SyncSaveRequest>(empty),[editing,setEditing]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[preview,setPreview]=useState<SyncPreview|null>(null),[choices,setChoices]=useState<Record<string,string>>({}),[history,setHistory]=useState<SyncBackup[]|null>(null)
 const alive=useRef(true),working=useRef(false)
 useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[])
 async function run(action:()=>Promise<void>){if(working.current)return;working.current=true;setBusy(true);setError('');try{await action()}catch(cause){if(alive.current)setError(cause instanceof Error?cause.message:String(cause))}finally{working.current=false;if(alive.current)setBusy(false)}}
 function configure(){void run(async()=>{const latest=await SyncService.settings();if(!alive.current)return;setDraft({...empty,version:latest.version,target:latest.target.provider?latest.target:empty.target});setEditing(true)})}
 async function save(){await run(async()=>{await SyncService.save(draft);if(alive.current)setEditing(false)});setDraft(d=>({...d,credential:'',password:''}))}
 useEffect(()=>{if(historyRequest&&historyRequest!==handledHistory.current){handledHistory.current=historyRequest;void run(async()=>setHistory(await SyncService.history()))}},[historyRequest,handledHistory])
 async function compare(){await run(async()=>{setPreview(await SyncService.preview());setChoices({})})}
 async function apply(){if(!preview)return;await run(async()=>{const result=await SyncService.apply(preview.id,choices);if(result.state==='conflict')throw Error(tr('请为每项冲突选择保留的版本。'));if(!alive.current)return;setPreview(null);setHistory(null)})}
 const field=(key:'endpoint'|'username'|'bucket'|'region'|'prefix',label:string,type:'text'|'url'='text')=><TextInput label={label} htmlName={'sync-'+key} value={draft.target[key]} type={type} onChange={text=>setDraft(d=>({...d,target:{...d.target,[key]:text}}))} autoComplete="off" hasSpellCheck={false} isDisabled={busy}/>
 const configured=!!value?.target.provider,canSync=value?.canSync
 const keepsCredential=!!value?.hasCredential&&draft.target.provider===value.target.provider&&draft.target.endpoint===value.target.endpoint&&draft.target.username===value.target.username
 const providerLabel=(provider:string)=>provider==='yaare'?tr('Yaare 自托管'):provider==='s3'?tr('S3 兼容存储'):'WebDAV'
 const errorNode=(error||statusError)&&<Text role="alert">{localizeMessage(error||statusError)}</Text>
 return <Stack gap={4}>
  {import.meta.env.MODE==='browser'&&<SyncDevices/>}
  <Heading level={4} accessibilityLevel={3} weight="semibold">{tr('同步与备份')}</Heading>
  <Stack gap={2}>
   <Stack direction="horizontal" align="center" wrap="wrap" gap={2}><StackItem size="fill"><Stack gap={1}><Text weight="semibold">{tr('同步')}</Text>{configured&&<Text type="supporting" color="secondary">{providerLabel(value.target.provider)}</Text>}</Stack></StackItem><Button label={configured?tr('配置…'):tr('选择存储…')} variant="ghost" onClick={configure} isDisabled={busy||!desktopAvailable()}/></Stack>
   {configured&&<><Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{value.target.endpoint}{value.target.bucket&&' / '+value.target.bucket}{value.target.prefix&&' / '+value.target.prefix}</Text>
    {canSync?<Stack direction="horizontal" align="center" gap={2} wrap="wrap"><StackItem size="fill"><Text type="supporting">{value.paused?tr('后台任务已暂停'):tr('后台自动同步已开启')}</Text></StackItem><Button label={tr('检查与合并…')} variant="ghost" onClick={()=>void compare()} isDisabled={busy}/></Stack>:<Text type="supporting" color="secondary">{tr('此目标仅用于备份与恢复；同步需要支持条件写入的存储服务。')}</Text>}
    {value.issue==='baseline'&&canSync?<Text type="supporting">{tr('同步基线不可用，请先检查并合并配置。')}</Text>:value.paused&&<Button label={tr('继续后台任务')} variant="ghost" isDisabled={busy} onClick={()=>void run(()=>SyncService.resume(cloudStatus.getVersion()))}/>}
    {value.lastSync>0&&<Text type="supporting" color="secondary">{tr('上次同步：')}{new Date(value.lastSync).toLocaleString(getLanguage())}</Text>}
    {value.issue==='conflict'&&<Text type="supporting">{tr('有同时修改的配置，请检查并选择保留的版本。')}</Text>}
    {value.issue==='unavailable'&&<Text type="supporting">{tr('暂时无法访问存储，后台会重试。请检查网络或存储配置。')}</Text>}
    {value.issue==='history'&&<Text type="supporting">{tr('旧备份清理未完成。请检查存储的删除权限。')}</Text>}
   </>}
  </Stack>
  <Stack gap={2}>
   {configured&&<Stack direction="horizontal" align="center" gap={2} wrap="wrap"><StackItem size="fill"><Text weight="semibold">{tr('备份与恢复')}</Text></StackItem><Stack direction="horizontal" gap={1}><Button label={tr('立即备份')} variant="ghost" isDisabled={busy} onClick={()=>void run(async()=>{await SyncService.backupNow()})}/><Button label={tr('恢复备份…')} variant="ghost" isDisabled={busy} onClick={()=>void run(async()=>{setHistory(await SyncService.history())})}/></Stack></Stack>}
   <BackupSettingsPanel/>
  </Stack>
  {!editing&&!preview&&history===null&&errorNode}
  <EditorDialog isOpen={editing} title={tr('配置同步与备份')} action="edit" busy={busy} onClose={()=>{if(!busy){setEditing(false);setError('');setDraft(empty)}}} formProps={{onSubmit:event=>{event.preventDefault();void save()},autoComplete:'off'}} actions={<Button type="submit" label={tr('保存')} isLoading={busy}/>}>
   <Selector label={tr('存储类型')} value={draft.target.provider} options={[{value:'yaare',label:tr('Yaare 自托管')},{value:'webdav',label:'WebDAV'},{value:'s3',label:tr('S3 兼容存储')},{value:'',label:tr('不使用远端存储')}]} onChange={provider=>setDraft(d=>({...d,target:{...empty.target,provider,...(provider==='yaare'?{prefix:'',endpoint:import.meta.env.MODE==='browser'?location.origin:''}:{})},credential:'',password:''}))} isDisabled={busy} size="md"/>
   {draft.target.provider&&<>
    {field('endpoint',tr('服务地址'),'url')}{draft.target.provider==='s3'&&<Stack direction="horizontal" gap={3} wrap="wrap"><StackItem size="fill">{field('bucket',tr('存储桶'))}</StackItem><StackItem size="fill">{field('region',tr('区域'))}</StackItem></Stack>}{draft.target.provider!=='yaare'&&field('username',draft.target.provider==='s3'?'Access Key ID':tr('用户名'))}{!(import.meta.env.MODE==='browser'&&draft.target.provider==='yaare'&&draft.target.endpoint===location.origin)&&<TextInput label={draft.target.provider==='yaare'?tr('设备令牌'):keepsCredential?tr('访问密码 / Secret Key（留空保留）'):tr('访问密码 / Secret Key')} htmlName="sync-secret" type="password" value={draft.credential} onChange={credential=>setDraft(d=>({...d,credential}))} autoComplete="new-password" isDisabled={busy}/> }
    {draft.target.provider!=='yaare'&&field('prefix',tr('配置目录 / 前缀'))}
    <TextInput label={value?.hasPassword?tr('配置加密密码（留空保留）'):tr('配置加密密码 · 至少 12 个字符')} htmlName="sync-encryption-password" type="password" value={draft.password} onChange={password=>setDraft(d=>({...d,password}))} autoComplete="new-password" isDisabled={busy}/>
    <Text type="supporting" color="secondary">{tr('每台设备使用相同的加密密码和目录。密码遗失后无法恢复。存储服务只收到加密内容。')}</Text>
    <Text type="supporting" color="secondary">{tr('同步外观与终端偏好；本机字体和快捷键只留在当前设备。')}</Text>
   </>}
   {editing&&errorNode}
  </EditorDialog>
  <EditorDialog isOpen={!!preview} title={preview?.restoring?tr('检查备份恢复'):tr('检查与合并配置')} action="edit" busy={busy} onClose={()=>{if(!busy){setPreview(null);setError('')}}} formProps={{onSubmit:event=>{event.preventDefault();void apply()}}} actions={<Button type="submit" label={preview?.restoring?tr('确认恢复并暂停同步'):tr('合并配置')} isLoading={busy} isDisabled={!!preview?.conflicts.some(c=>!choices[c.id])}/>}>
   {preview&&<><Table aria-label={tr('配置内容')} density="compact"><TableHeader><TableRow><TableHeaderCell>{tr('内容')}</TableHeaderCell><TableHeaderCell>{tr('本机')}</TableHeaderCell><TableHeaderCell>{preview.restoring?tr('备份'):tr('远端')}</TableHeaderCell></TableRow></TableHeader><TableBody>{(['hosts','groups','commands','identities','keys','preferences'] as const).map((key,i)=><TableRow key={key}><TableCell>{[tr('主机'),tr('分组'),tr('快捷命令'),tr('身份'),tr('密钥'),tr('自定义外观与终端偏好')][i]}</TableCell><TableCell>{preview.local[key]}</TableCell><TableCell>{preview.remote[key]}</TableCell></TableRow>)}</TableBody></Table>
    {preview.added.length>0&&<Text type="supporting" style={{overflowWrap:'anywhere'}}>{tr('新增：{p0}',preview.added.slice(0,20).join('、'))}</Text>}
    {preview.removed.length>0&&<Text type="supporting" style={{overflowWrap:'anywhere'}}>{tr('移除：{p0}',preview.removed.slice(0,20).join('、'))}</Text>}
    {preview.changed.length>0&&<Text type="supporting" style={{overflowWrap:'anywhere'}}>{tr('修改：{p0}',preview.changed.slice(0,20).join('、'))}</Text>}
    {preview.restoring?<Text type="supporting">{tr('将替换当前配置。恢复前保存加密副本，并暂停自动同步与备份。已有会话保留，任务不会自动运行。')}</Text>:<><Text type="supporting">{tr('不同项目的修改会自动合并；同时修改的项目由你选择。删除也会参与合并。')}</Text>{preview.conflicts.map(c=><Stack key={c.id} gap={1}><Text style={{overflowWrap:'anywhere'}}>{c.name}</Text><Selector label={tr('保留的版本')} value={choices[c.id]||''} placeholder={tr('请选择')} options={[{value:'local',label:c.localDeleted?tr('采用本机删除'):tr('保留本机版本')},{value:'remote',label:c.remoteDeleted?tr('采用远端删除'):tr('保留远端版本')}]} size="md" onChange={choice=>setChoices(values=>({...values,[c.id]:choice}))} isDisabled={busy}/></Stack>)}</>}
   </>}{preview&&errorNode}
  </EditorDialog>
  <EditorDialog isOpen={history!==null&&!preview} title={tr('恢复备份')} subtitle={tr('保留最近 30 天，最多 120 个版本。')} action="edit" busy={busy} onClose={()=>{if(!busy){setHistory(null);setError('')}}} formProps={{onSubmit:event=>event.preventDefault()}} hint="" cancelLabel={tr('关闭')} actions={null}>
   {history?.length===0&&<Text type="supporting">{tr('还没有远端备份。')}</Text>}
   {history?.map(item=><Stack key={item.name} direction="horizontal" align="center" gap={2}><StackItem size="fill"><Text type="supporting">{new Date(item.created).toLocaleString(getLanguage())}</Text></StackItem><Button label={tr('检查…')} variant="ghost" isDisabled={busy} onClick={()=>void run(async()=>{setPreview(await SyncService.previewRestore(item.name));setChoices({})})}/></Stack>)}
   {!preview&&history!==null&&errorNode}
  </EditorDialog>
 </Stack>
}
