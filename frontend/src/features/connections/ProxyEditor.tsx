import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import type { ProxyConfig } from '../../shared/generated/mygo'
import { emptyProxy } from './networkConfig'

export default function ProxyEditor({value,disabled,error,onChange,password,onPassword}:{value?:ProxyConfig;disabled:boolean;error?:string;onChange:(proxy:ProxyConfig)=>void;password?:string;onPassword?:(value:string)=>void}) {
  useLocale()

  const proxy=value??emptyProxy()
  const kind=proxy.kind||'none'
  return <Stack gap={3} paddingBlockEnd={3} style={{borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
    <Stack gap={1}>
      <Text type="label">{tr("连接代理")}</Text>
      <SegmentedControl label={tr("连接代理")} value={kind} layout="hug" size="md" isDisabled={disabled} onChange={next=>onChange(next==='none'||next==='auto'?{...emptyProxy(),kind:next}:{...proxy,kind:next,port:next==='http'?8080:1080})}>
        <SegmentedControlItem value="none" label={tr("直连")}/><SegmentedControlItem value="auto" label={tr("自动（系统代理）")}/><SegmentedControlItem value="socks5" label="SOCKS5"/><SegmentedControlItem value="http" label="HTTP"/>
      </SegmentedControl>
    </Stack>
    {(kind==='http'||kind==='socks5')&&<Stack gap={3}>
      <FormLayout defaultOptionality="required"><Stack direction="horizontal" align="start" gap={2} wrap="wrap">
        <StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 6)'}}><TextInput id="connection-proxy-address" label={tr("代理地址")} size="md" htmlName="proxy-address" value={proxy.address} onChange={address=>onChange({...proxy,address})} placeholder={tr("代理主机名或 IP")} autoComplete="off" autoCapitalize="none" hasAutoCorrect={false} hasSpellCheck={false} isRequired isDisabled={disabled}/></StackItem>
        <TextInput id="connection-proxy-port" label={tr("端口")} size="md" htmlName="proxy-port" width="calc(var(--spacing-10) * 3)" value={String(proxy.port||'')} onChange={text=>{if(/^\d{0,5}$/.test(text))onChange({...proxy,port:Number(text)})}} inputMode="numeric" autoComplete="off" isRequired isDisabled={disabled}/>
      </Stack></FormLayout>
      <Stack direction="horizontal" align="end" gap={3} wrap="wrap">
        <Stack height="var(--spacing-8)" justify="center"><CheckboxInput label={tr("代理需要认证")} value={proxy.useAuthentication} size="md" isDisabled={disabled} onChange={useAuthentication=>onChange({...proxy,useAuthentication,username:useAuthentication?proxy.username:''})}/></Stack>
        {proxy.useAuthentication&&<StackItem size="fill" style={{flexBasis:'calc(var(--spacing-10) * 5)'}}><FormLayout defaultOptionality="required"><TextInput id="connection-proxy-username" label={tr("代理用户名")} size="md" htmlName="proxy-username" value={proxy.username} onChange={username=>onChange({...proxy,username})} autoComplete="off" autoCapitalize="none" hasAutoCorrect={false} hasSpellCheck={false} isRequired isDisabled={disabled}/></FormLayout></StackItem>}
      </Stack>
      {proxy.useAuthentication&&onPassword&&<TextInput label={tr("代理密码（留空保留）")} htmlName="proxy-password" type="password" size="md" value={password??''} onChange={onPassword} autoComplete="new-password" isDisabled={disabled}/>}
    </Stack>}
    {error&&<Text id="connection-proxy-error" type="supporting" role="alert" tabIndex={-1}>{localizeMessage(error)}</Text>}
  </Stack>
}
