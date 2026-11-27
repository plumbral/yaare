import { tr } from '../../shared/i18n/messages'
import { matchesShortcut } from './shortcutReference'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {
  memo,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
} from 'react'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Item } from '@astryxdesign/core/Item'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { ContextMenu, type ContextMenuOption } from '@astryxdesign/core/ContextMenu'
import { Text } from '@astryxdesign/core/Text'
import {
  ArchiveBoxIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CodeBracketIcon,
  DocumentIcon,
  DocumentTextIcon,
  FilmIcon,
  FolderIcon,
  FolderOpenIcon,
  LinkIcon,
  MusicalNoteIcon,
  PhotoIcon,
} from '@heroicons/react/24/outline'
import { useVirtualizer, defaultRangeExtractor } from '@tanstack/react-virtual'
import type { FileEntry } from '../../shared/bridge/files'
import {
  fileEntryExtension,
  fileEntryIconCategory,
  flattenTree,
  selectTree,
  summarizeRootDirectory,
  treeMemory,
  type FileNode,
} from './fileTreeModel'
import {
  clampFileColumnWidth,
  fileColumnGap,
  fileColumnSpecs,
  nextFileSort,
  resolveFileColumnWidths,
  visibleFileColumnIds,
  type FileColumnId,
  type FileColumnWidths,
  type FileSort,
  type FileSortKey,
} from './fileColumns'
import { transferSize } from './transferModel'

type FileColumn = {
  id: FileColumnId
  label: string
  width: number
  align?: 'end'
  sortable?: FileSortKey
  mono?: boolean
  value?: (entry: FileEntry) => string
}

function fileType(entry: FileEntry) {
  if (entry.link) return tr("符号链接")
  if (entry.directory) return tr("文件夹")
  const ext = fileEntryExtension(entry)
  return ext ? ext.toUpperCase() : tr("文件")
}

function entryIcon(entry: FileEntry, expanded: boolean) {
  switch (fileEntryIconCategory(entry)) {
    case 'link': return LinkIcon
    case 'folder': return expanded ? FolderOpenIcon : FolderIcon
    case 'archive': return ArchiveBoxIcon
    case 'image': return PhotoIcon
    case 'video': return FilmIcon
    case 'audio': return MusicalNoteIcon
    case 'code': return CodeBracketIcon
    case 'text': return DocumentTextIcon
    default: return DocumentIcon
  }
}

function ColumnResizeHandle({
  columnId,
  label,
  width,
  onPreview,
  onCommit,
  onReset,
}: {
  columnId: FileColumnId
  label: string
  width: number
  onPreview: (id: FileColumnId, nextWidth: number) => void
  onCommit: (id: FileColumnId, nextWidth: number) => void
  onReset: (id: FileColumnId) => void
}) {
  const spec = fileColumnSpecs[columnId]
  const [active, setActive] = useState(false)
  const [hovered, setHovered] = useState(false)
  const drag = useRef<{ pointerId: number; startX: number; startWidth: number; latestWidth: number } | null>(null)
  const frame = useRef(0)

  useEffect(() => () => cancelAnimationFrame(frame.current), [])

  const finishDrag = (event: PointerEvent<HTMLElement>, commit: boolean) => {
    const state = drag.current
    if (!state || state.pointerId !== event.pointerId) return
    drag.current = null
    setActive(false)
    cancelAnimationFrame(frame.current)
    frame.current = 0
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    if (commit) onCommit(columnId, state.latestWidth)
  }

  const onPointerDown = (event: PointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    event.stopPropagation()
    event.currentTarget.setPointerCapture(event.pointerId)
    drag.current = { pointerId: event.pointerId, startX: event.clientX, startWidth: width, latestWidth: width }
    setActive(true)
  }

  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    const state = drag.current
    if (!state || state.pointerId !== event.pointerId) return
    const delta = event.clientX - state.startX
    state.latestWidth = clampFileColumnWidth(columnId, state.startWidth + delta)
    if (!frame.current) {
      frame.current = requestAnimationFrame(() => {
        frame.current = 0
        if (drag.current) onPreview(columnId, drag.current.latestWidth)
      })
    }
  }

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      event.preventDefault()
      event.stopPropagation()
      const step = event.shiftKey ? 4 : 16
      const next = clampFileColumnWidth(columnId, width + (event.key === 'ArrowRight' ? step : -step))
      onCommit(columnId, next)
    } else if (event.key === 'Home') {
      event.preventDefault()
      event.stopPropagation()
      onReset(columnId)
    }
  }

  return (
    <Stack
      role="separator"
      aria-orientation="vertical"
      aria-label={tr("调整{p0}列宽", label)}
      aria-valuemin={spec.min}
      aria-valuemax={spec.max}
      aria-valuenow={Math.round(width)}
      tabIndex={0}
      align="center"
      justify="center"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={event => finishDrag(event, true)}
      onPointerCancel={event => finishDrag(event, false)}
      onPointerEnter={() => setHovered(true)}
      onPointerLeave={() => setHovered(false)}
      onClick={event => event.stopPropagation()}
      onDoubleClick={event => {
        event.preventDefault()
        event.stopPropagation()
        onReset(columnId)
      }}
      onKeyDown={onKeyDown}
      style={{
        position: 'absolute',
        insetBlock: 'var(--spacing-1-5)',
        insetInlineEnd: 'calc(-1 * var(--spacing-3))',
        width: 'var(--spacing-3)',
        cursor: 'col-resize',
        touchAction: 'none',
        zIndex: 1,
        userSelect: 'none',
      }}
    >
      <Stack
        aria-hidden="true"
        style={{
          width: active ? 'var(--spacing-0-5)' : 'var(--border-width)',
          height: '100%',
          borderRadius: 'var(--radius-full)',
          background: active || hovered ? 'var(--color-border-emphasized)' : 'var(--color-border)',
          opacity: active || hovered ? 1 : 0.65,
        }}
      />
    </Stack>
  )
}

export default memo(function SftpFileTree({
  scope,
  root,
  entries,
  hidden,
  query,
  sort,
  onSort,
  wrapNames = false,
  showPermissions = false,
  showOwner = false,
  columnWidths = {},
  onColumnWidthsChange,
  onShowHidden,
  onClearQuery,
  readDirectory,
  onSelection,
  onOpen,
  onError,
  onRename,
  onDelete,
  menu,
  onDrag,
  onDrop,
}: {
  scope: string
  root: string
  entries: FileEntry[]
  hidden: boolean
  query: string
  sort: FileSort
  onSort: (next: FileSort) => void
  wrapNames?: boolean
  showPermissions?: boolean
  showOwner?: boolean
  columnWidths?: FileColumnWidths
  onColumnWidthsChange?: (next: FileColumnWidths) => void
  onShowHidden?: () => void
  onClearQuery?: () => void
  readDirectory: (path: string) => Promise<FileEntry[]>
  onSelection: (nodes: FileNode[]) => void
  onOpen: (node: FileNode) => void
  onError: (message: string) => void
  onRename: () => void
  onDelete: () => void
  menu: ContextMenuOption[]
  onDrag: (event: DragEvent, nodes: FileNode[]) => void
  onDrop: (event: DragEvent, path: string) => void
}) {
  const language = useLocale()

  const memory = useMemo(() => treeMemory(scope + ':' + root), [scope, root, getLanguage()])
  const [revision, redraw] = useState(0)
  const [structure, redrawTree] = useState(0)
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [hasFocus, setHasFocus] = useState(false)
  const [liveWidths, setLiveWidths] = useState<FileColumnWidths | null>(null)
  const scroll = useRef<HTMLElement>(null)
  const alive = useRef(true)
  const request = useRef(0)
  const focusRequested = useRef(false)
  const pending = useRef(new Set<string>())
  const id = useId()
  const [containerWidth, setContainerWidth] = useState(0)

  useLayoutEffect(() => {
    const node = scroll.current
    if (!node) return
    const measure = () => {
      const next = Math.round(node.clientWidth)
      setContainerWidth(prev => (prev === next ? prev : next))
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  const effectiveCustomWidths = liveWidths ?? columnWidths
  const columnIds = useMemo(
    () => visibleFileColumnIds(containerWidth || 520, { showPermissions, showOwner, customWidths: effectiveCustomWidths }),
    [containerWidth, showPermissions, showOwner, effectiveCustomWidths],
  )
  const columnLayout = useMemo(
    () => resolveFileColumnWidths(columnIds, containerWidth || 520, effectiveCustomWidths),
    [columnIds, containerWidth, effectiveCustomWidths],
  )
  const columns = useMemo<FileColumn[]>(() => {
    const date = new Intl.DateTimeFormat(language, { dateStyle: 'short', timeStyle: 'short' })
    const map: Record<FileColumnId, FileColumn> = {
      name: { id: 'name', label: tr("名称"), width: columnLayout.widths.name, sortable: 'name' },
      modified: {
        id: 'modified',
        label: tr("修改时间"),
        width: columnLayout.widths.modified,
        sortable: 'modified',
        value: entry => (entry.modified > 0 ? date.format(entry.modified) : '—'),
      },
      size: {
        id: 'size',
        label: tr("大小"),
        width: columnLayout.widths.size,
        align: 'end',
        sortable: 'size',
        value: entry => (entry.directory ? '' : transferSize(entry.size)),
      },
      type: {
        id: 'type',
        label: tr("类型"),
        width: columnLayout.widths.type,
        sortable: 'type',
        value: fileType,
      },
      permissions: {
        id: 'permissions',
        label: tr("权限"),
        width: columnLayout.widths.permissions,
        mono: true,
        value: entry => entry.permissions || entry.mode || '—',
      },
      owner: {
        id: 'owner',
        label: tr("所有者 UID"),
        width: columnLayout.widths.owner,
        align: 'end',
        value: entry => (entry.ownerUid == null ? '—' : String(entry.ownerUid)),
      },
    }
    return columnIds.map(colId => map[colId])
  }, [columnIds, columnLayout.widths, language])

  const metaColumns = useMemo(() => columns.slice(1), [columns])

  const previewColumnWidth = useCallback(
    (colId: FileColumnId, nextWidth: number) => {
      setLiveWidths(previous => ({ ...(previous ?? columnWidths), [colId]: nextWidth }))
    },
    [columnWidths],
  )
  const commitColumnWidth = useCallback(
    (colId: FileColumnId, nextWidth: number) => {
      const next = { ...columnWidths, [colId]: nextWidth }
      setLiveWidths(null)
      onColumnWidthsChange?.(next)
    },
    [columnWidths, onColumnWidthsChange],
  )
  const resetColumnWidth = useCallback(
    (colId: FileColumnId) => {
      const next = { ...columnWidths }
      delete next[colId]
      setLiveWidths(null)
      onColumnWidthsChange?.(next)
    },
    [columnWidths, onColumnWidthsChange],
  )

  const latest = useRef({ readDirectory, onError })
  latest.current = { readDirectory, onError }
  const menuRoot = useCallback((node: HTMLElement | null) => {
    if (node) Object.assign(node.style, { display: 'flex', flexDirection: 'column', flex: '1 1 auto', minWidth: '0', minHeight: '0' })
  }, [])

  memory.directories.set(root, entries)
  const rootSummary = useMemo(() => summarizeRootDirectory(entries, hidden), [entries, hidden])
  const rows = useMemo(
    () => flattenTree(root, memory.directories, memory.expanded, hidden, query, sort, language),
    [root, entries, hidden, query, sort, memory, structure, language],
  )
  const indices = useMemo(() => new Map(rows.map((n, i) => [n.path, i])), [rows])
  const focusedIndex = Math.max(0, indices.get(memory.focused) ?? 0)
  const ids = useMemo(() => rows.map(n => n.path), [rows])

  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroll.current,
    estimateSize: () => 32,
    getItemKey: index => rows[index]!.path,
    overscan: 8,
    scrollPaddingEnd: 40,
    useFlushSync: false,
    initialOffset: memory.scroll,
    rangeExtractor: useCallback(
      (range: Parameters<typeof defaultRangeExtractor>[0]) =>
        [...new Set([...defaultRangeExtractor(range), ...(rows.length ? [focusedIndex] : [])])].sort((a, b) => a - b),
      [focusedIndex, rows.length],
    ),
  })
  const virtualRef = useRef(virtual)
  virtualRef.current = virtual
  useLayoutEffect(() => {
    virtualRef.current.measure()
  }, [wrapNames, columnLayout.widths.name, entries])

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      request.current++
      pending.current.clear()
    }
  }, [])
  useEffect(() => {
    const generation = ++request.current
    pending.current.clear()
    setLoading(new Set())
    for (const path of memory.directories.keys()) if (path !== root && !memory.expanded.has(path)) memory.directories.delete(path)
    const paths = [...memory.expanded].sort((a, b) => a.split('/').length - b.split('/').length)
    void (async () => {
      for (const path of paths) {
        if (!alive.current || generation !== request.current) return
        if (!memory.expanded.has(path)) continue
        const parent = path.slice(0, path.lastIndexOf('/')) || '/'
        const name = path.slice(path.lastIndexOf('/') + 1)
        if (!memory.directories.get(parent)?.some(entry => entry.name === name && entry.directory && !entry.link)) {
          memory.expanded.delete(path)
          memory.directories.delete(path)
          continue
        }
        pending.current.add(path)
        setLoading(new Set(pending.current))
        try {
          const nextEntries = await latest.current.readDirectory(path)
          if (!alive.current || generation !== request.current) return
          let total = nextEntries.length
          for (const [key, value] of memory.directories) if (key !== path) total += value.length
          if (total > 100000) throw Error(tr("已加载文件超过 100,000 项，请切换根目录。"))
          memory.directories.set(path, nextEntries)
        } catch (cause) {
          if (generation !== request.current) return
          memory.directories.delete(path)
          memory.expanded.delete(path)
          latest.current.onError(cause instanceof Error ? cause.message : tr("目录刷新失败。"))
        } finally {
          if (alive.current && generation === request.current) {
            pending.current.delete(path)
            setLoading(new Set(pending.current))
            redrawTree(v => v + 1)
          }
        }
      }
    })()
    return () => {
      request.current++
    }
  }, [entries, memory, root])
  useEffect(() => {
    memory.selected = new Set([...memory.selected].filter(path => indices.has(path)))
    onSelection([...memory.selected].map(path => rows[indices.get(path)!]!))
  }, [rows, indices, memory, onSelection, revision])
  useEffect(() => {
    const current = scroll.current
    if (current) current.scrollTop = memory.scroll
  }, [memory])

  const rowId = (path: string) => id + '-' + encodeURIComponent(path)
  useLayoutEffect(() => {
    if (!focusRequested.current) return
    focusRequested.current = false
    document.getElementById(rowId(rows[focusedIndex]?.path ?? ''))?.focus({ preventScroll: true })
  }, [focusedIndex, revision, rows, virtual.getVirtualItems()])

  async function expand(node: FileNode, force?: boolean) {
    if (!node.entry.directory || node.entry.link) return
    const open = force ?? !memory.expanded.has(node.path)
    if (!open) {
      memory.expanded.delete(node.path)
      redrawTree(v => v + 1)
      return
    }
    memory.expanded.add(node.path)
    redrawTree(v => v + 1)
    if (memory.directories.has(node.path) || pending.current.has(node.path)) return
    if (memory.directories.size >= 128) {
      memory.expanded.delete(node.path)
      onError(tr("已展开的目录较多，请切换根目录后继续浏览。"))
      redrawTree(v => v + 1)
      return
    }
    const generation = request.current
    pending.current.add(node.path)
    setLoading(new Set(pending.current))
    try {
      const nextEntries = await latest.current.readDirectory(node.path)
      if (!alive.current || generation !== request.current) return
      let total = 0
      for (const items of memory.directories.values()) total += items.length
      if (total + nextEntries.length > 100000) throw Error(tr("已加载文件超过 100,000 项，请切换到更具体的目录。"))
      memory.directories.set(node.path, nextEntries)
      redrawTree(v => v + 1)
    } catch (cause) {
      if (alive.current && generation === request.current) {
        memory.expanded.delete(node.path)
        latest.current.onError(cause instanceof Error ? cause.message : tr("目录展开失败，请重试。"))
        redrawTree(v => v + 1)
      }
    } finally {
      if (alive.current && generation === request.current) {
        pending.current.delete(node.path)
        setLoading(new Set(pending.current))
      }
    }
  }

  function choose(node: FileNode, control = false, shift = false) {
    const next = selectTree(ids, memory.selected, node.path, memory.anchor, control, shift)
    if (next.limited) {
      onError(tr("一次最多选择 256 项，请缩小选择范围。"))
      return
    }
    memory.selected = next.selected
    memory.anchor = next.anchor
    memory.focused = node.path
    redraw(v => v + 1)
  }

  function move(index: number, control: boolean, shift: boolean) {
    const node = rows[Math.max(0, Math.min(rows.length - 1, index))]
    if (!node) return
    memory.focused = node.path
    if (!control || shift) choose(node, control, shift)
    focusRequested.current = true
    virtual.scrollToIndex(rows.indexOf(node), { align: 'auto' })
    redraw(v => v + 1)
  }

  const selectedNodes = () =>
    [...memory.selected].flatMap(path => {
      const index = indices.get(path)
      return index === undefined ? [] : [rows[index]!]
    })

  const contextRow = (target: EventTarget | null) => {
    const row = target instanceof Element ? target.closest<HTMLElement>('[data-file-tree-row]') : null
    const index = indices.get(row?.dataset.fileTreeRow ?? '')
    return row && index !== undefined ? { row, node: rows[index]! } : undefined
  }
  const selectContextRow = (target: EventTarget | null) => {
    const match = contextRow(target)
    if (!match) return false
    const { row, node } = match
    if (!memory.selected.has(node.path)) choose(node)
    memory.focused = node.path
    row.focus({ preventScroll: true })
    redraw(v => v + 1)
    return true
  }
  const touchTarget = useRef<EventTarget | null>(null)
  const clearContextSelection = () => {
    memory.selected = new Set()
    memory.anchor = ''
    onSelection([])
    redraw(v => v + 1)
  }

  const latestActions = useRef<RowActions>(null!)
  latestActions.current = {
    focus(node) {
      if (memory.focused !== node.path) {
        memory.focused = node.path
        redraw(v => v + 1)
      }
    },
    choose(node, event) {
      if (event.detail > 1) return
      const modified = event.ctrlKey || event.metaKey || event.shiftKey
      choose(node, event.ctrlKey || event.metaKey, event.shiftKey)
      event.currentTarget.focus({ preventScroll: true })
      if (!modified && event.detail === 1 && node.entry.directory) void expand(node)
    },
    expand: node => { void expand(node) },
    open: node => { if (!node.entry.link) onOpen(node) },
    drag: (node, event) => onDrag(event, memory.selected.has(node.path) ? selectedNodes() : [node]),
    drop: (node, event) => onDrop(event, node.entry.directory && !node.entry.link ? node.path : node.parent),
  }
  const rowActions = useMemo<RowActions>(() => ({
    focus: node => latestActions.current.focus(node),
    choose: (node, event) => latestActions.current.choose(node, event),
    expand: node => latestActions.current.expand(node),
    open: node => latestActions.current.open(node),
    drag: (node, event) => latestActions.current.drag(node, event),
    drop: (node, event) => latestActions.current.drop(node, event),
  }), [])

  return (
    <ContextMenu
      ref={menuRoot}
      label={tr("目录操作")}
      presentation="adaptive"
      items={menu}
      onContextMenu={event => {
        event.stopPropagation()
        touchTarget.current = null
        if (!contextRow(event.target)) clearContextSelection()
      }}
      onTouchStart={event => { touchTarget.current = event.target }}
      onTouchMove={() => { touchTarget.current = null }}
      onTouchEnd={() => { touchTarget.current = null }}
      onTouchCancel={() => { touchTarget.current = null }}
      onOpenChange={open => {
        if (open && touchTarget.current && !selectContextRow(touchTarget.current)) clearContextSelection()
        touchTarget.current = null
      }}
    >
      <StackItem
        ref={scroll}
        size="fill"
        onScroll={event => {
          memory.scroll = event.currentTarget.scrollTop
        }}
        style={{
          overflow: 'auto',
          minHeight: 0,
          minWidth: 0,
          scrollbarWidth: 'thin',
          scrollbarGutter: 'stable',
          overflowAnchor: 'none',
        }}
      >
        <Stack style={{ width: `${columnLayout.totalTrackWidth}px`, minWidth: '100%' }}>
          <Stack
            direction="horizontal"
            align="center"
            gap={3}
            role="row"
            onContextMenu={event => event.stopPropagation()}
            style={{
              position: 'sticky',
              top: 0,
              zIndex: 2,
              background: 'var(--color-background-body)',
              paddingInlineStart: 'var(--spacing-3)',
              paddingInlineEnd: 'var(--spacing-4)',
              minHeight: 'var(--spacing-9)',
              borderBlockEnd: 'var(--border-width) solid var(--color-border)',
              boxSizing: 'border-box',
            }}
          >
            {columns.map(column => {
              const activeSort = column.sortable && sort.key === column.sortable ? sort.direction : undefined
              return (
                <Stack
                  key={column.id}
                  role="columnheader"
                  aria-sort={activeSort}
                  direction="horizontal"
                  align="center"
                  justify={column.align === 'end' ? 'end' : 'start'}
                  style={{
                    width: `${column.width}px`,
                    flexShrink: 0,
                    minWidth: 0,
                    position: 'relative',
                    height: 'var(--spacing-9)',
                  }}
                >
                  {column.sortable ? (
                    <Button
                      label={column.label}
                      size="sm"
                      variant="ghost"
                      onClick={() => onSort(nextFileSort(sort, column.sortable!))}
                      endContent={
                        activeSort && (
                          <Icon icon={activeSort === 'ascending' ? ArrowUpIcon : ArrowDownIcon} size="xsm" />
                        )
                      }
                      style={{
                        height: 'var(--spacing-7)',
                        paddingInlineStart: 'var(--spacing-2)',
                        paddingInlineEnd: activeSort ? 'var(--spacing-1-5)' : 'var(--spacing-2)',
                        marginInlineStart: column.align !== 'end' ? 'calc(-1 * var(--spacing-1))' : undefined,
                        marginInlineEnd: column.align === 'end' ? 'calc(-1 * var(--spacing-1))' : undefined,
                        borderRadius: 'var(--radius-inner)',
                        maxWidth: 'calc(100% + var(--spacing-1))',
                        minWidth: 0,
                        color: activeSort ? 'var(--color-text-primary)' : 'var(--color-text-secondary)',
                      }}
                    />
                  ) : (
                    <Text
                      type="supporting"
                      color="secondary"
                      maxLines={1}
                      style={{ textAlign: column.align, width: '100%', paddingInline: 'var(--spacing-1)' }}
                    >
                      {column.label}
                    </Text>
                  )}
                  <ColumnResizeHandle
                    columnId={column.id}
                    label={column.label}
                    width={column.width}
                    onPreview={previewColumnWidth}
                    onCommit={commitColumnWidth}
                    onReset={resetColumnWidth}
                  />
                </Stack>
              )
            })}
          </Stack>

          <Stack
            role="tree"
            onContextMenuCapture={event => { selectContextRow(event.target) }}
            aria-label={tr("文件树")}
            aria-multiselectable="true"
            tabIndex={rows.length ? -1 : 0}
            onFocusCapture={() => setHasFocus(true)}
            onBlurCapture={event => {
              if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setHasFocus(false)
            }}
            onKeyDown={event => {
              if (
                event.defaultPrevented ||
                event.nativeEvent.isComposing ||
                event.target instanceof HTMLInputElement ||
                !event.currentTarget.contains(event.target as Node)
              )
                return
              const control = event.ctrlKey || event.metaKey
              const node = rows[focusedIndex]
              if (!node) return
              if (matchesShortcut(event.nativeEvent, 'file-select-all')) {
                event.preventDefault()
                if (event.repeat) return
                if (rows.length > 256) {
                  onError(tr("一次最多选择 256 项，请缩小选择范围。"))
                  return
                }
                memory.selected = new Set(ids)
                memory.anchor = node.path
                redraw(v => v + 1)
                return
              }
              if (matchesShortcut(event.nativeEvent, 'file-rename')) {
                event.preventDefault()
                if (!event.repeat && memory.selected.size === 1) onRename()
                return
              }
              if (event.altKey) return
              if (event.key === 'ContextMenu' || event.shiftKey && event.key === 'F10') {
                const row = contextRow(event.target)?.row
                if (!row) return
                event.preventDefault()
                const rect = row.getBoundingClientRect()
                row.dispatchEvent(new MouseEvent('contextmenu', {
                  bubbles: true, cancelable: true, clientX: rect.left, clientY: rect.bottom,
                }))
              } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
                event.preventDefault()
                move(
                  event.key === 'Home' ? 0 : event.key === 'End' ? rows.length - 1 : focusedIndex + (event.key === 'ArrowDown' ? 1 : -1),
                  control,
                  event.shiftKey,
                )
              } else if (event.key === 'ArrowRight') {
                event.preventDefault()
                if (node.entry.directory && !node.entry.link) {
                  if (!memory.expanded.has(node.path)) void expand(node, true)
                  else if (rows[focusedIndex + 1]?.parent === node.path) move(focusedIndex + 1, control, false)
                }
              } else if (event.key === 'ArrowLeft') {
                event.preventDefault()
                if (memory.expanded.has(node.path)) void expand(node, false)
                else {
                  const parent = rows.findIndex(r => r.path === node.parent)
                  if (parent >= 0) move(parent, control, false)
                }
              } else if (event.key === 'Enter') {
                event.preventDefault()
                if (!node.entry.link) onOpen(node)
              } else if (event.key === ' ') {
                event.preventDefault()
                choose(node, control, event.shiftKey)
              } else if (event.key === 'Delete') {
                event.preventDefault()
                if (memory.selected.size) onDelete()
              }
            }}
            style={{
              paddingInline: 'var(--spacing-2)',
              paddingBlock: 'var(--spacing-1)',
              minWidth: 0,
            }}
          >
            {rows.length ? (
              <Stack style={{ height: virtual.getTotalSize(), position: 'relative', minWidth: 0 }}>
                {virtual.getVirtualItems().map(item => {
                  const node = rows[item.index]!
                  return (
                    <Stack
                      key={node.path}
                      data-index={item.index}
                      ref={wrapNames ? virtual.measureElement : undefined}
                      style={{ position: 'absolute', top: 0, insetInline: 0, transform: `translateY(${item.start}px)` }}
                    >
                        <FileTreeRow
                          node={node}
                          nameWidth={columnLayout.widths.name}
                          metaColumns={metaColumns}
                          wrapNames={wrapNames}
                          id={rowId(node.path)}
                          expanded={memory.expanded.has(node.path)}
                          selected={memory.selected.has(node.path)}
                          focused={item.index === focusedIndex && hasFocus}
                          tabStop={item.index === focusedIndex}
                          loading={loading.has(node.path)}
                          actions={rowActions}
                        />

                    </Stack>
                  )
                })}
              </Stack>
            ) : (
              <Stack gap={2} align="start" padding={2}>
                <Text type="supporting" color="secondary">
                  {query
                    ? tr("没有匹配的文件。")
                    : !hidden && rootSummary.hidden > 0
                      ? tr("此目录有 {p0} 个隐藏项。", rootSummary.hidden)
                      : tr("此目录为空。")}
                </Text>
                {query && onClearQuery && <Button label={tr("清除筛选")} size="sm" variant="secondary" onClick={onClearQuery} />}
                {!query && !hidden && rootSummary.hidden > 0 && onShowHidden && (
                  <Button label={tr("显示隐藏文件")} size="sm" variant="secondary" onClick={onShowHidden} />
                )}
              </Stack>
            )}
          </Stack>
        </Stack>
      </StackItem>
    </ContextMenu>
  )
})

type RowActions = {
  focus: (node: FileNode) => void
  choose: (node: FileNode, event: MouseEvent<HTMLElement>) => void
  expand: (node: FileNode) => void
  open: (node: FileNode) => void
  drag: (node: FileNode, event: DragEvent) => void
  drop: (node: FileNode, event: DragEvent) => void
}

const FileTreeRow = memo(function FileTreeRow({
  node,
  nameWidth,
  metaColumns,
  wrapNames,
  id,
  expanded,
  selected,
  focused,
  tabStop,
  loading,
  actions,
}: {
  node: FileNode
  nameWidth: number
  metaColumns: FileColumn[]
  wrapNames: boolean
  id: string
  expanded: boolean
  selected: boolean
  focused: boolean
  tabStop: boolean
  loading: boolean
  actions: RowActions
}) {
  useLocale()

  const [hover, setHover] = useState(false)
  const RowIcon = entryIcon(node.entry, expanded)
  const indentPx = Math.min(node.depth, 5) * 16

  return (
    <Item
      id={id}
      role="treeitem"
      data-file-tree-row={node.path}
      data-native-upload-path={node.entry.directory && !node.entry.link ? node.path : node.parent}
      aria-label={node.entry.name}
      aria-level={node.depth + 1}
      aria-posinset={node.position}
      aria-setsize={node.siblings}
      aria-expanded={node.entry.directory && !node.entry.link ? expanded : undefined}
      aria-selected={selected}
      tabIndex={tabStop ? 0 : -1}
      density="compact"
      isSelected={selected}
      isHighlighted={hover}
      aria-description={
        metaColumns
          .map(column => {
            const value = column.value?.(node.entry)
            return value ? column.label + ' ' + value : ''
          })
          .filter(Boolean)
          .join(' · ') || undefined
      }
      label={
        <Stack
          direction="horizontal"
          align={wrapNames ? 'start' : 'center'}
          gap={3}
          width="100%"
          style={{ minWidth: 0 }}
        >
          <Stack
            direction="horizontal"
            align={wrapNames ? 'start' : 'center'}
            gap={1.5}
            style={{
              width: `${nameWidth}px`,
              flexShrink: 0,
              minWidth: 0,
              paddingInlineStart: `${indentPx}px`,
              boxSizing: 'border-box',
            }}
          >
            <Stack
              direction="horizontal"
              align="center"
              gap={1}
              style={{ flexShrink: 0, height: 'var(--spacing-6)' }}
              onDoubleClick={event => event.stopPropagation()}
            >
              <Stack width="var(--spacing-6)" height="var(--spacing-6)" align="center" justify="center">
                {node.entry.directory && !node.entry.link && (
                  <IconButton
                    label={(expanded ? tr("收起 ") : tr("展开 ")) + node.entry.name}
                    size="sm"
                    variant="ghost"
                    tabIndex={-1}
                    isLoading={loading}
                    icon={<Icon icon={expanded ? ChevronDownIcon : ChevronRightIcon} size="sm" />}
                    onPointerDown={event => event.preventDefault()}
                    onClick={event => {
                      event.stopPropagation()
                      if (event.detail < 2) actions.expand(node)
                    }}
                  />
                )}
              </Stack>
              <Icon
                icon={RowIcon}
                size="sm"
                color={node.entry.link ? 'secondary' : undefined}
              />
            </Stack>
            <StackItem
              size="fill"
              style={{
                minWidth: 0,
                paddingBlockStart: wrapNames ? 'var(--spacing-0-5)' : undefined,
              }}
            >
              <Text
                type="label"
                display="block"
                maxLines={wrapNames ? 0 : 1}
                hasTruncateTooltip={!wrapNames}
                style={
                  wrapNames
                    ? {
                        whiteSpace: 'normal',
                        overflowWrap: 'anywhere',
                        lineHeight: 'var(--line-height-snug, 1.35)',
                        fontStyle: node.entry.link ? 'italic' : undefined,
                      }
                    : {
                        fontStyle: node.entry.link ? 'italic' : undefined,
                      }
                }
              >
                {node.entry.name}
              </Text>
            </StackItem>
          </Stack>
          {metaColumns.map(column => {
            const value = column.value?.(node.entry) ?? ''
            return (
              <Stack
                key={column.id}
                style={{
                  width: `${column.width}px`,
                  flexShrink: 0,
                  minWidth: 0,
                  paddingBlockStart: wrapNames ? 'var(--spacing-0-5)' : undefined,
                }}
              >
                <Text
                  type="supporting"
                  color="secondary"
                  maxLines={1}
                  style={{
                    textAlign: column.align,
                    fontVariantNumeric: 'tabular-nums',
                    fontFamily: column.mono ? 'var(--font-family-code)' : undefined,
                  }}
                >
                  {value}
                </Text>
              </Stack>
            )
          })}
        </Stack>
      }
      onFocus={() => actions.focus(node)}
      onClickCapture={event => {
        if (!(event.target as Element).closest('button')) actions.choose(node, event)
      }}
      onDoubleClick={event => {
        if (!event.ctrlKey && !event.metaKey && !event.shiftKey && !(event.target as Element).closest('button')) actions.open(node)
      }}
      onPointerEnter={() => setHover(true)}
      onPointerLeave={() => setHover(false)}
      draggable={!node.entry.link}
      onDragStart={event => actions.drag(node, event)}
      onDrop={event => actions.drop(node, event)}
      onDragOver={event => {
        if (event.dataTransfer.types.includes('application/x-yaare-files')) event.preventDefault()
      }}
      style={{
        height: wrapNames ? 'auto' : 'var(--spacing-8)',
        minHeight: 'var(--spacing-8)',
        paddingBlock: wrapNames ? 'var(--spacing-1)' : 0,
        paddingInlineStart: 'var(--spacing-1)',
        paddingInlineEnd: 'var(--spacing-2)',
        borderRadius: 'var(--radius-element)',
        backgroundImage: focused ? 'linear-gradient(var(--color-overlay-pressed), var(--color-overlay-pressed))' : undefined,
        userSelect: 'none',
        cursor: 'default',
      }}
    />
  )
})
