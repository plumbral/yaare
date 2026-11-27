import { useCallback, useLayoutEffect, useMemo, useRef } from 'react'
import type { HostProfile } from '../workspace/mockGateway'
import { leaves, type PaneSession, type TerminalGroup } from '../workspace/terminalLayout'
import { sameCredentialTarget } from './authenticationDraft'

export function sameConnectionTarget(a: HostProfile, b: HostProfile) {
  const x = a.proxy, y = b.proxy
  const aKind = x?.kind || 'none', bKind = y?.kind || 'none'
  const hops = a.jumpHostIds ?? [], otherHops = b.jumpHostIds ?? []
  return sameCredentialTarget(a, b) && aKind === bKind &&
    (aKind === 'none' || aKind === 'auto' || (x?.address === y?.address && x?.port === y?.port &&
      !!x?.useAuthentication === !!y?.useAuthentication && x?.username === y?.username)) &&
    hops.length === otherHops.length && hops.every((id, i) => id === otherHops[i])
}

function hopTargets(host: HostProfile, hosts: ReadonlyMap<string, HostProfile>) {
  return JSON.stringify((host.jumpHostIds ?? []).map(id => {
    const hop = hosts.get(id)
    return hop && [id, hop.address, hop.port, hop.username, hop.authentication, hop.identityId, hop.keyId, !!hop.useAgent]
  }))
}

export function committedConnection(hostId: string, committed: readonly HostProfile[], current: readonly HostProfile[]) {
  const host = committed.find(item => item.id === hostId)
  if (!host || committed === current) return host
  const latest = current.find(item => item.id === hostId)
  if (!latest || !sameConnectionTarget(host, latest)) return
  if (host.jumpHostIds?.length) {
    const before = new Map(committed.map(item => [item.id, item]))
    const after = new Map(current.map(item => [item.id, item]))
    if (hopTargets(host, before) !== hopTargets(latest, after)) return
  }
  return host
}

export function useHostSessions(hosts: HostProfile[], groups: TerminalGroup[], panes: Map<string, PaneSession>, activeId: string) {
  const saved = useMemo(() => new Map(hosts.map(host => [host.id, host])), [hosts])
  const routes = useRef(new Map<string, string>())
  const recent = useRef<string[]>([])
  const active = panes.get(activeId)
  useLayoutEffect(() => {
    recent.current = recent.current.filter(id => panes.has(id) && id !== activeId)
    if (active && !active.pending) recent.current.unshift(activeId)
  }, [activeId, active?.pending, panes.size])
  useLayoutEffect(() => {
    for (const id of routes.current.keys()) if (!panes.has(id)) routes.current.delete(id)
    for (const pane of panes.values()) {
      if (pane.host.jumpHostIds?.length && !routes.current.has(pane.id)) {
        const inherited = pane.routeSourceId && routes.current.get(pane.routeSourceId)
        routes.current.set(pane.id, inherited || hopTargets(pane.host, saved))
      }
    }
  }, [panes, saved])
  const hopsMatch = useCallback((id: string, host: HostProfile) =>
    !routes.current.has(id) || routes.current.get(id) === hopTargets(host, saved), [saved])
  const index = useMemo(() => {
    const byHost = new Map<string, PaneSession[]>(), connected = new Set<string>()
    const connectedAnywhere = new Set<string>()
    for (const pane of panes.values()) {
      const host = saved.get(pane.host.id)
      if (host && pane.connection?.status === 'connected' && sameConnectionTarget(pane.host, host) && hopsMatch(pane.id, host))
        connectedAnywhere.add(host.id)
    }
    for (const group of groups) {
      const host = saved.get(group.workspaceHostId ?? '')
      if (!host) continue
      for (const id of leaves(group.root)) {
        const pane = panes.get(id)
        if (!pane || pane.pending || !sameConnectionTarget(pane.host, host)) continue
        if (!hopsMatch(id, host)) continue
        const list = byHost.get(host.id) ?? []
        list.push(pane)
        byHost.set(host.id, list)
        if (pane.connection?.status === 'connected') connected.add(host.id)
      }
    }
    return { byHost, connected, connectedAnywhere }
  }, [groups, panes, saved, hopsMatch])
  function preferred(host: HostProfile) {
    const candidates = index.byHost.get(host.id) ?? []
    const pool = index.connected.has(host.id) ? candidates.filter(pane => pane.connection?.status === 'connected') : candidates
    const rank = new Map(recent.current.map((id, i) => [id, i]))
    return pool.reduce<PaneSession | undefined>((best, pane) =>
      !best || (rank.get(pane.id) ?? Infinity) < (rank.get(best.id) ?? Infinity) ? pane : best, undefined)
  }
  function selectRoute(id: string, host: HostProfile) {
    if (host.jumpHostIds?.length) routes.current.set(id, hopTargets(host, saved))
    else routes.current.delete(id)
  }
  return { ...index, preferred, hopsMatch, selectRoute }
}
