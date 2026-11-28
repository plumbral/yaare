import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { Stack } from '@astryxdesign/core/Stack'
import { ArrowPathIcon, ArrowRightOnRectangleIcon } from '@heroicons/react/24/outline'
import type { SessionConnectionAction } from './sessionConnectionPrompt'

export default function ConnectionPromptActions({ actions, connectLabel, connectAriaLabel }: {
  actions: readonly SessionConnectionAction[]
  connectLabel?: string
  connectAriaLabel?: string
}) {
  if (!actions.length) return null
  return <Stack direction="horizontal" align="center" justify="center" wrap="wrap" gap={2}>
    {actions.map(action => {
      const connect = action.kind === 'connect' || action.kind === 'reconnect'
      return <Button key={action.kind} type="button" size="md"
        label={connect && connectLabel ? connectLabel : action.label}
        aria-label={action.kind === 'connect' ? connectAriaLabel : undefined}
        variant={action.kind === 'stop' || action.kind === 'cancel' ? 'secondary' : 'primary'}
        icon={connect ? <Icon icon={action.kind === 'connect' ? ArrowRightOnRectangleIcon : ArrowPathIcon} size="sm"/> : undefined}
        isDisabled={action.disabled}
        onClick={action.run}/>
    })}
  </Stack>
}
