import type { HostProfile } from '../workspace/mockGateway'

export type HostScope = 'all' | 'pinned' | 'recent'
function searchableHostText(host: HostProfile) {
  return `${host.name} ${host.address} ${host.port} ${host.username} ${host.group} ${host.system} ${host.notes ?? ''}`.toLowerCase()
}

export function createHostSearchIndex() {
  const cache = new WeakMap<HostProfile, string>()
  return (host: HostProfile) => {
    let text = cache.get(host)
    if (text === undefined) {
      text = searchableHostText(host)
      cache.set(host, text)
    }
    return text
  }
}

export function filterHostList(hosts: readonly HostProfile[], query: string, group = 'all', scope: HostScope = 'all', pinOrder: readonly string[] = [], textFor = searchableHostText): HostProfile[] {
  const search = query.trim().toLowerCase(), name = group.slice(6)
  const matches = hosts.filter(host => (group === 'all' || host.group === name || host.group.startsWith(name + '/'))
    && (scope === 'all' || scope === 'pinned' && host.pinned || scope === 'recent' && !!host.lastUsed)
    && (!search || textFor(host).includes(search)))
  return scope === 'recent' ? matches.sort((a, b) => (b.lastUsed ?? 0) - (a.lastUsed ?? 0))
    : [...orderedPins(matches,pinOrder), ...matches.filter(host => !host.pinned)]
}

export type SidebarHostRow = HostProfile | { id: string; group: string; count: number; pinned?: boolean }
export function sidebarHostRows(hosts: readonly HostProfile[], pinOrder: readonly string[] = []): SidebarHostRow[] {
  const pinned = orderedPins(hosts,pinOrder), groups = new Map<string, HostProfile[]>()
  for (const host of hosts) {
    if (host.pinned) continue
    const members = groups.get(host.group) ?? []
    members.push(host); groups.set(host.group, members)
  }
  return [
    ...(pinned.length ? [{ id: 'section-pinned', group: '', count: pinned.length, pinned: true }, ...pinned] : []),
    ...[...groups].flatMap<SidebarHostRow>(([group, members]) => group === '未分组' ? members : [{ id: `group-${group}`, group, count: members.length }, ...members]),
  ]
}

export function orderedPins(hosts: readonly HostProfile[], pinOrder: readonly string[] = []): HostProfile[] {
  const ranks=new Map(pinOrder.map((id,index)=>[id,index]))
  return hosts.filter(host=>host.pinned).sort((a,b)=>(ranks.get(a.id)??Infinity)-(ranks.get(b.id)??Infinity))
}
export function reorderHostSubset(hosts: readonly HostProfile[], pinOrder: readonly string[], ids: readonly string[], pinned: boolean) {
  const available=new Map(hosts.filter(h=>!!h.pinned===pinned).map(h=>[h.id,h])),selected=new Set(ids)
  if(!ids.length||selected.size!==ids.length||ids.some(id=>!available.has(id)))throw Error('主机排序范围无效，请刷新后重试。')
  let index=0
  return pinned?{hosts:[...hosts],pinOrder:orderedPins(hosts,pinOrder).map(h=>selected.has(h.id)?ids[index++]!:h.id)}
    :{hosts:hosts.map(h=>selected.has(h.id)?available.get(ids[index++]!)!:h),pinOrder:[...pinOrder]}
}
export type HostMove='first'|'up'|'down'|'last'
export function moveHostIDs(ids: readonly string[], id: string, destination: HostMove | number): string[] {
  const from=ids.indexOf(id);if(from<0)return [...ids]
  const to=typeof destination==='number'?destination:destination==='first'?0:destination==='last'?ids.length-1:from+(destination==='up'?-1:1)
  if(to<0||to>=ids.length||from===to)return [...ids]
  const next=[...ids];next.splice(from,1);next.splice(to,0,id);return next
}
