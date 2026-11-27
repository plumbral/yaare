import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { desktopAvailable } from '../../shared/bridge/application'
import { createSSHTransport, type SSHEvent, type SSHTransport } from '../../shared/bridge/ssh'
import { SSHService } from '../../shared/generated/mygo'
import type { Challenge, AuthChallenge, ForwardState, DirectoryState, ServerSystem } from '../../shared/generated/mygo'
import type { HostProfile } from '../workspace/mockGateway'
import type { ConnectionEvent } from '../workspace/sessionConnection'
import type {SessionLog} from '../../shared/bridge/sessionLogs'
import {reusableConnectionCredentials} from './authenticationDraft'
import { createSessionOutputActivity } from './sessionOutputActivity'
import { canReconnect, createReconnectQueue, reconnectAttempt, type ReconnectWait } from './sshReconnect'

export interface Credentials {
	useSavedCredential?: boolean
  authentication: 'password' | 'key'
  keyId: string
  useAgent?: boolean
  password: string
  privateKey?: string
  temporaryKeyToken?: string
  passphrase: string
  timeoutSeconds: number
  identityId?: string
}
export interface LiveSSH {
  attempt: number
  transport: SSHTransport
  keyId: string
  phase: 'connecting' | 'hostkey' | 'authentication' | 'connected' | 'failed' | 'disconnected'
  message: string
  code: string
  automatic?: boolean
  retry?: ReconnectWait
  challenge?: Challenge
  authentication?: AuthChallenge
  forwards?:Record<string,ForwardState>
  directory?:DirectoryState
  system?:ServerSystem
  log?:SessionLog|null
}

export interface PaneSSHSource {
  read(id: string): LiveSSH | undefined
  subscribe(id: string, listener: () => void): () => void
}

export function createPaneSSHSource() {
  const entries = new Map<string, LiveSSH>()
  const listeners = new Map<string, Set<() => void>>()
  return {
    read: (id: string) => entries.get(id),
    values: () => entries.values(),
    clear: () => entries.clear(),
    subscribe(id: string, listener: () => void) {
      let group = listeners.get(id)
      if (!group) listeners.set(id, group = new Set())
      group.add(listener)
      return () => {
        group.delete(listener)
        if (!group.size) listeners.delete(id)
      }
    },
    publish(id: string, next?: LiveSSH) {
      if (entries.get(id) === next) return
      if (next) entries.set(id, next)
      else entries.delete(id)
      listeners.get(id)?.forEach(listener => listener())
    },
  }
}

export function usePaneSSH(source: PaneSSHSource, id: string) {
  const subscribe = useCallback((listener: () => void) => source.subscribe(id, listener), [source, id])
  const read = useCallback(() => source.read(id), [source, id])
  return useSyncExternalStore(subscribe, read, read)
}

export function useSSHSessions(
  onState: (id: string, event: ConnectionEvent) => void,
  options = { autoReconnect: true, detectServerSystem: false },
  onServerSystem?: () => void,
  routeAvailable?: (id: string, host: HostProfile) => boolean,
) {
  const [entries, setEntries] = useState<Record<string, LiveSSH>>({})
  const [outputPanes, setOutputPanes] = useState<ReadonlySet<string>>(() => new Set())
  const [outputActivity] = useState(() => createSessionOutputActivity((id, active) => {
    setOutputPanes(previous => {
      const next = new Set(previous)
      if (active) next.add(id)
      else next.delete(id)
      return next
    })
  }))
  const [panes] = useState(createPaneSSHSource)
  const attempts = useRef(new Map<string, number>())
  const probes = useRef(new Map<string, { attempt: number; controller: AbortController }>())
  const notify = useRef(onState)
  notify.current = onState
  const notifySystem = useRef(onServerSystem)
  notifySystem.current = onServerSystem
  const currentRouteCheck = useRef(routeAvailable)
  currentRouteCheck.current = routeAvailable
  const policy = useRef(options)
  policy.current = options
  const connectAgain = useRef<
    (id: string, host: HostProfile, credentials: Credentials, retryCount: number) => void
  >(() => {})
  const [reconnect] = useState(() => createReconnectQueue({
    blocked: () => navigator.onLine === false ? 'offline' : undefined,
    changed: (id, retry) => {
      const entry = panes.read(id)
      if (!entry || entry.retry === retry) return
      const next = { ...entry, retry }
      panes.publish(id, next)
      setEntries(previous => ({ ...previous, [id]: next }))
    },
  }))
  const connect = useCallback(
    (id: string, host: HostProfile, credentials: Credentials, retryCount = 0) => {
      if (!desktopAvailable()) return
      const retryCredentials = reusableConnectionCredentials(credentials)
      const old = panes.read(id)
      if (
        old?.phase === 'connecting' ||
        old?.phase === 'hostkey' ||
        old?.phase === 'authentication' ||
        old?.phase === 'connected'
      )
        return
      reconnect.stop(id)
      old?.transport.close()
      const attempt = (attempts.current.get(id) ?? 0) + 1
      attempts.current.set(id, attempt)
      notify.current(id, { type: 'begin', attempt, reconnect: attempt > 1 })
      let connectedAt: number | undefined
      const publish = (event: SSHEvent) => {
        const entry = panes.read(id)
        if (!entry || entry.attempt !== attempt || entry.phase === 'failed' || entry.phase === 'disconnected')
          return
        if(event.type==='log'&&event.log){if(entry.log&&(entry.log.started>event.log.started||entry.log.id===event.log.id&&entry.log.ended>event.log.ended))return;const next={...entry,log:event.log};panes.publish(id,next);setEntries(previous=>({...previous,[id]:next}));return}
        if(event.type==='forward'&&event.forward){if((entry.forwards?.[event.forward.rule.id]?.revision??-1)>event.forward.revision)return;const next={...entry,forwards:{...entry.forwards,[event.forward.rule.id]:event.forward}};panes.publish(id,next);setEntries(previous=>({...previous,[id]:next}));return}
        if (event.type === 'directory' && event.directory) {
          panes.publish(id, { ...entry, directory: event.directory })
          return
        }
        if(event.type==='progress'){const next={...entry,phase:'connecting' as const,challenge:undefined,authentication:undefined,message:event.message??''};panes.publish(id,next);setEntries(previous=>({...previous,[id]:next}));return}
        if (
          event.type !== 'authentication' &&
          event.type !== 'hostkey' &&
          event.type !== 'connected' &&
          event.type !== 'failed' &&
          event.type !== 'disconnected'
        )
          return
        const next: LiveSSH = {
          ...entry,
          phase: event.type,
          message: event.message ?? '',
          code: event.code ?? '',
          challenge: event.challenge ?? undefined,
          authentication: event.authentication ?? undefined,
          system: undefined,
        }
        panes.publish(id, next)
        if (event.type === 'failed' || event.type === 'disconnected') outputActivity.clear(id)
        setEntries((previous) => ({ ...previous, [id]: next }))
        if (event.type === 'connected' && connectedAt === undefined) connectedAt = performance.now()
        if (event.type !== 'hostkey' && event.type !== 'authentication')
          notify.current(id, { type: 'status', attempt, revision: event.revision, status: event.type })
        if (
          (event.type === 'failed' || event.type === 'disconnected') &&
          policy.current.autoReconnect &&
          retryCredentials &&
          canReconnect(next.code)
        ) {
          const used = reconnectAttempt(retryCount, connectedAt === undefined ? undefined : performance.now() - connectedAt)
          if (used !== undefined) {
            reconnect.schedule(id, used, () => {
              if (policy.current.autoReconnect && panes.read(id)?.attempt === attempt && currentRouteCheck.current?.(id, host) !== false)
                connectAgain.current(id, host, { ...retryCredentials }, used + 1)
            })
          }
        }
      }
      const transport = createSSHTransport(
        {
          sessionId: id,
          hostId:host.id,
          attempt,
          address: host.address,
          port: host.port,
          username: host.username,
          name:host.name,
          proxy:host.proxy??{kind:'none',address:'',port:0,useAuthentication:false,username:''},
          jumpHostIds:host.jumpHostIds??[],
          forwards:host.forwards??[],
          ...credentials,
          identityId: credentials.identityId ?? '',
          useAgent: credentials.useAgent ?? false,
          privateKey: credentials.privateKey ?? '',
        },
        publish,
        () => outputActivity.record(id),
      )
      credentials.password = ''
      credentials.passphrase = ''
      credentials.privateKey = ''
      const entry: LiveSSH = {
        attempt,
        transport,
        keyId: credentials.authentication === 'key' ? credentials.keyId : '',
        phase: 'connecting',
        message: tr("正在连接服务器…"),
        code: '',
        automatic: retryCount > 0,
      }
      panes.publish(id, entry)
      setEntries((previous) => ({ ...previous, [id]: entry }))
    },
    [outputActivity, reconnect, panes],
  )
  connectAgain.current = connect
  useEffect(() => {
    for (const [id, probe] of probes.current) {
      if (!options.detectServerSystem || entries[id]?.phase !== 'connected' || entries[id]?.attempt !== probe.attempt) {
        probe.controller.abort()
        probes.current.delete(id)
      }
    }
    if (!options.detectServerSystem) return
    for (const [id, entry] of Object.entries(entries)) {
      if (entry.phase !== 'connected' || probes.current.has(id)) continue
      const probe = { attempt: entry.attempt, controller: new AbortController() }
      probes.current.set(id, probe)
      void SSHService.probeServerSystem.with({ signal: probe.controller.signal })(id, probe.attempt).then(system => {
        const live = panes.read(id)
        if (!system || !policy.current.detectServerSystem || probes.current.get(id) !== probe ||
          live?.phase !== 'connected' || live.attempt !== probe.attempt) return
        const next = { ...live, system }
        panes.publish(id, next)
        setEntries(previous => ({ ...previous, [id]: next }))
        notifySystem.current?.()
      }).catch(() => {})
    }
  }, [entries, options.detectServerSystem, panes])
  useEffect(() => {
    if (!options.autoReconnect) reconnect.clear()
  }, [options.autoReconnect, reconnect])
  useEffect(() => {
    window.addEventListener('online', reconnect.refresh)
    window.addEventListener('offline', reconnect.refresh)
    return () => {
      window.removeEventListener('online', reconnect.refresh)
      window.removeEventListener('offline', reconnect.refresh)

    }
  }, [reconnect])
  const disconnect = useCallback((id: string) => {
    reconnect.stop(id)
    panes.read(id)?.transport.close()
  }, [reconnect, panes])
  const remove = useCallback((id: string) => {
    const entry = panes.read(id)
    panes.publish(id)
    reconnect.stop(id)
    entry?.transport.close()
    attempts.current.delete(id)
    setEntries((previous) => {
      const next = { ...previous }
      delete next[id]
      return next
    })
  }, [reconnect, panes])
  useEffect(
    () => () => {
      outputActivity.dispose()
      const entries = [...panes.values()]
      panes.clear()
      for (const probe of probes.current.values()) probe.controller.abort()
      probes.current.clear()
      reconnect.clear()
      for (const entry of entries) entry.transport.close()
    },
    [outputActivity, reconnect],
  )
  return { entries, outputPanes, connect, disconnect, remove, retryNow: reconnect.tryNow, currentEntry: panes.read, panes }
}
