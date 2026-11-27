import { leaves, type PaneSession, type TerminalGroup } from './terminalLayout'

export interface ConnectionWorkspace {
  id: string
  name: string
  hostId?: string
  groups: TerminalGroup[]
  panes: PaneSession[]
}

export function indexWorkspaces(groups: readonly TerminalGroup[], sessions: ReadonlyMap<string,PaneSession>) {
  const spaces = new Map<string,ConnectionWorkspace>()
  for (const group of groups) {
    const id = group.workspaceId ?? group.id
    const space: ConnectionWorkspace = spaces.get(id) ?? { id, name:group.workspaceName ?? group.name, hostId:group.workspaceHostId, groups:[], panes:[] }
    space.groups.push(group)
    for (const paneId of leaves(group.root)) {
      const pane = sessions.get(paneId)
      if (pane) space.panes.push(pane)
    }
    spaces.set(id,space)
  }
  return spaces
}
