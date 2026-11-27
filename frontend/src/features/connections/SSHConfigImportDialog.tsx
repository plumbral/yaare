import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useCallback,useEffect,useMemo,useRef,useState} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput'
import {Selector} from '@astryxdesign/core/Selector'
import EditorDialog from '../../shared/ui/EditorDialog'
import WindowedList from '../../shared/ui/WindowedList'
import {configImportGateway,type ConfigImportPreview,type ConfigImportRow} from '../../shared/bridge/configImport'

export default function SSHConfigImportDialog({isOpen,onClose,onImported,gateway=configImportGateway}:{isOpen:boolean;onClose:()=>void;onImported:()=>Promise<void>;gateway?:typeof configImportGateway}){
  useLocale()

 const [preview,setPreview]=useState<ConfigImportPreview|null>(null),[selected,setSelected]=useState<Set<string>>(new Set()),[keyChoices,setKeyChoices]=useState<Record<string,number>>({})
 const [query,setQuery]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const guard=useRef(false),sequence=useRef(0),token=useRef('')
 useEffect(()=>{sequence.current++;if(isOpen){setPreview(null);setSelected(new Set());setKeyChoices({});setQuery('');setError('')}
  else if(token.current){const previous=token.current;token.current='';void gateway.discard(previous).catch(()=>{})}
 },[isOpen,gateway])
 useEffect(()=>()=>{sequence.current++;if(token.current)void gateway.discard(token.current).catch(()=>{})},[gateway])
 async function read(choose:boolean){if(guard.current)return;guard.current=true;setBusy(true);setError('');const request=++sequence.current
  try{const next=await gateway.preview(choose);if(request!==sequence.current){if(next)void gateway.discard(next.token).catch(()=>{});return};if(!next)return
   token.current=next.token;setPreview(next);setSelected(new Set(next.rows.filter(row=>!row.error&&!row.duplicate).map(row=>row.id)));setKeyChoices(Object.fromEntries(next.rows.map(row=>[row.id,row.keyAvailable.findIndex(Boolean)])));setQuery('')
  }catch(cause){if(request===sequence.current)setError(cause instanceof Error?cause.message:tr("配置未读取，请重试。"))}
  finally{guard.current=false;setBusy(false)}
 }
 async function commit(){if(guard.current||!preview||!selected.size)return;guard.current=true;setBusy(true);setError('')
  try{await gateway.commit(preview.token,[...selected].map(id=>({id,keyIndex:keyChoices[id]??-1})));token.current='';await onImported();onClose()}
  catch(cause){setError(cause instanceof Error?cause.message:tr("导入未完成，请重新读取配置后核对。"))}
  finally{guard.current=false;setBusy(false)}
 }
 const rows=useMemo(()=>preview?.rows.filter(row=>`${row.name} ${row.address} ${row.username}`.toLowerCase().includes(query.trim().toLowerCase()))??[],[preview,query, getLanguage()])
 const available=useMemo(()=>rows.filter(row=>!row.error&&!row.duplicate),[rows, getLanguage()])
 const selectedVisible=available.filter(row=>selected.has(row.id)).length
 const toggle=useCallback((id:string,value:boolean)=>setSelected(previous=>{const next=new Set(previous);if(value)next.add(id);else next.delete(id);return next}),[])
 const row=useCallback((item:ConfigImportRow)=>({label:<Stack gap={1} width="100%" style={{minWidth:0}}>
   <CheckboxInput label={item.name} value={selected.has(item.id)} isDisabled={busy||!!item.error||item.duplicate} size="md" onChange={value=>toggle(item.id,value)}/>
   <Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{item.username}@{item.address}:{item.port}{item.jumps.length?tr(" · 经 {p0}", item.jumps.join(' → ')):''}</Text>
   {selected.has(item.id)&&item.keyNames.length>0&&<Selector label={tr("认证密钥：{p0}", item.name)} size="md" value={String(keyChoices[item.id]??-1)} isDisabled={busy} options={[{value:'-1',label:tr("连接时选择认证")},...item.keyNames.map((name,index)=>({value:String(index),label:name+(item.keyAvailable[index]?'':tr("（不可用）")),isDisabled:!item.keyAvailable[index]}))]} onChange={value=>setKeyChoices(previous=>({...previous,[item.id]:Number(value)}))}/>}
   {(item.error||item.duplicate||item.warnings.length>0)&&<Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>{item.error||(item.duplicate?tr("同名主机已存在，将跳过。"):item.warnings.join(' '))}</Text>}
  </Stack>}),[selected,busy,keyChoices,toggle])
 return <EditorDialog isOpen={isOpen} title={tr("导入 SSH Config")} action="add" subtitle={tr("读取连接配置，确认后保存主机与密钥文件引用。")} busy={busy} onClose={onClose} closeLabel={tr("关闭 SSH Config 导入")} hint="" formProps={{'aria-label':tr("SSH Config 导入"),onSubmit:event=>{event.preventDefault();void commit()}}}
  actions={<Button label={tr("导入所选{p0}", (selected.size?'（'+selected.size+'）':''))} type="submit" variant="primary" size="md" isDisabled={!selected.size||!gateway.available()} isLoading={busy}/> }>
  <Stack direction="horizontal" gap={2} wrap="wrap"><Button label={tr("读取 ~/.ssh/config")} size="md" variant="secondary" isDisabled={busy||!gateway.available()} onClick={()=>void read(false)}/><Button label={tr("选择配置文件…")} size="md" variant="secondary" isDisabled={busy||!gateway.available()} onClick={()=>void read(true)}/></Stack>
  {!gateway.available()&&<Text type="supporting" color="secondary">{tr("请在桌面程序中读取本机 SSH Config。")}</Text>}
  {preview&&<><TextInput label={tr("筛选待导入主机")} isLabelHidden type="search" placeholder={tr("筛选主机名称或地址…")} startIcon="search" autoComplete="off" size="md" value={query} onChange={setQuery} hasClear/>
   <CheckboxInput label={tr("选择当前筛选中的可导入主机")} value={selectedVisible===0?false:selectedVisible===available.length?true:'indeterminate'} isDisabled={busy||!available.length} onChange={value=>setSelected(previous=>{const next=new Set(previous);for(const item of available){if(value)next.add(item.id);else next.delete(item.id)};return next})}/>
   <Stack height="min(calc(var(--spacing-10) * 8), 40dvh)" style={{minHeight:'calc(var(--spacing-10) * 3)',minWidth:0}}>{rows.length?<WindowedList items={rows} itemKey={item=>item.id} row={row} estimateSize={()=>112} resetKey={preview.token} label={tr("待导入主机")}/>:<Text type="supporting" color="secondary">{tr("没有匹配的主机。")}</Text>}</Stack>
   {preview.warnings.map((warning,index)=><Text key={index} type="supporting" color="secondary">{warning}</Text>)}
  </>}
  {error&&<Text role="alert" type="supporting">{localizeMessage(error)}</Text>}
 </EditorDialog>
}
