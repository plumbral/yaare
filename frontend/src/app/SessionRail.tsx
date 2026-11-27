import { createContext, lazy, Suspense, useContext, useEffect, useLayoutEffect, useRef, useState, type ComponentProps, type CSSProperties, type FocusEvent, type PointerEvent, type ReactNode, type RefObject } from 'react'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu'
import { LayoutHeader, LayoutPanel } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { useMediaQuery } from '@astryxdesign/core/hooks'
import { RectangleGroupIcon, Bars3BottomLeftIcon, CursorArrowRaysIcon, Cog6ToothIcon, DocumentTextIcon } from '@heroicons/react/24/outline'
import { PinFilledIcon } from '../shared/icons/PinIcon'
import { useLocale } from '../shared/i18n/locale'
import { tr } from '../shared/i18n/messages'
import { DialogPresence } from '../shared/ui/AppDialog'
import SyncStatusPicker from '../features/sync/SyncStatusPicker'
import { compactRailWidth, panelLayout, railHeaderPlacement } from '../features/workspace/panelLayout'
import { WorkspaceNavigation, type WorkspaceView } from '../features/workspace/SessionTabs'
import SessionHostPicker from '../features/workspace/SessionHostPicker'
import WorkspaceAppearancePicker from '../features/workspace/WorkspaceAppearancePicker'
import { shortcutAria, shortcutTooltip, type useShortcutOverrides } from '../features/workspace/shortcutReference'
import type { Preferences } from '../features/workspace/preferences'
import SessionRailResize from './SessionRailResize'

const ReleaseNotesDialog = lazy(() => import('../features/workspace/ReleaseNotesDialog'))
type WindowRegionStyle = CSSProperties & { '--app-region': 'drag' | 'no-drag' }
const titlebarInsetLeft = 'var(--mygo-titlebar-inset-left, 0px)'
const titlebarInsetRight = 'var(--mygo-titlebar-inset-right, 0px)'
const titlebarControlGap = 'var(--spacing-6)'
const captionControlInset = (inset:string) =>
  `calc(${inset} + clamp(var(--spacing-2), ${inset}, ${titlebarControlGap}))`
const railModeButtonOffset = `(${compactRailWidth}px - var(--size-element-sm)) / 2`
const railCaptionStart = `calc(${titlebarInsetLeft} + min(${titlebarInsetLeft}, ${titlebarControlGap} - ${railModeButtonOffset}))`
const titlebarStyle: WindowRegionStyle = {
  '--app-region': 'drag',
  userSelect: 'none',
  paddingInlineEnd: captionControlInset(titlebarInsetRight),
}
export const titlebarHeight = 'max(calc(var(--spacing-4) * 3), var(--mygo-titlebar-height, 0px))'
const railFooterColumnWidth = `calc(${compactRailWidth}px - var(--spacing-1-5) * 2)`

type Props = {
  top:boolean
  hasNewTerminal:boolean
  onPosition:(top:boolean)=>void
  viewportWidth: number
  pinned: boolean
  startWidth: number
  endWidth: number
  desiredMinimum: number
  surface: RefObject<HTMLElement | null>
  onPinnedChange: (pinned: boolean) => void
  onCommitWidth: (size: number) => void
  pickerOpen: boolean
  onPickerOpen: (open: boolean) => void
  onConnect: ComponentProps<typeof SessionHostPicker>['onConnect']
  onCreate: ComponentProps<typeof SessionHostPicker>['onCreate']
  renderTabs: (top:boolean,compact:boolean,railWidth:number,onMenuOpenChange:(open:boolean)=>void)=>ReactNode
  hosts: ComponentProps<typeof SessionHostPicker>['hosts']
  pinOrder: ComponentProps<typeof SessionHostPicker>['pinOrder']
  editable: boolean
  view: WorkspaceView
  showShortcuts: boolean
  onView: ComponentProps<typeof WorkspaceNavigation>['onView']
  preferences: Preferences
  systemMode: 'light' | 'dark'
  onAppearance: ComponentProps<typeof WorkspaceAppearancePicker>['onChange']
  onHistory: () => void
  onSettings: () => void
  updateAvailable: boolean
  settingsLoading: boolean
  shortcutOverrides: ReturnType<typeof useShortcutOverrides>
  children: ReactNode
}
type Rail = { top:boolean; topTabs:ReactNode; header: ReactNode; content: ReactNode; releaseNotes: ReactNode; floating: boolean }
const RailContext = createContext<Rail>(null!)

export function SessionRailProvider({
  top,hasNewTerminal,onPosition,viewportWidth, pinned: railPinned, startWidth, endWidth, desiredMinimum, surface, onPinnedChange, onCommitWidth,
  pickerOpen, onPickerOpen, onConnect, onCreate, renderTabs, hosts, pinOrder,
  editable, view, showShortcuts, onView, preferences, systemMode, onAppearance, onHistory, onSettings,
  updateAvailable, settingsLoading, shortcutOverrides, children,
}: Props) {
  useLocale()
  const switchPosition=()=>{
    onPosition(!top)
    requestAnimationFrame(()=>document.getElementById(top?'tabs-position':'open-settings')?.focus())
  }
  const sizes = { start: startWidth, end: endWidth }
  const budget = panelLayout(viewportWidth, startWidth, endWidth, railPinned, false, 'start',
    Math.min(Math.max(0, viewportWidth - compactRailWidth - 2), desiredMinimum))
  const [hoverRail,setHoverRail]=useState(()=>{try{return localStorage.getItem('yaare.rail-hover')==='on'}catch{return false}})
  const [railPeek,setRailPeek]=useState(false)
  const [railContextOpen,setRailContextOpen]=useState(false)
  const [railAppearanceOpen,setRailAppearanceOpen]=useState(false)
  const [railSyncOpen,setRailSyncOpen]=useState(false)
  const [releaseNotesOpen,setReleaseNotesOpen]=useState(false)
  const railPointer=useRef(false)
  const railPreviewUntil=useRef(0)
  const railDelay=useRef<ReturnType<typeof setTimeout>|undefined>(undefined)
  const railDocked=!budget.leftCollapsed
  const railFloating=!railDocked&&hoverRail&&railPeek
  const railExpanded=railDocked||railFloating
  const reduceRailMotion=useMediaQuery('(prefers-reduced-motion: reduce)')
  const railCompact=!railExpanded
  const railBody=useRef<HTMLElement>(null)
  const railHeader=useRef<HTMLElement>(null)
  const railMode=railPinned?'pinned':hoverRail?'hover':'collapsed'
  const anchoredRail=railMode==='hover'
  const railAppearanceVisible=top||railExpanded||railMode==='collapsed'
  const nextRailMode=railMode==='collapsed'?'hover':railMode==='hover'?'pinned':'collapsed'
  const frameTone='var(--color-workspace-frame)'
  const expandedRailWidth=railDocked?Math.min(sizes.start,budget.leftMax):Math.max(compactRailWidth,Math.min(sizes.start,360,viewportWidth-compactRailWidth))
  const visualRailWidth=railDocked?`var(--yaare-live-rail-width, ${expandedRailWidth}px)`:`${expandedRailWidth}px`
  const railFootprint=railDocked?visualRailWidth:`${compactRailWidth}px`
  const railClip=railExpanded?'inset(0 0 0 0)':`inset(0 calc(100% - ${compactRailWidth}px) 0 0)`
  const railHeaderLayout=railHeaderPlacement(railDocked,railExpanded,railFootprint,visualRailWidth,railCaptionStart)
  const railTransition=reduceRailMotion?undefined:'clip-path var(--duration-fast-max) var(--ease-standard)'
  const railPopupOpen=pickerOpen||railContextOpen||railAppearanceOpen||railSyncOpen||releaseNotesOpen
  const railFrames=useRef<Map<string,DOMRect>|null>(null)
  const railMoves=useRef<Animation[]>([])
  const railControls=()=>[railHeader.current,railBody.current].flatMap(root=>root?Array.from(root.querySelectorAll<HTMLElement>(
    '[id^="tab-"],[id^="close-tab-"],#new-session,#workspace-appearance',
  )):[])
  const captureRail=()=>{
    if(!reduceRailMotion)railFrames.current=new Map(railControls().map(node=>[node.id,node.getBoundingClientRect()]))
  }
  const updateRailPeek=(open:boolean)=>{
    if(!anchoredRail)captureRail()
    setRailPeek(open)
  }
  useLayoutEffect(()=>{
    const before=railFrames.current
    railFrames.current=null
    railMoves.current.forEach(animation=>animation.cancel())
    railMoves.current=[]
    if(!before||reduceRailMotion||!railBody.current)return
    const tokens=getComputedStyle(railBody.current),time=tokens.getPropertyValue('--duration-fast-max').trim()
    const duration=Number.parseFloat(time)*(time.endsWith('ms')?1:1000)||165
    const easing=tokens.getPropertyValue('--ease-standard').trim()||'ease-out'
    const controls=railControls().map(node=>({node,after:node.getBoundingClientRect(),prior:before.get(node.id)}))
    for(const {node,after,prior} of controls){
      if(!prior){railMoves.current.push(node.animate([{opacity:0},{opacity:1}],{duration,easing}));continue}
      const x=prior.left-after.left,y=prior.top-after.top
      if(Math.abs(x)+Math.abs(y)>.5)railMoves.current.push(node.animate([{translate:`${x}px ${y}px`},{translate:'0 0'}],{duration,easing}))
    }
  },[railExpanded,reduceRailMotion])
  const railPopupRef=useRef(railPopupOpen)
  railPopupRef.current=railPopupOpen
  const dismissIdleRail=()=>{
    const focused=document.activeElement
    if(railPointer.current||railPopupRef.current||focused?.matches(':focus-visible')&&
      (railHeader.current?.contains(focused)||railBody.current?.contains(focused)))return
    updateRailPeek(false)
  }
  useEffect(()=>{
    if(!railAppearanceVisible)setRailAppearanceOpen(false)
  },[railAppearanceVisible])
  useEffect(()=>{
    if(!railFloating||railPopupOpen||railPointer.current)return
    const timer=setTimeout(dismissIdleRail,Math.max(180,railPreviewUntil.current-Date.now()))
    return ()=>clearTimeout(timer)
  },[railFloating,railPopupOpen])
  useEffect(()=>()=>clearTimeout(railDelay.current),[])
  useEffect(()=>{
    if(!railFloating||railPopupOpen)return
    const dismiss=(event:KeyboardEvent)=>{
      if(event.key!=='Escape'||event.defaultPrevented||document.querySelector('dialog[open]'))return
      clearTimeout(railDelay.current)
      if(document.activeElement?.closest('#session-rail, [data-session-rail-header]')){
        document.getElementById('session-rail-mode')?.focus({preventScroll:true})
      }
      updateRailPeek(false)
    }
    window.addEventListener('keydown',dismiss)
    return ()=>window.removeEventListener('keydown',dismiss)
  },[railFloating,railPopupOpen])
  const railInteractionHandlers={
    onFocusCapture:(event:FocusEvent)=>{
      if(anchoredRail&&!railDocked&&!railPeek&&event.target.matches(':focus-visible'))updateRailPeek(true)
    },
    onBlurCapture:(event:FocusEvent)=>{
      const next=event.relatedTarget
      if(!railFloating||railPointer.current||next instanceof Node&&
        (railHeader.current?.contains(next)||railBody.current?.contains(next)))return
      clearTimeout(railDelay.current)
      railDelay.current=setTimeout(dismissIdleRail,0)
    },
    onPointerEnter:(event:PointerEvent)=>{
      if(event.pointerType!=='mouse')return
      railPointer.current=true
      clearTimeout(railDelay.current)
      if(!railDocked&&hoverRail&&!railPeek&&!railPopupOpen)railDelay.current=setTimeout(()=>{
        if(railPointer.current&&!railPopupRef.current&&!document.querySelector('dialog[open]'))updateRailPeek(true)
      },160)
    },
    onPointerLeave:(event:PointerEvent)=>{
      if(event.pointerType!=='mouse')return
      railPointer.current=false
      railPreviewUntil.current=0
      clearTimeout(railDelay.current)
      if(!railPopupOpen)railDelay.current=setTimeout(dismissIdleRail,180)
    },
  }
  const setRailMode=(mode:'collapsed'|'hover'|'pinned')=>{
    clearTimeout(railDelay.current)
    captureRail()
    setHoverRail(mode!=='collapsed')
    try{localStorage.setItem('yaare.rail-hover',mode==='collapsed'?'off':'on')}catch{}
    onPinnedChange(mode==='pinned')
    railPreviewUntil.current=mode==='hover'?Date.now()+1100:0
    setRailPeek(mode==='hover')
  }
  const workspaceNavigation=<WorkspaceNavigation view={view} compact={!top&&railCompact} showShortcuts={showShortcuts} onView={onView}/>
  const railModeLabel=(mode:typeof railMode)=>mode==='collapsed'?tr("始终收起"):mode==='hover'?tr("悬停展开"):tr("固定展开")
  const railModeHint=tr("会话栏：{p0}。点击切换到{p1}。",railModeLabel(railMode),railModeLabel(nextRailMode))
  const railAppearanceAction=railAppearanceVisible&&<WorkspaceAppearancePicker key="appearance"
    value={preferences} systemMode={systemMode} onOpenChange={setRailAppearanceOpen}
    onChange={onAppearance}/>
  const positionLabel=top?tr('标签栏移到左侧'):tr('标签栏移到顶部')
  const positionButton=<IconButton id="tabs-position" label={positionLabel} tooltip={positionLabel} variant="ghost" size="sm" icon={<Icon icon={RectangleGroupIcon} size="sm"/>} onClick={switchPosition}/>
  const syncAction=<SyncStatusPicker visible={railAppearanceVisible} onOpenChange={setRailSyncOpen} onHistory={onHistory}/>
  const railSecondaryActions=<Stack key="secondary" direction={railExpanded?'horizontal':'vertical'} align="center" gap={1}>
    {positionButton}
    {railAppearanceVisible&&<IconButton id="open-release-notes" label={tr('更新日志')} tooltip={tr('更新日志')}
      aria-haspopup="dialog" variant="ghost" size="sm" icon={<Icon icon={DocumentTextIcon} size="sm"/>}
      onClick={()=>setReleaseNotesOpen(true)}/>}
    {syncAction}
    {railAppearanceAction}
  </Stack>
  const railSettingsAction=<Stack key="settings" width={railFooterColumnWidth} align="center" style={{flexShrink:0}}>
    <Stack style={{position:'relative'}}>
      {top?<DropdownMenu alignment="end" presentation="adaptive" items={[
        {label:updateAvailable?tr('设置，有可用更新'):tr('设置'),onClick:onSettings},
        {label:positionLabel,onClick:switchPosition},
        {label:tr('更新日志'),onClick:()=>setReleaseNotesOpen(true)},
      ]} renderTrigger={props=><IconButton {...props} id="open-settings" label={tr('工作区')} tooltip={tr('工作区')} size="sm" variant={view==='settings'?'secondary':'ghost'} icon={<Icon icon={Cog6ToothIcon} size="sm"/>}/>}/>:<IconButton id="open-settings" label={updateAvailable ? tr('设置，有可用更新') : tr("设置")} tooltip={shortcutTooltip(tr('设置'),'settings',shortcutOverrides)} aria-keyshortcuts={shortcutAria('settings',shortcutOverrides)}
        aria-current={view === 'settings' ? 'page' : undefined} aria-controls="panel-settings"
        variant={view === 'settings' ? 'secondary' : 'ghost'} size="sm" isLoading={settingsLoading}
        icon={<Icon icon={Cog6ToothIcon} size="sm" />} onClick={onSettings} />}
      {updateAvailable && <Stack aria-hidden style={{position:'absolute',insetBlockStart:'var(--spacing-1)',insetInlineEnd:'var(--spacing-1)',width:'var(--spacing-1-5)',height:'var(--spacing-1-5)',borderRadius:'var(--radius-full)',background:'var(--color-text-primary)',pointerEvents:'none'}}/>}
    </Stack>
  </Stack>
  const picker=(compact:boolean,anchored=false)=><SessionHostPicker besideNewTerminal={compact&&hasNewTerminal} hosts={hosts} pinOrder={pinOrder} open={pickerOpen} compact={compact} anchored={anchored} editable={editable}
    onOpenChange={onPickerOpen} onConnect={onConnect} onCreate={onCreate}/>
  const topTabs=top&&<Stack direction="horizontal" align="center" gap={1} height={titlebarHeight} style={{minWidth:0}}>
    <Stack direction="horizontal" gap={1} align="center" style={{minWidth:0}}>
      {renderTabs(true,false,0,setRailContextOpen)}{picker(true)}
    </Stack>
    <StackItem id="window-drag-region" size="fill" crossAlignSelf="stretch" style={{minWidth:'var(--spacing-6)'}} aria-hidden="true"/>
    <Stack data-top-workspace-tools="" direction="horizontal" align="center" gap={1} style={{flexShrink:0}}>
      {syncAction}{railAppearanceAction}{railSettingsAction}
    </Stack>
  </Stack>
  const header = top?workspaceNavigation:(
          <Stack style={{width:railHeaderLayout.slotWidth,minWidth:railHeaderLayout.slotMinWidth,height:titlebarHeight,flexShrink:0,position:'relative',zIndex:railHeaderLayout.overlay?2:undefined}}>
            <Stack ref={railHeader} data-session-rail-header="" direction="horizontal" align="center" justify="between" paddingInlineEnd={railCompact?0:2}
              {...railInteractionHandlers} style={{
                ...(railHeaderLayout.overlay
                  ? {
                    position:'absolute',insetBlockStart:0,insetInlineStart:0,
                    width:railHeaderLayout.surfaceWidth,minWidth:railHeaderLayout.surfaceMinWidth,
                    height:titlebarHeight,
                    background:frameTone,clipPath:railClip,transition:railTransition,overflow:'clip',
                    borderRadius:railFloating?'var(--radius-element) var(--radius-element) 0 0':undefined,
                    ...(railFloating?{'--app-region':'no-drag'}:{}),
                  } as WindowRegionStyle
                  : {width:railHeaderLayout.surfaceWidth,minWidth:railHeaderLayout.surfaceMinWidth,height:titlebarHeight,position:'relative'}),
              }}>
              <Stack width={compactRailWidth} height="100%" align="center" justify="center" style={{flexShrink:0}}>
                <IconButton id="session-rail-mode" label={railModeHint} tooltip={railModeHint} aria-controls="session-rail"
                  variant="ghost" size="sm" icon={<Icon icon={railMode==='collapsed'?Bars3BottomLeftIcon:railMode==='hover'?CursorArrowRaysIcon:PinFilledIcon} size="sm"/>}
                  onClick={()=>setRailMode(nextRailMode)}/>
              </Stack>
              {railExpanded&&workspaceNavigation}
            </Stack>
          </Stack>
  )
  const content = top?null:<>
          <LayoutPanel id="session-rail" width={railFootprint} padding={0} isScrollable={false} hasDivider={false}
            style={{background:frameTone,position:'relative',overflow:'visible',zIndex:!railDocked?4:undefined}}>
            <Stack ref={railBody} {...railInteractionHandlers}
              style={{position:'absolute',insetBlock:0,insetInlineStart:0,
                width:visualRailWidth,clipPath:railClip,transition:railTransition,overflow:'clip',
                background:frameTone,borderRadius:railFloating?'0 0 var(--radius-element) var(--radius-element)':undefined}}>
            <Stack height="100%" width={railCompact?compactRailWidth:'100%'} gap={2} paddingBlock={1.5} paddingInline={railCompact?0:1.5} align={railCompact?'center':undefined}
              style={{minHeight:0}}>
              {railCompact&&!anchoredRail&&workspaceNavigation}
              <Stack width="100%" gap={1} paddingBlock={1}
                style={{minHeight:0}}>
                {renderTabs(false,railCompact,expandedRailWidth,setRailContextOpen)}
                <Stack style={{flexShrink:0}} align={railCompact?'center':undefined} paddingInline={anchoredRail&&!railCompact?1:undefined}>
                  {picker(railCompact,anchoredRail)}
                </Stack>
              </Stack>
              <StackItem size="fill" />
              <Stack direction={railExpanded?'horizontal':'vertical'} align="center" justify={railExpanded?'between':undefined}
                width={railExpanded||!anchoredRail?'100%':railFooterColumnWidth}
                gap={1} style={{flexShrink:0}}>
                {railExpanded?[railSettingsAction,railSecondaryActions]:[railSecondaryActions,railSettingsAction]}
              </Stack>
            </Stack>
            </Stack>
          </LayoutPanel>
          {railDocked&&<SessionRailResize surface={surface} size={expandedRailWidth} maxSize={budget.leftMax} onCommit={onCommitWidth} />}
  </>
  const releaseNotes = (
      <Suspense fallback={null}><DialogPresence value={releaseNotesOpen ? true : null}>
        {(_snapshot,isOpen,onAfterClose)=><ReleaseNotesDialog isOpen={isOpen} onClose={()=>setReleaseNotesOpen(false)} onAfterClose={onAfterClose}/>}
      </DialogPresence></Suspense>
  )
  return <RailContext value={{ top,topTabs,header, content, releaseNotes, floating: !top&&railFloating }}>
    {children}
    {!top&&anchoredRail&&!railDocked&&<Stack aria-hidden="true" data-floating-rail-frame=""
      style={{width:railExpanded?visualRailWidth:railFootprint,opacity:railFloating?1:0}}/>}
  </RailContext>
}

export function TopSessionTabs(){return useContext(RailContext).topTabs}
export function SessionRail() { return useContext(RailContext).content }
export function SessionRailHeader() { return useContext(RailContext).header }
export function SessionRailReleaseNotes() { return useContext(RailContext).releaseNotes }
export function SessionRailCaption({ children }: { children: ReactNode }) {
  const { floating,top } = useContext(RailContext)
  useLocale()
  return <LayoutHeader padding={0} height={titlebarHeight} data-window-caption="" role="banner" label={tr("会话与窗口操作")}
    style={{ ...titlebarStyle, paddingInlineStart:top?captionControlInset(titlebarInsetLeft):railCaptionStart,
      position: 'relative', zIndex: floating ? 5 : undefined, background: 'var(--color-workspace-frame)' }}>
    {children}
  </LayoutHeader>
}
