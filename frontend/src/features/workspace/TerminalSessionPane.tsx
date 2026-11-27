import { lazy, memo, Suspense, useCallback, useMemo, type ComponentProps } from 'react'
import { Text } from '@astryxdesign/core/Text'
import { usePaneSSH, type PaneSSHSource } from '../connections/useSSHSessions'
import { useLocale } from '../../shared/i18n/locale'
import { localizeMessage, tr } from '../../shared/i18n/messages'
import TerminalPane from '../terminal/TerminalPane'
import { reconnectWaitMessage } from './sessionConnection'
import type { Session } from './mockGateway'

const SessionLogWorkspace = lazy(() => import('./SessionLogWorkspace'))
const FileDiffWorkspace = lazy(() => import('./FileDiffWorkspace'))
const RemoteDesktopWorkspace = lazy(() => import('./RemoteDesktopWorkspace'))

type TerminalProps = ComponentProps<typeof TerminalPane>
type Props = Pick<TerminalProps, 'tabId' | 'focused' | 'mode' | 'terminalPalette' | 'fontSize' |
  'fontFamily' | 'fallbackFont' | 'lineHeight' | 'scrollback' | 'cursorBlink' | 'screenReaderMode' | 'onFileGuard' |
  'onProtection' | 'onReady' | 'onFocusAvailable' | 'onError' | 'onActionError'> & {
  session: Session
  source: PaneSSHSource
  shown: boolean
  desktopLoaded: boolean
  desktopOpen: boolean
  logsOpen: boolean
  diffOpen: boolean
  diffRequest?: { id: string; relative: string }
  showTerminal: (id: string) => void
  closeDiff: (id: string) => void
  closeLogs: (id: string) => void
  connect: (id: string) => void
  disconnect: (id: string) => void
  retry: (id: string) => void
}

export default memo(function TerminalSessionPane({
  session, source, shown, desktopLoaded, desktopOpen, logsOpen,
  diffOpen, diffRequest, showTerminal, closeDiff, closeLogs, connect, disconnect, retry, ...terminal
}: Props) {
  const language = useLocale()
  const id = session.id
  const live = usePaneSSH(source, id)
  const { phase, message, retry: waiting, automatic } = live ?? {}
  const status = waiting ? 'reconnecting' : session.connection?.status ?? 'idle'
  const connecting = status === 'connecting' || status === 'reconnecting'
  const onConnect = useCallback(() => connect(id), [connect, id])
  const onDisconnect = useCallback(() => disconnect(id), [disconnect, id])
  const onRetryNow = useCallback(() => retry(id), [retry, id])
  const onCloseDesktop = useCallback(() => showTerminal(id), [showTerminal, id])
  const onCloseDiff = useCallback(() => closeDiff(id), [closeDiff, id])
  const onCloseLogs = useCallback(() => closeLogs(id), [closeLogs, id])
  const connectionPrompt = useMemo<TerminalProps['connectionPrompt']>(() => status === 'connected' ? undefined : {
    status,
    message: waiting ? reconnectWaitMessage(waiting, localizeMessage(message ?? ''))
      : connecting && phase === 'hostkey' ? tr("等待指纹确认")
      : connecting && phase === 'authentication' ? tr("等待认证")
      : connecting || status === 'failed' ? localizeMessage(message ?? '') : undefined,
    onAction: connecting ? onDisconnect : onConnect,
    waiting, automatic: connecting && !!automatic, onRetryNow,
  }, [status, waiting, connecting, phase, message, automatic, onConnect, onDisconnect, onRetryNow, language])
  const connected = phase === 'connected'
  const attempt = live?.attempt ?? 0

  return <>
    {desktopLoaded && <Suspense key="desktop" fallback={shown && desktopOpen ? <Text type="supporting">{tr("正在打开远程桌面…")}</Text> : null}>
      <RemoteDesktopWorkspace sessionId={id} attempt={attempt} connected={connected} active={shown && desktopOpen} onClose={onCloseDesktop}/>
    </Suspense>}
    {diffRequest && <Suspense key="comparison" fallback={shown && diffOpen ? <Text type="supporting">{tr("正在打开文件比较…")}</Text> : null}>
      <FileDiffWorkspace request={diffRequest} active={shown && diffOpen} onClose={onCloseDiff}/>
    </Suspense>}
    {shown && logsOpen && <Suspense key="logs" fallback={<Text type="supporting">{tr("正在打开会话日志…")}</Text>}>
      <SessionLogWorkspace sessionId={id} attempt={attempt} connected={connected} current={live?.log} active onClose={onCloseLogs}/>
    </Suspense>}
    <TerminalPane {...terminal} key="terminal" id={id} host={session.host} hideToolbar connectionPrompt={connectionPrompt}
      active={shown && !desktopOpen && !logsOpen && !diffOpen}
      attempt={attempt} transport={live?.transport} inputEnabled={connected}/>
  </>
})
