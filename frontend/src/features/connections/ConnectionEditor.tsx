import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { matchesShortcut } from '../workspace/shortcutReference'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextArea } from '@astryxdesign/core/TextArea'
import { TextInput } from '@astryxdesign/core/TextInput'
import { FieldStatus } from '@astryxdesign/core/FieldStatus'
import GroupCombobox from './GroupCombobox'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { Button } from '@astryxdesign/core/Button'
import {DropdownMenu} from '@astryxdesign/core/DropdownMenu'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {EllipsisHorizontalIcon} from '@heroicons/react/24/outline'
import { Tab, TabList } from '@astryxdesign/core/TabList'
import JumpHostsEditor from './JumpHostsEditor'
import ForwardRulesEditor from './ForwardRulesEditor'
import ProxyEditor from './ProxyEditor'
import { networkErrors } from './networkConfig'
import { desktopAvailable } from '../../shared/bridge/application'
import type { KeyReference } from '../../shared/bridge/keys'
import { validateHost, type HostProfile } from '../workspace/mockGateway'
import EditorDialog from '../../shared/ui/EditorDialog'
import OsIconPicker from './OsIconPicker'
import AuthenticationFields from './AuthenticationFields'
import AuthenticationSource, {type AuthenticationSourceValue} from './AuthenticationSource'
import { authenticationError, clearAuthentication, sameAuthenticationSource, useGeneratedKeyCleanup, credentialsFromDraft, emptyAuthentication, hasSavedPassword, type ConnectionPasswordSave } from './authenticationDraft'
import type { Credentials } from './useSSHSessions'
import DraftRevisionNotice from '../workspace/DraftRevisionNotice'
import { needsDraftReview, reviewDraft } from '../workspace/draftRevision'
import type { Identity } from './useIdentities'

export default function ConnectionEditor({ host: initialHost, initialRevision, revision, hosts = [], groups, identities = [], defaultGroup = '未分组', onClose, onConnectClosed, onEditorClosed, onSave, onQuickConnect, keys, choosingKey, keyError, onChooseKey, editable = true, storageError = '', isOpen = true, temporary = false }: {
  initialRevision?: number; revision?: number
  host?: HostProfile; defaultGroup?: string; onClose: () => void; onSave: (host: HostProfile, credentials?: Credentials, passwordSave?: ConnectionPasswordSave, expectedRevision?: number) => Promise<boolean>; editable?: boolean
  onQuickConnect?: (host:HostProfile,credentials:Credentials)=>Promise<boolean>
  onConnectClosed?: () => void
  onEditorClosed?: () => void
  groups: readonly string[]
  hosts?:readonly HostProfile[]
  identities?: Identity[]
  storageError?: string
  isOpen?: boolean
  temporary?: boolean
  keys: KeyReference[]; choosingKey: boolean; keyError: string; onChooseKey: () => Promise<KeyReference | null>
}) {
  useLocale()

  const [base, setBase] = useState({ host: initialHost, revision: initialRevision })
  const host = base.host
  const [initialForm] = useState<HostProfile>(() => initialHost ?? { id: crypto.randomUUID(), name: '', address: '', port: 22, username: 'root', group: defaultGroup, system: 'Linux', icon: 'linux', authentication: 'password' })
  const [form, setForm] = useState<HostProfile>(() => ({ ...initialForm, authentication: initialForm.authentication === 'key' ? 'key' : 'password' }))
  const latest = useMemo(() => !temporary && isOpen ? hosts.find(item => item.id === form.id) : undefined,
    [hosts, form.id, temporary, isOpen])
  const stale = needsDraftReview(base.revision, revision, !!host, !!latest)
  const [authentication, setAuthentication] = useState(() => emptyAuthentication(form))
  useGeneratedKeyCleanup(isOpen?authentication.generatedKey?.token:undefined)
  const [source,setSource]=useState<AuthenticationSourceValue>(()=>form.identityId?'identity':form.authentication==='key'?'key':'password')
  const [authError, setAuthError] = useState('')
  const [clearPassword,setClearPassword]=useState(false)
  const rememberPassword=!clearPassword||!!authentication.password
  const identity = identities.find(i => i.id === form.identityId)
  const [port, setPort] = useState(String(form.port))
  const [tab,setTab]=useState('basic')
  const [timeout,setTimeoutValue]=useState(String(form.timeoutSeconds||15))
  const [errors, setErrors] = useState<Partial<Record<keyof HostProfile, string>>>({})
  const formRef = useRef<HTMLFormElement>(null)
  const composing = useRef(false)
  const submitting = useRef(false)
  const connectedOnClose = useRef(false)
  const [saving, setSaving] = useState(false)
  const busy=saving
  const [savingConnect, setSavingConnect] = useState(false)
  const [proxyPassword,setProxyPassword] = useState('')
  const [saveError, setSaveError] = useState('')
  type SecretField = 'password' | 'privateKey' | 'passphrase' | 'proxyPassword'
  const [retryFields, setRetryFields] = useState<SecretField[]>([])
  const [focusRetry, setFocusRetry] = useState(false)
  const fieldLabels = { password: tr('登录密码'), privateKey: tr('私钥内容'), passphrase: tr('私钥口令'), proxyPassword: tr('代理密码') }
  const fieldValues = { password: authentication.password, privateKey: authentication.privateKey.trim(), passphrase: authentication.passphrase, proxyPassword }
  const missingFields = retryFields.filter(field => !fieldValues[field])
  const firstMissing = missingFields[0]
  useLayoutEffect(() => {
    if (!isOpen || !focusRetry || busy || stale || !firstMissing) return
    const page = firstMissing === 'proxyPassword' ? 'network' : 'basic'
    if (tab !== page) { setTab(page); return }
    const fieldName = firstMissing === 'privateKey' ? 'ssh-private-key' : firstMissing === 'proxyPassword' ? 'proxy-password' : 'ssh-secret'
    formRef.current?.querySelector<HTMLElement>(`[name="${fieldName}"]`)?.focus()
    setFocusRetry(false)
  }, [isOpen, focusRetry, busy, stale, firstMissing, tab])
  function resetAuthRetry() { setRetryFields(fields => fields.filter(field => field === 'proxyPassword')) }
  useLayoutEffect(() => { if (!isOpen) { setRetryFields([]); setFocusRetry(false); setProxyPassword(''); setAuthentication(draft=>({...clearAuthentication(draft),generatedKey:undefined,temporaryFile:undefined})); setAuthError('') } }, [isOpen])
  const update = <K extends keyof HostProfile>(field:K,value:HostProfile[K]) => { setForm(previous => ({ ...previous, [field]: value })); setErrors(previous => ({ ...previous, [field]: undefined })) }
  const changeTab=(next:string)=>{if(next===tab||busy||choosingKey)return;setTab(next)}
  function reviewLatest() {
    if (!latest) return
    const original = host ?? initialForm
    if (original.port !== latest.port) setPort(String(latest.port))
    if (original.timeoutSeconds !== latest.timeoutSeconds) setTimeoutValue(String(latest.timeoutSeconds || 15))
    if (!latest.proxy?.useAuthentication) setRetryFields(fields => fields.filter(field => field !== 'proxyPassword'))
    setProxyPassword('')
    setClearPassword(false)
    const authChanged = !host || !sameAuthenticationSource(host, latest)
    setForm(current => {
      const next = reviewDraft(original, current, latest)
      return authChanged ? { ...next, authentication: latest.authentication, identityId: latest.identityId,
        keyId: latest.keyId, useAgent: latest.useAgent } : next
    })
    if (authChanged) {
      resetAuthRetry()
      setSource(latest.identityId ? 'identity' : latest.authentication === 'key' ? 'key' : 'password')
      setAuthentication(emptyAuthentication(latest))
    } else {
      setAuthentication(clearAuthentication)
    }
    setErrors({})
    setAuthError('')
    setSaveError('')
    setBase({ host: latest, revision })
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (stale || !isOpen || composing.current || submitting.current || busy || choosingKey || !editable) return
    if (firstMissing) { setFocusRetry(true); return }
    const action=(event.nativeEvent as SubmitEvent).submitter?.getAttribute('value');const quick=temporary;const connect=quick||action==='connect'
    if(source==='identity'&&!identity){setAuthError(tr("请选择一个可用的已保存身份。"));changeTab('basic');return}
    if (connect && !desktopAvailable()) return
    if (form.identityId && !identity) { setAuthError(tr("关联身份不可用，请重新选择。"));changeTab('basic'); return }
    const candidate = { ...form, name: form.name.trim() || (quick?form.address.trim():''), address: form.address.trim(), username: identity?.username ?? form.username.trim(), group: form.group.trim(), authentication: identity ? (identity.authentication === 'key' ? 'key' as const : 'password' as const) : authentication.authentication, useAgent: !identity && authentication.authentication === 'key' && authentication.keySource === 'agent', keyId: identity ? undefined : authentication.authentication === 'key' && authentication.keySource === 'file' ? authentication.keyId : undefined, port: /^\d{1,5}$/.test(port.trim()) ? Number(port.trim()) : NaN, timeoutSeconds:/^\d{1,3}$/.test(timeout)?Number(timeout):NaN,jumpHostIds:form.jumpHostIds??[],forwards:(form.forwards??[]).map(r=>({...r,name:r.name.trim()})) }
    const nextErrors = {...validateHost(candidate),...networkErrors(candidate,hosts)}
    if(!Number.isInteger(candidate.timeoutSeconds)||candidate.timeoutSeconds<3||candidate.timeoutSeconds>120)nextErrors.timeoutSeconds=tr("连接超时应为 3–120 秒。")
    if(proxyPassword && new TextEncoder().encode(proxyPassword).length > (candidate.proxy?.kind==='socks5'?255:4096)) nextErrors.proxy=tr("代理密码长度无效。")
    if(groups.includes(candidate.group))delete nextErrors.group
    setErrors(nextErrors)
    if (Object.keys(nextErrors).length) { const first=Object.keys(nextErrors)[0];const page=first==='jumpHostIds'||first==='proxy'?'network':first==='forwards'?'forwards':['notes','timeoutSeconds'].includes(first??'')?'advanced':'basic';changeTab(page);requestAnimationFrame(() => (formRef.current?.querySelector<HTMLElement>(`#connection-panel-${page} [aria-invalid="true"]`)??document.getElementById(first==='proxy'?'connection-proxy-error':page==='forwards'?'connection-forward-error':`connection-${page}-error`))?.focus()); return }
    const authMessage = !identity && (connect || authentication.authentication==='key') ? authenticationError(authentication, keys) : ''
    setAuthError(authMessage)
    if (authMessage) {changeTab('basic');return}
    const credentials:Credentials|undefined = connect ? identity ? { authentication: identity.authentication === 'key' ? 'key' as const : 'password' as const, identityId: identity.id, keyId: '', password: '', passphrase: '', timeoutSeconds: candidate.timeoutSeconds } : {...credentialsFromDraft(authentication,candidate.timeoutSeconds), useSavedCredential: !temporary && source==='password' && rememberPassword && (authentication.password!=='' || hasSavedPassword(host,candidate))} : undefined
    const passwordSave:ConnectionPasswordSave={rememberPassword:!temporary&&source==='password'&&rememberPassword,password:!temporary&&source==='password'&&rememberPassword?authentication.password:'',
      privateKey:!temporary&&source==='key'&&authentication.keySource==='text'?authentication.privateKey.trim():undefined,
      proxyPassword:!temporary&&candidate.proxy?.useAuthentication?proxyPassword:undefined,
      generatedKeyToken:!temporary&&source==='key'&&authentication.keySource==='generated'?authentication.generatedKey?.token:undefined,
      keyToken:!temporary&&source==='key'?authentication.temporaryFile?.token:undefined,
      passphrase:!temporary&&source==='key'&&authentication.keySource!=='agent'?authentication.passphrase:undefined}
    if(credentials?.useSavedCredential)credentials.password=''
    submitting.current = true
    connectedOnClose.current = connect
    const submittedFields = (Object.keys(fieldValues) as SecretField[]).filter(field => !!fieldValues[field])
    setRetryFields([])
    const failed = () => {
      connectedOnClose.current = false
      setRetryFields(submittedFields)
      setFocusRetry(!!submittedFields.length)
      setSaveError(quick ? tr('连接未启动，请重试。') : tr('连接未保存。'))
    }
    setAuthentication(clearAuthentication)
    setSavingConnect(connect); setSaving(true); setSaveError('')
    try {
      if (!await (quick&&credentials&&onQuickConnect?onQuickConnect(candidate,credentials):onSave(candidate, credentials,passwordSave,base.revision))) {
        failed()
      }
    } catch { failed() }
    finally { setProxyPassword(''); passwordSave.proxyPassword=''; passwordSave.password='';passwordSave.privateKey='';passwordSave.passphrase='';setAuthentication(clearAuthentication); if (credentials) { credentials.password = ''; credentials.passphrase = ''; credentials.privateKey = '' }; submitting.current = false; setSaving(false) }
  }
  const status = (field: keyof HostProfile) => errors[field] ? { type: 'error' as const, message: localizeMessage(errors[field]) } : undefined
  return <EditorDialog isOpen={isOpen} busy={busy || choosingKey} onClose={onClose} onAfterClose={() => {
    if (connectedOnClose.current) { connectedOnClose.current = false; onConnectClosed?.() }
    else onEditorClosed?.()
  }} action={host ? 'edit' : 'add'} title={temporary?tr("临时连接"):host ? tr("编辑连接") : tr("新建连接")}
    closeId="connection-editor-close" closeLabel={tr("关闭连接编辑")} hint={temporary?tr("本次连接不保存配置"):undefined}
    startContent={<OsIconPicker value={form.icon} customIcons={hosts.map(host => host.icon)} disabled={busy || choosingKey || !isOpen} onChange={icon => setForm(previous => ({ ...previous, icon }))} />}
    navigation={<TabList role="tablist" aria-label={tr("连接配置")} value={tab} onChange={changeTab} size="md" hasDivider>
      {[['basic',tr("基本连接")],['network',tr("网络")],['forwards',tr("端口转发")],['advanced',tr("高级")]].map(([value,label])=><Tab key={value} id={`connection-tab-${value}`} value={value!} label={label!} panelId={`connection-panel-${value}`} onFocus={()=>changeTab(value!)} />)}
    </TabList>}
    formProps={{ ref: formRef, id: 'connection-editor', 'aria-label': host ? tr("编辑连接") : tr("新建连接"), onSubmit: submit,
      onCompositionStart: () => { composing.current = true }, onCompositionEnd: () => { composing.current = false }, onKeyDown: event => {
      if (event.nativeEvent.isComposing || composing.current || event.defaultPrevented) return
      if(temporary&&matchesShortcut(event.nativeEvent,'save')){event.preventDefault();return}
      if (matchesShortcut(event.nativeEvent,'submit')) { event.preventDefault(); if(!event.repeat) event.currentTarget.requestSubmit() }
      else if(event.key==='Enter'&&event.target instanceof HTMLInputElement){event.preventDefault();event.currentTarget.requestSubmit()}
    } }} actions={temporary?<Button label={tr("连接")} type="submit" value="quick" variant="primary" size="md" isLoading={saving} isDisabled={busy||choosingKey||!desktopAvailable()||!onQuickConnect}/>:<><Button label={tr("保存并连接")} type="submit" value="connect" variant="secondary" size="md" isLoading={saving && savingConnect} isDisabled={stale || busy || choosingKey || !editable || !desktopAvailable()} tooltip={!desktopAvailable() ? tr("请在桌面程序中连接 SSH") : undefined} />
      <Button label={tr("保存")} type="submit" value="save" variant="primary" size="md" isLoading={saving && !savingConnect} isDisabled={stale || busy || choosingKey || !editable} /></>}>
        <Stack gap={3} style={{minHeight:'min(calc(var(--spacing-10) * 9), 48dvh)'}}>
          <Stack id="connection-panel-basic" role="tabpanel" aria-labelledby="connection-tab-basic" gap={3} style={{display:tab==='basic'?'flex':'none'}} inert={tab!=='basic'}>
          <FormLayout defaultOptionality="required"><Stack gap={3}>
          <Stack direction="horizontal" gap={2} align="start" wrap="wrap">
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 4)' }}><TextInput id="connection-name" label={tr("连接名称")} isDisabled={busy} htmlName="name" autoComplete="off" size="md" width="100%" value={form.name} onChange={value => update('name', value)} hasAutoFocus isRequired status={status('name')} statusVariant="detached" /></StackItem>
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 4)' }}><GroupCombobox value={form.group} groups={groups} onChange={value=>update('group',value)} disabled={busy||!editable} active={isOpen&&tab==='basic'} error={errors.group}/></StackItem>
          </Stack>
          <Stack direction="horizontal" gap={2} align="start" wrap="wrap">
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 5)' }}><TextInput label={tr("主机地址")} isDisabled={busy} htmlName="address" autoComplete="off" size="md" width="100%" value={form.address} onChange={value => update('address', value)} isRequired hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" status={status('address')} statusVariant="detached" /></StackItem>
            <TextInput label={tr("端口")} isDisabled={busy} htmlName="port" autoComplete="off" size="md" width="calc(var(--spacing-10) * 2)" containerStyle={{ flexShrink: 0 }} value={port} onChange={value => { setPort(value); setErrors(previous => ({ ...previous, port: undefined })) }} inputMode="numeric" isRequired status={errors.port ? { type: 'error' } : undefined} aria-describedby={errors.port ? 'connection-port-error' : undefined} />
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 3)' }}><TextInput label={tr("用户名")} isDisabled={busy || source==='identity'} htmlName="username" size="md" width="100%" value={identity?.username ?? form.username} onChange={value => update('username', value)} isRequired autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" status={status('username')} statusVariant="detached" /></StackItem>
          </Stack>
          {errors.port && <FieldStatus id="connection-port-error" type="error" message={errors.port} variant="detached" />}
          </Stack></FormLayout>
          <AuthenticationSource value={source} identityId={form.identityId??''} identities={identities} selectorId="connection-identity" disabled={busy||choosingKey||!editable} error={source==='identity'?authError:''}
            onChange={next=>{resetAuthRetry();setSource(next);update('identityId','');setClearPassword(false);setAuthentication(previous=>({...clearAuthentication(previous),temporaryFile:undefined,generatedKey:undefined,authentication:next==='key'?'key':'password'}));setAuthError('')}}
            onIdentityChange={id=>{update('identityId',id);setAuthentication(clearAuthentication);setAuthError('')}}/>
          {source==='identity' ? identity&&<Text type="supporting" color="secondary" role="status">{tr("使用「{p0}」的用户名与认证内容。", identity.name)}</Text> : <>
            <AuthenticationFields key={source} hideMethod value={authentication} onChange={next => { if(next.keySource!==authentication.keySource || next.keyId!==authentication.keyId || next.temporaryFile?.token!==authentication.temporaryFile?.token || next.generatedKey?.token!==authentication.generatedKey?.token)resetAuthRetry(); setAuthentication(next);if(next.password)setClearPassword(false); setAuthError('') }} keys={keys}
              choosingKey={choosingKey} keyError={localizeMessage(keyError)} onChooseKey={onChooseKey} disabled={busy || !editable} error={localizeMessage(authError)} active={isOpen&&tab==='basic'} secretPlaceholder={!temporary&&source==='password'&&hasSavedPassword(host,{...form,port:Number(port)})?(clearPassword?tr('保存后清除密码'):tr("已保存密码，留空保持不变")):undefined}
              secretActions={!temporary&&source==='password'&&hasSavedPassword(host,{...form,port:Number(port)})?<DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} label={tr('密码操作')} tooltip={tr('密码操作')} type="button" size="md" variant="ghost" isDisabled={busy||!editable} icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={[{label:clearPassword?tr('保留已保存密码'):tr('清除已保存密码'),onClick:()=>{setClearPassword(!clearPassword);resetAuthRetry();setAuthentication(clearAuthentication)}}]}/>:undefined}
              allowFileImport={false} allowGenerate={!temporary} />
          </>}
          </Stack>
          <Stack id="connection-panel-network" role="tabpanel" aria-labelledby="connection-tab-network" gap={3} style={{display:tab==='network'?'flex':'none'}} inert={tab!=='network'}>
            <ProxyEditor value={form.proxy} disabled={busy||!editable} error={errors.proxy} password={proxyPassword} onPassword={temporary?undefined:value=>{setProxyPassword(value);setErrors(previous=>({...previous,proxy:undefined}))}} onChange={proxy=>{setRetryFields(fields=>fields.filter(field=>field!=='proxyPassword'));setProxyPassword('');update('proxy',proxy)}}/>
            <JumpHostsEditor host={form} hosts={hosts} disabled={busy||!editable} onChange={ids=>update('jumpHostIds',ids)}/>
            {errors.jumpHostIds&&<Text id="connection-network-error" type="supporting" role="alert" tabIndex={-1}>{errors.jumpHostIds}</Text>}
          </Stack>
          <Stack id="connection-panel-forwards" role="tabpanel" aria-labelledby="connection-tab-forwards" style={{display:tab==='forwards'?'flex':'none'}} inert={tab!=='forwards'}>
            <ForwardRulesEditor rules={form.forwards??[]} disabled={busy||!editable} error={errors.forwards} onChange={rules=>update('forwards',rules)}/>
          </Stack>
          <Stack id="connection-panel-advanced" role="tabpanel" aria-labelledby="connection-tab-advanced" gap={3} style={{display:tab==='advanced'?'flex':'none'}} inert={tab!=='advanced'}>
            <TextInput label={tr("连接超时（秒）")} size="md" width="calc(var(--spacing-10) * 4)" value={timeout} onChange={value=>{setTimeoutValue(value);setErrors(previous=>({...previous,timeoutSeconds:undefined}))}} inputMode="numeric" autoComplete="off" isDisabled={busy} status={status('timeoutSeconds')} statusVariant="detached"/>
            <Text type="supporting" color="secondary">{tr("3–120 秒，默认 15 秒；每台跳板机使用其自身的超时配置。")}</Text>
            <TextArea isDisabled={busy} label={tr("备注")} value={form.notes??''} onChange={value=>update('notes',value)} size="md" rows={3} maxLength={2048} autoComplete="off" status={status('notes')} statusVariant="detached"/>
          </Stack>
          {stale && <DraftRevisionNotice onReview={latest ? reviewLatest : undefined} busy={busy || choosingKey || !editable} />}
          {!stale && saveError && <Text type="supporting" role="alert">{localizeMessage(storageError || saveError)}</Text>}
          {!!missingFields.length && <Text type="supporting" role="alert">
            {tr('其他配置已保留，请重新输入：{p0}。', new Intl.ListFormat(getLanguage()).format(missingFields.map(field => fieldLabels[field])))}
          </Text>}
          {Object.values(errors).some(Boolean) && <Text type="supporting" role="status">{tr("请修正标出的字段后再保存。")}</Text>}
        </Stack>
  </EditorDialog>
}
