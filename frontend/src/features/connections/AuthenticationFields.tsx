import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { TextArea } from '@astryxdesign/core/TextArea'
import { InputGroup } from '@astryxdesign/core/InputGroup'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { EyeIcon, EyeSlashIcon } from '@heroicons/react/24/outline'
import { Button } from '@astryxdesign/core/Button'
import { Selector } from '@astryxdesign/core/Selector'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { desktopAvailable, writeClipboardText } from '../../shared/bridge/application'
import { discardKeySelection, generateKey, pickKey, type KeyReference } from '../../shared/bridge/keys'
import { clearAuthentication, type AuthenticationDraft } from './authenticationDraft'
import AgentStatus from './AgentStatus'

export default function AuthenticationFields({ value, onChange, keys, choosingKey, keyError, onChooseKey, disabled = false, error = '', active = true, helpText, secretPlaceholder, secretActions, secretOnly = false, hideMethod = false, allowFileImport=true, allowGenerate=false }: {
  value: AuthenticationDraft; onChange: (value: AuthenticationDraft) => void; keys: KeyReference[]
  choosingKey: boolean; keyError: string; onChooseKey: () => Promise<KeyReference | null>; disabled?: boolean; error?: string; active?: boolean; helpText?: string; secretPlaceholder?:string; secretActions?:ReactNode; secretOnly?: boolean; hideMethod?:boolean
  allowFileImport?:boolean; allowGenerate?:boolean
}) {
  useLocale()

  const [showSecret, setShowSecret] = useState(false)
  const [fileError,setFileError]=useState('')
  const [pickingTemporary,setPickingTemporary]=useState(false)
  const activity=useRef<AbortSignal | null>(null)
  const [copied,setCopied]=useState(false)
  useLayoutEffect(() => {
    if (!active) { setShowSecret(false); setPickingTemporary(false); return }
    const scope = new AbortController()
    activity.current = scope.signal
    return () => scope.abort()
  }, [active])
  async function generate() {
    const scope = activity.current
    if (!scope || scope.aborted) return
    setPickingTemporary(true)
    setFileError('')
    setCopied(false)
    onChange({...value,generatedKey:undefined})
    try {
      const key=await generateKey()
      if(!key)throw Error(tr("密钥生成失败，请重试。"))
      if (!scope.aborted) onChange({...value,generatedKey:key})
      else await discardKeySelection(key.token)
    } catch(cause) {
      if (!scope.aborted) setFileError(cause instanceof Error?cause.message:tr("密钥生成失败，请重试。"))
    } finally {
      if (!scope.aborted) setPickingTemporary(false)
    }
  }
  const change = (next: Partial<AuthenticationDraft>) => onChange({ ...value, ...next })
  const help=value.authentication==='key'&&value.keySource==='agent'?tr("使用 Agent 已加载的密钥、OpenSSH 用户证书或 FIDO2 安全密钥。私钥保留在 Agent 中；连接时可能需要确认或触摸设备。"):helpText
  return <Stack gap={3}>
    {!secretOnly && !hideMethod && <><Text type="label">{tr("认证方式")}</Text>
    <SegmentedControl label={tr("认证方式")} value={value.authentication} layout="hug" size="md" isDisabled={disabled || choosingKey || pickingTemporary}
      onChange={method => { setShowSecret(false); onChange({ ...clearAuthentication(value), generatedKey:undefined, temporaryFile:undefined, authentication: method === 'key' ? 'key' : 'password' }) }}>
      <SegmentedControlItem value="password" label={tr("密码")} /><SegmentedControlItem value="key" label={tr("私钥")} />
    </SegmentedControl></>}
    {!secretOnly && value.authentication === 'key' && <Stack gap={2}>
      <SegmentedControl label={tr("私钥来源")} value={value.keySource} layout="hug" size="md" isDisabled={disabled || choosingKey || pickingTemporary}
        onChange={source => { setShowSecret(false); setFileError(''); setCopied(false); onChange({ ...clearAuthentication(value), temporaryFile:undefined, generatedKey:undefined, keySource: source === 'generated' || source === 'agent' || source === 'text' ? source : 'file' }) }}>
        <SegmentedControlItem value="file" label={tr("选择密钥")} /><SegmentedControlItem value="text" label={tr("粘贴私钥")} />{allowGenerate&&desktopAvailable()&&<SegmentedControlItem value="generated" label={tr("生成密钥")}/>}<SegmentedControlItem value="agent" label="SSH Agent" />
      </SegmentedControl>
      {value.keySource === 'file' ? <Stack gap={2}>
        <Selector label={tr("已保存密钥")} value={value.keyId} size="md" isDisabled={disabled || choosingKey}
          options={keys.map(key => ({ value: key.id, label: key.name, description: key.available ? `${key.format} · ${key.source==='stored'?tr("加密保存"):tr("文件引用")}` : tr("密钥不可用，请重新添加") }))}
          placeholder={value.temporaryFile?.name??tr("选择已保存的密钥")} emptyText={tr("请在密钥页添加，或直接选择文件")} status={error ? { type: 'error', message: localizeMessage(error) } : undefined} statusVariant="detached" onChange={keyId => change({ keyId, passphrase:'',temporaryFile:undefined })} />
        <Button label={tr("选择私钥文件")} type="button" variant="secondary" size="md" style={{alignSelf:'start'}} isLoading={choosingKey||pickingTemporary} isDisabled={disabled || choosingKey || pickingTemporary || !desktopAvailable()}
          onClick={async () => {
            if(import.meta.env.MODE==='browser'){const scope=activity.current;setPickingTemporary(true);setFileError('');try{const {readBrowserPrivateKey}=await import('../../shared/bridge/browserFiles');const file=await readBrowserPrivateKey();if(file&&scope&&!scope.aborted)onChange({...clearAuthentication(value),keySource:'text',keyId:'',temporaryFile:undefined,generatedKey:undefined,privateKey:file.content})}catch(cause){if(scope&&!scope.aborted)setFileError(cause instanceof Error?cause.message:tr('文件未选择，请检查文件是否可读且为 OpenSSH 或 PEM 私钥。'))}finally{if(scope&&!scope.aborted)setPickingTemporary(false)};return}

            const scope = activity.current
            if (!scope || scope.aborted) return
            setFileError('')
            try {
              if(!allowFileImport){
                setPickingTemporary(true)
                const file=await pickKey()
                if (file) {
                  if (scope.aborted) await discardKeySelection(file.token)
                  else change({temporaryFile:file,keyId:'',passphrase:''})
                }
              } else {
                const key=await onChooseKey()
                if (key && !scope.aborted) change({keyId:key.id,passphrase:'',temporaryFile:undefined})
              }
            } catch(e){if (!scope.aborted) setFileError(e instanceof Error?e.message:tr("读取私钥失败，请重新选择。"))}
            finally{if (!scope.aborted) setPickingTemporary(false)}
          }} />
        {keyError && <Text type="supporting" role="alert">{localizeMessage(keyError)}</Text>}
        {fileError&&<Text type="supporting" role="alert">{localizeMessage(fileError)}</Text>}
      </Stack> : value.keySource === 'generated' ? <Stack gap={2} align="start">
        <Button type="button" label={value.generatedKey?tr("重新生成密钥"):tr("生成 Ed25519 密钥")} size="md" variant="secondary" isDisabled={disabled||pickingTemporary} isLoading={pickingTemporary} onClick={()=>void generate()}/>
        {value.generatedKey?.publicKey&&<Button type="button" label={tr("复制公钥")} size="sm" variant="ghost" isDisabled={disabled} onClick={async()=>{
          setFileError('')
          try { await writeClipboardText(value.generatedKey!.publicKey!);setCopied(true) }
          catch { setCopied(false);setFileError(tr("复制失败，请重试。")) }
        }}/>}
        {copied&&<Text role="status" type="supporting">{tr("已复制公钥")}</Text>}
        {(error||fileError)&&<Text role="alert" type="supporting">{localizeMessage(error||fileError)}</Text>}
      </Stack> : value.keySource === 'agent' ? <AgentStatus disabled={disabled} /> : <TextArea label={tr("私钥内容")} htmlName="ssh-private-key" size="sm" rows={4} maxLength={1048576} value={value.privateKey}
        onChange={privateKey => change({ privateKey })} placeholder={tr("粘贴 OpenSSH 或 PEM 私钥")} autoComplete="off" hasAutoCorrect={false} hasSpellCheck={false} autoCapitalize="none" isDisabled={disabled}
        status={error ? { type: 'error', message: localizeMessage(error) } : undefined} statusVariant="detached" />}
    </Stack>}
    {!(value.authentication === 'key' && (value.keySource === 'agent'||value.keySource==='generated'||!secretOnly&&value.keySource==='file'&&keys.some(key=>key.id===value.keyId&&key.source==='stored'))) && <InputGroup label={value.authentication === 'key' ? tr("私钥口令（未加密可留空）") : tr("登录密码")} size="md" isDisabled={disabled || choosingKey}>
    <TextInput label={tr("认证内容")} htmlName="ssh-secret" type={showSecret ? 'text' : 'password'} size="md"
      controlStyle={{ borderWidth: 0, boxShadow: 'none', backgroundColor: 'transparent', marginInlineStart: 0 }}
      autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" isDisabled={disabled || choosingKey}
      value={value.authentication === 'key' ? value.passphrase : value.password}
      placeholder={secretPlaceholder}
      onChange={secret => change(value.authentication === 'key' ? { passphrase: secret } : { password: secret })} />
    <IconButton label={value.authentication === 'key' ? (showSecret ? tr("隐藏私钥口令") : tr("显示私钥口令")) : (showSecret ? tr("隐藏密码") : tr("显示密码"))} aria-pressed={showSecret} type="button" variant="ghost" size="md" isDisabled={disabled || choosingKey}
      icon={<Icon icon={showSecret ? EyeSlashIcon : EyeIcon} size="sm" />} onClick={() => setShowSecret(previous => !previous)} />
    {secretActions}</InputGroup>}
    {error && value.authentication === 'password' && <Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
    {help&&<Text type="supporting" color="secondary">{help}</Text>}
  </Stack>
}
