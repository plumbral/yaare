import { localizeMessage, tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentProps,
  type KeyboardEvent,
  type PointerEvent,
} from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import SftpPanel, { rememberLocalFolder, type TaskSource } from './SftpPanel'
import TransferTasks from './TransferTasks'
import { usePaneSSH, type PaneSSHSource } from '../connections/useSSHSessions'
import { clampSftpSplitRatio, readSftpSplitRatio, writeSftpSplitRatio } from './fileColumns'
import { fileGateway, type FileTask } from '../../shared/bridge/files'

type Side = 'local' | 'remote'
type Props = Omit<ComponentProps<typeof SftpPanel>, 'view'|'onStatus'|'taskSource'|'localRevision'|'onLocalChanged'|'onFullWorkspace'>

export default function ConnectedSftpWorkspace({ source, ...props }: Omit<Props, 'live'> & { source: PaneSSHSource }) {
  const live = usePaneSSH(source, props.session?.id ?? '')
  return <SftpWorkspace {...props} live={live}/>
}

function SftpWorkspace({active=true,gateway=fileGateway,reloadVersion=0,...props}:Props){
  useLocale()
  const root=useRef<HTMLDivElement>(null), id=useId()
  const [workspaceWidth,setWorkspaceWidth]=useState(0), [tasksOpen,setTasksOpen]=useState(false), [side,setSide]=useState<Side>('remote')
  const [splitRatio,setSplitRatio]=useState(()=>readSftpSplitRatio())
  const [liveRatio,setLiveRatio]=useState<number|null>(null)
  const [dividerActive,setDividerActive]=useState(false)
  const [dividerHovered,setDividerHovered]=useState(false)
  const drag=useRef<{pointerId:number;startX:number;startRatio:number;width:number;latestRatio:number}|null>(null)
  const frame=useRef(0)
  const [taskSource] = useState(() => {
    let tasks: FileTask[] = []
    const listeners = new Set<() => void>()
    return {
      read: () => tasks,
      subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
      publish(next: FileTask[]) { tasks = next; listeners.forEach(listener => listener()) },
    }
  })
  const [error,setError]=useState('')
  const [localRevision,setLocalRevision]=useState(0), [taskRevision,setTaskRevision]=useState(0)
  const [status,setStatus]=useState<Partial<Record<Side,{summary:string;error:string}>>>({})
  const completed=useRef('')
  const wide=workspaceWidth>=704
  const effectiveRatio=clampSftpSplitRatio(liveRatio??splitRatio,workspaceWidth)
  const localChanged=useCallback(()=>setLocalRevision(v=>v+1),[])
  const updateStatus=useCallback((column:Side,summary:string,error:string)=>
    setStatus(previous=>previous[column]?.summary===summary&&previous[column]?.error===error?previous:{...previous,[column]:{summary,error}}),[])
  useEffect(()=>{
    const el=root.current;if(!el)return
    const observer=new ResizeObserver(entries=>{
      const width=Math.round(entries[0]?.contentRect.width??0)
      setWorkspaceWidth(prev=>prev===width?prev:width)
    })
    observer.observe(el)
    return()=>observer.disconnect()
  },[])
  useEffect(()=>()=>cancelAnimationFrame(frame.current),[])
  useEffect(()=>{if(!active||!gateway.available())return;return gateway.watch(next=>{
    taskSource.publish(next)
    const signature=next.filter(task=>['completed','failed','cancelled'].includes(task.state)).map(task=>task.id+':'+task.state).sort().join(',')
    if(signature!==completed.current){completed.current=signature;setTaskRevision(v=>v+1)}
  },()=>setError(tr("任务状态连接已结束，请重新打开文件工作区。")))},[active,gateway,taskSource])

  const commitRatio=(next:number)=>{
    const clamped=clampSftpSplitRatio(next,workspaceWidth)
    setLiveRatio(null)
    setSplitRatio(clamped)
    writeSftpSplitRatio(clamped)
  }
  const finishDrag=(event:PointerEvent<HTMLElement>,commit:boolean)=>{
    const state=drag.current
    if(!state||state.pointerId!==event.pointerId)return
    drag.current=null
    setDividerActive(false)
    cancelAnimationFrame(frame.current)
    frame.current=0
    if(event.currentTarget.hasPointerCapture(event.pointerId))event.currentTarget.releasePointerCapture(event.pointerId)
    if(commit)commitRatio(state.latestRatio)
    else setLiveRatio(null)
  }
  const onDividerPointerDown=(event:PointerEvent<HTMLElement>)=>{
    if(event.button!==0||!workspaceWidth)return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    drag.current={pointerId:event.pointerId,startX:event.clientX,startRatio:effectiveRatio,width:workspaceWidth,latestRatio:effectiveRatio}
    setDividerActive(true)
  }
  const onDividerPointerMove=(event:PointerEvent<HTMLElement>)=>{
    const state=drag.current
    if(!state||state.pointerId!==event.pointerId||state.width<=0)return
    state.latestRatio=clampSftpSplitRatio(state.startRatio+(event.clientX-state.startX)/state.width,state.width)
    if(!frame.current){
      frame.current=requestAnimationFrame(()=>{
        frame.current=0
        if(drag.current)setLiveRatio(drag.current.latestRatio)
      })
    }
  }
  const onDividerKeyDown=(event:KeyboardEvent<HTMLElement>)=>{
    if(event.key==='ArrowLeft'||event.key==='ArrowRight'){
      event.preventDefault()
      const step=event.shiftKey?0.01:0.03
      commitRatio(effectiveRatio+(event.key==='ArrowRight'?step:-step))
    }else if(event.key==='Home'){
      event.preventDefault()
      commitRatio(0.5)
    }
  }

  const columns:Side[]=wide?['local','remote']:[side]
  const paneTrack=wide?`minmax(0, ${effectiveRatio}fr) var(--border-width) minmax(0, ${1-effectiveRatio}fr)`:'minmax(0,1fr)'
  const footerTrack=wide?`minmax(0, ${effectiveRatio}fr) minmax(0, ${1-effectiveRatio}fr)`:'minmax(0,1fr)'

  return <Stack ref={root} role="region" aria-label={tr("双栏文件管理")} hidden={!active} inert={!active}
    style={{display:active?'flex':'none',height:'100%',minWidth:0,minHeight:0}}>
    {!wide&&<Stack direction="horizontal" justify="end" paddingInline={2} paddingBlock={1}>
      <SegmentedControl label={tr("当前文件栏")} size="sm" value={side} onChange={value=>setSide(value==='local'?'local':'remote')}>
        <SegmentedControlItem value="local" label={tr("本地")}/><SegmentedControlItem value="remote" label={tr("远程")}/>
      </SegmentedControl>
    </Stack>}
    <Stack style={{display:'grid',gridTemplateColumns:paneTrack,flex:1,minHeight:0,minWidth:0}}>
      <Stack key="local" hidden={!(wide||side==='local')} inert={!(wide||side==='local')}
        style={{display:wide||side==='local'?'flex':'none',minWidth:0,minHeight:0}}>
        <SftpPanel {...props} gateway={gateway} active={active&&(wide||side==='local')} view="local" onStatus={updateStatus}
          taskSource={taskSource} localRevision={localRevision} onLocalChanged={localChanged} reloadVersion={reloadVersion+taskRevision}/>
      </Stack>
      {wide&&<Stack
        role="separator"
        aria-orientation="vertical"
        aria-label={tr("调整本地与远程栏宽度")}
        aria-valuemin={25}
        aria-valuemax={75}
        aria-valuenow={Math.round(effectiveRatio*100)}
        tabIndex={0}
        onPointerDown={onDividerPointerDown}
        onPointerMove={onDividerPointerMove}
        onPointerUp={event=>finishDrag(event,true)}
        onPointerCancel={event=>finishDrag(event,false)}
        onPointerEnter={()=>setDividerHovered(true)}
        onPointerLeave={()=>setDividerHovered(false)}
        onDoubleClick={()=>commitRatio(0.5)}
        onKeyDown={onDividerKeyDown}
        style={{
          position:'relative',
          width:'var(--border-width)',
          height:'100%',
          background:dividerActive||dividerHovered?'var(--color-border-emphasized)':'var(--color-border)',
          cursor:'col-resize',
          touchAction:'none',
          userSelect:'none',
          zIndex:3,
        }}>
        <Stack aria-hidden="true" style={{position:'absolute',insetBlock:0,insetInline:'calc(-1 * var(--spacing-1-5))',cursor:'col-resize'}}/>
      </Stack>}
      <Stack key="remote" hidden={!(wide||side==='remote')} inert={!(wide||side==='remote')}
        style={{display:wide||side==='remote'?'flex':'none',minWidth:0,minHeight:0}}>
        <SftpPanel {...props} gateway={gateway} active={active&&(wide||side==='remote')} view="remote" onStatus={updateStatus}
          taskSource={taskSource} localRevision={localRevision} onLocalChanged={localChanged} reloadVersion={reloadVersion+taskRevision}/>
      </Stack>
    </Stack>
    {tasksOpen&&<Stack id={id+'-queue'} role="region" aria-label={tr("传输任务")} padding={3}
      style={{minHeight:0,maxHeight:'38%',overflow:'auto',borderBlockStart:'var(--border-width) solid var(--color-border)'}}
      onKeyDown={event=>{if(event.key==='Escape'&&!event.defaultPrevented){setTasksOpen(false);requestAnimationFrame(()=>document.getElementById(id+'-tasks')?.focus())}}}>
      <WorkspaceTransferTasks session={props.session} onCollapse={()=>{setTasksOpen(false);requestAnimationFrame(()=>document.getElementById(id+'-tasks')?.focus())}} source={taskSource} gateway={gateway} onError={setError} onChooseLocal={async()=>{
        const chosen=await gateway.selectLocal();if(chosen){rememberLocalFolder(chosen);localChanged()};return chosen
      }}/>
    </Stack>}
    <Stack as="footer" paddingInline={2} gap={1} style={{flexShrink:0,borderBlockStart:'var(--border-width) solid var(--color-border)'}}>
      {(error||columns.some(column=>status[column]?.error))&&<Text type="supporting" role="alert">
        {localizeMessage(error)||columns.map(column=>status[column]?.error).filter(Boolean).join(' · ')}
      </Text>}
      <Stack style={{display:'grid',gridTemplateColumns:footerTrack,minWidth:0}}>
        {columns.map((column,index)=><Stack key={column} direction="horizontal" align="center" justify="between" gap={2}
          style={{minWidth:0,paddingInlineStart:index?'var(--spacing-2)':undefined}}>
          <Text type="supporting" color="secondary" maxLines={1} role="status">{status[column]?.summary}</Text>
          {index===columns.length-1&&<WorkspaceTaskButton source={taskSource} id={id} open={tasksOpen}
            toggle={()=>setTasksOpen(value=>!value)}/>}
        </Stack>)}
      </Stack>
    </Stack>
  </Stack>
}

function WorkspaceTaskButton({source,id,open,toggle}:{
  source: TaskSource
  id: string
  open: boolean
  toggle: () => void
}) {
  const readCount = () => source.read().length
  const count = useSyncExternalStore(source.subscribe, readCount, readCount)
  const readFailed=()=>source.read().filter(task=>task.state==='failed').length
  const failed=useSyncExternalStore(source.subscribe,readFailed,readFailed)
  return <Button id={id+'-tasks'} label={failed?tr("传输任务 {p0} · {p1} 失败",count,failed):count?tr("传输任务 {p0}",count):tr("传输任务")} size="sm" variant={open?'secondary':'ghost'}
    aria-expanded={open} aria-controls={open?id+'-queue':undefined} style={{flexShrink:0}} onClick={toggle}/>
}

function WorkspaceTransferTasks({source,...props}:Omit<ComponentProps<typeof TransferTasks>,'tasks'>&{
  source: TaskSource
}) {
  const tasks = useSyncExternalStore(source.subscribe, source.read, source.read)
  return <TransferTasks {...props} tasks={tasks}/>
}
