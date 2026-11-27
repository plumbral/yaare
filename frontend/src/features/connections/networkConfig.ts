import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type { ForwardRule, ProxyConfig } from '../../shared/generated/mygo'
import type { HostProfile } from '../workspace/mockGateway'

export const forwardKinds: Record<string, string> = { get local() { return tr("本地转发") }, get remote() { return tr("远程转发") }, get dynamic() { return tr("动态 SOCKS5") } }
const portValid = (port: number) => Number.isInteger(port) && port >= 1 && port <= 65535
const hostValid = (host: string) => /^[a-zA-Z0-9.:[\]-]{1,253}$/.test(host)
export const emptyProxy=():ProxyConfig=>({kind:'none',address:'',port:0,useAuthentication:false,username:''})
export function proxyError(proxy?:ProxyConfig):string {
  if(!proxy)return ''
  if(!proxy.kind||proxy.kind==='none'||proxy.kind==='auto')return proxy.address||proxy.port||proxy.username||proxy.useAuthentication?tr("直连配置不能包含代理参数。"):''
  if(!['socks5','http'].includes(proxy.kind))return tr("请选择 SOCKS5 或 HTTP 代理。")
  if(!hostValid(proxy.address)||!portValid(proxy.port))return tr("请填写代理主机名或 IP，以及 1–65535 的端口；不要包含协议或路径。")
  if(!proxy.useAuthentication)return proxy.username?tr("填写用户名时请启用代理认证。"):''
  const bytes=new TextEncoder().encode(proxy.username).byteLength
  if(!bytes||bytes>255||/[\p{Cc}]/u.test(proxy.username)||(proxy.kind==='http'&&proxy.username.includes(':')))return tr("代理用户名应为 1–255 字节；HTTP 用户名不能包含冒号。")
  return ''
}
export function proxySummary(proxy?: ProxyConfig):string {
  if(!proxy?.kind || proxy.kind==='none')return tr("直连")
  if(proxy.kind==='auto')return tr("自动（系统代理）")
  return `${proxy.kind.toUpperCase()} · ${proxy.address}:${proxy.port}`
}
export function forwardError(rule: ForwardRule): string {
  if (!/^[a-zA-Z0-9_-]{1,80}$/.test(rule.id)) return tr("转发规则编号无效，请删除后重新添加。")
  if (!rule.name.trim() || [...rule.name].length > 64 || /[\p{Cc}\u2028\u2029]/u.test(rule.name)) return tr("请输入 1–64 个字符的规则名称。")
  if (!forwardKinds[rule.kind]) return tr("请选择有效的转发类型。")
  const ip = rule.listenAddress
  const ipv4 = /^(\d{1,3}\.){3}\d{1,3}$/.test(ip) && ip.split('.').every(part => +part <= 255)
  let ipv6 = false
  try { ipv6 = ip.includes(':') && new URL(`http://[${ip}]/`).hostname.length > 0 } catch {}
  if (ip !== 'localhost' && !ipv4 && !ipv6) return tr("监听地址应为 IP 或 localhost。")
  if (!portValid(rule.listenPort)) return tr("监听端口应为 1–65535。")
  if (rule.kind === 'dynamic' && (rule.targetAddress !== '' || rule.targetPort !== 0)) return tr("动态转发不应指定固定目标。")
  if (rule.kind !== 'dynamic' && (!hostValid(rule.targetAddress) || !portValid(rule.targetPort))) return tr("请填写有效的目标地址和端口。")
  return ''
}
export function networkErrors(host: HostProfile, hosts: readonly HostProfile[]) {
  const errors: Partial<Record<'jumpHostIds' | 'forwards' | 'timeoutSeconds' | 'proxy', string>> = {}
  const invalidProxy=proxyError(host.proxy);if(invalidProxy)errors.proxy=invalidProxy
  const ids = host.jumpHostIds ?? []
  if (ids.length > 8 || ids.includes(host.id) || new Set(ids).size !== ids.length) errors.jumpHostIds = tr("最多 8 跳，不能选择自身或重复主机。")
  else if (ids.some(id => !hosts.some(h => h.id === id))) errors.jumpHostIds = tr("跳板机已不存在，请重新选择。")
  else {
    const graph = new Map(hosts.map(h => [h.id, h.jumpHostIds ?? []])); graph.set(host.id, ids)
    const visiting = new Set<string>(), done = new Set<string>()
    const visit = (id: string, depth: number): boolean => {
      if (visiting.has(id) || depth > 64) return false
      if (done.has(id)) return true
      visiting.add(id)
      for (const next of graph.get(id) ?? []) if (!visit(next, depth + 1)) return false
      visiting.delete(id); done.add(id); return true
    }
    if (!visit(host.id, 0)) errors.jumpHostIds = tr("跳板机配置不能形成循环引用。")
  }
  const rules = host.forwards ?? []
  if (rules.length > 32) errors.forwards = tr("每个连接最多保存 32 条转发规则。")
  else {
    const listeners = new Set<string>(), ruleIds = new Set<string>()
    for (const r of rules) {
      const error = forwardError(r)
      const key = `${r.kind === 'remote' ? 'remote' : 'local'}:${r.listenAddress}:${r.listenPort}`
      if (error || ruleIds.has(r.id) || (r.enabled && listeners.has(key))) { errors.forwards = error || tr("规则重复，或使用了相同的监听地址和端口。"); break }
      ruleIds.add(r.id); if (r.enabled) listeners.add(key)
    }
  }
  const seconds = host.timeoutSeconds === undefined || host.timeoutSeconds === 0 ? 15 : host.timeoutSeconds
  if (!Number.isInteger(seconds) || seconds < 3 || seconds > 120) errors.timeoutSeconds = tr("连接超时应为 3–120 秒。")
  return errors
}
export const forwardSummary = (r: ForwardRule) => `${r.listenAddress}:${r.listenPort || tr("端口")} → ${r.kind === 'dynamic' ? 'SOCKS5' : `${r.targetAddress || tr("目标")}:${r.targetPort || tr("端口")}`}`
