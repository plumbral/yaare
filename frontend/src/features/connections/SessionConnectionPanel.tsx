import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { matchesShortcut } from '../workspace/shortcutReference'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { Layout, LayoutHeader, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Stack } from '@astryxdesign/core/Stack'
import { Text, Heading } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { Spinner } from '@astryxdesign/core/Spinner'
import { InputGroup } from '@astryxdesign/core/InputGroup'
import { XMarkIcon, EyeIcon, EyeSlashIcon } from '@heroicons/react/24/outline'
import ActionDialogHeader from '../../shared/ui/ActionDialogHeader'
import type { Identity } from './useIdentities'
import { desktopAvailable } from '../../shared/bridge/application'
import type { KeyReference } from '../../shared/bridge/keys'
import type { HostProfile } from '../workspace/mockGateway'
import type { Credentials, LiveSSH } from './useSSHSessions'
import AuthenticationFields from './AuthenticationFields'
import AuthenticationSource, {type AuthenticationSourceValue} from './AuthenticationSource'
import { authenticationError, clearAuthentication, credentialsFromDraft, emptyAuthentication } from './authenticationDraft'

export default function SessionConnectionPanel({ host, live, keys, keyError, active, onChooseKey, onConnect, onDisconnect, identities = [], onClose, allowFileImport=true }: {
  active: boolean
  host: HostProfile; live?: LiveSSH; keys: KeyReference[]; keyError: string; onChooseKey: () => Promise<KeyReference | null>
  onConnect: (credentials: Credentials) => Promise<boolean>; allowFileImport?:boolean; onDisconnect: () => void
  identities?: Identity[]; onClose?:()=>void
}) {
  useLocale()

  const [answers,setAnswers]=useState<string[]>([])
  useLayoutEffect(()=>{setAnswers([])},[live?.authentication?.id,active])
  const [authentication, setAuthentication] = useState(() => emptyAuthentication(host))
  const [identityId,setIdentityId] = useState(host.identityId || '')
  const [source,setSource]=useState<AuthenticationSourceValue>(host.identityId?'identity':host.authentication==='key'?'key':'password')
  const identity = identities.find(i=>i.id===identityId)
  const [timeout, setTimeoutValue] = useState(String(host.timeoutSeconds||15))
  const [error, setError] = useState('')
  const [errorField, setErrorField] = useState<'keyId' | 'timeout' | null>(null)
  const [busy, setBusy] = useState(false)
  const composing = useRef(false)
  const submitting = useRef(false)
  const form = useRef<HTMLFormElement>(null)
  const challengeFocus = useRef<HTMLHeadingElement>(null)
  const phase = live?.phase ?? 'idle'
  const challenge = phase === 'hostkey' ? live?.challenge : undefined
  const changed = !!challenge?.previousFingerprints?.length
  const pending = phase === 'connecting' || phase === 'hostkey'||phase==='authentication'
  useEffect(() => { submitting.current = pending; if (pending || phase === 'connected') setAuthentication(clearAuthentication) }, [pending, phase])
  useLayoutEffect(() => { if (!active) setAuthentication(draft => ({...clearAuthentication(draft), temporaryFile:undefined, generatedKey:undefined})) }, [active])
  useEffect(() => { if (active && live?.challenge) challengeFocus.current?.focus({ preventScroll: true }) }, [active, live?.challenge?.id])
  useLayoutEffect(() => { if (active && errorField) form.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus({ preventScroll: true }) }, [active, errorField, error])
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!active) return
    if(phase==='authentication'&&live?.authentication){if(busy||composing.current)return;setBusy(true);const values=live.authentication.prompts.map((_,i)=>answers[i]??'');setAnswers([]);void live.transport.answerAuthentication(live.authentication.id,values).then(ok=>{if(!ok)setError(tr("认证请求已结束，请重新连接。"))}).finally(()=>setBusy(false));return}
    if (composing.current || submitting.current || busy || !desktopAvailable()) return
    const seconds = Number(timeout)
    if (!/^\d{1,3}$/.test(timeout) || seconds < 3 || seconds > 120) { setError(tr("连接超时应为 3–120 秒。")); setErrorField('timeout'); return }
    const authMessage = source==='identity' ? !identity?tr("身份不可用，请重新选择。"):'' : authenticationError(authentication, keys)
    if (authMessage) { setError(authMessage); setErrorField('keyId'); return }
    submitting.current = true; setError(''); setErrorField(null)
    const credentials:Credentials=identity ? {authentication:identity.authentication==='key'?'key':'password',identityId:identity.id,keyId:'',password:'',passphrase:identity.useAgent?'':authentication.passphrase,timeoutSeconds:seconds} : {...credentialsFromDraft(authentication, seconds),useSavedCredential:source==='password'&&host.authentication==='password'&&!host.identityId&&!!host.hasCredential&&!authentication.password}
    setBusy(true)
    setAuthentication(clearAuthentication)
    try {
      if(!await onConnect(credentials)){submitting.current=false;setError(tr("连接未保存，编辑内容已保留。请查看配置提示后重试。"))}
    } catch(cause) {
      submitting.current=false;setError(cause instanceof Error?cause.message:tr("保存失败，编辑内容已保留，请重试。"))
    } finally {
      credentials.password='';credentials.privateKey='';credentials.passphrase='';setBusy(false)
    }
  }
  const decide = async (decision: 'once' | 'remember' | 'replace') => {
    if (!active || !live?.challenge || busy) return
    setBusy(true); setError('')
    if (!await live.transport.confirm(live.challenge.id, decision)) setError(tr("此指纹请求已结束，请取消后重新连接。"))
    setBusy(false)
  }
  return <form ref={form} id="session-auth-form" aria-label={tr("SSH 连接")} autoComplete="off" noValidate onSubmit={submit}
    onCompositionStart={() => { composing.current = true }} onCompositionEnd={() => { composing.current = false }}
    onKeyDown={event => { if (!composing.current && matchesShortcut(event.nativeEvent,'submit')) { event.preventDefault(); if(!event.repeat) form.current?.requestSubmit() } }}
    style={{ display: 'flex', flexDirection: 'column', maxHeight: '90dvh', minHeight: 0 }}>
    <Layout height="auto" style={{ minHeight: 0 }} header={<LayoutHeader padding={4} paddingBlockEnd={challenge?0:undefined}><Stack gap={1}>
      <ActionDialogHeader action="add" title={tr("SSH 连接 · {p0}", host.name)} subtitle={challenge?undefined:`${identity?.username ?? host.username}@${host.address}:${host.port}`} endContent={onClose?<IconButton label={tr("关闭连接面板")} size="md" variant="ghost" icon={<Icon icon={XMarkIcon} size="md"/>} onClick={onClose}/>:undefined}/>
    </Stack></LayoutHeader>} content={<LayoutContent padding={4}>
      <Stack gap={3}>
        {!desktopAvailable() && <Text type="supporting">{tr("浏览器仅预览界面，请在 Yaare 桌面程序中连接服务器。")}</Text>}
        {!challenge && live?.message && <Stack direction="horizontal" align="center" gap={2} role="status">
          {phase==='connecting'&&<Stack aria-hidden="true"><Spinner size="sm"/></Stack>}
          <Text type="supporting">{localizeMessage(live.message)}</Text>
        </Stack>}
        {phase==='authentication'&&live?.authentication?<Stack gap={3}>
          <Heading level={4} accessibilityLevel={3}>{live.authentication.kind==='proxy'?tr("代理认证"):tr("服务器交互认证")}</Heading>
          {live.authentication.address&&<Text type="supporting">{live.authentication.hostName||tr("当前服务器")} · {live.authentication.address}</Text>}
          {live.authentication.name&&<Text type="label">{live.authentication.name}</Text>}
          {live.authentication.instruction&&<Text type="supporting" style={{whiteSpace:'pre-wrap',overflowWrap:'anywhere'}}>{live.authentication.instruction}</Text>}
          {live.authentication.prompts.map((prompt,i)=><AuthResponseInput key={live.authentication!.id+':'+i} label={prompt.text||tr("认证响应")} echo={prompt.echo} value={answers[i]??''} onChange={value=>setAnswers(previous=>{const next=[...previous];next[i]=value;return next})} autoFocus={i===0} disabled={busy} active={active}/>)}
          <Button type="submit" label={live.authentication.prompts.length?tr("提交认证"):tr("继续")} size="md" isDisabled={busy}/>
        </Stack>:challenge ? <Stack gap={2}>
          <Heading ref={challengeFocus} level={4} accessibilityLevel={3} tabIndex={-1}>{changed ? tr("服务器指纹已变化")
            : challenge.name&&challenge.name!==host.name?tr("核实「{p0}」的服务器身份", challenge.name):tr("首次连接，核实服务器身份")}</Heading>
          <Text type="supporting">{changed ? tr("请先通过服务商控制台或管理员核对新指纹。确认后会更新本机记录并继续连接。")
            : tr("请通过可信渠道核对以下 SHA256 指纹。确认前不会继续认证。")}</Text>
          <Stack direction="horizontal" align="center" wrap="wrap" gap={2}>
            {changed && challenge.name && challenge.name !== host.name && <Text type="supporting">{challenge.name}</Text>}
            <Text type="code" size="sm" style={{overflowWrap:'anywhere'}}>{challenge.address}</Text>
            <Text type="supporting" color="secondary">{tr("密钥算法：")}{challenge.algorithm}</Text>
          </Stack>
          {changed && <Stack gap={1}>
            <Text type="label">{tr("已保存的指纹")}</Text>
            {challenge.previousFingerprints?.map(fingerprint => <Text key={fingerprint} type="code" size="sm" style={{overflowWrap:'anywhere',userSelect:'text'}}>{fingerprint}</Text>)}
            <Text type="label">{tr("服务器提供的新指纹")}</Text>
          </Stack>}
          <Text type="code" size="sm" style={{overflowWrap:'anywhere',userSelect:'text'}}>{challenge.fingerprint}</Text>
        </Stack> : !pending && phase !== 'connected' ? <Stack gap={3}>
          <AuthenticationSource value={source} identityId={identityId} identities={identities} disabled={busy} error={source==='identity'&&errorField==='keyId'?error:''}
            onChange={next=>{setSource(next);setIdentityId('');setAuthentication(previous=>({...clearAuthentication(previous),authentication:next==='key'?'key':'password'}));setError('');setErrorField(null)}}
            onIdentityChange={id=>{setIdentityId(id);setAuthentication(clearAuthentication);setError('');setErrorField(null)}}/>
          {source==='identity'?<Stack gap={2}>{identity?.authentication==='key'&&!identity.useAgent&&<AuthenticationFields key={identityId} value={{...authentication,authentication:'key'}} onChange={setAuthentication} keys={keys} choosingKey={false} keyError="" onChooseKey={onChooseKey} active={active} secretOnly/>}</Stack>:<AuthenticationFields key={source} hideMethod value={authentication} onChange={next => { setAuthentication(next); setError(''); setErrorField(null) }} keys={keys}
            active={active} keyError={localizeMessage(keyError)} choosingKey={busy} error={errorField === 'keyId' ? error : ''}
            secretPlaceholder={source==='password'&&host.hasCredential&&!host.identityId?tr("已保存密码，留空保持不变"):undefined} allowFileImport={allowFileImport}
            onChooseKey={async () => { setBusy(true); try { return await onChooseKey() } finally { setBusy(false) } }} />}
          <TextInput label={tr("连接超时（秒）")} htmlName="ssh-timeout" autoComplete="off" inputMode="numeric" value={timeout} onChange={value => { setTimeoutValue(value); if (errorField === 'timeout') { setError(''); setErrorField(null) } }} size="md"
            status={errorField === 'timeout' ? { type: 'error', message: localizeMessage(error) } : undefined} statusVariant="detached" />
        </Stack> : phase === 'connected' ? <Text type="supporting">{tr("终端已连接服务器，可以输入命令或打开 SFTP 文件管理。")}</Text> : null}
        {error && errorField !== 'keyId' && <Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
        {!challenge && live?.code && <Text type="supporting" color="secondary">{tr("状态代码：")}{live.code}</Text>}
      </Stack>
    </LayoutContent>} footer={<LayoutFooter padding={4} hasDivider><Stack direction="horizontal" justify="end" wrap="wrap" gap={2}>
      {pending || phase === 'connected' ? <Button key="disconnect" label={pending ? tr("取消连接") : tr("断开连接")} type="button" variant="secondary" size="md"
        onClick={event => { event.preventDefault(); onDisconnect() }} /> :
        <Button key="connect" label={live ? tr("重新连接") : tr("连接服务器")} type="submit" size="md" isDisabled={!desktopAvailable() || busy} />}
      {challenge && (changed ? <Button label={tr("更新指纹并连接")} type="button" variant="primary" size="md" isDisabled={busy} onClick={()=>void decide('replace')}/>
      : <>
        <Button label={tr("仅本次信任")} type="button" variant="secondary" size="md" isDisabled={busy} onClick={()=>void decide('once')}/>
        <Button label={tr("信任并保存")} type="button" size="md" isDisabled={busy} onClick={()=>void decide('remember')}/>
      </>)}
    </Stack></LayoutFooter>} />
  </form>
}

function AuthResponseInput({label,echo,value,onChange,autoFocus,disabled,active}:{label:string;echo:boolean;value:string;onChange:(value:string)=>void;autoFocus:boolean;disabled:boolean;active:boolean}) {
  useLocale()

  const [visible,setVisible]=useState(false)
  useLayoutEffect(() => { if (!active) setVisible(false) }, [active])
  if(echo)return <TextInput label={label} size="md" value={value} onChange={onChange} hasAutoFocus={autoFocus} autoComplete="off" isDisabled={disabled}/>
  return <InputGroup label={label} size="md" isDisabled={disabled}>
    <TextInput label={tr("认证响应")} isLabelHidden type={visible?'text':'password'} size="md" value={value} onChange={onChange} hasAutoFocus={autoFocus} autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" isDisabled={disabled} controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent',marginInlineStart:0}}/>
    <IconButton label={visible ? tr("隐藏{p0}", label) : tr("显示{p0}", label)} aria-pressed={visible} type="button" size="md" variant="ghost" isDisabled={disabled} icon={<Icon icon={visible?EyeSlashIcon:EyeIcon} size="sm"/>} onClick={()=>setVisible(previous=>!previous)}/>
  </InputGroup>
}
