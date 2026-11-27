export type FileColumnId = 'name' | 'modified' | 'size' | 'type' | 'permissions' | 'owner'
export type FileSortKey = 'name' | 'modified' | 'size' | 'type'
export type FileSortDirection = 'ascending' | 'descending'
export interface FileSort {
  key: FileSortKey
  direction: FileSortDirection
}
export type FileColumnWidths = Partial<Record<FileColumnId, number>>
export interface FileSideViewPreferences {
  wrapNames: boolean
  showHidden: boolean
  showPermissions: boolean
  showOwner: boolean
  sort: FileSort
  columnWidths: FileColumnWidths
}

export const fileColumnGap = 12
export const fileRowHorizontalInset = 28

export const fileColumnSpecs: Readonly<Record<FileColumnId, { min: number; max: number; defaultWidth: number }>> = {
  name: { min: 140, max: 1200, defaultWidth: 240 },
  modified: { min: 92, max: 260, defaultWidth: 132 },
  size: { min: 56, max: 180, defaultWidth: 78 },
  type: { min: 56, max: 180, defaultWidth: 76 },
  permissions: { min: 68, max: 180, defaultWidth: 96 },
  owner: { min: 64, max: 180, defaultWidth: 84 },
}

export const defaultFileSort: FileSort = { key: 'name', direction: 'ascending' }

export function defaultDirectionForFileSort(key: FileSortKey): FileSortDirection {
  return key === 'modified' || key === 'size' ? 'descending' : 'ascending'
}

export function nextFileSort(current: FileSort, key: FileSortKey): FileSort {
  if (current.key !== key) return { key, direction: defaultDirectionForFileSort(key) }
  return { key, direction: current.direction === 'ascending' ? 'descending' : 'ascending' }
}

const sortKeys: readonly FileSortKey[] = ['name', 'modified', 'size', 'type']

export function normalizeFileSort(value: unknown): FileSort {
  if (typeof value === 'string') {
    const key = sortKeys.find(item => item === value)
    return key ? { key, direction: defaultDirectionForFileSort(key) } : defaultFileSort
  }
  if (value && typeof value === 'object' && 'key' in value) {
    const rawKey = (value as { key?: unknown }).key
    const rawDirection = (value as { direction?: unknown }).direction
    const key = sortKeys.find(item => item === rawKey)
    if (key) {
      return {
        key,
        direction: rawDirection === 'ascending' || rawDirection === 'descending'
          ? rawDirection
          : defaultDirectionForFileSort(key),
      }
    }
  }
  return defaultFileSort
}

export function clampFileColumnWidth(id: FileColumnId, width: number): number {
  const spec = fileColumnSpecs[id]
  if (!Number.isFinite(width)) return spec.defaultWidth
  return Math.round(Math.max(spec.min, Math.min(spec.max, width)))
}

export function normalizeColumnWidths(value: unknown): FileColumnWidths {
  if (!value || typeof value !== 'object') return {}
  const result: FileColumnWidths = {}
  for (const id of Object.keys(fileColumnSpecs) as FileColumnId[]) {
    const raw = (value as Record<string, unknown>)[id]
    if (typeof raw === 'number' && Number.isFinite(raw)) {
      result[id] = clampFileColumnWidth(id, raw)
    }
  }
  return result
}

export function visibleFileColumnIds(
  containerWidth: number,
  options: { showPermissions?: boolean; showOwner?: boolean; customWidths?: FileColumnWidths },
): FileColumnId[] {
  const visible: FileColumnId[] = ['name']
  const custom = options.customWidths ?? {}
  if (containerWidth >= 360 || custom.modified !== undefined) visible.push('modified')
  if (containerWidth >= 280 || custom.size !== undefined) visible.push('size')
  if (containerWidth >= 460 || custom.type !== undefined) visible.push('type')
  if (options.showPermissions) visible.push('permissions')
  if (options.showOwner) visible.push('owner')
  return visible
}

export function resolveFileColumnWidths(
  columnIds: readonly FileColumnId[],
  containerWidth: number,
  customWidths: FileColumnWidths,
): { widths: Record<FileColumnId, number>; totalTrackWidth: number; horizontalOverflow: boolean } {
  const widths = {} as Record<FileColumnId, number>
  for (const id of Object.keys(fileColumnSpecs) as FileColumnId[]) {
    widths[id] = customWidths[id] !== undefined
      ? clampFileColumnWidth(id, customWidths[id]!)
      : fileColumnSpecs[id].defaultWidth
  }

  const gaps = Math.max(0, columnIds.length - 1) * fileColumnGap
  let otherWidth = 0
  for (const id of columnIds) {
    if (id !== 'name') otherWidth += widths[id]
  }

  const availableContent = Math.max(0, containerWidth - fileRowHorizontalInset)
  if (customWidths.name === undefined) {
    widths.name = Math.max(fileColumnSpecs.name.min, Math.floor(availableContent - otherWidth - gaps))
  }

  const contentTotal = widths.name + otherWidth + gaps
  const naturalTrackWidth = contentTotal + fileRowHorizontalInset
  const totalTrackWidth = Math.max(containerWidth, naturalTrackWidth)
  const horizontalOverflow = naturalTrackWidth > containerWidth + 0.5
  return { widths, totalTrackWidth, horizontalOverflow }
}

const sftpViewStorageKey = 'yaare.sftp-view.v1'
const sftpSplitStorageKey = 'yaare.sftp-split.v1'

export function defaultSideViewPreferences(): FileSideViewPreferences {
  return {
    wrapNames: false,
    showHidden: false,
    showPermissions: false,
    showOwner: false,
    sort: defaultFileSort,
    columnWidths: {},
  }
}

export function normalizeSideViewPreferences(value: unknown): FileSideViewPreferences {
  const base = defaultSideViewPreferences()
  if (!value || typeof value !== 'object') return base
  const raw = value as Record<string, unknown>
  return {
    wrapNames: raw.wrapNames === true,
    showHidden: raw.showHidden === true,
    showPermissions: raw.showPermissions === true,
    showOwner: raw.showOwner === true,
    sort: normalizeFileSort(raw.sort),
    columnWidths: normalizeColumnWidths(raw.columnWidths),
  }
}

export function readSideViewPreferences(side: 'local' | 'remote'): FileSideViewPreferences {
  try {
    const parsed = JSON.parse(localStorage.getItem(sftpViewStorageKey) ?? 'null')
    if (parsed && typeof parsed === 'object') {
      return normalizeSideViewPreferences((parsed as Record<string, unknown>)[side])
    }
  } catch {
  }
  return defaultSideViewPreferences()
}

export function writeSideViewPreferences(side: 'local' | 'remote', prefs: FileSideViewPreferences) {
  try {
    const parsed = JSON.parse(localStorage.getItem(sftpViewStorageKey) ?? '{}')
    const root = parsed && typeof parsed === 'object' ? { ...(parsed as Record<string, unknown>) } : {}
    root[side] = prefs
    localStorage.setItem(sftpViewStorageKey, JSON.stringify(root))
  } catch {
  }
}

export function clampSftpSplitRatio(ratio: number, containerWidth?: number): number {
  if (!Number.isFinite(ratio)) return 0.5
  let min = 0.25
  let max = 0.75
  if (containerWidth && containerWidth >= 560) {
    const floor = Math.min(0.45, 260 / containerWidth)
    min = Math.max(min, floor)
    max = Math.min(max, 1 - floor)
  }
  return Math.round(Math.max(min, Math.min(max, ratio)) * 1000) / 1000
}

export function readSftpSplitRatio(): number {
  try {
    const raw = Number(localStorage.getItem(sftpSplitStorageKey))
    if (Number.isFinite(raw) && raw > 0 && raw < 1) return clampSftpSplitRatio(raw)
  } catch {
  }
  return 0.5
}

export function writeSftpSplitRatio(ratio: number) {
  try {
    localStorage.setItem(sftpSplitStorageKey, String(clampSftpSplitRatio(ratio)))
  } catch {
  }
}
