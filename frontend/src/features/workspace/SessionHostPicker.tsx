import { useMemo, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { Popover } from '@astryxdesign/core/Popover'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { ChevronDownIcon,PlusIcon,ServerStackIcon } from '@heroicons/react/24/outline'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { shortcutAria, shortcutTooltip, useShortcutOverrides } from './shortcutReference'
import OsIcon from '../../shared/icons/OsIcon'
import WindowedList from '../../shared/ui/WindowedList'
import { filterHostList } from '../connections/hostOrdering'
import type { HostProfile } from './mockGateway'

export default function SessionHostPicker({ hosts, pinOrder, open, compact, anchored, besideNewTerminal, editable, onOpenChange, onConnect, onCreate }: {
  hosts: HostProfile[]; pinOrder: readonly string[]; open: boolean; compact: boolean; editable: boolean
  besideNewTerminal?:boolean;anchored?: boolean; onOpenChange: (open: boolean) => void; onConnect: (host: HostProfile) => void; onCreate: (trigger: HTMLElement | null) => void
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()
  const [query,setQuery] = useState('')
  const results = useMemo(()=>open?filterHostList(hosts,query,'all','all',pinOrder):[],[hosts,query,pinOrder,open])
  const changeOpen = (next: boolean) => { if (!next) setQuery(''); onOpenChange(next) }
  const connect = (host: HostProfile) => { changeOpen(false); onConnect(host) }
  const label = tr("新建会话")
  const pickerIcon=besideNewTerminal?ChevronDownIcon:compact?PlusIcon:ServerStackIcon
  return <Popover isOpen={open} onOpenChange={changeOpen} placement="end" width="calc(var(--spacing-10) * 9)"
    label={label} isModal={false} closeButtonLabel={tr("关闭")}
    content={open&&<Stack gap={2}>
      <TextInput label={tr("搜索服务器")} isLabelHidden placeholder={tr("搜索主机或分组")} type="search" size="md"
        value={query} startIcon="search" hasClear autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} onChange={setQuery}
        onKeyDown={event=>{
          if(event.defaultPrevented||event.nativeEvent.isComposing||event.altKey||event.ctrlKey||event.metaKey)return
          if(event.key==='Enter'&&results[0]&&!event.repeat){event.preventDefault();connect(results[0])}
          if(event.key==='ArrowDown'&&results[0]){
            event.preventDefault()
            document.getElementById('session-host-results')?.querySelector<HTMLElement>('li button')?.focus()
          }
        }}/>
      {results.length?<Stack id="session-host-results" style={{
        height:`min(36dvh, calc(var(--spacing-10) * 7), calc(var(--spacing-3) * 2 + var(--spacing-4) * 3.5 * ${results.length}))`,minHeight:0,
      }}>
        <WindowedList items={results} itemKey={host=>host.id} estimateSize={()=>48} resetKey={query} label={tr("服务器配置")}
          row={host=>({label:host.name,description:`${host.username}@${host.address}:${host.port}`,startContent:<OsIcon name={host.icon}/>,onClick:()=>connect(host)})}/>
      </Stack>:<Text type="supporting" color="secondary">{tr("没有匹配的服务器")}</Text>}
      <Button label={tr("新建连接")} variant="ghost" size="sm" isDisabled={!editable} icon={<Icon icon={PlusIcon} size="sm"/>} style={{alignSelf:'flex-end'}}
        onClick={()=>{changeOpen(false);onCreate(document.getElementById('new-session'))}}/>
    </Stack>}>
    <Button id="new-session" label={label} tooltip={shortcutTooltip(label,'host-search',shortcuts)} aria-keyshortcuts={shortcutAria('host-search',shortcuts)} isIconOnly={compact}
      width={compact?undefined:'100%'} variant="ghost" size="sm"
      style={{backgroundImage:'none',
        ...(anchored&&!compact?{justifyContent:'flex-start',paddingInlineStart:'calc(var(--size-element-sm) + var(--spacing-4))'}:{})}}
      icon={anchored?<Stack as="span" style={{position:'absolute',insetInlineStart:'calc((var(--size-element-sm) - var(--spacing-4)) / 2)',insetBlockStart:'50%',translate:'0 -50%',display:'inline-flex'}}><Icon icon={pickerIcon} size="sm"/></Stack>:<Icon icon={pickerIcon} size="sm"/>}/>
  </Popover>
}
