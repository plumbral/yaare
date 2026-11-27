import { useCallback } from 'react'
import { useToast } from '@astryxdesign/core/Toast'
import { Stack } from '@astryxdesign/core/Stack'
import { Icon } from '@astryxdesign/core/Icon'
import { Text } from '@astryxdesign/core/Text'
import { CheckCircleIcon, ExclamationCircleIcon, InformationCircleIcon } from '@heroicons/react/24/outline'
import { localizeMessage } from '../i18n/messages'
import './notice.css'

const icons = { info: InformationCircleIcon, success: CheckCircleIcon, error: ExclamationCircleIcon }

export function useNotice() {
  const showToast = useToast()
  return useCallback((message: string, kind: keyof typeof icons = 'info') => {
    if (!message) return
    showToast({
      type: kind === 'error' ? 'error' : 'info',
      uniqueID: kind + ':' + message,
      autoHideDuration: kind === 'success' ? 3500 : 5500,
      body: <Stack direction="horizontal" align="center" gap={2}>
        <Icon icon={icons[kind]} size="sm" color="inherit" aria-hidden="true"/>
        <Text type="supporting" style={{color:'inherit',overflowWrap:'anywhere'}}>{localizeMessage(message)}</Text>
      </Stack>,
    })
  }, [showToast])
}
