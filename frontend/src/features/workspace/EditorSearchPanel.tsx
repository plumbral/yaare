import { useEffect, useId, useMemo, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { DropdownMenu, DropdownMenuCheckboxItem } from '@astryxdesign/core/DropdownMenu'
import { ChevronDownIcon, ChevronUpIcon, EllipsisHorizontalIcon, XMarkIcon } from '@heroicons/react/24/outline'
import { closeSearchPanel, findNext, findPrevious, getSearchQuery, replaceAll, replaceNext, SearchQuery, setSearchQuery } from '@codemirror/search'
import type { EditorView } from '@codemirror/view'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'

export default function EditorSearchPanel({view,inline=false}: {view: EditorView;inline?:boolean}) {
  useLocale()
  const id=useId(), [replacing,setReplacing]=useState(false)
  const query=getSearchQuery(view.state), doc=view.state.doc
  const count=useMemo(()=>{
    if(!query.valid)return 0
    const cursor=query.getCursor(view.state)
    let total=0
    while(total<10000&&!cursor.next().done)total++
    return total
  },[doc,query])
  const update=(patch:Partial<Pick<SearchQuery,'search'|'replace'|'caseSensitive'|'regexp'|'wholeWord'>>)=>
    view.dispatch({effects:setSearchQuery.of(new SearchQuery({...query,...patch}))})
  const close=()=>{closeSearchPanel(view);view.focus()}
  useEffect(()=>{
    const input=view.dom.ownerDocument.getElementById(id) as HTMLInputElement|null
    input?.setAttribute('main-field','true')
    input?.focus()
    input?.select()
  },[id,view])

  return <Stack align="end" paddingInline={inline?0:2} paddingBlock={inline?0:1} gap={1} role="search" aria-label={tr("查找")}
    style={{minWidth:0,position:'relative'}}
    onKeyDown={event=>{
      if(event.nativeEvent.isComposing||event.nativeEvent.keyCode===229)return
      if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close()}
    }}>
    <Stack direction="horizontal" align="center" wrap={inline?undefined:'wrap'} gap={1} style={{minWidth:0,maxWidth:'100%'}}>
      <TextInput id={id} label={tr("查找")} isLabelHidden placeholder={tr("查找")} size="sm" width="100%"
        containerStyle={{width:'min(24vw, calc(var(--spacing-10) * 5))',minWidth:'var(--spacing-10)',flex:'0 1 auto'}}
        value={query.search} onChange={search=>update({search})} autoComplete="off" onKeyDown={event=>{
          if(event.key!=='Enter'||event.nativeEvent.isComposing||event.nativeEvent.keyCode===229)return
          event.preventDefault();event.stopPropagation()
          if(count)(event.shiftKey?findPrevious:findNext)(view)
        }}/>
      <Text type="supporting" color="secondary" maxLines={1} style={{color:query.search&&!query.valid?'var(--color-text-error)':undefined}} role="status">
        {query.search?(query.valid?tr("{p0} 个匹配", count===10000?'10,000+':count):tr("正则表达式无效")):''}
      </Text>
      <IconButton label={tr("上一个匹配")} tooltip={tr("上一个匹配")} size="sm" variant="ghost" isDisabled={!count}
        icon={<Icon icon={ChevronUpIcon} size="sm"/>} onClick={()=>findPrevious(view)}/>
      <IconButton label={tr("下一个匹配")} tooltip={tr("下一个匹配")} size="sm" variant="ghost" isDisabled={!count}
        icon={<Icon icon={ChevronDownIcon} size="sm"/>} onClick={()=>findNext(view)}/>
      {inline?<DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} label={tr("更多")} tooltip={tr("更多")}
        size="sm" variant="ghost" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>}>
        <DropdownMenuCheckboxItem label={tr("区分大小写")} value={query.caseSensitive} onChange={caseSensitive=>update({caseSensitive})}/>
        <DropdownMenuCheckboxItem label={tr("正则表达式")} value={query.regexp} onChange={regexp=>update({regexp})}/>
        <DropdownMenuCheckboxItem label={tr("全字")} value={query.wholeWord} onChange={wholeWord=>update({wholeWord})}/>
      </DropdownMenu>:<>
      <Button label="Aa" aria-label={tr("区分大小写")} tooltip={tr("区分大小写")} aria-pressed={query.caseSensitive}
        variant={query.caseSensitive?'secondary':'ghost'} size="sm" onClick={()=>update({caseSensitive:!query.caseSensitive})}/>
      <Button label=".*" aria-label={tr("正则表达式")} tooltip={tr("正则表达式")} aria-pressed={query.regexp}
        variant={query.regexp?'secondary':'ghost'} size="sm" onClick={()=>update({regexp:!query.regexp})}/>
      <Button label={tr("全字")} aria-pressed={query.wholeWord} variant={query.wholeWord?'secondary':'ghost'}
        size="sm" onClick={()=>update({wholeWord:!query.wholeWord})}/></>}
      {!view.state.readOnly&&<Button label={tr("替换")} size="sm" variant="ghost" aria-expanded={replacing} onClick={()=>setReplacing(value=>!value)}/>}
      <IconButton label={tr("关闭查找")} tooltip="Esc" size="sm" variant="ghost"
        icon={<Icon icon={XMarkIcon} size="sm"/>} onClick={close}/>
    </Stack>
    {replacing&&!view.state.readOnly&&<Stack direction="horizontal" align="center" wrap="wrap" gap={1} padding={inline?2:0}
      style={inline?{position:'absolute',insetBlockStart:'calc(100% + var(--spacing-1))',insetInlineEnd:0,zIndex:4,width:'max-content',maxWidth:'min(90vw, calc(var(--spacing-10) * 8))',borderRadius:'var(--radius-element)',
        background:'var(--color-background-popover)',boxShadow:'var(--shadow-med)'}:undefined}>
      <TextInput label={tr("替换为")} isLabelHidden placeholder={tr("替换为")} size="sm"
        value={query.replace} onChange={replace=>update({replace})}/>
      <Button label={tr("替换当前")} size="sm" variant="ghost" isDisabled={!count} onClick={()=>replaceNext(view)}/>
      <Button label={tr("全部替换")} size="sm" variant="ghost" isDisabled={!count} onClick={()=>replaceAll(view)}/>
    </Stack>}
  </Stack>
}
