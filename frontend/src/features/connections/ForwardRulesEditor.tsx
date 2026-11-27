import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useState } from 'react'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { PencilSquareIcon, TrashIcon, PlusIcon } from '@heroicons/react/24/outline'
import type { ForwardRule } from '../../shared/generated/mygo'
import { forwardError, forwardKinds, forwardSummary } from './networkConfig'

export default function ForwardRulesEditor({ rules, disabled, error, onChange }: {
  rules: ForwardRule[]; disabled: boolean; error?: string; onChange: (rules: ForwardRule[]) => void
}) {
  useLocale()

  const [editing, setEditing] = useState('')
  const [fieldError, setFieldError] = useState('')
  useEffect(() => { if (error) { const invalid = rules.find(r => forwardError(r)); if (invalid) setEditing(invalid.id) } }, [error, rules])
  const update = (id: string, patch: Partial<ForwardRule>) => { onChange(rules.map(r => r.id === id ? { ...r, ...patch } : r)); setFieldError('') }
  const add = () => { const rule: ForwardRule = { id: crypto.randomUUID(), name: '', kind: 'local', listenAddress: '127.0.0.1', listenPort: 0, targetAddress: '127.0.0.1', targetPort: 0, enabled: true }; onChange([...rules, rule]); setEditing(rule.id); setFieldError(''); requestAnimationFrame(() => document.getElementById(`forward-name-${rule.id}`)?.focus()) }
  const portInput = (id: string, field: 'listenPort' | 'targetPort', value: string) => { if (/^\d{0,5}$/.test(value)) update(id, { [field]: Number(value) }) }
  return <Stack gap={3}>
    <Stack direction="horizontal" align="center" justify="between" gap={2}>
      <Text type="supporting" color="secondary">{tr("规则在 SSH 连接成功后启动。")}</Text>
      <Button label={tr("添加规则")} type="button" size="md" variant="secondary" icon={<Icon icon={PlusIcon} size="sm" />} isDisabled={disabled || rules.length >= 32} onClick={add} />
    </Stack>
    {!rules.length && <Stack gap={1} paddingBlock={4}><Text type="label">{tr("尚未配置端口转发")}</Text><Text type="supporting" color="secondary">{tr("可将本地端口映射到远程服务，或创建 SOCKS5 入口。")}</Text></Stack>}
    {rules.map(rule => <Stack key={rule.id} gap={3} paddingBlock={2} style={{ borderBlockEnd: 'var(--border-width) solid var(--color-border)' }}>
      <Stack direction="horizontal" align="center" gap={2}>
        <CheckboxInput label={tr("连接后启用{p0}", rule.name || tr("新规则"))} isLabelHidden value={rule.enabled} size="md" isDisabled={disabled} onChange={enabled => update(rule.id, { enabled })} />
        <StackItem size="fill"><Stack gap={0}><Text type="label">{rule.name || tr("新规则")} · {forwardKinds[rule.kind]}</Text><Text type="supporting" color="secondary" style={{ overflowWrap: 'anywhere' }}>{forwardSummary(rule)}</Text></Stack></StackItem>
        <IconButton type="button" label={tr("编辑{p0}", rule.name || tr("新规则"))} variant="ghost" size="md" aria-expanded={editing === rule.id} aria-controls={`forward-fields-${rule.id}`} icon={<Icon icon={PencilSquareIcon} size="sm" />} isDisabled={disabled} onClick={() => { setEditing(editing === rule.id ? '' : rule.id); setFieldError('') }} />
        <IconButton type="button" label={tr("删除{p0}", rule.name || tr("新规则"))} variant="ghost" size="md" icon={<Icon icon={TrashIcon} size="sm" />} isDisabled={disabled} onClick={() => { onChange(rules.filter(r => r.id !== rule.id)); if (editing === rule.id) setEditing('') }} />
      </Stack>
      {editing === rule.id && <Stack id={`forward-fields-${rule.id}`} gap={3}>
        <FormLayout defaultOptionality="required"><Stack gap={3}>
          <TextInput id={`forward-name-${rule.id}`} label={tr("规则名称")} size="md" value={rule.name} onChange={name => update(rule.id, { name })} placeholder={tr("例如：数据库访问")} autoComplete="off" isDisabled={disabled} isRequired />
          <Stack gap={1}><Text type="label">{tr("转发类型")}</Text><SegmentedControl label={tr("转发类型")} value={rule.kind} layout="hug" size="md" isDisabled={disabled} onChange={kind => update(rule.id, { kind, targetAddress: kind === 'dynamic' ? '' : rule.targetAddress || '127.0.0.1', targetPort: kind === 'dynamic' ? 0 : rule.targetPort })}>
            <SegmentedControlItem value="local" label={tr("本地")} /><SegmentedControlItem value="remote" label={tr("远程")} /><SegmentedControlItem value="dynamic" label={tr("动态 SOCKS5")} />
          </SegmentedControl></Stack>
          <Stack direction="horizontal" gap={2} align="start" wrap="wrap">
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 5)' }}><TextInput label={rule.kind === 'remote' ? tr("服务器监听地址") : tr("本机监听地址")} size="md" value={rule.listenAddress} onChange={listenAddress => update(rule.id, { listenAddress })} autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" isDisabled={disabled} isRequired /></StackItem>
            <TextInput label={tr("监听端口")} size="md" width="calc(var(--spacing-10) * 3)" value={String(rule.listenPort || '')} inputMode="numeric" autoComplete="off" placeholder={rule.kind === 'dynamic' ? '1080' : '15432'} onChange={value => portInput(rule.id, 'listenPort', value)} isDisabled={disabled} isRequired />
          </Stack>
          {rule.kind !== 'dynamic' && <Stack direction="horizontal" gap={2} align="start" wrap="wrap">
            <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 5)' }}><TextInput label={rule.kind === 'remote' ? tr("本机侧目标地址") : tr("远程目标地址")} size="md" value={rule.targetAddress} onChange={targetAddress => update(rule.id, { targetAddress })} autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none" isDisabled={disabled} isRequired /></StackItem>
            <TextInput label={tr("目标端口")} size="md" width="calc(var(--spacing-10) * 3)" value={String(rule.targetPort || '')} inputMode="numeric" autoComplete="off" placeholder="5432" onChange={value => portInput(rule.id, 'targetPort', value)} isDisabled={disabled} isRequired />
          </Stack>}
        </Stack></FormLayout>
        <Text type="supporting" color="secondary">{rule.kind === 'remote' ? tr("目标地址由本机访问；服务器实际监听范围受 GatewayPorts 设置影响。") : rule.kind === 'dynamic' ? tr("提供 SOCKS5 TCP 连接，域名由远程服务器解析。") : tr("目标地址由远程服务器访问。")}</Text>
        {!['127.0.0.1', '::1', 'localhost'].includes(rule.listenAddress) && <Text type="supporting">{tr("此监听地址可能允许其他设备访问转发服务。")}</Text>}
        <Stack direction="horizontal" align="center" justify="end" gap={2}>
          {fieldError && <Text type="supporting" role="alert">{localizeMessage(fieldError)}</Text>}
          <Button label={tr("完成")} type="button" size="md" variant="secondary" isDisabled={disabled} onClick={() => { const invalid = forwardError(rule); setFieldError(invalid); if (!invalid) setEditing('') }} />
        </Stack>
      </Stack>}
    </Stack>)}
    {error && <Text id="connection-forward-error" type="supporting" role="alert" tabIndex={-1}>{localizeMessage(error)}</Text>}
  </Stack>
}
