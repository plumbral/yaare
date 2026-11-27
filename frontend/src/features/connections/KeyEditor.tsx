import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { TextInput } from '@astryxdesign/core/TextInput'
import { TextArea } from '@astryxdesign/core/TextArea'
import { InputGroup } from '@astryxdesign/core/InputGroup'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { EyeIcon, EyeSlashIcon } from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import { desktopAvailable, writeClipboardText } from '../../shared/bridge/application'
import { generateKey, pickKey, saveKey, discardKeySelection, type KeyReference, type KeySelection, type SaveKeyRequest } from '../../shared/bridge/keys'

const nativeGateway={pick:pickKey,generate:generateKey,save:saveKey,discard:discardKeySelection,available:desktopAvailable}
export default function KeyEditor({ isOpen, onClose, onAfterClose, onSaved, gateway=nativeGateway }: { isOpen: boolean; onClose: () => void; onAfterClose?: () => void; onSaved: (key: KeyReference) => Promise<void>; gateway?:typeof nativeGateway }) {
  useLocale()

 const [name,setName]=useState(''),[source,setSource]=useState<'file'|'text'|'generated'>('file')
 const [selection,setSelection]=useState<KeySelection|null>(null),[privateKey,setPrivateKey]=useState(''),[passphrase,setPassphrase]=useState('')
 const [show,setShow]=useState(false),[picking,setPicking]=useState(false),[saving,setSaving]=useState(false),[error,setError]=useState('')
 const [copied,setCopied]=useState(false)
 const token=useRef(''),pending=useRef(false),open=useRef(isOpen),form=useRef<HTMLFormElement>(null)
 open.current=isOpen
 const discard=()=>{const previous=token.current;token.current='';if(previous)void gateway.discard(previous).catch(()=>{})}
 useEffect(()=>{if(!isOpen){setPrivateKey('');setPassphrase('');setShow(false);setError('');setCopied(false);setSelection(null);discard()}},[isOpen])
 useEffect(()=>{open.current=isOpen;return()=>{open.current=false;discard()}},[])
 const changeSource=(value:string)=>{setSource(value==='generated'?'generated':value==='text'?'text':'file');setPrivateKey('');setPassphrase('');setShow(false);setError('');setCopied(false);setSelection(null);discard()}
 async function generate(){
  if(pending.current)return
  pending.current=true;setPicking(true);setError('');setCopied(false)
  try{
   const next=await gateway.generate()
   if(!next)throw Error(tr("密钥生成失败，请重试。"))
   if(!open.current){void gateway.discard(next.token).catch(()=>{});return}
   token.current=next.token;setSelection(next);setName(old=>old||next.name)
  }catch(cause){if(open.current)setError(cause instanceof Error?cause.message:tr("密钥生成失败，请重试。"))}
  finally{pending.current=false;setPicking(false)}
 }
 async function choose(){
  if(pending.current)return;pending.current=true;setPicking(true);setError('')
  try{if(import.meta.env.MODE==='browser'){const {readBrowserPrivateKey}=await import('../../shared/bridge/browserFiles');const file=await readBrowserPrivateKey();if(file&&open.current){setSource('text');setPrivateKey(file.content);setName(old=>old||file.name)};return};const next=await gateway.pick();if(next){if(!open.current){void gateway.discard(next.token).catch(()=>{});return}token.current=next.token;setSelection(next);setName(old=>!old||old===selection?.name?next.name:old)}}
  catch{setError(tr("文件未选择，请检查文件是否可读且为 OpenSSH 或 PEM 私钥。"))}
  finally{pending.current=false;setPicking(false)}
 }
 async function submit(event:FormEvent<HTMLFormElement>){
  event.preventDefault();if(pending.current||!gateway.available())return
  const problem=!name.trim()||[...name.trim()].length>255?tr("请输入 1–255 个字符的密钥名称。"):source==='generated'&&!selection?tr("请重新生成密钥。"):source==='file'&&!selection?tr("请选择私钥文件。"):source==='text'&&(!privateKey.trim()||new TextEncoder().encode(privateKey).byteLength>1048576)?tr("请粘贴有效私钥，最大 1 MB。"):passphrase.length>16384?tr("私钥口令过长。"):''
  if(problem){setError(problem);requestAnimationFrame(()=>form.current?.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')?.focus());return}
  const request:SaveKeyRequest={name:name.trim(),source,token:selection?.token??'',privateKey:source==='text'?privateKey:'',passphrase}
  pending.current=true;setSaving(true);setError('');setPrivateKey('');setPassphrase('');setShow(false)
  try{const key=await gateway.save(request);request.privateKey='';request.passphrase='';await onSaved(key);onClose()}
  catch(cause){if(source==='generated'){setSelection(null);discard()}setError(`${cause instanceof Error?cause.message:tr("密钥保存失败。")} ${source==='generated'?tr("请重新生成密钥。"):source==='text'?tr("名称已保留，请重新粘贴私钥和输入口令后重试。"):tr("名称和文件选择已保留；如填写了口令，请重新输入后重试。")}`)}
  finally{request.privateKey='';request.passphrase='';pending.current=false;setSaving(false)}
 }
 const busy=picking||saving
 return <EditorDialog isOpen={isOpen} title={tr("添加密钥")} action="add" subtitle={tr("生成 Ed25519 密钥，或导入 OpenSSH / PEM 私钥。")} closeLabel={tr("关闭密钥编辑")} onClose={onClose} onAfterClose={onAfterClose} busy={saving}
  formProps={{ref:form,'aria-label':tr("添加密钥"),onSubmit:submit}}
  hint={gateway.available()?undefined:tr("浏览器仅预览，请在桌面程序中保存")}
  actions={<Button label={tr("保存密钥")} type="submit" size="md" variant="primary" isDisabled={busy||!gateway.available()} isLoading={saving}/> }>
  <FormLayout defaultOptionality="required"><Stack direction="horizontal" align="start" gap={3} wrap="wrap">
   <StackItem size="fill" style={{minWidth:0,flexBasis:'calc(var(--spacing-10) * 8)'}}><TextInput label={tr("密钥名称")} htmlName="key-name" size="md" value={name} onChange={value=>{setName(value);setError('')}} isRequired hasAutoFocus autoComplete="off" isDisabled={busy}
    status={error.includes(tr("密钥名称"))?{type:'error',message:localizeMessage(error)}:undefined} statusVariant="detached"/></StackItem>
   <StackItem size="fill" style={{minWidth:0,flexBasis:'calc(var(--spacing-10) * 5)'}}><Stack gap={1}>
    <Text type="label" color="secondary">{tr("密钥来源")}</Text>
    <SegmentedControl label={tr("密钥来源")} value={source} onChange={changeSource} layout="hug" size="md" isDisabled={busy}>
     <SegmentedControlItem value="file" label={tr("选择文件")}/><SegmentedControlItem value="text" label={tr("粘贴私钥")}/>
     <SegmentedControlItem value="generated" label={tr("生成密钥")}/>
    </SegmentedControl>
   </Stack></StackItem>
  </Stack></FormLayout>
  {source==='file'?<Stack gap={2}>
   <Stack direction="horizontal" align="end" gap={2}><StackItem size="fill"><TextInput label={tr("私钥文件")} size="md" value={selection?.name||''} isReadOnly placeholder={tr("尚未选择文件")} autoComplete="off"/></StackItem>
    <Button label={tr("选择私钥文件")} type="button" size="md" variant="secondary" isDisabled={busy||!gateway.available()} isLoading={picking} onClick={()=>void choose()}/></Stack>
  </Stack>:source==='generated'?<Stack gap={2}>
    <Stack direction="horizontal" align="center" wrap="wrap" gap={2}>
      <Button label={tr("生成 Ed25519 密钥")} type="button" size="md" variant="secondary" isLoading={picking}
        isDisabled={busy||!gateway.available()||!!selection} onClick={()=>void generate()}/>
      {selection?.publicKey&&<Button label={tr("复制公钥")} type="button" size="md" variant="ghost" isDisabled={busy}
        onClick={()=>void writeClipboardText(selection.publicKey!).then(()=>setCopied(true)).catch(()=>setError(tr("复制失败，请重试。")))}/>}
      {copied&&<Text type="supporting" role="status">{tr("已复制公钥")}</Text>}
    </Stack>
    {selection?.publicKey&&<TextArea label={tr("公钥")} rows={3} size="sm" value={selection.publicKey} isReadOnly controlStyle={{fontFamily:'var(--font-family-code)'}}/>}
    <Text type="supporting" color="secondary">{tr("保存后可在连接中选用该密钥。请将公钥添加到服务器的授权密钥列表。")}</Text>
  </Stack>:<TextArea label={tr("私钥内容")} htmlName="saved-key-content" rows={5} size="sm" value={privateKey} onChange={value=>{setPrivateKey(value);setError('')}} placeholder={tr("粘贴 OpenSSH 或 PEM 私钥")}
    maxLength={1048576} autoComplete="off" autoCapitalize="none" hasAutoCorrect={false} hasSpellCheck={false} isDisabled={busy}
    controlStyle={{fontFamily:'var(--font-family-code)'}}/>}
  {source!=='generated'&&<InputGroup label={tr("私钥口令（未加密可留空）")} size="md" isDisabled={busy}>
   <TextInput label={tr("认证内容")} htmlName="saved-key-passphrase" size="md" type={show?'text':'password'} value={passphrase} onChange={setPassphrase} autoComplete="off"
    hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" isDisabled={busy} controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent',marginInlineStart:0}}/>
   <IconButton label={show ? tr("隐藏私钥口令") : tr("显示私钥口令")} aria-pressed={show} type="button" size="md" variant="ghost" isDisabled={busy} icon={<Icon icon={show?EyeSlashIcon:EyeIcon} size="sm"/>} onClick={()=>setShow(value=>!value)}/>
  </InputGroup>}
  {error&&<Text type="supporting" role="alert" tabIndex={-1}>{localizeMessage(error)}</Text>}
 </EditorDialog>
}
