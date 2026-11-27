import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useState } from 'react'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Selector } from '@astryxdesign/core/Selector'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { ArrowUpIcon, ArrowDownIcon, XMarkIcon } from '@heroicons/react/24/outline'
import type { HostProfile } from '../workspace/mockGateway'

export default function JumpHostsEditor({ host, hosts, disabled, error, onChange }: {
  host: HostProfile; hosts: readonly HostProfile[]; disabled: boolean; error?: string; onChange: (ids: string[]) => void
}) {
  useLocale()

  const [selected, setSelected] = useState('')
  const ids = host.jumpHostIds ?? []
  const available = hosts.filter(h => h.id !== host.id && !ids.includes(h.id))
  const move = (index: number, offset: number) => { const next = [...ids]; [next[index], next[index + offset]] = [next[index + offset]!, next[index]!]; onChange(next) }
  return <Stack gap={3}>
    <Stack direction="horizontal" align="center" justify="between" gap={2}>
      <Text type="label">{tr("跳板链路")}</Text>
      <Text type="supporting" color="secondary">{ids.length ? tr("{p0} 跳", ids.length) : tr("未使用跳板机")}</Text>
    </Stack>
    <Stack gap={1}>
      {ids.map((id, i) => { const item = hosts.find(h => h.id === id); return <Stack key={id} direction="horizontal" align="center" gap={2} paddingBlock={2} style={{ borderBlockEnd: 'var(--border-width) solid var(--color-border)' }}>
        <Text type="supporting" color="secondary">{i + 1}</Text>
        <StackItem size="fill"><Stack gap={0}><Text type="label">{item?.name ?? tr("主机已不存在")}</Text><Text type="supporting" color="secondary">{item ? `${item.username}@${item.address}:${item.port}` : tr("请移除此项并重新选择")}</Text></Stack></StackItem>
        <IconButton type="button" label={tr("上移{p0}", item?.name ?? tr("跳板机"))} size="md" variant="ghost" icon={<Icon icon={ArrowUpIcon} size="sm" />} isDisabled={disabled || i === 0} onClick={() => move(i, -1)} />
        <IconButton type="button" label={tr("下移{p0}", item?.name ?? tr("跳板机"))} size="md" variant="ghost" icon={<Icon icon={ArrowDownIcon} size="sm" />} isDisabled={disabled || i === ids.length - 1} onClick={() => move(i, 1)} />
        <IconButton type="button" label={tr("移除{p0}", item?.name ?? tr("跳板机"))} size="md" variant="ghost" icon={<Icon icon={XMarkIcon} size="sm" />} isDisabled={disabled} onClick={() => onChange(ids.filter(x => x !== id))} />
      </Stack> })}
    </Stack>
    <Stack direction="horizontal" align="end" gap={2}>
      <StackItem size="fill"><Selector label={tr("添加跳板机")} isLabelHidden size="md" value={selected} placeholder={tr("搜索并选择已有连接…")} hasSearch searchPlaceholder={tr("名称或地址…")} isDisabled={disabled || ids.length >= 8}
        options={available.map(h => ({ value: h.id, label: h.name, description: `${h.username}@${h.address}:${h.port}` }))} onChange={setSelected} emptyText={tr("请先在主机页保存一台跳板机")} /></StackItem>
      <Button label={tr("添加")} type="button" variant="secondary" size="md" isDisabled={disabled || !available.some(h => h.id === selected) || ids.length >= 8} onClick={() => { onChange([...ids, selected]); setSelected('') }} />
    </Stack>
    {error && <Text type="supporting" role="alert">{localizeMessage(error)}</Text>}
    <Text type="supporting" color="secondary" style={{ overflowWrap: 'anywhere' }}>{[tr("本机"), ...(host.proxy?.kind&&host.proxy.kind!=='none'?[host.proxy.kind==='auto'?tr("自动（系统代理）"):tr("{p0} 代理（{p1}:{p2}）", host.proxy.kind==='http'?'HTTP':'SOCKS5', host.proxy.address||tr("地址"), host.proxy.port||tr("端口"))]:[]), ...ids.map(id => hosts.find(h => h.id === id)?.name ?? tr("不可用")), host.name || tr("目标主机")].join(' → ')}</Text>
    {ids.length > 0 && <Text type="supporting" color="secondary">{tr("仅使用这里排列的链路，不叠加所选主机自身的代理、跳板或转发规则。")}</Text>}
  </Stack>
}
