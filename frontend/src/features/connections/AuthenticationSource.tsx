import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Selector } from '@astryxdesign/core/Selector'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import type { Identity } from './useIdentities'

export type AuthenticationSourceValue = 'password' | 'key' | 'identity'
export default function AuthenticationSource({value,onChange,identityId,onIdentityChange,identities,disabled=false,error='',selectorId}: {
  value:AuthenticationSourceValue;onChange:(value:AuthenticationSourceValue)=>void
  identityId:string;onIdentityChange:(id:string)=>void;identities:Identity[];disabled?:boolean;error?:string;selectorId?:string
}) {
  useLocale()

  return <Stack gap={3}>
    <Text type="label">{tr("认证方式")}</Text>
    <SegmentedControl label={tr("认证方式")} size="md" layout="hug" value={value} isDisabled={disabled}
      onChange={next=>onChange(next==='identity'?'identity':next==='key'?'key':'password')}>
      <SegmentedControlItem value="password" label={tr("密码")}/>
      <SegmentedControlItem value="key" label={tr("密钥")}/>
      <SegmentedControlItem value="identity" label={tr("身份")}/>
    </SegmentedControl>
    {value==='identity'&&<Selector id={selectorId} label={tr("选择身份")} size="md" hasClear value={identityId||null}
      placeholder={tr("选择已保存身份…")} hasSearch searchPlaceholder={tr("搜索身份…")} emptyText={tr("尚无已保存身份，请先在身份页添加。")}
      options={identities.map(identity=>({value:identity.id,label:identity.name,description:`${identity.username} · ${identity.useAgent?'SSH Agent':identity.authentication==='key'?tr("密钥"):tr("密码")}`}))}
      isDisabled={disabled} onChange={id=>onIdentityChange(id??'')}
      status={error?{type:'error',message:localizeMessage(error)}:undefined} statusVariant="detached"/>}
  </Stack>
}
