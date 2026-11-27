import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useRef,useState,type ReactNode} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Selector} from '@astryxdesign/core/Selector'
import {TextInput} from '@astryxdesign/core/TextInput'
import {TextArea} from '@astryxdesign/core/TextArea'
import {Tab,TabList} from '@astryxdesign/core/TabList'
import EditorDialog from '../../shared/ui/EditorDialog'
import type {HostProfile} from '../workspace/mockGateway'
import type {WorkspaceChange} from '../workspace/savedWorkspace'
import type {Identity} from './useIdentities'
import {readCustomIcon} from '../../shared/icons/customIcon'
import OsIconPicker from './OsIconPicker'
import {osIconOptions,type OsIconName} from '../../shared/icons/OsIcon'
import ProxyEditor from './ProxyEditor'
import JumpHostsEditor from './JumpHostsEditor'
import {emptyProxy, proxySummary} from './networkConfig'
import {patchBatchHosts,validateBatchHosts,type BatchHostPatch} from './batchHosts'

type Field='group'|'icon'|'identity'|'proxy'|'jumps'|'timeout'|'notes'
type Mode='keep'|'set'|'clear'
const fieldNames:Record<Field,string>={get group() { return tr("分组") },get icon() { return tr("系统图标") },get identity() { return tr("已保存身份") },get proxy() { return tr("连接代理") },get jumps() { return tr("跳板链路") },get timeout() { return tr("连接超时") },get notes() { return tr("备注") }}
const initialModes:Record<Field,Mode>={group:'keep',icon:'keep',identity:'keep',proxy:'keep',jumps:'keep',timeout:'keep',notes:'keep'}
function FieldControl({field,mode,onChange,current,clearable=true,disabled,children}:{field:Field;mode:Mode;onChange:(mode:Mode)=>void;current:string;clearable?:boolean;disabled:boolean;children:ReactNode}){
  useLocale()

  const label=fieldNames[field]
  return <Stack gap={2} paddingBlock={2}>
    <Stack direction="horizontal" align="center" gap={3} wrap="wrap">
      <StackItem size="fill"><Stack gap={0}><Text type="label">{label}</Text><Text type="supporting" color="secondary" maxLines={1}>{tr("当前：")}{current}</Text></Stack></StackItem>
      <Selector label={tr("{p0}操作", label)} isLabelHidden size="md" value={mode} isDisabled={disabled} width="calc(var(--spacing-10) * 4)"
        options={[{value:'keep',label:tr("不修改")},{value:'set',label:tr("设置相同值")},...(clearable?[{value:'clear',label:tr("清空")}]:[])]}
        onChange={next=>onChange(next==='set'?'set':next==='clear'?'clear':'keep')}/>
    </Stack>
    {mode==='set'&&children}
    {mode==='clear'&&<Text type="supporting" color="secondary">{field==='proxy'?tr("改为直连"):field==='timeout'?tr("恢复默认 15 秒"):field==='identity'?tr("解除身份关联，连接时手动认证"):field==='group'?tr("移至未指定分组"):tr("清空{p0}", label)}</Text>}
  </Stack>
}
export default function BatchHostEditor({hosts,allHosts,groups,identities,revision,onApply,onClose,storageError='',deleting=false}:{
  hosts:HostProfile[];allHosts:HostProfile[];groups:string[];identities:Identity[];revision:number
  onApply:(change:WorkspaceChange,expectedRevision?:number)=>Promise<boolean>;onClose:(message?:string)=>void;storageError?:string;deleting?:boolean
}){
  useLocale()

  const [original]=useState(()=>structuredClone(hosts)),[baseRevision]=useState(revision)
  const [modes,setModes]=useState(initialModes),[tab,setTab]=useState('basic'),[review,setReview]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false)
  const [group,setGroup]=useState(groups.find(g=>g!=='未分组')??''),[icon,setIcon]=useState<OsIconName>('linux'),[identityId,setIdentityId]=useState('')
  const [proxy,setProxy]=useState(emptyProxy),[jumps,setJumps]=useState<string[]>([]),[timeout,setTimeoutValue]=useState('15'),[notes,setNotes]=useState('')
  const [prepared,setPrepared]=useState<HostProfile[]>([])
  const pending=useRef(false),focus=useRef<HTMLDivElement>(null)
  useEffect(()=>{if(review||error)focus.current?.focus({preventScroll:true})},[review,error])
  const changed=(Object.keys(modes) as Field[]).filter(field=>modes[field]!=='keep')
  const mode=(field:Field,next:Mode)=>{setModes(previous=>({...previous,[field]:next}));setError('')}
  function current(field:Field):string {
    const value=(host:HostProfile)=>field==='group'?host.group:field==='icon'?host.icon??'linux':field==='identity'?host.identityId??'':field==='proxy'?JSON.stringify(!host.proxy?.kind||host.proxy.kind==='none'?emptyProxy():host.proxy):field==='jumps'?(host.jumpHostIds??[]).join(' → '):field==='timeout'?String(host.timeoutSeconds||15):host.notes??''
    const values=original.map(value);if(values.some(v=>v!==values[0]))return tr("多个值")
    const first=original[0]!,text=values[0]??''
    if(field==='identity')return identities.find(i=>i.id===text)?.name??(text?tr("不可用的身份"):tr("未关联"))
    if(field==='icon')return readCustomIcon(text)?.label??osIconOptions.find(i=>i.value===text)?.label??'Linux'
    if(field==='proxy')return proxySummary(first.proxy)
    if(field==='jumps')return first.jumpHostIds?.map(id=>allHosts.find(h=>h.id===id)?.name??tr("不可用")).join(' → ')||tr("未使用")
    if(field==='group')return text==='未分组'?tr("未指定分组"):text
    return field==='timeout'?tr("{p0} 秒", text):text||tr("空")
  }
  function prepare():HostProfile[] {
    if(!changed.length)throw Error(tr("请至少选择一个需要修改的字段。"))
    const patch:BatchHostPatch={}
    for(const field of changed){const clear=modes[field]==='clear'
      if(field==='group'){if(!clear&&!groups.includes(group))throw Error(tr("请选择有效分组。"));patch.group=clear?'未分组':group}
      if(field==='icon')patch.icon=icon
      if(field==='identity'){const identity=identities.find(i=>i.id===identityId);if(!clear&&!identity)throw Error(tr("请选择可用的已保存身份。"));patch.identity=clear?null:identity!}
      if(field==='proxy')patch.proxy=clear?emptyProxy():proxy
      if(field==='jumps'){if(!clear&&!jumps.length)throw Error(tr("请添加跳板机；如需取消使用，请选择清空。"));patch.jumpHostIds=clear?[]:jumps}
      if(field==='timeout'){if(!clear&&(!/^\d{1,3}$/.test(timeout)||Number(timeout)<3||Number(timeout)>120))throw Error(tr("连接超时应为 3–120 秒。"));patch.timeoutSeconds=clear?15:Number(timeout)}
      if(field==='notes'){if(!clear&&!notes.length)throw Error(tr("请输入备注；如需删除原备注，请选择清空。"));patch.notes=clear?'':notes}
    }
    const next=patchBatchHosts(original,patch),invalid=validateBatchHosts(next,allHosts)
    if(invalid)throw Error(invalid)
    return next
  }
  async function submit(){
    if(pending.current)return
    setError('')
    try{
      if(!deleting&&!review){setPrepared(prepare());setReview(true);return}
      pending.current=true;setBusy(true)
      const ok=await onApply(deleting?{kind:'deleteHosts',ids:original.map(h=>h.id)}:{kind:'saveHosts',hosts:prepared},baseRevision)
      if(!ok)throw Error(tr("批量操作未完成。请关闭弹窗，重新载入配置后选择。"))
      onClose(deleting?tr("已删除 {p0} 台连接配置，已打开会话保持原状。", original.length):tr("已更新 {p0} 台主机。", original.length))
    }catch(cause){setError(cause instanceof Error?cause.message:tr("批量操作未完成，请重试。"))}
    finally{pending.current=false;setBusy(false)}
  }
  const field=(name:Field,children:ReactNode,clearable=true)=><FieldControl field={name} mode={modes[name]} onChange={next=>mode(name,next)} current={current(name)} clearable={clearable} disabled={busy}>{children}</FieldControl>
  return <EditorDialog title={deleting?tr("删除 {p0} 台连接配置", original.length):review?tr("检查批量修改"):tr("批量设置 · {p0} 台主机", original.length)} action={deleting?'delete':'edit'} busy={busy} onClose={()=>onClose()}
    hint={deleting?tr("删除配置不会关闭已有会话"):tr("仅修改明确选择的字段，其他设置保留")} formProps={{noValidate:true,onSubmit:event=>{event.preventDefault();void submit()}}}
    navigation={!deleting&&!review&&<TabList role="tablist" aria-label={tr("批量设置分类")} value={tab} onChange={setTab} size="md" hasDivider>{[['basic',tr("基本连接")],['network',tr("网络")],['advanced',tr("高级")]].map(([id,label])=><Tab key={id} id={'batch-tab-'+id} value={id!} label={label!} panelId={'batch-panel-'+id}/>)}</TabList>}
    actions={<>{review&&!deleting&&<Button label={tr("返回修改")} type="button" variant="ghost" isDisabled={busy} onClick={()=>setReview(false)}/>}<Button label={deleting?tr("确认删除 {p0} 台", original.length):review?tr("确认应用到 {p0} 台", original.length):tr("检查修改")} type="submit" isLoading={busy} variant={deleting?'destructive':'primary'}/></>}>
    {deleting?<><Text>{tr("将删除以下连接配置，不删除服务器文件。")}</Text><Text type="supporting" style={{overflowWrap:'anywhere'}}>{original.slice(0,20).map(h=>h.name).join('、')}{original.length>20?'…':''}</Text></>:review?<>
      <Stack ref={focus} tabIndex={-1}><Text type="label">{tr("将应用到 ")}{original.length} {tr(" 台主机")}</Text></Stack>
      <Text type="supporting" style={{overflowWrap:'anywhere'}}>{original.slice(0,20).map(h=>h.name).join('、')}{original.length>20?'…':''}</Text>
      {changed.map(name=><Stack key={name} gap={1}><Text type="label">{fieldNames[name]} · {modes[name]==='clear'?tr("清空／恢复默认"):tr("设置相同值")}</Text><Text type="supporting" color="secondary">{name==='proxy'?proxySummary(prepared[0]?.proxy):name==='jumps'?(prepared[0]?.jumpHostIds?.map(id=>allHosts.find(h=>h.id===id)?.name??id).join(' → ')||tr("未使用")):name==='identity'?(identities.find(i=>i.id===prepared[0]?.identityId)?.name??tr("未关联")):name==='icon'?(readCustomIcon(icon)?.label??osIconOptions.find(i=>i.value===icon)?.label??icon):name==='timeout'?(tr("{p0} 秒", prepared[0]?.timeoutSeconds)):name==='group'?(prepared[0]?.group==='未分组'?tr("未指定分组"):prepared[0]?.group):prepared[0]?.notes||tr("空")}</Text></Stack>)}
      <Text type="supporting" color="secondary">{tr("只替换以上字段。已有 SSH 会话保持原连接，新的设置用于下次连接。")}</Text>
    </>:<>
      <Stack id="batch-panel-basic" role="tabpanel" aria-labelledby="batch-tab-basic" style={{display:tab==='basic'?'flex':'none'}} inert={tab!=='basic'} gap={2}>
        {field('group',<Selector label={tr("统一分组")} isLabelHidden value={group} onChange={setGroup} size="md" hasSearch options={groups.filter(g=>g!=='未分组')}/>)}
        {field('icon',<OsIconPicker value={icon} customIcons={allHosts.map(host => host.icon)} onChange={setIcon} disabled={busy} showLabel />,false)}
        {field('identity',<Selector label={tr("统一登录身份")} isLabelHidden hasClear value={identityId||null} onChange={next=>setIdentityId(next??'')} size="md" hasSearch placeholder={tr("选择已保存身份…")} options={identities.map(i=>({value:i.id,label:i.name,description:i.username}))}/>)}
      </Stack>
      <Stack id="batch-panel-network" role="tabpanel" aria-labelledby="batch-tab-network" style={{display:tab==='network'?'flex':'none'}} inert={tab!=='network'} gap={2}>
        {field('proxy',<ProxyEditor value={proxy} disabled={busy} onChange={setProxy}/>)}
        {field('jumps',<JumpHostsEditor host={{...original[0]!,id:'batch-target',name:tr("所选主机"),jumpHostIds:jumps,proxy:modes.proxy==='set'?proxy:undefined}} hosts={allHosts.filter(h=>!original.some(target=>target.id===h.id))} disabled={busy} onChange={setJumps}/>)}
      </Stack>
      <Stack id="batch-panel-advanced" role="tabpanel" aria-labelledby="batch-tab-advanced" style={{display:tab==='advanced'?'flex':'none'}} inert={tab!=='advanced'} gap={2}>
        {field('timeout',<TextInput label={tr("统一连接超时（秒）")} size="md" value={timeout} inputMode="numeric" autoComplete="off" onChange={setTimeoutValue}/>)}
        {field('notes',<TextArea label={tr("统一备注")} size="md" rows={3} maxLength={2048} value={notes} onChange={setNotes}/>)}
      </Stack>
    </>}
    {error&&<Stack ref={focus} role="alert" tabIndex={-1}><Text>{localizeMessage(storageError||error)}</Text></Stack>}
  </EditorDialog>
}
