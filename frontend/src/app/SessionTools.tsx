import { lazy, Suspense, useImperativeHandle, useState, type Ref } from 'react'
import { tr } from '../shared/i18n/messages'
import type { LiveSSH } from '../features/connections/useSSHSessions'
import type { Session } from '../features/workspace/mockGateway'
import { leaves, type TerminalWorkspace } from '../features/workspace/terminalLayout'

const BroadcastDialog = lazy(() => import('../features/workspace/BroadcastDialog'))
const SessionForwardsDialog = lazy(() => import('../features/connections/SessionForwardsDialog'))

export interface SessionToolsHandle {
  openForwards(): void
  openBroadcast(groupId: string): void
}

export default function SessionTools({ ref, visible, session, live, runtime, entries }: {
  ref: Ref<SessionToolsHandle>
  visible: boolean
  session?: Session
  live?: LiveSSH
  runtime: Pick<TerminalWorkspace, 'groups' | 'sessions'>
  entries: Record<string, LiveSSH>
}) {
  const [forwards, setForwards] = useState<'unloaded' | 'open' | 'closed'>('unloaded')
  const [broadcast, setBroadcast] = useState<{ groupId: string; open: boolean } | null>(null)
  useImperativeHandle(ref, () => ({
    openForwards: () => setForwards('open'),
    openBroadcast: groupId => setBroadcast({ groupId, open: true }),
  }), [])

  const group = broadcast && runtime.groups.find(item => item.id === broadcast.groupId)
  const targets = group ? leaves(group.root).flatMap((id, index) => {
    const entry = entries[id]
    const target = runtime.sessions.find(item => item.id === id)
    if (entry?.phase !== 'connected' || !target) return []
    const { host } = target
    return [{
      sessionId: id,
      attempt: entry.attempt,
      label: tr("窗格 {p0} · {p1}", index + 1, host.name),
      description: host.username + '@' + host.address + ':' + host.port,
    }]
  }) : []

  return <>
    {broadcast && <Suspense fallback={null}>
      <BroadcastDialog isOpen={broadcast.open} options={targets}
        onClose={() => setBroadcast(previous => previous && { ...previous, open: false })}/>
    </Suspense>}
    {forwards !== 'unloaded' && session && <Suspense fallback={null}>
      <SessionForwardsDialog key={session.id} open={forwards === 'open' && visible}
        host={session.host} live={live} onClose={() => setForwards('closed')}/>
    </Suspense>}
  </>
}
