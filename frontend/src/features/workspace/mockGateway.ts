import { tr } from '../../shared/i18n/messages'
import type { OsIconName } from '../../shared/icons/OsIcon'
import type { ForwardRule, ProxyConfig } from '../../shared/generated/mygo'
import { idleConnection, reduceConnection, type ConnectionState, type ConnectionEvent } from './sessionConnection'
export interface HostProfile { hasCredential?:boolean; id: string; name: string; address: string; port: number; username: string; group: string; system: string; icon?: OsIconName; authentication?: 'none' | 'password' | 'key'; keyId?: string; useAgent?: boolean; identityId?: string; notes?: string; pinned?: boolean; lastUsed?: number; jumpHostIds?: string[]; forwards?:ForwardRule[]; timeoutSeconds?:number; proxy?:ProxyConfig }

function createPreviewWorkspace() {
  const exampleHosts: HostProfile[] = [
    { id: 'tokyo', name: tr("东京生产机"), address: '192.0.2.20', port: 22, username: 'deploy', group: tr("生产环境"), system: 'Ubuntu 24.04' },
    { id: 'hongkong', name: tr("香港节点"), address: '192.0.2.21', port: 22, username: 'admin', group: tr("生产环境"), system: 'Debian 12' },
    { id: 'database', name: tr("数据库主机"), address: '192.0.2.40', port: 22, username: 'admin', group: tr("生产环境"), system: 'Ubuntu 24.04' },
    { id: 'sandbox', name: tr("开发服务器"), address: '198.51.100.30', port: 22, username: 'developer', group: tr("开发环境"), system: 'Alpine Linux' },
    { id: 'test', name: tr("测试服务器"), address: '198.51.100.60', port: 2222, username: 'tester', group: tr("测试环境"), system: 'Debian 12' },
  ]
  const groups = [
    tr("生产环境"), tr("开发环境"), tr("测试环境"), tr("预发布环境"), tr("北京节点"),
    tr("上海节点"), tr("广州节点"), tr("香港节点"), tr("东京节点"), tr("新加坡节点"),
    tr("欧美节点"), tr("数据库集群"), tr("监控平台"), tr("边缘网关"), '未分组',
  ]
  const distributions = ['Ubuntu 24.04', 'Debian 12', 'Alpine Linux', 'Rocky Linux 9', 'Fedora Linux']
  const reservedAddresses = new Set(exampleHosts.map(host => host.address))
  const exampleNetworks = ['192.0.2', '198.51.100', '203.0.113']
  let addressIndex = 0
  function nextExampleAddress() {
    let address: string
    do {
      address = `${exampleNetworks[Math.floor(addressIndex / 254)]}.${addressIndex % 254 + 1}`
      addressIndex++
    } while (reservedAddresses.has(address))
    return address
  }
  const hosts: HostProfile[] = [...exampleHosts, ...groups.flatMap((group, groupIndex) => {
    const existing = exampleHosts.filter(host => host.group === group).length
    return Array.from({ length: 30 - existing }, (_, index): HostProfile => {
      const number = existing + index + 1
      return {
        id: `demo-${groupIndex + 1}-${number}`, name: tr("{p0}主机 {p1}", group, String(number).padStart(2, '0')),
        address: nextExampleAddress(), port: number % 10 === 0 ? 2222 : 22,
        username: ['deploy', 'admin', 'developer'][index % 3]!, group,
        system: distributions[(groupIndex + index) % distributions.length]!,
      }
    })
  })]
  return { hosts, groups }
}

const preview = import.meta.env.PROD === true ? { hosts: [], groups: [] } : createPreviewWorkspace()
export const initialHosts: HostProfile[] = preview.hosts
export const initialGroups: string[] = preview.groups
export const mockGateway = {
  listHosts: () => initialHosts.map(host => ({ ...host })),
  listGroups: () => [...initialGroups],
}
export interface Session { id: string; host: HostProfile; connection?: ConnectionState; pending?: boolean }
export interface WorkspaceState { hosts: HostProfile[]; sessions: Session[]; activeId: string }
export type Action = { type: 'replaceHosts'; hosts: HostProfile[] } | { type: 'open' | 'connect'; host: HostProfile; id: string } | { type: 'activate'; id: string } | { type: 'close'; id: string } | { type: 'save'; host: HostProfile } | { type: 'connection'; id: string; event: ConnectionEvent }
export function workspaceReducer(state: WorkspaceState, action: Action): WorkspaceState {
  if (action.type === 'replaceHosts') return { ...state, hosts: action.hosts }
  if (action.type === 'connection') {
    const session = state.sessions.find(session => session.id === action.id)
    if (!session) return state
    const connection = reduceConnection(session.connection ?? idleConnection, action.event)
    if (connection === session.connection || connection === idleConnection) return state
    return { ...state, sessions: state.sessions.map(item => item === session ? { ...item, connection } : item) }
  }
  if (action.type === 'save') return { ...state, hosts: state.hosts.some(host => host.id === action.host.id) ? state.hosts.map(host => host.id === action.host.id ? action.host : host) : [...state.hosts, action.host] }
  if (action.type === 'activate') return state.sessions.some(session => session.id === action.id) ? { ...state, activeId: action.id } : state
  if (action.type === 'open' || action.type === 'connect') {
    const existing = state.sessions.find(session => session.host.id === action.host.id)
    return existing && action.type === 'open' ? { ...state, activeId: existing.id } : { ...state, activeId: action.id, sessions: [...state.sessions, { id: action.id, host: { ...action.host } }] }
  }
  const index = state.sessions.findIndex(session => session.id === action.id)
  const sessions = state.sessions.filter(session => session.id !== action.id)
  return { ...state, sessions, activeId: state.activeId === action.id ? sessions[Math.max(0, index - 1)]?.id ?? '' : state.activeId }
}
export function validateHost(host: HostProfile): Partial<Record<keyof HostProfile, string>> {
  const errors: Partial<Record<keyof HostProfile, string>> = {}
  if (!host.name.trim() || host.name.length > 64) errors.name = tr("请输入 1–64 个字符的连接名称。")
  if (!/^[a-zA-Z0-9.:[\]-]{1,253}$/.test(host.address)) errors.address = tr("请输入主机名、IPv4 或 IPv6 地址，不含协议和路径。")
  if (!Number.isInteger(host.port) || host.port < 1 || host.port > 65535) errors.port = tr("端口应为 1–65535 之间的整数。")
  if (!host.username.trim() || host.username.length > 64) errors.username = tr("请输入有效的用户名。")
  if (!host.group.trim() || host.group.split('/').length>8 || host.group.split('/').some(p=>!p||p.trim()!==p||[...p].length>32||p==='.'||p==='..'||/[\p{Cc}\\]/u.test(p))) errors.group=tr("分组最多 8 层，每层 1–32 个字符。")
  if(host.notes&&([...(host.notes)].length>2048||/[\x00\r]/.test(host.notes)))errors.notes=tr("备注最多 2048 个字符。")
  return errors
}
