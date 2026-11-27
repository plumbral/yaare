import { Button } from '@astryxdesign/core/Button'
import { Stack } from '@astryxdesign/core/Stack'
import { tr } from '../../shared/i18n/messages'
import type { ReconnectWait } from '../connections/sshReconnect'

export default function ReconnectActions({ waiting, onStop, onRetry }: {
  waiting?: ReconnectWait
  onStop: () => void
  onRetry?: () => void
}) {
  return <Stack direction="horizontal" align="center" justify="center" gap={2}>
    <Button type="button" size="md" variant="secondary" label={tr("停止重连")} onClick={onStop}/>
    {waiting && <Button type="button" size="md" label={tr("立即重试")} isDisabled={waiting !== 'waiting'} onClick={onRetry}/>}
  </Stack>
}
