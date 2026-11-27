import { localizeMessage } from '../i18n/messages'
import { tr } from '../i18n/messages'
import { useLocale } from '../i18n/locale'
import { useState, useSyncExternalStore } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { Dialog } from './AppDialog'
import { DialogHeader } from '@astryxdesign/core/Dialog'
import {
  browserState,
  subscribeBrowser,
  browserReconnect,
  browserLogout,
} from '../bridge/browserRuntime'

export default function BrowserStatus({
  account = false,
}: {
  account?: boolean
}) {
  useLocale()
  const state = useSyncExternalStore(
      subscribeBrowser,
      browserState,
      browserState,
    ),
    [confirm, setConfirm] = useState(false),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false)
  if (!state || (state.connected && !account)) return null
  async function logout() {
    setBusy(true)
    setError('')
    try {
      await browserLogout()
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('退出未完成，请重试。'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Stack
        direction="horizontal"
        align="center"
        gap={2}
        paddingInline={3}
        wrap="wrap"
        style={{
          flexShrink: 0,
          borderBlockEnd: 'var(--border-width) solid var(--color-border)',
        }}
      >
        <Text
          type="supporting"
          color="secondary"
          style={{ flex: 1 }}
          role="status"
        >
          {state.connected
            ? tr('浏览器工作区 · {p0}', state.user)
            : state.message || tr('服务连接已断开。')}
        </Text>
        {!state.connected && (
          <Button
            label={tr('重新连接')}
            variant="ghost"
            size="sm"
            isLoading={state.connecting}
            onClick={() => {
              void browserReconnect()?.catch(() => {})
            }}
          />
        )}
        <Button
          label={tr('退出登录')}
          variant="ghost"
          size="sm"
          onClick={() => setConfirm(true)}
        />
      </Stack>
      {confirm && (
        <Dialog
          isOpen
          purpose={busy ? 'required' : 'form'}
          padding={4}
          width="calc(var(--spacing-10) * 12)"
          onOpenChange={(open) => {
            if (!open && !busy) setConfirm(false)
          }}
        >
          <Stack gap={3}>
            <DialogHeader title={tr('退出登录？')} hasDivider={false} />
            <Text>
              {tr('将结束此浏览器登录下的连接与后台任务。')}
              {state.protected
                ? tr('存在尚未保存或正在进行的工作，请先处理。')
                : ''}
            </Text>
            {error && <Text role="alert">{localizeMessage(error)}</Text>}
            <Stack direction="horizontal" justify="end" gap={2}>
              <Button
                label={tr('取消')}
                variant="ghost"
                isDisabled={busy}
                onClick={() => setConfirm(false)}
              />
              <Button
                label={tr('退出登录')}
                isLoading={busy}
                onClick={() => void logout()}
              />
            </Stack>
          </Stack>
        </Dialog>
      )}
    </>
  )
}
