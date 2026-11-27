import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer, defaultRangeExtractor } from '@tanstack/react-virtual'
import { Stack,StackItem } from '@astryxdesign/core/Stack'
import { List, ListItem, type ListItemProps } from '@astryxdesign/core/List'
import {rowMotion} from './listMotion'

function WindowedRow<T>({ item, renderRow, index, start, total, measure, rowKey, motionOrder, motionScope, animateReorder }: {
  item: T; renderRow: (item: T) => ListItemProps; index: number; start: number; total: number
  measure: (element: HTMLLIElement | null) => void
  rowKey:string;motionOrder:string;motionScope?:string;animateReorder:boolean
}) {
  const props = useMemo(() => renderRow(item), [item, renderRow])
  const element=useRef<HTMLLIElement|null>(null),previous=useRef({start,motionOrder,motionScope}),animation=useRef<Animation|undefined>(undefined)
  useLayoutEffect(()=>{
    const before=previous.current,el=element.current
    previous.current={start,motionOrder,motionScope}
    if(!el||!animateReorder||before.motionOrder===motionOrder||before.motionScope!==motionScope||el.dataset.reorderDragging==='dragging')return
    const current=Number.parseFloat(getComputedStyle(el).translate.split(' ')[1]??'0')||0
    const offset=before.start-start+current
    animation.current?.cancel();el.style.translate='';delete el.dataset.reorderDragging
    if(Math.abs(offset)<.5)return
    animation.current=rowMotion(el,offset,Math.abs(offset)>Math.max(1,el.closest('[data-windowed-list]')?.clientHeight??0))
    if(animation.current){el.dataset.reorderAnimating='true';const active=animation.current;void active.finished.catch(()=>{}).finally(()=>{if(animation.current===active){delete el.dataset.reorderAnimating;el.style.zIndex=''}})}
  },[start,motionOrder,motionScope,animateReorder])
  useEffect(()=>()=>animation.current?.cancel(),[])
  const attach=useCallback((node:HTMLLIElement|null)=>{element.current=node;measure(node)},[measure])
  return <ListItem {...props} ref={attach} data-index={index} data-row-key={rowKey} data-row-start={start} aria-posinset={index + 1} aria-setsize={total}
    style={{ ...props.style, position: 'absolute', insetInline: 0, top: 0, width: '100%', transform: `translateY(${start}px)` }} />
}
const MemoWindowedRow = memo(WindowedRow) as typeof WindowedRow

export default function WindowedList<T>({ items, itemKey, row, estimateSize, label, selectedKey, resetKey, isItemFocusable,header,measurementKey,headerInteractive=false,rowGap=0,animateReorder=false,retainedKey,revealRequest,savedRow,onSavedRowShown }: {
  items: readonly T[]; itemKey: (item: T) => string; row: (item: T) => ListItemProps
  estimateSize: (item: T) => number; label: string; selectedKey?: string; resetKey?: string
  isItemFocusable?: (item: T) => boolean
  header?:ListItemProps
  measurementKey?:string
  headerInteractive?:boolean
  rowGap?:number
  animateReorder?:boolean;retainedKey?:string
  revealRequest?:{key:string;serial:number;focusId:string;fallbackId?:string}
  savedRow?: { key: string }
  onSavedRowShown?: () => void
}) {
  const scroll = useRef<HTMLElement>(null)
  const heading=useRef<HTMLElement>(null)
  const [headerSize,setHeaderSize]=useState(0)
  const [viewportPadding,setViewportPadding]=useState(0)
  useLayoutEffect(()=>{
    if (scroll.current) setViewportPadding(parseFloat(getComputedStyle(scroll.current).paddingBlockStart) || 0)
    const element=heading.current
    if(!element){setHeaderSize(0);return}
    const measure=()=>setHeaderSize(element.getBoundingClientRect().height)
    measure()
    const observer=new ResizeObserver(measure)
    observer.observe(element)
    return()=>observer.disconnect()
  },[!!header])
  const [focusedKey, setFocusedKey] = useState<string>()
  const focused = focusedKey === undefined ? -1 : items.findIndex(item => itemKey(item) === focusedKey)
  const handledSave = useRef<typeof savedRow>(undefined)
  const pendingSave = savedRow !== handledSave.current ? savedRow : undefined
  const keepKey = pendingSave?.key ?? retainedKey
  const retained=keepKey===undefined?-1:items.findIndex(item=>itemKey(item)===keepKey)
  const motionOrder=useMemo(()=>animateReorder ? items.map(itemKey).join('\0') : '',[animateReorder,items,itemKey])
  const focusedElement = useRef<HTMLElement | null>(null)
  const previousItems = useRef(items)
  const reorderedWithFocus = previousItems.current !== items && !!scroll.current?.contains(document.activeElement)
  useLayoutEffect(() => {
    previousItems.current = items
    const element = focusedElement.current
    if (reorderedWithFocus && element?.isConnected && scroll.current?.contains(element) && document.activeElement === document.body) element.focus({ preventScroll: true })
  })
  const focusRequest = useRef(false)
  const keyFunction = useRef(itemKey)
  const sizeFunction = useRef(estimateSize)
  keyFunction.current = itemKey
  sizeFunction.current = estimateSize
  const getScrollElement = useCallback(() => scroll.current, [])
  const getItemKey = useCallback((index: number) => keyFunction.current(items[index]!), [items])
  const estimate = useCallback((index: number) => sizeFunction.current(items[index]!), [items])
  const listOffset = headerSize + viewportPadding
  const virtual = useVirtualizer<HTMLElement, HTMLLIElement>({
    count: items.length, getScrollElement,scrollMargin:listOffset,scrollPaddingStart:listOffset,scrollPaddingEnd:viewportPadding,gap:rowGap,
    getItemKey, estimateSize: estimate, overscan: 6,
    useFlushSync: false,
    rangeExtractor: useCallback((range: Parameters<typeof defaultRangeExtractor>[0]) => {
      const visible = defaultRangeExtractor(range)
      return [...new Set([...visible,...(focused>=0&&focused<items.length?[focused]:[]),...(retained>=0?[retained]:[])])].sort((a,b)=>a-b)
    }, [focused, retained, items.length]),
  })
  const revealed=useRef(0)
  useEffect(()=>{
    if(!revealRequest||revealed.current===revealRequest.serial)return
    const index=items.findIndex(item=>itemKey(item)===revealRequest.key);if(index<0)return
    revealed.current=revealRequest.serial;virtual.scrollToIndex(index,{align:'auto'})
    const frame=requestAnimationFrame(()=>{
      const active = document.activeElement
      if (active !== document.body && active?.id !== revealRequest.fallbackId) return
      const target = document.getElementById(revealRequest.focusId)
      const fallback = revealRequest.fallbackId && document.getElementById(revealRequest.fallbackId)
      const enabledTarget = target && !target.matches(':disabled,[aria-disabled="true"]') && !target.closest('[inert]') ? target : fallback
      if (enabledTarget) enabledTarget.focus({preventScroll:true})
    })
    return()=>cancelAnimationFrame(frame)
  },[items,revealRequest,virtual,itemKey])
  useLayoutEffect(()=>{if(measurementKey!==undefined)virtual.measure()},[measurementKey])
  useEffect(() => { virtual.scrollToOffset(0); setFocusedKey(undefined) }, [resetKey])
  useEffect(() => {
    const index = items.findIndex(item => itemKey(item) === selectedKey)
    if (index >= 0) virtual.scrollToIndex(index, { align: 'auto' })
  }, [selectedKey])
  useEffect(() => {
    if (!pendingSave || retained < 0 || !scroll.current?.getClientRects().length) return
    const targetRow = scroll.current.querySelector<HTMLElement>(`[data-index="${retained}"]`)
    if (!targetRow || targetRow.dataset.rowKey !== pendingSave.key) return
    handledSave.current = pendingSave
    onSavedRowShown?.()
    const viewport = scroll.current.getBoundingClientRect()
    const bounds = targetRow.getBoundingClientRect()
    const visible = bounds.top >= viewport.top + listOffset && bounds.bottom <= viewport.bottom - viewportPadding
    if (!visible) virtual.scrollToIndex(retained, { align: 'center' })
    if (!targetRow.contains(document.activeElement) && !document.querySelector('dialog[open]')) {
      targetRow.querySelector<HTMLElement>('input:not(:disabled), button:not(:disabled)')?.focus({ preventScroll: true })
    }
  })
  useLayoutEffect(() => {
    if (!focusRequest.current) return
    focusRequest.current = false
    scroll.current?.querySelector<HTMLElement>(`[data-index="${focused}"] input[type="checkbox"]:not(:disabled), [data-index="${focused}"] button:not([aria-disabled="true"])`)?.focus({ preventScroll: true })
  }, [focused, virtual.getVirtualItems()])
  return <StackItem size="fill" isScrollable ref={scroll} aria-label={label} data-windowed-list=""
    style={{ scrollbarWidth: 'thin', overflowAnchor: 'none', padding: 'var(--spacing-3)' }}
    onFocusCapture={event => {
      const viewport = event.currentTarget
      if (event.target === viewport && viewport.matches(':focus-visible')) {
        const bounds = viewport.getBoundingClientRect()
        for (const row of viewport.querySelectorAll<HTMLElement>('[data-index]')) {
          const rect = row.getBoundingClientRect()
          if (rect.bottom <= bounds.top + headerSize || rect.top >= bounds.bottom) continue
          const action = row.querySelector<HTMLElement>('input[type="checkbox"]:not(:disabled), button:not(:disabled):not([aria-disabled="true"])')
          if (!action?.getClientRects().length) continue
          action.focus({ preventScroll: true })
          if (viewport.ownerDocument.activeElement === action) break
        }
        return
      }
      const value = (event.target as Element).closest('[data-index]')?.getAttribute('data-index')
      if (value !== undefined && value !== null && items[Number(value)]) {
        focusedElement.current = event.target as HTMLElement
        setFocusedKey(itemKey(items[Number(value)]!))
      }
    }} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocusedKey(undefined) }}
    onKeyDown={event => {
      const target = event.target as Node
      if (event.defaultPrevented || !event.currentTarget.contains(target) || heading.current?.contains(target)
        || event.altKey || event.ctrlKey || event.metaKey || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
      let index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : focused + (event.key === 'ArrowDown' ? 1 : -1)
      const step = event.key === 'ArrowUp' || event.key === 'End' ? -1 : 1
      while (index >= 0 && index < items.length && !(isItemFocusable ? isItemFocusable(items[index]!) : row(items[index]!).onClick)) index += step
      if (index < 0 || index >= items.length) return
      event.preventDefault(); focusRequest.current = true; setFocusedKey(itemKey(items[index]!)); virtual.scrollToIndex(index, { align: 'auto' })
    }}>
    {header&&<Stack ref={heading} aria-hidden={!headerInteractive} style={{position:'sticky',top:0,zIndex:2,backgroundColor:'var(--color-background-body)',boxShadow:'0 calc(-1 * var(--spacing-3)) 0 var(--color-background-body)'}}>
      <List density="compact" edge="padded"><ListItem {...header}/></List>
    </Stack>}
    <List density="compact" edge="padded" aria-label={label} data-windowed-items="" style={{ height: virtual.getTotalSize(), position: 'relative' }}>
      {virtual.getVirtualItems().map(item => <MemoWindowedRow key={item.key} item={items[item.index]!} renderRow={row}
        measure={virtual.measureElement} index={item.index} start={item.start-listOffset} total={items.length}
        rowKey={String(item.key)} motionOrder={motionOrder} motionScope={resetKey} animateReorder={animateReorder} />)}
    </List>
  </StackItem>
}
