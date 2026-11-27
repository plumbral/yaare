import { useLayoutEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { LayoutHeader } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'

export const managementSearchStyle: CSSProperties = {
  width: 'calc(var(--spacing-10) * 6)',
  minWidth: 'min(100%, calc(var(--spacing-10) * 2))',
  maxWidth: '100%', flex: '0 1 auto',
}

export default function ManagementToolbar({ title, navigation, children, status, caption, search }: {
  title: string; navigation: ReactNode; children: ReactNode | ((compact:boolean)=>ReactNode); status?: ReactNode; search?: ReactNode
  caption?: HTMLElement|null
}) {
  const [captionMode,setCaptionMode]=useState<'wide'|'compact'|'narrow'>('wide')
  const hasSearch=!!search
  useLayoutEffect(()=>{
    if(!caption)return
    const measure=()=>setCaptionMode(hasSearch&&caption.clientWidth<420?'narrow':caption.clientWidth<620?'compact':'wide')
    measure()
    const observer=new ResizeObserver(measure)
    observer.observe(caption)
    return()=>observer.disconnect()
  },[caption,hasSearch])
  const compact=!!caption&&captionMode!=='wide'
  const searchBelow=!!caption&&captionMode==='narrow'
  const controls=typeof children==='function'?children(compact):children
  if(caption)return <>
    {createPortal(<search aria-label={title} data-management-caption="" style={{width:'100%',minWidth:0}}>
      <Stack direction="horizontal" align="center" gap={compact?1:2}
        style={hasSearch ? {display:'grid',gridTemplateColumns:'minmax(0, 1fr) auto',minWidth:0,width:'100%'} : {minWidth:0}}>
        <StackItem size="fill" style={{minWidth:0}}>{navigation}</StackItem>
        <Stack direction="horizontal" align="center" gap={compact?1:2} style={{minWidth:0,maxWidth:search?'100%':compact?'78%':'80%',flex:'0 1 max-content'}}>
          {!searchBelow&&search}
          {controls}
        </Stack>
      </Stack>
    </search>,caption)}
    {searchBelow&&<LayoutHeader padding={3} hasDivider={false}>{search}</LayoutHeader>}
    {status&&<LayoutHeader padding={3} hasDivider={false}>{status}</LayoutHeader>}
  </>
  return <LayoutHeader padding={3} hasDivider data-management-header="">
    <VisuallyHidden as="h2">{title}</VisuallyHidden>
    <Stack gap={2}>
      <search aria-label={title}><Stack direction="horizontal" align="center" gap={3} wrap="wrap"
        style={{ minWidth: 0, minHeight: 'calc(var(--size-element-md) + var(--spacing-2))' }}>
        <StackItem size="fill" style={{ minWidth: 0, flexBasis: 'calc(var(--spacing-10) * 2)' }}>{navigation}</StackItem>
        <Stack direction="horizontal" align="center" gap={2} wrap="wrap"
          style={{ minWidth: 0, maxWidth: '100%', flex: '0 1 auto' }}>{search}{controls}</Stack>
      </Stack></search>
      {status}
    </Stack>
  </LayoutHeader>
}
