import type { FileEntry, TransferRequest } from '../../shared/bridge/files'
import { normalizeFileSort, type FileSort } from './fileColumns'

export const joinPath = (base: string, name: string) => (base === '/' ? '' : base) + '/' + name
export const parentPath = (path: string) => path.slice(0, path.lastIndexOf('/')) || '/'
export interface FileNode {
  path: string
  parent: string
  entry: FileEntry
  depth: number
  position: number
  siblings: number
}

const isSafeName = (name: unknown): name is string =>
  typeof name === 'string' && name.length > 0 && name !== '.' && name !== '..' && !/[\\/\x00]/.test(name)
const isSafeEntry = (entry: FileEntry) => isSafeName(entry.name)
const maxSelectedFiles = 256

export const fileDragType = 'application/x-yaare-files'
export type FileDragGroup = Pick<TransferRequest, 'source' | 'names'>

export function parseFileDrag(text: string): FileDragGroup[] | null {
  try {
    const payload = JSON.parse(text)
    const groups = payload?.groups
    if (!Array.isArray(groups) || groups.length === 0 || groups.length > maxSelectedFiles) return null
    let count = 0
    for (const group of groups) {
      const source = group?.source
      if ((source?.side !== 'local' && source?.side !== 'remote') ||
          typeof source.path !== 'string' || typeof source.token !== 'string' ||
          typeof source.sessionId !== 'string' || !Number.isSafeInteger(source.attempt) || source.attempt < 0 ||
          !Array.isArray(group.names) || group.names.length === 0) return null
      count += group.names.length
      if (count > maxSelectedFiles || !group.names.every(isSafeName)) return null
    }
    return groups
  } catch {
    return null
  }
}

export function fileEntryExtension(entry: FileEntry): string {
  if (entry.directory || entry.link) return ''
  const dot = entry.name.lastIndexOf('.')
  return dot > 0 && entry.name.length - dot <= 10 ? entry.name.slice(dot + 1).toLowerCase() : ''
}

export type FileIconCategory = 'link' | 'folder' | 'archive' | 'image' | 'video' | 'audio' | 'code' | 'text' | 'file'

const archiveExts = new Set(['zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', '7z', 'rar', 'zst', 'lz4', 'cab', 'deb', 'rpm'])
const imageExts = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'ico', 'bmp', 'avif', 'tif', 'tiff'])
const videoExts = new Set(['mp4', 'mkv', 'mov', 'webm', 'avi', 'flv', 'wmv', 'm4v'])
const audioExts = new Set(['mp3', 'wav', 'flac', 'ogg', 'm4a', 'aac', 'opus'])
const codeExts = new Set([
  'ts', 'tsx', 'js', 'jsx', 'mjs', 'cjs', 'go', 'py', 'rs', 'sh', 'bash', 'zsh', 'ps1', 'bat', 'cmd',
  'json', 'yaml', 'yml', 'toml', 'xml', 'html', 'htm', 'css', 'scss', 'less', 'sql',
  'c', 'h', 'cc', 'cpp', 'hpp', 'cs', 'java', 'kt', 'rb', 'php', 'lua', 'pl', 'r', 'swift',
  'ini', 'conf', 'cfg', 'env', 'properties', 'service', 'tf',
])
const codeNames = new Set(['dockerfile', 'makefile', 'cmakelists.txt', 'caddyfile', 'vagrantfile', 'gemfile', 'rakefile', '.gitignore', '.bashrc', '.zshrc', '.profile', '.env'])
const textExts = new Set(['txt', 'md', 'markdown', 'log', 'csv', 'tsv', 'pdf', 'doc', 'docx', 'rtf'])

export function fileEntryIconCategory(entry: FileEntry): FileIconCategory {
  if (entry.link) return 'link'
  if (entry.directory) return 'folder'
  const lower = entry.name.toLowerCase()
  if (codeNames.has(lower)) return 'code'
  const ext = fileEntryExtension(entry)
  if (!ext) return 'file'
  if (archiveExts.has(ext)) return 'archive'
  if (imageExts.has(ext)) return 'image'
  if (videoExts.has(ext)) return 'video'
  if (audioExts.has(ext)) return 'audio'
  if (codeExts.has(ext)) return 'code'
  if (textExts.has(ext)) return 'text'
  return 'file'
}

function fileTypeSortKey(entry: FileEntry): string {
  if (entry.link) return '\u0001link'
  if (entry.directory) return '\u0000folder'
  const ext = fileEntryExtension(entry)
  return ext ? `1:${ext}` : '2:'
}

export function summarizeRootDirectory(entries: readonly FileEntry[], showHidden: boolean): { total: number; visible: number; hidden: number } {
  let total = 0
  let hidden = 0
  for (const entry of entries) {
    if (!isSafeEntry(entry)) continue
    total++
    if (entry.name.startsWith('.')) hidden++
  }
  return { total, visible: showHidden ? total : total - hidden, hidden }
}

export function summarizeSelection(nodes: readonly FileNode[]): { count: number; fileCount: number; totalBytes: number } {
  let fileCount = 0
  let totalBytes = 0
  for (const node of nodes) {
    if (!node.entry.directory && !node.entry.link) {
      fileCount++
      totalBytes += Math.max(0, node.entry.size || 0)
    }
  }
  return { count: nodes.length, fileCount, totalBytes }
}

const collators = new Map<string, Intl.Collator>()

export function flattenTree(
  root: string,
  directories: ReadonlyMap<string, FileEntry[]>,
  expanded: ReadonlySet<string>,
  hidden: boolean,
  query: string,
  sort: FileSort | string,
  locale?: string,
): FileNode[] {
  const rows: FileNode[] = []
  const seen = new Set<string>()
  const needle = query.trim().toLocaleLowerCase()
  const matchCache = new Map<string, boolean>()
  const activeSort = normalizeFileSort(sort)
  const key = locale ?? ''
  let collator = collators.get(key)
  if (!collator) {
    collator = new Intl.Collator(locale, { numeric: true, sensitivity: 'base' })
    collators.set(key, collator)
    if (collators.size > 8) collators.delete(collators.keys().next().value!)
  }
  const factor = activeSort.direction === 'ascending' ? 1 : -1

  const compareEntries = (a: FileEntry, b: FileEntry): number => {
    const dirOrder = Number(b.directory) - Number(a.directory)
    if (dirOrder !== 0) return dirOrder
    let delta = 0
    if (activeSort.key === 'size') {
      delta = (a.directory ? 0 : a.size) - (b.directory ? 0 : b.size)
    } else if (activeSort.key === 'modified') {
      delta = (a.modified || 0) - (b.modified || 0)
    } else if (activeSort.key === 'type') {
      delta = collator.compare(fileTypeSortKey(a), fileTypeSortKey(b))
    } else {
      delta = collator.compare(a.name, b.name)
    }
    if (delta !== 0) return delta * factor
    return collator.compare(a.name, b.name)
  }

  const matches = (base: string, visited = new Set<string>()): boolean => {
    if (visited.has(base) || visited.size > 64) return false
    if (matchCache.has(base)) return matchCache.get(base)!
    visited.add(base)
    const result = (directories.get(base) ?? []).some(
      e =>
        isSafeEntry(e) &&
        (hidden || !e.name.startsWith('.')) &&
        (e.name.toLocaleLowerCase().includes(needle) ||
          (e.directory && !e.link && matches(joinPath(base, e.name), new Set(visited)))),
    )
    matchCache.set(base, result)
    return result
  }

  function visit(base: string, depth: number) {
    if (depth > 64 || seen.has(base)) return
    seen.add(base)
    const entries = [...(directories.get(base) ?? [])]
      .filter(e => isSafeEntry(e) && (hidden || !e.name.startsWith('.')))
      .sort(compareEntries)
    const shown = needle
      ? entries.filter(
          e =>
            e.name.toLocaleLowerCase().includes(needle) ||
            (e.directory && !e.link && matches(joinPath(base, e.name))),
        )
      : entries
    shown.forEach((entry, index) => {
      const path = joinPath(base, entry.name)
      rows.push({ path, parent: base, entry, depth, position: index + 1, siblings: shown.length })
      if (entry.directory && !entry.link && expanded.has(path)) visit(path, depth + 1)
    })
  }
  visit(root, 0)
  return rows
}

export function selectTree(
  visible: readonly string[],
  previous: ReadonlySet<string>,
  target: string,
  anchor: string,
  control = false,
  shift = false,
) {
  let next: Set<string>
  if (shift && visible.includes(anchor)) {
    const a = visible.indexOf(anchor)
    const b = visible.indexOf(target)
    if (Math.abs(a - b) + 1 > maxSelectedFiles) return { selected: new Set(previous), anchor, limited: true }
    const range = visible.slice(Math.min(a, b), Math.max(a, b) + 1)
    next = new Set(control ? [...previous, ...range] : range)
  } else if (control) {
    next = new Set(previous)
    if (next.has(target)) next.delete(target)
    else next.add(target)
  } else {
    next = new Set([target])
  }
  if (next.size > maxSelectedFiles) return { selected: new Set(previous), anchor, limited: true }
  return { selected: next, anchor: shift && visible.includes(anchor) ? anchor : target, limited: false }
}

export function selectionGroups(nodes: readonly FileNode[]) {
  const unique = nodes.filter((node, i) => nodes.findIndex(n => n.path === node.path) === i)
  const roots = unique.filter(
    node => !unique.some(other => other !== node && other.entry.directory && !other.entry.link && node.path.startsWith(other.path + '/')),
  )
  const groups = new Map<string, string[]>()
  for (const node of roots) groups.set(node.parent, [...(groups.get(node.parent) ?? []), node.entry.name])
  return [...groups].map(([path, names]) => ({ path, names }))
}

export interface TreeMemory {
  directories: Map<string, FileEntry[]>
  expanded: Set<string>
  selected: Set<string>
  anchor: string
  focused: string
  scroll: number
}
const memories = new Map<string, TreeMemory>()
export function treeMemory(key: string) {
  let value = memories.get(key)
  if (!value) {
    value = { directories: new Map(), expanded: new Set(), selected: new Set(), anchor: '', focused: '', scroll: 0 }
    memories.set(key, value)
    while (memories.size > 32) memories.delete(memories.keys().next().value!)
  }
  return value
}
export function forgetFileTrees(sessionId: string) {
  for (const key of memories.keys()) if (key.startsWith(sessionId + ':')) memories.delete(key)
}
