import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useState } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { SSHService, type AgentInfo } from '../../shared/generated/mygo'
import { desktopAvailable } from '../../shared/bridge/application'

export default function AgentStatus({ disabled = false }: { disabled?: boolean }) {
  useLocale()

  const [info, setInfo] = useState<AgentInfo | null>(null)
  const [revision, setRevision] = useState(0), [loading, setLoading] = useState(false)
  useEffect(() => {
    let active = true
    if (!desktopAvailable()) { setInfo({available:false,keys:[],message:tr("浏览器预览无法读取本机 SSH Agent。")}); return }
    setLoading(true)
    void SSHService.agentInfo().then(value => { if (active) setInfo(value) }, () => { if (active) setInfo({available:false,keys:[],message:tr("无法读取 SSH Agent，请检查 Agent 后重试。")}) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [revision])
  return <Stack gap={2}>
    <Stack direction="horizontal" gap={2} align="center" wrap="wrap">
      <Text type="supporting" role="status">{loading ? tr("正在检查 SSH Agent…") : info?.available && info.keys.length ? tr("SSH Agent 已就绪 · {p0} 个密钥", info.keys.length) : info?.message}</Text>
      <Button label={tr("重新检查 Agent")} type="button" size="md" variant="ghost" isLoading={loading} isDisabled={disabled || loading || !desktopAvailable()} onClick={() => setRevision(value => value + 1)} />
    </Stack>
    {!!info?.keys.length && <details><summary>{tr("查看公钥摘要")}</summary><Stack gap={2}>{info.keys.map((key,index) => <div key={`${key.fingerprint}-${index}`} style={{overflowWrap:'anywhere'}}><Text type="supporting">{key.name} · {key.algorithm}</Text><Text type="supporting" color="secondary">{key.fingerprint}</Text></div>)}</Stack></details>}
  </Stack>
}
