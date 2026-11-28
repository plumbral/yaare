import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { shortcutAria, shortcutTooltip, useShortcutOverrides } from './shortcutReference'
import {Button} from '@astryxdesign/core/Button'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {DropdownMenu,type DropdownMenuOption} from '@astryxdesign/core/DropdownMenu'
import {Selector} from '@astryxdesign/core/Selector'
import {MegaphoneIcon,ChevronDownIcon,MagnifyingGlassIcon,ViewColumnsIcon,EllipsisHorizontalIcon} from '@heroicons/react/24/outline'
import type {WorkspaceControls} from './TerminalWorkspaceView'
import { defaultPreferences, maxTerminalFontSize, minTerminalFontSize, stepTerminalFontSize } from './preferences'

export type TerminalAction='focus'|'search'|'copy'|'paste'|'clear'|'zmodemUpload'
export default function SessionToolbar({pending,connected,busy,reconnecting,onConnection,onForwards,
  onTerminal,onShellIntegration,zmodemAvailable,fontSize,onFontSize,controls,forwards=0,remoteView=false,onLogs,recording=false,onBroadcast,broadcastAvailable=false,onDesktop}:{
  pending:boolean;connected:boolean;busy:boolean;reconnecting?:boolean
  onConnection:()=>void;onForwards:()=>void;onTerminal:(action:TerminalAction)=>void
  onShellIntegration:()=>void
  zmodemAvailable:boolean
  fontSize:number;onFontSize:(size:number)=>void;controls:WorkspaceControls;forwards?:number
  remoteView?:boolean
  onLogs?:()=>void;recording?:boolean
  onBroadcast?:()=>void;broadcastAvailable?:boolean
  onDesktop?:()=>void
}){
  useLocale()
  const shortcuts = useShortcutOverrides()

  const terminalAvailable=!pending&&!remoteView
  function splitMenuChanged(open: boolean) {
    if (open || !terminalAvailable || !connected) return
    const pane = document.getElementById('pane-' + controls.activeId)
    requestAnimationFrame(() => {
      if (document.activeElement?.id === 'split-menu' && pane?.getAttribute('aria-current') === 'true') onTerminal('focus')
    })
  }
  const items:DropdownMenuOption[]=[
    {type:'section',title:tr("终端"),items:[
      {label:tr("复制所选内容"),isDisabled:!terminalAvailable,onClick:()=>onTerminal('copy')},
      {label:tr("粘贴"),isDisabled:!terminalAvailable||!connected,onClick:()=>onTerminal('paste')},
      {label:tr("清屏"),isDisabled:!terminalAvailable,onClick:()=>onTerminal('clear')},
      {label:tr("终端显示"),isDisabled:!terminalAvailable,items:[
        {label:tr("增大字号"),isDisabled:fontSize>=maxTerminalFontSize,onClick:()=>onFontSize(stepTerminalFontSize(fontSize,1))},
        {label:tr("减小字号"),isDisabled:fontSize<=minTerminalFontSize,onClick:()=>onFontSize(stepTerminalFontSize(fontSize,-1))},
        {label:tr("恢复默认字号"),isDisabled:fontSize===defaultPreferences.fontSize,onClick:()=>onFontSize(defaultPreferences.fontSize)},
      ]},
      {label:recording?tr("会话日志 · 录制中"):tr("会话日志"),isDisabled:pending,onClick:onLogs},
      ...(controls.panes.length<=1?[{label:tr("命令广播…"),isDisabled:!broadcastAvailable,onClick:onBroadcast}]:[]),
      {label:tr("ZMODEM 上传…"),isDisabled:!terminalAvailable||!connected||!zmodemAvailable,onClick:()=>onTerminal('zmodemUpload')},
      {label:tr("Bash 终端集成…"),isDisabled:!terminalAvailable||!connected,onClick:onShellIntegration},
    ]},
    {type:'divider'},
    {type:'section',title:tr("连接"),items:[
      {label:tr("远程桌面"),isDisabled:pending||!connected,onClick:onDesktop},
      {label:tr("端口转发"),isDisabled:pending,onClick:onForwards,endContent:forwards?<Text type="supporting">{forwards}</Text>:undefined},
      {label:connected?tr("断开连接"):reconnecting?tr("停止重连"):busy?tr("取消连接"):tr("连接服务器"),isDisabled:pending,variant:connected?'destructive':undefined,onClick:onConnection},
    ]},
  ]
  return <Stack role="group" aria-label={tr("终端工作区操作")} direction="horizontal" align="center" gap={1} style={{minWidth:0}}>
      {controls.focusOnly&&<StackItem style={{minWidth:0,width:'min(18vw, calc(var(--spacing-10) * 4))'}}>
        <Selector label={tr("当前窗格")} isLabelHidden variant="ghost" size="sm" width="100%" value={controls.activeId} options={controls.panes} onChange={controls.onActivate}/>
      </StackItem>}
      <Stack direction="horizontal" align="center" gap={1} style={{flexShrink:0}}>
        <IconButton label={tr("搜索终端")} tooltip={shortcutTooltip(tr('搜索终端'),'terminal-search',shortcuts)} aria-keyshortcuts={shortcutAria('terminal-search',shortcuts)} size="sm" variant="ghost" isDisabled={!terminalAvailable} icon={<Icon icon={MagnifyingGlassIcon} size="sm"/>} onClick={()=>onTerminal('search')}/>
        {controls.panes.length>1&&<IconButton label={tr("命令广播…")} tooltip={tr("命令广播…")} isDisabled={!broadcastAvailable} size="sm" variant="ghost" icon={<Icon icon={MegaphoneIcon} size="sm"/>} onClick={onBroadcast}/>}
        {controls.maximized&&<Button label={tr('返回 {p0} 分屏',controls.panes.length)} variant="secondary" size="sm" onClick={controls.onRestore}/>}
        <IconButton id="quick-split" label={tr('向右新建同主机终端')} tooltip={tr('向右新建同主机终端')} size="sm" variant="ghost" isDisabled={!terminalAvailable||!controls.canQuickSplit} icon={<Icon icon={ViewColumnsIcon} size="sm"/>} onClick={controls.quickSplit}/>
        <DropdownMenu alignment="end" presentation="adaptive" onOpenChange={splitMenuChanged} renderTrigger={props=><IconButton {...props} id="split-menu" label={tr("分屏")} tooltip={tr("拆分与排列终端")} size="sm" variant="ghost" isDisabled={remoteView} icon={<Icon icon={ChevronDownIcon} size="sm"/>}/>} items={controls.maximized?[{label:tr("还原布局"),onClick:controls.onRestore},...controls.splitMenu]:controls.splitMenu}/>
        <DropdownMenu alignment="end" presentation="adaptive" renderTrigger={props=><IconButton {...props} label={tr("会话更多操作")} tooltip={tr("会话更多操作")} size="sm" variant="ghost" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/>} items={items}/>
      </Stack>
    </Stack>
}
