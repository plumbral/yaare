import { localizeMessage, tr } from '../../shared/i18n/messages'
import type { ReconnectWait } from '../connections/sshReconnect'
import { connectionLabels, reconnectWaitMessage, type ConnectionStatus } from './sessionConnection'

export type SessionConnectionAction = {
  kind: 'connect' | 'reconnect' | 'cancel' | 'stop' | 'retry'
  label: string
  run: () => void
  disabled?: boolean
}

export type SessionConnectionPrompt = {
  label: string
  message: string
  actions: readonly SessionConnectionAction[]
} & (
  | { kind: 'idle' | 'failed' | 'disconnected' | 'offline'; busy: false }
  | { kind: 'connecting' | 'reconnecting' | 'retrying' | 'waiting'; busy: true }
)

type ConnectionLiveState = {
  attempt: number
  phase: string
  message?: string
  retry?: ReconnectWait
  automatic?: boolean
}

function connectionStatus(snapshot: ConnectionStatus | undefined, live?: ConnectionLiveState): ConnectionStatus {
  switch (live?.phase) {
    case 'connected':
    case 'failed':
    case 'disconnected':
      return live.phase
    case 'connecting':
    case 'hostkey':
    case 'authentication':
      return live.automatic || live.attempt > 1 ? 'reconnecting' : 'connecting'
    default:
      return snapshot === 'connected' ? 'disconnected' : snapshot ?? 'idle'
  }
}

export function resolveSessionConnectionPrompt(
  snapshot: ConnectionStatus | undefined,
  live?: ConnectionLiveState,
  onConnect?: () => void,
  onDisconnect?: () => void,
): SessionConnectionPrompt | undefined {
  const status = connectionStatus(snapshot, live)
  if (status === 'connected') return
  const label = connectionLabels[status]
  const detail = localizeMessage(live?.message ?? '')

  if (live?.retry) {
    const actions: SessionConnectionAction[] = []
    if (onDisconnect) actions.push({ kind: 'stop', label: tr('停止重连'), run: onDisconnect })
    if (onConnect) actions.push({ kind: 'retry', label: tr('立即重试'), run: onConnect, disabled: live.retry === 'offline' })
    const prompt = { label: connectionLabels.reconnecting, message: reconnectWaitMessage(live.retry, detail), actions }
    return live.retry === 'offline'
      ? { ...prompt, kind: 'offline', busy: false }
      : { ...prompt, kind: 'waiting', busy: true }
  }
  if (status === 'connecting' || status === 'reconnecting') {
    const message = live?.phase === 'hostkey' ? tr('等待指纹确认') : live?.phase === 'authentication' ? tr('等待认证') : detail
    if (live?.automatic) {
      return { kind: 'retrying', label, message, busy: true,
        actions: onDisconnect ? [{ kind: 'stop', label: tr('停止重连'), run: onDisconnect }] : [],
      }
    }
    return { kind: status, label, message, busy: true,
      actions: onDisconnect ? [{ kind: 'cancel', label: tr('取消连接'), run: onDisconnect }] : [],
    }
  }
  if (status === 'idle') {
    return { kind: status, label, message: '', busy: false,
      actions: onConnect ? [{ kind: 'connect', label: tr('连接'), run: onConnect }] : [],
    }
  }
  return { kind: status, label, message: detail, busy: false,
    actions: onConnect ? [{ kind: 'reconnect', label: tr('重新连接'), run: onConnect }] : [],
  }
}
