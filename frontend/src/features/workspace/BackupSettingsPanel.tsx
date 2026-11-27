import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text,Heading} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {InputGroup} from '@astryxdesign/core/InputGroup'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {Table,TableHeader,TableBody,TableRow,TableHeaderCell,TableCell} from '@astryxdesign/core/Table'
import {EyeIcon,EyeSlashIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import {backupGateway,type BackupFile,type BackupPreview} from '../../shared/bridge/backup'
import {supportsDesktopRecovery} from '../../shared/bridge/application'

function BackupPassword({label,value,onChange,first=false,creating=false}:{label:string;value:string;onChange:(value:string)=>void;first?:boolean;creating?:boolean}){
  useLocale()

 const[visible,setVisible]=useState(false)
 useEffect(()=>{if(!value)setVisible(false)},[value])
 return <InputGroup label={label} size="md"><TextInput label={label} htmlName={creating?'backup-password-new':'backup-password'} hasAutoFocus={first} value={value} onChange={onChange} type={visible?'text':'password'} autoComplete={creating?'new-password':'current-password'} hasSpellCheck={false} hasAutoCorrect={false} controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent'}}/><IconButton type="button" variant="ghost" label={visible?tr("隐藏{p0}", label):tr("显示{p0}", label)} aria-pressed={visible} icon={<Icon icon={visible?EyeSlashIcon:EyeIcon} size="sm"/>} onClick={()=>setVisible(v=>!v)}/></InputGroup>
}
const message=(cause:unknown)=>cause instanceof Error?cause.message:tr("配置操作未完成，请检查后重试。")
export default function BackupSettingsPanel({gateway=backupGateway}:{gateway?:typeof backupGateway}){
  useLocale()

 const[mode,setMode]=useState<'export'|'import'|null>(null),[file,setFile]=useState<BackupFile|null>(null),[preview,setPreview]=useState<BackupPreview|null>(null),[password,setPassword]=useState(''),[repeat,setRepeat]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState(''),[status,setStatus]=useState(''),[point,setPoint]=useState('')
 const working=useRef(false),alive=useRef(true),errorFocus=useRef<HTMLDivElement>(null),previewFocus=useRef<HTMLDivElement>(null),importButton=useRef<HTMLButtonElement>(null)
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;if(gateway.available())void gateway.discard().catch(()=>{})}},[])
 useEffect(()=>{if(error)requestAnimationFrame(()=>errorFocus.current?.focus({preventScroll:true}))},[error])
 useEffect(()=>{if(preview)requestAnimationFrame(()=>previewFocus.current?.focus({preventScroll:true}))},[preview])
 function close(){if(working.current)return;setMode(null);setPassword('');setRepeat('');setError('');setPreview(null);if(gateway.available())void gateway.discard().catch(()=>{})}
 async function operation(action:()=>Promise<void>){if(working.current)return;working.current=true;setBusy(true);setError('');try{await action()}catch(cause){if(alive.current)setError(message(cause))}finally{working.current=false;if(alive.current){setBusy(false);setPassword('');setRepeat('')}}}
 function choose(){void operation(async()=>{const next=await gateway.chooseImport();if(!alive.current)return;if(!next.id)return;setFile(next);setPreview(null);setMode('import');setStatus('');setPoint('')})}
 function exportFile(){void operation(async()=>{
  if([...password].length<12||new TextEncoder().encode(password).length>1024)throw Error(tr("备份密码至少 12 个字符，最多 1 KiB。"))
  if(password!==repeat)throw Error(tr("两次输入的备份密码不一致。"))
  const exported=await gateway.export({password,includeCredentials:true})
  if(alive.current){if(exported){setMode(null);setStatus(tr("加密配置已导出。请保存好备份密码，换机导入时需要使用。"))}else setStatus(tr("已取消导出。"))}
 })}
 function inspect(){void operation(async()=>{if(!file)throw Error(tr("请先选择配置文件。"));setPreview(await gateway.preview(file.id,password))})}
 function restoreSeparate(){void operation(async()=>{
  if(!file)return
  const result=await gateway.restoreNewWorkspace(file.id,password)
  if(!alive.current||!result.directory)return
  setMode(null)
  setPoint('')
  setStatus(result.launched?tr("已打开恢复的工作区。数据目录为 {p0}。",result.directory):tr("工作区已恢复到 {p0}。使用 --yaare-data-dir 指定该目录即可打开。",result.directory))
 })}
 function apply(){if(!preview)return;void operation(async()=>{const current=preview;try{const result=await gateway.apply(current.id);if(!alive.current)return;setPoint(result.restorePoint);setStatus(result.message);setMode(null)}catch(cause){setPreview(null);throw cause}})}
 const errorNode=error&&<Stack role="alert" tabIndex={-1} ref={errorFocus}><Text>{localizeMessage(error)}</Text></Stack>
 const counts=(label:string,key:'hosts'|'groups'|'commands'|'keys'|'identities'|'preferences')=><TableRow key={key}><TableCell>{label}</TableCell><TableCell>{preview?.current[key]}</TableCell><TableCell>{preview?.incoming[key]}</TableCell></TableRow>
 return <Stack gap={3}><Stack direction="horizontal" align="center" gap={3} wrap="wrap"><StackItem size="fill"><Heading level={4} accessibilityLevel={3} weight="semibold">{tr("本地加密备份")}</Heading></StackItem><Stack direction="horizontal" gap={1}><Button label={tr("导出配置…")} variant="ghost" isDisabled={!gateway.available()||busy} onClick={()=>{setError('');setStatus('');setMode('export')}}/><Button ref={importButton} label={tr("导入配置…")} variant="ghost" isDisabled={!gateway.available()||busy} onClick={choose}/></Stack></Stack>
 {!gateway.available()&&<Text type="supporting" color="secondary">{tr("本地配置文件的导出与导入由桌面客户端提供。")}</Text>}
 {mode===null&&errorNode}{status&&<Text role="status" type="supporting">{status}</Text>}
 {point&&<Stack direction="horizontal"><Button label={tr("另存导入前备份…")} variant="secondary" isDisabled={busy} onClick={()=>void operation(async()=>{if(await gateway.saveRestorePoint(point))setStatus(tr("导入前的加密副本已另存，使用刚才导入时输入的备份密码。"))})}/></Stack>}
 <EditorDialog isOpen={mode==='export'} title={tr("导出加密配置")} action="edit" busy={busy} onClose={close} hint={tr("密码仅用于加密本次备份")} formProps={{onSubmit:event=>{event.preventDefault();exportFile()}}} actions={<Button type="submit" label={tr("选择位置并导出")} isLoading={busy}/>}>
  <Text type="supporting">{tr('导出配置及外观与终端偏好；本机字体和快捷键不导出。')}</Text>
  <BackupPassword label={tr("备份密码")} value={password} onChange={setPassword} first creating/>
  <BackupPassword label={tr("再次输入备份密码")} value={repeat} onChange={setRepeat} creating/>
  <Text type="supporting" color="secondary">{tr("至少 12 个字符。密码遗失后无法从文件中找回。")}</Text>
  {mode==='export'&&errorNode}
 </EditorDialog>
 <EditorDialog isOpen={mode==='import'} title={preview?tr("检查配置导入"):tr("导入加密配置")} action="edit" busy={busy} onClose={close} onAfterClose={()=>importButton.current?.focus({preventScroll:true})} hint={preview?tr("确认前不会替换配置"):tr("密码仅用于解锁本次文件")} formProps={{onSubmit:event=>{event.preventDefault();if(preview)apply();else inspect()}}} actions={<Button type="submit" label={preview?tr("确认导入并替换"):tr("解锁并检查")} isLoading={busy}/>}>
  {file&&<Text type="supporting" style={{overflowWrap:'anywhere'}}>{file.name}</Text>}
  {preview?<><Stack tabIndex={-1} ref={previewFocus}><Text type="label">{tr("请检查将要替换的配置")}</Text></Stack><Text type="supporting" color="secondary">{tr("创建于 ")}{new Date(preview.created).toLocaleString(getLanguage())} · {preview.includeCredentials?tr("包含连接密码、身份与托管私钥"):tr("未包含认证；保留匹配的本机凭据")}</Text>
   <Table aria-label={tr("配置导入内容")} density="compact"><TableHeader><TableRow><TableHeaderCell>{tr("内容")}</TableHeaderCell><TableHeaderCell>{tr("当前")}</TableHeaderCell><TableHeaderCell>{tr("文件")}</TableHeaderCell></TableRow></TableHeader><TableBody>{counts(tr("主机"),'hosts')}{counts(tr("分组"),'groups')}{counts(tr("快捷命令"),'commands')}{counts(tr("身份"),'identities')}{counts(tr("密钥"),'keys')}{counts(tr('自定义外观与终端偏好'),'preferences')}</TableBody></Table>
   <Text type="supporting">{tr("主机新增 ")}{preview.added.length}{tr("、移除 ")}{preview.removed.length}{tr("、修改 ")}{preview.changed.length}。</Text>
   {preview.removed.length>0&&<Text type="supporting" style={{overflowWrap:'anywhere'}}>{tr("将移除：")}{preview.removed.slice(0,20).join('、')}</Text>}
   <Text type="supporting">{tr('将恢复配置并暂停自动同步。本机字体、快捷键和已有 SSH 会话会保留。')}</Text>
   <Text type="supporting" color="secondary">{tr("导入前会自动保存当前配置的加密副本，使用刚才输入的备份密码。主机指纹信任、本机授权与外部私钥文件不导入。")}</Text>
   {preview.incoming.fileReferences>0&&<Text type="supporting">{tr("文件含 ")}{preview.incoming.fileReferences} {tr(" 个外部密钥引用，新设备需要重新选择原文件。")}</Text>}
  </>:<><BackupPassword label={tr("备份密码")} value={password} onChange={setPassword} first/><Text type="supporting" color="secondary">{tr("解锁后先显示差异；确认前不会替换配置。")}</Text>
   {supportsDesktopRecovery()&&<Button type="button" variant="ghost" label={tr("恢复到新工作区")} isDisabled={busy||!password} onClick={restoreSeparate}/>}
  </>}
  {mode==='import'&&errorNode}
 </EditorDialog>
 </Stack>
}
