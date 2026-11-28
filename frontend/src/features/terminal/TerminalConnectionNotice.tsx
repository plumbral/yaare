import { Heading } from '@astryxdesign/core/Heading'
import { Spinner } from '@astryxdesign/core/Spinner'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { tr } from '../../shared/i18n/messages'
import OsIcon from '../../shared/icons/OsIcon'
import ConnectionPromptActions from '../workspace/ConnectionPromptActions'
import type { HostProfile } from '../workspace/mockGateway'
import type { SessionConnectionPrompt } from '../workspace/sessionConnectionPrompt'

export default function TerminalConnectionNotice({ prompt, host, presentation }: {
  prompt: SessionConnectionPrompt
  host: HostProfile
  presentation: 'card' | 'banner'
}) {
  const name = host.name || host.address
  const address = host.address.includes(':') && !host.address.startsWith('[') ? `[${host.address}]` : host.address
  const endpoint = presentation === 'card' && prompt.kind === 'idle' && address ? `${host.username}@${address}:${host.port}` : ''
  const description = [endpoint, prompt.label, prompt.message !== prompt.label && prompt.message].filter(Boolean).join(' · ')
  const status = <Text color="secondary" role="status">{description}</Text>
  const actions = <ConnectionPromptActions actions={prompt.actions} connectAriaLabel={tr('连接 {p0}', name)}/>

  if (presentation === 'banner') {
    return <Stack direction="horizontal" align="center" justify="between" wrap="wrap" paddingInline={2} paddingBlock={1} gap={2} style={{ flexShrink: 0 }}>
      {status}
      {actions}
    </Stack>
  }

  return <Stack align="center" padding={2} style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
    <Stack gap={3} style={{ display: 'grid', gridTemplateRows: 'auto minmax(0, 1fr) auto',
      gridTemplateColumns: 'minmax(0, 1fr)', justifyItems: 'center', boxSizing: 'border-box', marginBlock: 'auto',
      inlineSize: 'min(100%, calc(var(--spacing-10) * 11))', blockSize: 'min(100%, calc(var(--spacing-10) * 5.5))',
      paddingInline: 'var(--spacing-6)', paddingBlock: 'min(var(--spacing-7), 3dvh)', textAlign: 'center',
      borderRadius: 'var(--radius-container)', background: 'var(--color-background-popover)', boxShadow: 'var(--shadow-med)' }}>
      <Stack align="center" justify="center" aria-hidden="true">
        {prompt.busy ? <Spinner size="lg"/> : <OsIcon name={host.icon} size="lg"/>}
      </Stack>
      <Stack width="100%" style={{ minHeight: 0, justifyContent: 'safe center', overflow: 'auto', overflowWrap: 'anywhere', scrollbarGutter: 'stable both-edges' }}>
        <Heading level={4} accessibilityLevel={3}>{name || prompt.label}</Heading>
        {status}
      </Stack>
      {actions}
    </Stack>
  </Stack>
}
