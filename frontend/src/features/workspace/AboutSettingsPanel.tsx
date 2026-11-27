import { lazy, Suspense, useEffect, useState, type ReactNode } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Section } from '@astryxdesign/core/Section'
import { Text, Heading } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { Link } from '@astryxdesign/core/Link'
import { Switch } from '@astryxdesign/core/Switch'
import { GlobeAltIcon, ChatBubbleLeftRightIcon, CodeBracketIcon } from '@heroicons/react/24/outline'
import BrandMark from '../../shared/branding/BrandMark'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { getApplicationInfo } from '../../shared/bridge/application'
import type { Info } from '../../shared/generated/mygo'
import { DialogPresence } from '../../shared/ui/AppDialog'
import { useApplicationUpdates } from './useApplicationUpdates'

const LicenseDialog = lazy(() => import('./LicenseDialog'))
const ReleaseNotesDialog = lazy(() => import('./ReleaseNotesDialog'))
export const applicationContacts = { website: 'https://yaare.app', discord: 'https://discord.gg/pcpfXrGcA4', repository: 'https://github.com/plumbral/yaare' } as const

function ContactRow({ icon, label, children }: { icon: ReactNode; label: string; children: ReactNode }) {
  return <Stack direction="horizontal" gap={3} align="center" wrap="wrap" paddingBlock={2}>
    <Stack direction="horizontal" gap={2} align="center" style={{ width: 'min(100%, calc(var(--spacing-10) * 5))', flexShrink: 0 }}>
      {icon}<Text type="label">{label}</Text>
    </Stack>
    {children}
  </Stack>
}

function DesktopUpdates() {
  const updates = useApplicationUpdates()
  const state = updates.status.state
  const canCheck = state !== 'preview' && state !== 'unconfigured' && state !== 'loading'
  const status = updates.busy ? tr('正在检查更新…')
    : state === 'available' ? tr('新版本 {p0} 已发布。', updates.status.latestVersion)
    : state === 'unconfigured' ? tr('尚未配置更新源。')
    : state === 'preview' ? tr('请在桌面客户端中检查更新。')
    : state === 'failed' ? tr('暂时无法检查更新。')
    : state === 'current' ? tr('当前已是最新版本。')
    : state === 'loading' ? tr('正在读取版本信息…') : updates.automatic ? tr('等待首次检查。') : tr('自动检查已关闭。')

  return <>
    <Stack direction="horizontal" gap={3} align="center" justify="between" wrap="wrap">
      <Text id="about-update-status" type="supporting" color="secondary" role="status">{status}</Text>
      <Stack direction="horizontal" gap={2} wrap="wrap">
        <Button id="about-check-updates" label={tr('立即检查')} size="md" variant="secondary"
          isDisabled={!canCheck || updates.busy} onClick={() => void updates.check()}/>
        {state === 'available' && <Button id="about-download" label={tr('查看新版')} size="md" variant="secondary"
          href={`${applicationContacts.repository}/releases`} target="_blank" rel="noopener noreferrer"/>}
      </Stack>
    </Stack>
    <Stack direction="horizontal" justify="between" gap={3} align="center">
      <label htmlFor="about-automatic-updates"><Text type="label">{tr('自动检查更新')}</Text></label>
      <Switch id="about-automatic-updates" label={tr('自动检查更新')} isLabelHidden size="sm" value={updates.automatic}
        isDisabled={!canCheck} onChange={updates.setAutomatic}/>
    </Stack>
    {state === 'available' && updates.status.notes && <Stack gap={2}><Text type="label">{tr('更新说明')}</Text><Text type="supporting" color="secondary" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{updates.status.notes}</Text></Stack>}
  </>
}

function DockerUpdates({version}: {version?: string}) {
  const [notesOpen, setNotesOpen] = useState(false)
  const imageVersion = version || import.meta.env.YAARE_RELEASE_NOTE?.tag
  return <>
    <Text id="about-image-version" type="label">{imageVersion ? tr('当前镜像版本 {p0}', imageVersion) : tr('正在读取版本信息…')}</Text>
    <Text type="supporting" color="secondary">{tr('此实例由部署管理员通过 Docker Compose 更新。')}</Text>
    <Stack direction="horizontal" align="center" gap={3} wrap="wrap">
      <Button id="about-release-notes" label={tr('更新日志')} size="md" variant="secondary" onClick={() => setNotesOpen(true)}/>
      <Link id="about-releases" href={`${applicationContacts.repository}/releases`}
        isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>GitHub Releases</Link>
      <Link id="about-docker-upgrade" href={`${applicationContacts.repository}/blob/HEAD/deployment/docker/README.md#升级`}
        isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>{tr('Docker 升级指南')}</Link>
    </Stack>
    <Suspense fallback={null}><DialogPresence value={notesOpen ? true : null}>
      {(_, isOpen, onAfterClose) => <ReleaseNotesDialog isOpen={isOpen} onClose={() => setNotesOpen(false)} onAfterClose={onAfterClose}/>}
    </DialogPresence></Suspense>
  </>
}

export default function AboutSettingsPanel() {
  useLocale()
  const browser = import.meta.env.MODE === 'browser'
  const [info, setInfo] = useState<Info | null>(null)
  const [licenses, setLicenses] = useState(false)
  useEffect(() => {
    let disposed = false
    void getApplicationInfo().then(value => { if (!disposed) setInfo(value) }).catch(() => {})
    return () => { disposed = true }
  }, [])
  return <Stack gap={6} style={{ maxWidth: 'calc(var(--spacing-10) * 20)', width: '100%' }}>
    <Section padding={0} variant="transparent">
      <Stack direction="horizontal" gap={3} align="center">
        <BrandMark/>
        <Stack gap={1}>
          <Heading level={4} accessibilityLevel={3} weight="semibold">Yaare{!browser && info?.version ? ` ${info.version}` : ''}</Heading>
          <Text type="supporting" color="secondary">{tr('免费、开源的现代化 SSH / SFTP 客户端。')}</Text>
          {info && <Text type="supporting" color="secondary">{info.platform === 'windows' ? 'Windows' : info.platform === 'darwin' ? 'macOS' : info.platform === 'linux' ? 'Linux' : info.platform} · {info.architecture === 'amd64' ? 'x64' : info.architecture}</Text>}
        </Stack>
      </Stack>
    </Section>
    <Section padding={0} variant="transparent">
      <Stack gap={3}>
        <Heading level={4} accessibilityLevel={3} weight="semibold">{tr('版本与更新')}</Heading>
        {browser ? <DockerUpdates version={info?.version}/> : <DesktopUpdates/>}
      </Stack>
    </Section>
    <Section padding={0} variant="transparent">
      <Heading level={4} accessibilityLevel={3} weight="semibold">{tr('联系与社区')}</Heading>
      <Stack paddingBlockStart={2}>
        <ContactRow icon={<Icon icon={GlobeAltIcon} size="sm"/>} label={tr('官方网站')}><Link id="about-website" href={applicationContacts.website} isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>yaare.app</Link></ContactRow>
        <ContactRow icon={<Icon icon={ChatBubbleLeftRightIcon} size="sm"/>} label={tr('Discord 社区')}><Link id="about-discord" href={applicationContacts.discord} isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>{tr('加入 Discord 社区')}</Link></ContactRow>
        <ContactRow icon={<Icon icon={CodeBracketIcon} size="sm"/>} label={tr('项目仓库')}>
          <Link id="about-repository" href={applicationContacts.repository} isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>plumbral/yaare</Link>
        </ContactRow>
      </Stack>
    </Section>
    <Stack direction="horizontal">
      <Button id="about-licenses" label={tr('第三方组件与字体许可')} variant="secondary" size="md" onClick={() => setLicenses(true)}/>
    </Stack>
    <Stack paddingBlockStart={4} style={{ borderBlockStart: 'var(--border-width) solid var(--color-border)' }}>
      <Text id="about-colophon" type="supporting" color="secondary">Built with ❤️</Text>
    </Stack>
    {licenses && <Suspense fallback={<Text type="supporting">{tr('正在读取许可…')}</Text>}><LicenseDialog onClose={() => setLicenses(false)}/></Suspense>}
  </Stack>
}
