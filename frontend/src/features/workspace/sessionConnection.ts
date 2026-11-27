import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type { ReconnectWait } from '../connections/sshReconnect'
export type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'reconnecting' | 'failed'
export interface ConnectionState { attempt: number; revision: number; status: ConnectionStatus }
export const idleConnection: ConnectionState = { attempt: 0, revision: 0, status: 'idle' }
export type ConnectionEvent = { type: 'begin'; attempt: number; reconnect?: boolean } |
  { type: 'status'; attempt: number; revision: number; status: 'connected' | 'disconnected' | 'failed' }

export function reduceConnection(state: ConnectionState, event: ConnectionEvent): ConnectionState {
  if (event.type === 'begin') {
    if (event.attempt <= state.attempt) return state
    return { attempt: event.attempt, revision: 0, status: event.reconnect ? 'reconnecting' : 'connecting' }
  }
  if (event.attempt !== state.attempt || event.revision <= state.revision || state.status === 'idle') return state
  if (state.status === 'disconnected' || state.status === 'failed') return state
  if (state.status === 'connected' && event.status === 'connected') return { ...state, revision: event.revision }
  return { ...state, revision: event.revision, status: event.status }
}
export const connectionLabels: Record<ConnectionStatus, string> = {
  get idle() { return tr("尚未连接 SSH") }, get connecting() { return tr("正在连接") }, get connected() { return tr("SSH 已连接") }, get disconnected() { return tr("连接已断开") }, get reconnecting() { return tr("正在重连") }, get failed() { return tr("连接失败") },
}

export function reconnectWaitMessage(wait: ReconnectWait, lastError: string) {
  if (wait === 'offline') return tr("当前离线，网络恢复后会自动重试。")
  return lastError || tr("正在等待下一次重试。")
}
