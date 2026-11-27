import type { HostProfile } from '../workspace/mockGateway'
import { compareListValues, readListSort, type ListSort } from '../../shared/ui/listSort'
import { tr } from '../../shared/i18n/messages'

export const hostColumnWidths = {
  group: 3, address: 4.5, port: 2, username: 3.5, system: 4, status: 3.5,
  lastUsed: 5, authentication: 4, notes: 5,
} as const
export type HostColumn = keyof typeof hostColumnWidths
export type HostSortKey = 'name' | HostColumn
export const hostColumns = Object.keys(hostColumnWidths) as HostColumn[]

type ColumnChoice = { key: HostColumn; visible: boolean }
export type HostView = { columns: ColumnChoice[]; sort: ListSort<HostSortKey> }

export function normalizeHostView(value: unknown): HostView {
  const saved = value && typeof value === 'object' ? value : {}
  const order = 'columns' in saved && Array.isArray(saved.columns) ? saved.columns : []
  const columns = new Map<HostColumn, boolean>()
  for (const item of order) {
    if (!item || typeof item !== 'object' || !hostColumns.includes(item.key) || typeof item.visible !== 'boolean') continue
    if (!columns.has(item.key)) columns.set(item.key, item.visible)
  }
  for (const key of hostColumns) if (!columns.has(key)) columns.set(key, key === 'group')
  const keys: HostSortKey[] = ['name', ...[...columns].filter(([, visible]) => visible).map(([key]) => key)]
  return { columns: [...columns].map(([key, visible]) => ({ key, visible })), sort: readListSort('sort' in saved ? saved.sort : null, keys) }
}

export function hostWithColumnVisibility(view: HostView, key: HostColumn, visible: boolean): HostView {
  return {
    columns: view.columns.map(column => column.key === key ? { key, visible } : column),
    sort: !visible && view.sort?.key === key ? null : view.sort,
  }
}

export function hostWithColumnMoved(view: HostView, key: HostColumn, to: number): HostView {
  const from = view.columns.findIndex(column => column.key === key)
  if (from < 0 || to < 0 || to >= view.columns.length || from === to) return view
  const columns = [...view.columns]
  const [moved] = columns.splice(from, 1)
  columns.splice(to, 0, moved!)
  return { ...view, columns }
}

export function sortHostRows(hosts: HostProfile[], sort: ListSort<HostSortKey>, collator: Intl.Collator,
  value: (host: HostProfile, key: HostSortKey) => string | number | undefined) {
  if (!sort) return hosts
  const { key, direction } = sort
  const rows = hosts.map(host => ({ host, value: value(host, key) }))
  rows.sort((a, b) => Number(!!b.host.pinned) - Number(!!a.host.pinned) || compareListValues(a.value, b.value, collator, direction))
  return rows.map(row => row.host)
}

export function hostSubtitle(host: HostProfile, system: string, visible: readonly HostColumn[]) {
  const user = visible.includes('username') ? '' : host.username
  const address = visible.includes('address') ? '' : host.address
  const port = visible.includes('port') ? 0 : host.port
  const endpoint = address && port ? `${address.includes(':') && !address.startsWith('[') ? `[${address}]` : address}:${port}` : address
  return [visible.includes('system') ? '' : system, [user, endpoint].filter(Boolean).join('@'), !address && port ? tr("端口 {p0}", String(port)) : '']
    .filter(Boolean).join(' · ')
}
