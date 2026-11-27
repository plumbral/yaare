import { tr } from '../../shared/i18n/messages'
import type { ServerSystem } from '../../shared/generated/mygo'
import { isSystemIcon, type SystemIconName } from '../../shared/icons/OsIcon'
import { compareListValues, type ListSort } from '../../shared/ui/listSort'
export interface QuickCommand { id: string; name: string; command: string; group?: string; icon?: string }
export const commandUngrouped = '未分组'
export const maxCommandBytes = 64 * 1024
export const normalizeCommandText = (value: string) => value.replace(/\r\n/g, '\n')
export const commandGroup = (command: QuickCommand) => command.group || commandUngrouped
export const commandSortKeys = ['name', 'group'] as const
export type CommandSortKey = typeof commandSortKeys[number]

export function sortCommandRows<Row>(rows: Row[], command: (row: Row) => QuickCommand, sort: ListSort<CommandSortKey>, collator: Intl.Collator) {
  if (!sort) return rows
  return [...rows].sort((left, right) => {
    const a = command(left), b = command(right)
    if (sort.key === 'name') return compareListValues(a.name, b.name, collator, sort.direction)
    const aGroup = commandGroup(a), bGroup = commandGroup(b)
    return compareListValues(aGroup === commandUngrouped ? undefined : aGroup, bGroup === commandUngrouped ? undefined : bGroup,
      collator, sort.direction) || collator.compare(a.name, b.name)
  })
}
export const commandIcon = (command: QuickCommand): SystemIconName =>
  command.icon && isSystemIcon(command.icon) ? command.icon : 'linux'

const distroIcons: Readonly<Record<string, SystemIconName>> = {
  alpine: 'alpinelinux', arch: 'archlinux', rhel: 'redhat', rocky: 'rockylinux', alma: 'almalinux',
  'centos-stream': 'centos', 'opensuse-leap': 'opensuse', 'opensuse-tumbleweed': 'opensuse', 'opensuse-microos': 'opensuse',
  raspbian: 'raspberrypi', 'raspberry-pi-os': 'raspberrypi', kali: 'kalilinux', mint: 'linuxmint',
}
export const canAutoUpdateHostIcon = (icon?: string) => !icon || isSystemIcon(icon)
export function detectedSystemIcon(system?: ServerSystem): SystemIconName | undefined {
  if (!system) return undefined
  if (system.family === 'linux') {
    const raw = (system.id ?? '').toLowerCase()
    const mapped = Object.hasOwn(distroIcons, raw) ? distroIcons[raw]! : raw
    return mapped && isSystemIcon(mapped) ? mapped : 'linux'
  }
  return isSystemIcon(system.family) ? system.family : undefined
}
export function resolveDetectedHostIcon(currentIcon: string | undefined, system?: ServerSystem): SystemIconName | undefined {
  if (!canAutoUpdateHostIcon(currentIcon)) return undefined
  const detected = detectedSystemIcon(system)
  if (!detected || detected === (currentIcon || 'linux')) return undefined
  return detected
}
export function commandMatchesSystem(command: QuickCommand, system?: ServerSystem): boolean {
  if (!system || (command.icon && !isSystemIcon(command.icon))) return false
  const icon = commandIcon(command)
  if (icon === 'linux') return system.family === 'linux'
  if (system.family !== 'linux') return icon === system.family
  const id = (system.id ?? '').toLowerCase()
  return icon === (Object.hasOwn(distroIcons, id) ? distroIcons[id] : id)
}
export const commandAvailable = (command: QuickCommand, filterBySystem: boolean, system?: ServerSystem) =>
  !filterBySystem || commandMatchesSystem(command, system)
export function validCommand(command: string): boolean {
  const text = normalizeCommandText(command)
  return !!text.trim() && new TextEncoder().encode(text).byteLength <= maxCommandBytes && !/[\x00-\x08\x0b-\x1f\x7f-\x9f\u2028\u2029]/.test(text)
}
export function validateQuickCommand(command: QuickCommand): string | null {
  if (command.icon && !isSystemIcon(command.icon)) return tr("快捷命令只能使用操作系统图标。")
  if (!command.name.trim() || [...command.name.trim()].length > 40 || /[\p{Cc}\u2028\u2029]/u.test(command.name.trim())) return tr("请输入 1–40 个字符的命令名称。")
  if (!validCommand(command.command)) return tr("请输入不超过 64 KiB 的命令，允许换行和缩进，不允许其他控制字符。")
  if (!commandGroup(command).trim() || [...commandGroup(command)].length > 32 || /[\p{Cc}\u2028\u2029]/u.test(commandGroup(command))) return tr("请选择有效的命令分组。")
  return null
}
export function commandInput(command: string, execute: boolean): string | null {
  const text = normalizeCommandText(command)
  if (!validCommand(text) || (!execute && text.includes('\n'))) return null
  return text.replace(/\n/g, '\r') + (execute ? '\r' : '')
}

export const quickCommandExecutes = (command: string) => command.endsWith('\n')

export function quickCommandInput(command: string, bracketedPaste: boolean): { input: string } | { error: string } {
  const text = normalizeCommandText(command)
  if (!validCommand(text)) return { error: tr("请输入不超过 64 KiB 的命令，允许换行和缩进，不允许其他控制字符。") }
  const execute = quickCommandExecutes(text)
  const body = execute ? text.replace(/\n+$/, '') : text
  if (!bracketedPaste && (body.includes('\t') || (!execute && body.includes('\n')))) {
    return { error: tr("当前 Shell 未启用括号粘贴，无法原样填入这条命令。") }
  }
  const input = body.replace(/\n/g, '\r')
  const paste = bracketedPaste ? `\x1b[200~${input}\x1b[201~` : input
  return { input: paste + (execute ? '\r' : '') }
}

