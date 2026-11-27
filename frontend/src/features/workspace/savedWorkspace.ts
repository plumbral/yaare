import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type { HostProfile } from './mockGateway'
import { networkErrors } from '../connections/networkConfig'
import { orderedPins, reorderHostSubset } from '../connections/hostOrdering'
import {
  canAutoUpdateHostIcon,
  commandGroup,
  commandUngrouped,
  normalizeCommandText,
  validateQuickCommand,
  type QuickCommand,
} from './shortcuts'
import { isSystemIcon } from '../../shared/icons/OsIcon'
export const ungrouped = '未分组'
export interface SavedWorkspace {
  pinOrder?: string[]
  revision: number
  hosts: HostProfile[]
  groups: string[]
  commands: QuickCommand[]
  commandGroups: string[]
}
export type WorkspaceChange =
  | { kind: 'reorderHosts'; ids: string[] }
  | { kind: 'reorderPinnedHosts'; ids: string[] }
  | { kind: 'reorderGroups'; groups: string[] }
  | { kind: 'saveHost'; host: HostProfile }
  | { kind: 'saveHosts'; hosts: HostProfile[] }
  | { kind: 'deleteHosts'; ids: string[] }
  | { kind: 'deleteHost'; id: string }
  | { kind: 'duplicateHost'; id: string }
  | { kind: 'pinHost'; id: string; pinned: boolean }
  | { kind: 'markHostUsed'; id: string }
  | { kind: 'setHostSystemIcon'; id: string; name: string }
  | { kind: 'addGroup'; name: string }
  | { kind: 'deleteGroup'; name: string }
  | { kind: 'renameGroup'; name: string; newName: string }
  | { kind: 'saveCommands'; commands: QuickCommand[] }
  | { kind: 'saveCommand'; command: QuickCommand }
  | { kind: 'deleteCommand'; id: string }
  | { kind: 'addCommandGroup'; name: string }
  | { kind: 'deleteCommandGroup'; name: string }
  | { kind: 'renameCommandGroup'; name: string; newName: string }
  | { kind: 'reorderCommandGroups'; commandGroups: string[] }
export function previewChange(current: SavedWorkspace, change: WorkspaceChange): SavedWorkspace {
  const next = {
    ...current,
    hosts: current.hosts.map((h) => ({ ...h })),
    groups: [...current.groups],
    commandGroups: [...current.commandGroups],
    commands: current.commands.map((c) => ({ ...c })),
    revision: current.revision + 1,
  }
  if(change.kind==='reorderHosts'||change.kind==='reorderPinnedHosts'){
    const result=reorderHostSubset(next.hosts,next.pinOrder??[],change.ids,change.kind==='reorderPinnedHosts');next.hosts=result.hosts;next.pinOrder=result.pinOrder
  } else if (change.kind === 'reorderGroups') {
    if (change.groups.length !== next.groups.length || change.groups.indexOf(ungrouped) !== next.groups.indexOf(ungrouped)
      || new Set(change.groups).size !== next.groups.length || change.groups.some(group => !next.groups.includes(group)))
      throw new Error(tr("分组排序无效。"))
    next.groups = [...change.groups]
  } else if (change.kind === 'saveHosts' || change.kind === 'deleteHosts') {
    const ids=change.kind==='saveHosts'?change.hosts.map(h=>h.id):change.ids
    if(!ids.length||new Set(ids).size!==ids.length||ids.some(id=>!next.hosts.some(h=>h.id===id)))throw Error(tr("所选主机已变化，请重新选择。"))
    const selected=new Set(ids)
    if(change.kind==='deleteHosts') {
      if(next.hosts.some(h=>!selected.has(h.id)&&h.jumpHostIds?.some(id=>selected.has(id))))throw Error(tr("所选主机仍被其他连接用作跳板机，请先解除关联。"))
      next.hosts=next.hosts.filter(h=>!selected.has(h.id))
    } else {
      const replacements=new Map(change.hosts.map(h=>[h.id,h]))
      next.hosts=next.hosts.map(h=>replacements.has(h.id)?{...replacements.get(h.id)!,lastUsed:h.lastUsed}:h)
      for(const host of next.hosts){const errors=networkErrors(host,next.hosts);if(Object.keys(errors).length)throw Error(Object.values(errors)[0])}
      for(const host of change.hosts)if(!next.groups.includes(host.group))next.groups.push(host.group)
    }
  } else if (change.kind === 'saveHost') {
    const host = { ...change.host }
    const errors=networkErrors(host,[...next.hosts.filter(h=>h.id!==host.id),host]);if(Object.keys(errors).length)throw new Error(Object.values(errors)[0])
    if (!next.groups.includes(host.group)) next.groups.push(host.group)
    const index = next.hosts.findIndex((h) => h.id === host.id)
    if (index < 0) next.hosts.push({ ...host, lastUsed: 0 })
    else next.hosts[index] = { ...host, lastUsed: next.hosts[index]?.lastUsed }
  } else if (change.kind === 'pinHost')
    next.hosts = next.hosts.map((h) => (h.id === change.id ? { ...h, pinned: change.pinned } : h))
  else if (change.kind === 'markHostUsed')
    next.hosts = next.hosts.map((h) => (h.id === change.id ? { ...h, lastUsed: Date.now() } : h))
  else if (change.kind === 'setHostSystemIcon') {
    const icon = change.name.trim()
    if (!icon || !isSystemIcon(icon)) throw new Error(tr("系统图标无效。"))
    const index = next.hosts.findIndex((h) => h.id === change.id)
    if (index < 0) throw new Error(tr("此主机配置已不存在。"))
    if (!canAutoUpdateHostIcon(next.hosts[index]!.icon) || (next.hosts[index]!.icon || 'linux') === icon) return current
    next.hosts[index] = { ...next.hosts[index]!, icon }
  }
  else if (change.kind === 'deleteHost') {if(next.hosts.some(h=>h.jumpHostIds?.includes(change.id)))throw new Error(tr("此主机被用作跳板机，请先从其他连接的网络配置中移除。"));next.hosts = next.hosts.filter((h) => h.id !== change.id)}
  else if (change.kind === 'duplicateHost') {
    const host = next.hosts.find((h) => h.id === change.id)
    if (!host) throw new Error(tr("此主机已不存在。"))
    let name = tr("{p0}（副本）", host.name.slice(0, 58))
    let number = 1
    while (next.hosts.some((h) => h.name === name)) name = `${host.name.slice(0, 52)}（副本 ${++number}）`
    next.hosts.push({ ...host, id: crypto.randomUUID(), name, lastUsed: 0 })
  } else if (change.kind === 'saveCommands')
    next.commands = change.commands.map((c) => ({
      ...c,
      group: commandGroup(c),
      command: normalizeCommandText(c.command),
    }))
  else if (change.kind === 'saveCommand') {
    const command = {
      ...change.command,
      name: change.command.name.trim(),
      group: commandGroup(change.command),
      command: normalizeCommandText(change.command.command),
    }
    const error = validateQuickCommand(command)
    if (error || !next.commandGroups.includes(command.group)) throw new Error(error || tr("此命令分组已不存在。"))
    const index = next.commands.findIndex((c) => c.id === command.id)
    if (index < 0) next.commands.push(command)
    else next.commands[index] = command
  } else if (change.kind === 'deleteCommand') next.commands = next.commands.filter((c) => c.id !== change.id)
  else if (change.kind === 'reorderCommandGroups') {
    if (
      change.commandGroups.length !== next.commandGroups.length ||
      change.commandGroups[0] !== commandUngrouped ||
      new Set(change.commandGroups).size !== next.commandGroups.length ||
      change.commandGroups.some((g) => !next.commandGroups.includes(g))
    )
      throw new Error(tr("命令分组排序无效。"))
    next.commandGroups = [...change.commandGroups]
  } else if (
    change.kind === 'addCommandGroup' ||
    change.kind === 'renameCommandGroup' ||
    change.kind === 'deleteCommandGroup'
  ) {
    const name = change.name.trim()
    if (change.kind === 'deleteCommandGroup') {
      if (name === commandUngrouped) throw new Error(tr("未分组不能删除。"))
      next.commandGroups = next.commandGroups.filter((g) => g !== name)
      next.commands = next.commands.map((c) =>
        commandGroup(c) === name ? { ...c, group: commandUngrouped } : c,
      )
    } else {
      const target = change.kind === 'addCommandGroup' ? name : change.newName.trim()
      if (!target || [...target].length > 32 || /[\p{Cc}\u2028\u2029]/u.test(target))
        throw new Error(tr("请输入 1–32 个字符的分组名称。"))
      if (next.commandGroups.includes(target)) throw new Error(tr("此命令分组已存在。"))
      if (change.kind === 'addCommandGroup') next.commandGroups.push(target)
      else {
        if (name === commandUngrouped) throw new Error(tr("未分组不能重命名。"))
        next.commandGroups = next.commandGroups.map((g) => (g === name ? target : g))
        next.commands = next.commands.map((c) => (commandGroup(c) === name ? { ...c, group: target } : c))
      }
    }
  } else {
    const name = change.name.trim()
    if (change.kind === 'addGroup' || change.kind === 'renameGroup') {
      const target = change.kind === 'addGroup' ? name : change.newName.trim()
      if (!validHostGroup(target)) throw new Error(tr("最多 8 层，每层 1–32 个字符，用 / 分隔层级。"))
      if (next.groups.includes(target)) throw new Error(tr("此分组已存在。"))
      if (change.kind === 'addGroup') next.groups.push(target)
      else {
        if (name === ungrouped) throw new Error(tr("未分组不能重命名。"))
        if (inGroup(target, name)) throw new Error(tr("不能将分组移入自身。"))
        if (
          next.groups.some(
            (g) =>
              inGroup(g, name) &&
              next.groups.includes(target + g.slice(name.length)) &&
              !inGroup(target + g.slice(name.length), name),
          )
        )
          throw new Error(tr("目标层级已存在同名分组。"))
        next.groups = next.groups.map((g) => (inGroup(g, name) ? target + g.slice(name.length) : g))
        next.hosts = next.hosts.map((h) =>
          inGroup(h.group, name) ? { ...h, group: target + h.group.slice(name.length) } : h,
        )
      }
    } else {
      if (name === ungrouped) throw new Error(tr("未分组不能删除。"))
      next.groups = next.groups.filter((g) => !inGroup(g, name))
      next.hosts = next.hosts.map((h) => (inGroup(h.group, name) ? { ...h, group: ungrouped } : h))
    }
  }
  for (const g of [...next.groups]) {
    const parts = g.split('/')
    for (let i = 1; i < parts.length; i++) {
      const parent = parts.slice(0, i).join('/')
      if (!next.groups.includes(parent)) next.groups.push(parent)
    }
  }
  if (
    next.commandGroups.length > 256 ||
    next.commands.length > 1000 ||
    next.commands.reduce((sum, c) => sum + new TextEncoder().encode(c.command).byteLength, 0) >
      4 * 1024 * 1024
  )
    throw new Error(tr("命令数量或内容总量超出支持范围。"))
  if (
    !next.commandGroups.includes(commandUngrouped) ||
    new Set(next.commandGroups).size !== next.commandGroups.length
  )
    throw new Error(tr("命令分组无效。"))
  for (const command of next.commands) {
    const error = validateQuickCommand(command)
    if (error || !next.commandGroups.includes(commandGroup(command)))
      throw new Error(error || tr("命令分组无效。"))
  }
  if(next.pinOrder?.length)next.pinOrder=orderedPins(next.hosts,next.pinOrder).map(h=>h.id)
  return next
}

export const inGroup = (group: string, parent: string) => group === parent || group.startsWith(parent + '/')
export const validHostGroup = (name: string) =>
  name.split('/').length <= 8 &&
  name
    .split('/')
    .every(
      (p) =>
        !!p &&
        p.trim() === p &&
        [...p].length <= 32 &&
        !/[\p{Cc}\u2028\u2029\\]/u.test(p) &&
        p !== '.' &&
        p !== '..',
    ) &&
  (!name.includes('/') || !name.startsWith('未分组/'))
