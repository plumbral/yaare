import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react'

import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Layout, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { List, ListItem } from '@astryxdesign/core/List'
import { Tab, TabList } from '@astryxdesign/core/TabList'
import ManagementToolbar, {managementSearchStyle} from '../../shared/ui/ManagementToolbar'
import { CheckIcon, ClipboardDocumentIcon, LockClosedIcon, KeyIcon, PlusIcon, PencilSquareIcon, TrashIcon, XMarkIcon } from '@heroicons/react/24/outline'
import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'
import EditorDialog from '../../shared/ui/EditorDialog'
import ConfirmDelete from '../../shared/ui/ConfirmDelete'
import AuthenticationFields from './AuthenticationFields'
import KeyEditor from './KeyEditor'
import DraftRevisionNotice from '../workspace/DraftRevisionNotice'
import { authenticationError, clearAuthentication, emptyAuthentication, useGeneratedKeyCleanup } from './authenticationDraft'
import type { HostProfile } from '../workspace/mockGateway'
import { copyKeyPrivate, readKeyPublic, type KeyReference } from '../../shared/bridge/keys'
import { desktopAvailable, writeClipboardText } from '../../shared/bridge/application'
import type { Identity, SaveIdentityRequest, useIdentities } from './useIdentities'

function CopyKeyButton({ keyId, kind, disabled, onError }: {
 keyId: string; kind: 'private' | 'public'; disabled: boolean; onError: (message: string) => void
}) {
 const [copied, setCopied] = useState(false)
 const reset = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
 const attempt = useRef(0)
 useEffect(() => () => {
  attempt.current++
  clearTimeout(reset.current)
 }, [])
 const label = kind === 'private' ? tr("复制私钥") : tr("复制公钥")
 const success = kind === 'private' ? tr("已复制私钥") : tr("已复制公钥")
 async function copy() {
  const current = ++attempt.current
  onError('')
  try {
   if (kind === 'private') await copyKeyPrivate(keyId)
   else await writeClipboardText(await readKeyPublic(keyId))
   if (current !== attempt.current) return
   clearTimeout(reset.current)
   setCopied(true)
   reset.current = setTimeout(() => setCopied(false), 2500)
  } catch (cause) {
   if (current !== attempt.current) return
   clearTimeout(reset.current)
   setCopied(false)
   onError(cause instanceof Error ? cause.message : tr("复制失败，请重试。"))
  }
 }
 return <>
  <IconButton type="button" label={label} tooltip={copied ? success : label} size="sm"
    variant={copied ? 'secondary' : 'ghost'} isDisabled={disabled}
    icon={<Icon icon={copied ? CheckIcon : kind === 'private' ? LockClosedIcon : ClipboardDocumentIcon} size="sm"/>}
    onClick={() => void copy()}/>
  <VisuallyHidden role="status">{copied ? success : ''}</VisuallyHidden>
 </>
}

export default function IdentityPanel({store,keys,onChooseKey,onForgetKey,onKeyAdded,onKeysChanged,keyBusy,keyError,keyUsage,hosts,active,caption}: {
 store:ReturnType<typeof useIdentities>;keys:KeyReference[];onChooseKey:()=>Promise<KeyReference|null>;onForgetKey:(id:string)=>Promise<boolean>
 keyBusy:boolean;keyError:string;keyUsage:(id:string)=>number;hosts:HostProfile[];active:boolean
 onKeyAdded:(key:KeyReference)=>Promise<void>
 onKeysChanged:()=>Promise<void>
 caption?:HTMLElement|null
}){
  useLocale()

 const [tab,setTab]=useState<'identities'|'keys'>('keys')
 const [queries,setQueries]=useState({identities:'',keys:''})
 const identityViewport=useRef<HTMLElement>(null);const keyViewport=useRef<HTMLElement>(null)
 const scrollPositions=useRef({identities:0,keys:0})
 const [editor,setEditor]=useState<Identity|null>(null);const [deleting,setDeleting]=useState<Identity|null>(null)
 const [editorOpen,setEditorOpen]=useState(false);const [editorRevision,setEditorRevision]=useState(0);const [deleteOpen,setDeleteOpen]=useState(false)
 const savedTarget = useRef<string | undefined>(undefined)
 const revealSaved = () => {
   const id = savedTarget.current
   savedTarget.current = undefined
   if (!id || !active) return
   const target = document.getElementById(id)
   if (!target?.getClientRects().length || document.querySelector('dialog[open]')) return
   target.scrollIntoView({block:'nearest'})
   target.focus({preventScroll:true})
 }
 const [message,setMessage]=useState('')
 const [keyEditorOpen,setKeyEditorOpen]=useState(false);const [keyEditorRevision,setKeyEditorRevision]=useState(0)
 const [deletingKey,setDeletingKey]=useState<KeyReference|null>(null);const [keyDeleteOpen,setKeyDeleteOpen]=useState(false)
 const [keyMessage,setKeyMessage]=useState('');const [keyActionError,setKeyActionError]=useState('');const [removing,setRemoving]=useState('');const keyPending=useRef(false)
 const hostCounts=useMemo(()=>{const counts=new Map<string,number>();for(const host of hosts)if(host.identityId)counts.set(host.identityId,(counts.get(host.identityId)||0)+1);return counts},[hosts, getLanguage()])
 const keyCounts=useMemo(()=>{const counts=new Map<string,number>();for(const identity of store.items)if(identity.keyId)counts.set(identity.keyId,(counts.get(identity.keyId)||0)+1);return counts},[store.items, getLanguage()])
 useEffect(()=>{if(!active){setEditorOpen(false);setDeleteOpen(false);setKeyEditorOpen(false);setKeyDeleteOpen(false)}},[active])
 useLayoutEffect(()=>{if(active){const viewport=tab==='identities'?identityViewport.current:keyViewport.current;if(viewport)viewport.scrollTop=scrollPositions.current[tab]}},[tab,active])
 const changeTab=(next:string)=>{if(next!==tab&&(next==='identities'||next==='keys')){const viewport=tab==='identities'?identityViewport.current:keyViewport.current;scrollPositions.current[tab]=viewport?.scrollTop||0;setTab(next)}}
 const changeQuery=(value:string)=>{scrollPositions.current[tab]=0;const viewport=tab==='identities'?identityViewport.current:keyViewport.current;if(viewport)viewport.scrollTop=0;setQueries(previous=>({...previous,[tab]:value}))}
 const openEditor=(identity:Identity)=>{setEditor(identity);setEditorRevision(value=>value+1);setEditorOpen(true)}
 const filtered=store.items.filter(i=>`${i.name} ${i.username}`.toLowerCase().includes(queries.identities.trim().toLowerCase()))
 const filteredKeys=keys.filter(key=>`${key.name} ${key.format}`.toLowerCase().includes(queries.keys.trim().toLowerCase()))
 const keyDisabled=keyBusy||!!removing
 const addKey=()=>{setKeyEditorRevision(value=>value+1);setKeyEditorOpen(true)}
 const removeKey=async(key:KeyReference)=>{
  if(keyPending.current||keyDisabled)return
  keyPending.current=true;setRemoving(key.id);setKeyMessage('');setKeyActionError('');document.getElementById('identity-key-search')?.focus({preventScroll:true})
  try{if(await onForgetKey(key.id)){setKeyDeleteOpen(false);setKeyMessage(key.source==='stored'?tr("已删除保存的密钥「{p0}」", key.name):tr("已移除「{p0}」的引用，原文件未删除。", key.name))}else setKeyActionError(tr("密钥未移除，请重试。"))}
  catch{setKeyActionError(tr("密钥未移除，请重试。"))}
  finally{setRemoving('');keyPending.current=false}
 }
 const requestKeyRemoval=(key:KeyReference)=>{if(key.source!=='stored'){void removeKey(key);return}document.getElementById('identity-key-search')?.focus({preventScroll:true});setKeyActionError('');setDeletingKey(key);setKeyDeleteOpen(true)}
 return <><Layout height="fill" header={<ManagementToolbar title={tr("身份与密钥管理")} caption={caption}
  search={<TextInput key={tab} id={tab==='identities'?'identity-search':'identity-key-search'} label={tab==='identities'?tr("搜索身份"):tr("搜索密钥")} isLabelHidden size="md" type="search"
      placeholder={tab==='identities'?tr("搜索名称或用户名…"):tr("搜索名称或格式…")} value={queries[tab]} onChange={changeQuery} startIcon="search" hasClear autoComplete="off"
      width="100%" containerStyle={managementSearchStyle}/>}
  navigation={<TabList role="tablist" aria-label={tr("身份与密钥")} value={tab} onChange={changeTab} size="md" style={{maxWidth:'100%'}}>
    <Tab id="identity-mode-keys" value="keys" label={tr("密钥")} panelId="identity-key-panel" onFocus={()=>changeTab('keys')}/>
    <Tab id="identity-mode-identities" value="identities" label={tr("身份")} panelId="identity-list-panel" onFocus={()=>changeTab('identities')}/>
  </TabList>}>
    {compact=>tab==='identities'?<Button id="identity-add" label={tr("新建身份")} tooltip={tr("新建身份")} size="md" variant="secondary" isIconOnly={compact} icon={<Icon icon={PlusIcon} size="sm"/>} isDisabled={store.busy} onClick={()=>openEditor({id:crypto.randomUUID(),version:0,name:'',username:'root',authentication:'password',keyId:''})}/>
      :<Button id="identity-key-add" label={tr("添加密钥")} tooltip={tr("添加密钥")} size="md" variant="secondary" isIconOnly={compact} icon={<Icon icon={PlusIcon} size="sm"/>} isDisabled={keyDisabled} onClick={addKey}/>}
 </ManagementToolbar>}
 content={<LayoutContent padding={0} isScrollable={false} style={{minHeight:0}}>
 <Stack ref={identityViewport} id="identity-list-panel" role="tabpanel" aria-labelledby="identity-mode-identities" hidden={tab!=='identities'} tabIndex={filtered.length?undefined:0}
  height="100%" style={{display:tab==='identities'?'flex':'none',minHeight:0,overflow:'auto',scrollbarWidth:'thin'}}
  onScroll={event=>{if(active&&tab==='identities')scrollPositions.current.identities=event.currentTarget.scrollTop}}>
  <Stack padding={3} gap={3} height={filtered.length?'auto':'100%'} style={{minHeight:0,flexShrink:0}}>
  {store.error&&<Stack gap={2}><Text role="alert">{localizeMessage(store.error)}</Text><Button label={tr("重新读取身份")} size="sm" variant="secondary" onClick={()=>void store.reload()}/></Stack>}
  {filtered.length>0&&<List aria-label={tr("身份列表")} density="compact">{filtered.map(i=><ListItem key={i.id} label={i.name} description={tr("{p0} · {p1} · {p2} 台主机关联", i.username, i.authentication==='key'?tr("私钥"):tr("密码"), hostCounts.get(i.id)||0)}
   startContent={<Icon icon={KeyIcon} size="sm"/>} endContent={<Stack direction="horizontal" gap={1}>
    <IconButton id={`identity-edit-${i.id}`} label={tr("编辑身份 {p0}", i.name)} tooltip={tr("编辑身份")} size="sm" variant="ghost" icon={<Icon icon={PencilSquareIcon} size="sm"/>} isDisabled={store.busy} onClick={()=>openEditor(i)}/>
    <IconButton label={tr("删除身份 {p0}", i.name)} tooltip={hostCounts.get(i.id)?tr("请先解除主机关联"):tr("删除身份")} size="sm" variant="ghost" isDisabled={store.busy||!!hostCounts.get(i.id)} icon={<Icon icon={TrashIcon} size="sm"/>} onClick={()=>{document.getElementById('identity-add')?.focus({preventScroll:true});setDeleting(i);setDeleteOpen(true)}}/>
   </Stack>}/>)}</List>}
  {!filtered.length&&<Stack padding={6} gap={2} align="center" justify="center" height="100%"><Icon icon={KeyIcon} size="lg" color="secondary"/><Text type="label">{queries.identities.trim()?tr("没有匹配的身份"):tr("还没有身份")}</Text><Text type="supporting" color="secondary">{queries.identities.trim()?tr("尝试其他名称或用户名。"):tr("新建身份后，可在多台主机间复用。")}</Text></Stack>}
  </Stack>
 </Stack>
 <Stack ref={keyViewport} id="identity-key-panel" role="tabpanel" aria-labelledby="identity-mode-keys" hidden={tab!=='keys'} tabIndex={filteredKeys.length?undefined:0}
  height="100%" style={{display:tab==='keys'?'flex':'none',minHeight:0,overflow:'auto',scrollbarWidth:'thin'}}
  onScroll={event=>{if(active&&tab==='keys')scrollPositions.current.keys=event.currentTarget.scrollTop}}>
  <Stack padding={3} gap={3} height={filteredKeys.length?'auto':'100%'} style={{minHeight:0,flexShrink:0}}>
   {(keyActionError||keyError)&&<Text role="alert">{localizeMessage(keyError||keyActionError)}</Text>}
   {filteredKeys.length>0&&<List aria-label={tr("密钥文件引用")} density="compact">{filteredKeys.map(k=>{
    const identities=keyCounts.get(k.id)||0,uses=keyUsage(k.id),inUse=!!identities||!!uses
    return <ListItem key={k.id} id={`saved-key-${k.id}`} tabIndex={-1} label={k.name} description={tr("{p0} · {p1} · {p2} · {p3} 个身份关联{p4}", k.format, k.source==='stored'?tr("加密保存"):tr("文件引用"), k.available?tr("可用"):tr("文件不可用"), identities, uses?tr(" · 连接或会话使用中"):'')} startContent={<Icon icon={KeyIcon} size="sm"/>}
     endContent={<Stack direction="horizontal" gap={1}>
      {k.source==='stored'&&desktopAvailable()&&<CopyKeyButton keyId={k.id} kind="private" disabled={!k.available} onError={setKeyActionError}/>}
      {k.source==='stored'&&<CopyKeyButton keyId={k.id} kind="public" disabled={!k.available} onError={setKeyActionError}/>}
      <IconButton label={`${k.source==='stored'?tr("删除密钥"):tr("移除密钥引用")} ${k.name}`} tooltip={inUse?tr("请先解除身份、连接和会话中的关联"):k.source==='stored'?tr("删除保存的密钥"):tr("移除引用，保留原文件")} size="sm" variant="ghost" isDisabled={keyDisabled||inUse} icon={<Icon icon={k.source==='stored'?TrashIcon:XMarkIcon} size="sm"/>} onClick={()=>requestKeyRemoval(k)}/>
     </Stack>}/>
   })}</List>}
   {!filteredKeys.length&&<Stack padding={6} gap={2} align="center" justify="center" height="100%"><Icon icon={KeyIcon} size="lg" color="secondary"/><Text type="label">{queries.keys.trim()?tr("没有匹配的密钥"):tr("还没有密钥")}</Text><Text type="supporting" color="secondary">{queries.keys.trim()?tr("尝试其他名称或格式。"):tr("选择本地文件或粘贴私钥，供连接和身份使用。")}</Text></Stack>}
  </Stack>
 </Stack>
 </LayoutContent>} footer={(tab==='keys'?keyMessage:message)?<LayoutFooter padding={3}><Text type="supporting" color="secondary" role="status">{localizeMessage(tab==='keys'?keyMessage:message)}</Text></LayoutFooter>:undefined}/>
 <KeyEditor onAfterClose={revealSaved} key={keyEditorRevision} isOpen={keyEditorOpen} onClose={()=>setKeyEditorOpen(false)} onSaved={async key=>{await onKeyAdded(key);savedTarget.current = `saved-key-${key.id}`;if (!`${key.name} ${key.format}`.toLowerCase().includes(queries.keys.trim().toLowerCase())) changeQuery('');setKeyMessage(tr("已添加密钥「{p0}」", key.name))}}/>
 {deletingKey&&<ConfirmDelete title={tr("删除密钥「{p0}」？", deletingKey.name)} description={tr("从密钥库移除保存的私钥和口令，原文件不受影响。")} isOpen={keyDeleteOpen} busy={!!removing} error={localizeMessage(keyActionError||keyError)}
  onClose={()=>setKeyDeleteOpen(false)} onConfirm={()=>void removeKey(deletingKey)}/>}
 {editor&&<IdentityEditor onAfterClose={revealSaved} key={editorRevision} isOpen={editorOpen} identity={editor} store={store} keys={keys} onChooseKey={onChooseKey} keyBusy={keyBusy} keyError={localizeMessage(keyError)} onSaved={(name,username,generated)=>{savedTarget.current = `identity-edit-${editor.id}`;if (!`${name} ${username}`.toLowerCase().includes(queries.identities.trim().toLowerCase())) changeQuery('');if(generated)void onKeysChanged();setMessage(tr("已保存身份「{p0}」", name))}} onClose={()=>setEditorOpen(false)}/>}
 {deleting&&<ConfirmDelete title={tr("删除身份「{p0}」？", deleting.name)} description={tr("删除保存的身份和认证内容，已连接的会话保持原状。原私钥文件不会删除。")} isOpen={deleteOpen} busy={store.busy} error={localizeMessage(store.error)} onClose={()=>setDeleteOpen(false)} onConfirm={()=>void store.remove(deleting).then(ok=>{if(ok){setDeleteOpen(false);setMessage(tr("已删除身份。"))}})}/>}
 </>
}
function IdentityEditor({identity: original,store,keys,keyBusy,keyError,onChooseKey,onClose,onAfterClose,onSaved,isOpen}:{identity:Identity;store:ReturnType<typeof useIdentities>;keys:KeyReference[];keyBusy:boolean;keyError:string;onChooseKey:()=>Promise<KeyReference|null>;onClose:()=>void;onAfterClose:()=>void;onSaved:(name:string,username:string,generated:boolean)=>void;isOpen:boolean}){
  useLocale()

 const [identity, setIdentity] = useState(original)
 const latest = store.items.find(item => item.id === identity.id)
 const stale = latest ? latest.version !== identity.version : identity.version > 0
 const [name,setName]=useState(identity.name);const [username,setUsername]=useState(identity.username)
 const [draft,setDraft]=useState(()=>emptyAuthentication({...identity,authentication:identity.authentication==='key'?'key':'password'}))
 useGeneratedKeyCleanup(isOpen?draft.generatedKey?.token:undefined)
 const [replace,setReplace]=useState(!identity.version);const [error,setError]=useState('')
 const pending=useRef(false);const [saving,setSaving]=useState(false)
 useEffect(()=>{if(!isOpen){setDraft(previous=>({...clearAuthentication(previous),generatedKey:undefined}));setError('')}},[isOpen])
 const busy=store.busy||keyBusy||saving
 async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();if(stale||busy||pending.current)return
  const message=!name.trim()||name.trim().length>64?tr("请输入 1–64 个字符的身份名称。"):!username.trim()||username.trim().length>64?tr("请输入有效用户名。"):replace?(draft.authentication==='password'&&!draft.password?tr("请输入登录密码。"):authenticationError(draft,keys)):''
  if(message){setError(message);const form=event.currentTarget;requestAnimationFrame(()=>form.querySelector<HTMLElement>('[aria-invalid="true"], [role="alert"]')?.focus());return}
  const request:SaveIdentityRequest={id:identity.id,version:identity.version,name:name.trim(),username:username.trim(),authentication:draft.authentication,keyId:draft.authentication==='key'&&draft.keySource==='file'?draft.keyId:'',useAgent:draft.authentication==='key'&&draft.keySource==='agent',replaceSecret:replace,generatedKeyToken:draft.keySource==='generated'?draft.generatedKey?.token:undefined,password:draft.password,privateKey:draft.keySource==='text'?draft.privateKey:'',passphrase:draft.passphrase}
  pending.current=true;setSaving(true);setDraft(clearAuthentication)
  try{const ok=await store.save(request);if(ok){onSaved(request.name,request.username,!!request.generatedKeyToken);onClose()}else setError(replace && !request.generatedKeyToken?tr("身份未保存，名称和用户名已保留；请重新输入认证内容后重试。"):tr("身份未保存，名称和用户名已保留；请重试。"))}
  catch{setError(replace && !request.generatedKeyToken?tr("身份保存失败，名称和用户名已保留；请重新输入认证内容后重试。"):tr("身份保存失败，名称和用户名已保留；请重试。"))}
  finally{request.password='';request.privateKey='';request.passphrase='';pending.current=false;setSaving(false)}
 }
 return <EditorDialog isOpen={isOpen} onAfterClose={onAfterClose} busy={saving} onClose={onClose} action={identity.version?'edit':'add'} title={identity.version?tr("编辑身份"):tr("新建身份")} closeLabel={tr("关闭身份编辑")}
 subtitle={tr("用户名与认证方式可供多台主机共用，端口在连接中设置。")} formProps={{'aria-label':tr("身份编辑"),onSubmit:submit,onKeyDown:e=>{if(e.key==='Enter'&&(e.ctrlKey||e.metaKey)&&!e.nativeEvent.isComposing){e.preventDefault();e.currentTarget.requestSubmit()}}}}
 actions={<Button label={tr("保存身份")} type="submit" variant="primary" size="md" isDisabled={busy || stale} isLoading={saving}/> }>
 <FormLayout defaultOptionality="required"><Stack direction="horizontal" gap={3} wrap="wrap">
 <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)'}}><TextInput label={tr("身份名称")} value={name} onChange={v=>{setName(v);setError('')}} isRequired hasAutoFocus status={error.includes(tr("身份名称"))?{type:'error',message:localizeMessage(error)}:undefined} statusVariant="detached" size="md" isDisabled={busy} autoComplete="off"/></StackItem>
 <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)'}}><TextInput label={tr("用户名")} value={username} onChange={v=>{setUsername(v);setError('')}} isRequired status={error.includes(tr("用户名"))?{type:'error',message:localizeMessage(error)}:undefined} statusVariant="detached" size="md" isDisabled={busy} autoComplete="off"/></StackItem>
 </Stack></FormLayout>
 {!replace?<Stack gap={2}><Text type="supporting" color="secondary">{tr("保留已保存的")}{identity.useAgent?'SSH Agent':identity.authentication==='key'?tr("私钥认证"):tr("密码")}{tr("，不会回填秘密。")}</Text><Button label={tr("更新认证内容")} size="md" variant="secondary" onClick={()=>setReplace(true)}/></Stack>:<AuthenticationFields value={draft} onChange={next=>{setDraft(next);setError('')}} keys={keys} choosingKey={keyBusy} keyError={localizeMessage(keyError)} onChooseKey={onChooseKey} disabled={busy} active={isOpen} error={localizeMessage(error)} allowGenerate/>}
 {stale && <DraftRevisionNotice busy={busy} onReview={latest ? () => {
   if (identity.name !== latest.name) setName(latest.name)
   if (identity.username !== latest.username) setUsername(latest.username)
   if (!identity.version || identity.authentication !== latest.authentication || identity.keyId !== latest.keyId || !!identity.useAgent !== !!latest.useAgent) {
     setDraft(emptyAuthentication({ ...latest, authentication: latest.authentication === 'key' ? 'key' : 'password' }))
     setReplace(false)
   } else setDraft(clearAuthentication)
   setIdentity(latest)
   setError('')
 } : undefined} />}
 {!stale && (error||store.error)&&<Text role="alert" tabIndex={-1}>{localizeMessage(error||store.error)}</Text>}
 </EditorDialog>
}
