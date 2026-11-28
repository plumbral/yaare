import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { writeClipboardText } from '../../shared/bridge/application'
import { WrapTextIcon } from '../../shared/icons/WrapTextIcon'
import { findShortcut, shortcutAria, shortcutTooltip, useShortcutOverrides } from './shortcutReference'
import {
  memo,
  lazy,Suspense,
  useCallback,
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type DragEvent,
} from 'react'
import { Layout, LayoutHeader, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { EmptyState } from '@astryxdesign/core/EmptyState'
import { Spinner } from '@astryxdesign/core/Spinner'
import { DialogPresence } from '../../shared/ui/AppDialog'
import type { DropdownMenuOption } from '@astryxdesign/core/DropdownMenu'
import SftpFileTree from './SftpFileTree'
import FilePropertiesDialog from './FilePropertiesDialog'
import DirectoryBreadcrumbs from './DirectoryBreadcrumbs'
import {fileDragType,parseFileDrag,selectionGroups,summarizeRootDirectory,summarizeSelection,treeMemory,joinPath,type FileDragGroup,type FileNode} from './fileTreeModel'
import {
  defaultDirectionForFileSort,
  readSideViewPreferences,
  writeSideViewPreferences,
  type FileSideViewPreferences,
  type FileSortKey,
} from './fileColumns'
import { TabList, Tab } from '@astryxdesign/core/TabList'
import BrowserFileImport from '../../shared/ui/BrowserFileImport'
import TransferTasks from './TransferTasks'


import { DropdownMenu,DropdownMenuItem,DropdownMenuDivider,DropdownMenuCheckboxItem,DropdownMenuSubMenu,DropdownMenuRadioGroup,DropdownMenuRadioItem } from '@astryxdesign/core/DropdownMenu'
import {
  ArrowUpIcon,
  ArrowLeftIcon,ArrowRightIcon,
  ArrowPathIcon,
  EllipsisHorizontalIcon,
  EyeIcon,
  MagnifyingGlassIcon,
  ServerStackIcon,
  LinkIcon,
} from '@heroicons/react/24/outline'

import EditorDialog from '../../shared/ui/EditorDialog'
import {directoryStep} from './directoryHistory'
import {useSftpDirectory} from './useSftpDirectory'
import {isMyGo,onFileDrop} from 'mygo-runtime'
import {matchNativeDrop,type DropDestination} from './nativeDrop'
const FileSearchDialog=lazy(()=>import('./FileSearchDialog'))
const FileCompareDialog=lazy(()=>import('./FileCompareDialog'))
const FileSyncDialog=lazy(()=>import('./FileSyncDialog'))
const RemoteCopyDialog=lazy(()=>import('./RemoteCopyDialog'))
import ConfirmDelete from '../../shared/ui/ConfirmDelete'
import {
  fileGateway,
  type FileEntry,
  type FileLocation,
  type FileTask,
  type LocalFolder,
} from '../../shared/bridge/files'
import type { Session } from './mockGateway'
import { connectionLabels } from './sessionConnection'
import { resolveSessionConnectionPrompt } from './sessionConnectionPrompt'
import ConnectionPromptActions from './ConnectionPromptActions'
import type { ReconnectWait } from '../connections/sshReconnect'
import { transferSize as fileSize } from './transferModel'

const emptyEntries: FileEntry[] = []
const emptyTasks: FileTask[] = []
const readEmptyTasks = () => emptyTasks
const subscribeEmptyTasks = () => () => {}
export type TaskSource = { read: () => FileTask[]; subscribe: (listener: () => void) => () => void }

const finished = (state: string) => ['completed', 'cancelled', 'failed'].includes(state)

const parentPath = (value: string) => value.slice(0, value.lastIndexOf('/')) || '/'
type Action = 'create' | 'mkdir' | 'rename' | 'copy' | 'move' | 'chmod'
let rememberedLocalFolder:LocalFolder|null=null
let rememberedLocalPath='/'
let homeRequest:Promise<LocalFolder|null>|undefined
export function rememberLocalFolder(folder:LocalFolder){rememberedLocalFolder=folder;rememberedLocalPath='/'}

export default memo(function SftpPanel({
  session,
  live,
  path,
  onPath,
  gateway = fileGateway,
  onEdit,
  reloadVersion=0,
  active=true,
  directorySync=false,
  onConnect,
  onCancelConnection,
  onDirectorySync,
  onEnterDirectory,
  onShellIntegration,
  onComparisonText,
  view,onStatus,taskSource,localRevision=0,onLocalChanged,onFullWorkspace,
  onFileGuard,onProtection,sessionLabel,
}: {
  session?: Session
  sessionLabel?:(id:string)=>string
  live?: { attempt: number; phase: string; message?: string; retry?: ReconnectWait; automatic?: boolean; directory?: {path:string;ready:boolean;integrated?:boolean} }
  path: string
  onPath: (path: string) => void
  gateway?: typeof fileGateway
  onEdit?: (path:string)=>void
  reloadVersion?:number
  active?:boolean
  directorySync?:boolean
  onConnect?:()=>void
  onCancelConnection?:()=>void
  onDirectorySync?:(enabled:boolean)=>void
  onEnterDirectory?:(path:string)=>Promise<void>
  onShellIntegration?:()=>void
  onComparisonText?:(id:string,relative:string)=>void
  view?:'remote'|'local';onStatus?:(side:'remote'|'local',summary:string,error:string)=>void;taskSource?:TaskSource
  localRevision?:number;onLocalChanged?:()=>void;onFullWorkspace?:()=>void
  onFileGuard?:import('./fileCloseGuard').RegisterFileGuard
  onProtection?:(key:string,value:boolean)=>void
}) {
  useLocale()
  const shortcuts = useShortcutOverrides()

  const panelId = useId()
  const [internalTab, setInternalTab] = useState('remote'),
    [folder, setFolder] = useState<LocalFolder | null>(rememberedLocalFolder),
    [localPath, setLocalPath] = useState(rememberedLocalPath)
  const tab=view??internalTab
  const setTab=useCallback((next:string)=>{if(!view)setInternalTab(next)},[view])
  const [selectionState,setSelectionState]=useState<{key:string;items:FileNode[]}>({key:'',items:[]}),[searchOpen,setSearchOpen]=useState(false),[properties,setProperties]=useState<FileNode|null>(null)
  const operationItems=useRef<FileNode[]>([]),actionBase=useRef('')
  const [query, setQuery] = useState('')
  const [prefsBySide, setPrefsBySide] = useState<Record<'local' | 'remote', FileSideViewPreferences>>(() => ({
    local: readSideViewPreferences('local'),
    remote: readSideViewPreferences('remote'),
  }))
  const side: 'local' | 'remote' = tab === 'local' ? 'local' : 'remote'
  const viewPrefs = prefsBySide[side]
  const { wrapNames, showHidden, showPermissions, showOwner, sort, columnWidths } = viewPrefs
  const hasCustomWidths = Object.keys(columnWidths).length > 0
  const updateViewPrefs = useCallback(
    (patch: Partial<FileSideViewPreferences> | ((previous: FileSideViewPreferences) => FileSideViewPreferences)) => {
      setPrefsBySide(previous => {
        const current = previous[side]
        const next = typeof patch === 'function' ? patch(current) : { ...current, ...patch }
        writeSideViewPreferences(side, next)
        return { ...previous, [side]: next }
      })
    },
    [side],
  )
  const [message, setMessage] = useState(''),
    [error, setError] = useState('')
  const [ownTasks, setTasks] = useState<FileTask[]>([]),
    [action, setAction] = useState<Action | null>(null),
    [editValue, setEditValue] = useState(''),
    [editDestination, setEditDestination] = useState(''),
    [saving, setSaving] = useState(false),
    [deleteOpen, setDeleteOpen] = useState(false)
  const externalTasks = useSyncExternalStore(
    tab === 'tasks' && active ? taskSource?.subscribe ?? subscribeEmptyTasks : subscribeEmptyTasks,
    tab === 'tasks' && active ? taskSource?.read ?? readEmptyTasks : readEmptyTasks,
    readEmptyTasks,
  )
  const tasks = taskSource ? externalTasks : ownTasks
  const busyAction = useRef(false),
    completed = useRef<string | null>(null)
  const deferredQuery = useDeferredValue(query.trim().toLocaleLowerCase())
  const linkedPath=directorySync&&tab==='remote'?live?.directory?.path??'':''
  const connected = !!session && live?.phase === 'connected',
    native = gateway.available()
  const currentPath = side === 'local' ? localPath : path
  const [searchContext,setSearchContext]=useState<{location:FileLocation;scope:string}|null>(null),[recursiveSearchOpen,setRecursiveSearchOpen]=useState(false)
  const [compareContext,setCompareContext]=useState<{remote:FileLocation;scope:string}|null>(null),[compareOpen,setCompareOpen]=useState(false)
  const [syncContext,setSyncContext]=useState<FileLocation|null>(null),[syncOpen,setSyncOpen]=useState(false),[syncRunning,setSyncRunning]=useState(false)
  const [syncGeneration,setSyncGeneration]=useState(0)
  const [remoteCopySources,setRemoteCopySources]=useState<import('./RemoteCopyDialog').RemoteCopySource[]>([]),[remoteCopyOpen,setRemoteCopyOpen]=useState(false)
  const nativeDestination=useRef<DropDestination|null>(null),[dropHint,setDropHint]=useState('')
  const location = useCallback(
    (targetSide: string, p?: string, root = folder): FileLocation => ({
      side: targetSide,
      token: root?.token || '',
      path: p ?? (targetSide === 'local' ? localPath : path),
      sessionId: session?.id || '',
      attempt: live?.attempt || 0,
    }),
    [folder, localPath, path, session?.id, live?.attempt],
  )
  const onLocalDirectory = useCallback((next: string, root?: LocalFolder) => {
    rememberedLocalPath = next
    setLocalPath(next)
    if (root) {
      rememberedLocalFolder = root
      setFolder(root)
    }
    onLocalChanged?.()
  }, [onLocalChanged])
  const {
    treeScope, scopeNow, mounted, currentHistory, browseHistory, load, loadRef, loaded, busy, pathDraft, setPathDraft,
  } = useSftpDirectory({
    active, tab, native, connected, side, sessionId: session?.id, attempt: live?.attempt, phase: live?.phase,
    workspaceView: !!view, folder, currentPath, reloadVersion, directorySync, linkedPath,
    gateway, location, onPath, onLocalDirectory, onMessage: setMessage, onError: setError,
  })
  const loadedScope = loaded?.scope ?? ''
  const loadedRoot = loaded?.path ?? ''
  const entries = loaded?.entries ?? emptyEntries
  const actionScope = useRef(treeScope)
  const canBrowse = native && (side === 'local' ? !!folder : connected)
  const remotePrompt = side === 'remote' && native && !canBrowse
    ? resolveSessionConnectionPrompt(session?.connection?.status, live, onConnect, onCancelConnection)
    : undefined
  const remoteStatus = native ? remotePrompt?.label ?? connectionLabels.connected : tr("桌面服务暂不可用")
  const remoteDetail = remotePrompt?.message || (!remotePrompt?.busy ? tr("连接 SSH 后即可浏览远程目录。") : undefined)
  useEffect(()=>{
    if(!native||!isMyGo())return
    return onFileDrop(event=>{
      const target=matchNativeDrop(event,nativeDestination.current,Date.now());nativeDestination.current=null
      if(!target)return
      setDropHint('');setError('')
      void gateway.uploadDropped(target.token,target.location).then(()=>{if(mounted.current){setMessage(tr("拖入文件已加入上传队列，目标：{p0}", target.location.path));setTab('tasks')}}).catch(cause=>{if(mounted.current)setError(cause instanceof Error?cause.message:tr("拖入上传未完成，请重试。"))})
    })
  },[native,gateway])
  const selectionKey=treeScope+':'+loadedRoot
  const selectedItems=useMemo(()=>canBrowse&&loadedScope===treeScope&&selectionState.key===selectionKey?selectionState.items:[],[canBrowse,loadedScope,treeScope,selectionKey,selectionState, getLanguage()])
  const selected=useMemo(()=>new Set(selectedItems.map(item=>item.path)),[selectedItems, getLanguage()])
  const receiveSelection=useCallback((items:FileNode[])=>setSelectionState({key:selectionKey,items}),[selectionKey])
  useEffect(() => {
    setQuery('')
    setPathDraft(side==='local'?localPath:path)
  }, [side, session?.id])
  useEffect(() => {
    if (!native || !active || taskSource !== undefined) return
    return gateway.watch(
      (next) => {
        if (!mounted.current) return
        setTasks(next)
        const signature = next
          .filter((t) => finished(t.state))
          .map((t) => t.id + ':' + t.state)
          .sort()
          .join(',')
        if (completed.current !== null && signature !== completed.current) void loadRef.current()
        completed.current = signature
      },
      () => setError(tr("任务状态连接已结束，请重新打开文件面板。")),
    )
  }, [native, gateway,active,taskSource!==undefined])
  useEffect(()=>{if(active){setFolder(rememberedLocalFolder);setLocalPath(rememberedLocalPath)}},[active,localRevision])
  useEffect(()=>{
    if(!active||side!=='local'||!native||rememberedLocalFolder)return
    let cancelled=false
    homeRequest??=gateway.openLocalHome().finally(()=>{homeRequest=undefined})
    void homeRequest.then(home=>{
      if(!home)return
      if(!rememberedLocalFolder)rememberLocalFolder(home)
      if(!cancelled){setFolder(rememberedLocalFolder);setLocalPath(rememberedLocalPath);onLocalChanged?.()}
    }).catch(cause=>{if(!cancelled)setError(cause instanceof Error?cause.message:tr("无法打开本机用户目录。"))})
    return()=>{cancelled=true}
  },[active,side,native,gateway,onLocalChanged])
  const changeTab = (next: string) => {
    setTab(next)
    setError('')
  }
  const chooseLocal = async () => {
    if (busyAction.current) return null
    busyAction.current = true
    setError('')
    try {
      const chosen = await gateway.selectLocal()
      if (chosen) {
        rememberedLocalFolder=chosen;rememberedLocalPath='/'
        setFolder(chosen)
        setLocalPath('/')
        setPathDraft('/')
        onLocalChanged?.()
      }
      return chosen
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : tr("目录未选择。"))
      return null
    } finally {
      busyAction.current = false
    }
  }
  const single=selectedItems.length===1?selectedItems[0]:undefined
  const groupsFor=(nodes:FileNode[])=>selectionGroups(nodes).map(group=>({source:location(side,group.path),names:group.names}))
  async function enqueueGroups(groups:FileDragGroup[],targetSide:string,targetPath?:string,protocol?:'scp'){
    if(!connected||busyAction.current)return
    let root=folder;if(targetSide==='local'&&!root){root=await chooseLocal();if(!root)return}
    busyAction.current=true;let queued=0
    try{for(const group of groups){await gateway.transfer({...group,destination:location(targetSide,targetPath,root),policy:'ask',protocol});queued++};setMessage(tr("已加入 {p0} 组文件任务。", queued));setTab('tasks')}
    catch(cause){setError((queued?tr("已加入 {p0} 组，后续任务未添加。", queued):'')+(cause instanceof Error?cause.message:tr("任务未添加，请重试。")))}
    finally{busyAction.current=false}
  }
  const dragStart = (event: DragEvent, nodes: FileNode[]) => {
    event.dataTransfer.effectAllowed = 'copy'
    event.dataTransfer.setData(fileDragType, JSON.stringify({ groups: groupsFor(nodes) }))
  }
  const drop = (event: DragEvent, targetSide: string, path?: string) => {
    if (!event.dataTransfer.types.includes(fileDragType)) return
    event.preventDefault()
    event.stopPropagation()
    const groups = parseFileDrag(event.dataTransfer.getData(fileDragType))
    if (groups) void enqueueGroups(groups, targetSide, path)
    else setError(tr("拖放数据无效，请重新选择文件。"))
  }
  const readDirectory=useCallback(async(p:string)=>(await gateway.list(location(side,p))).entries,[gateway,location,side])
  const beginAction=(kind:Action)=>{
    actionScope.current=treeScope;operationItems.current=[...selectedItems];actionBase.current=single?((kind==='mkdir'||kind==='create')&&single.entry.directory&&!single.entry.link?single.path:single.parent):(loadedScope===treeScope?loadedRoot:currentPath)
    const value=kind==='rename'||kind==='move'?single?.entry.name??'':kind==='chmod'?single?.entry.permissions??'644':''
    setAction(kind);setError('');setEditValue(value);setEditDestination(currentPath)
  }
  const beginDelete=()=>{operationItems.current=[...selectedItems];document.getElementById(panelId+'-path')?.focus({preventScroll:true});setError('');setDeleteOpen(true)}
  async function saveAction(openCreated=false){
    if(!action||busyAction.current)return;busyAction.current=true;setSaving(true);setError('')
    let completedGroups=0
    try{
      const nodes=operationItems.current,groups=selectionGroups(nodes)
      if(actionScope.current!==treeScope)throw Error(tr("连接或目录授权已变化，请重新打开文件操作。"))
      if(action==='create'){
        const parent=actionBase.current,name=editValue.trim(),created=joinPath(parent,name),scope=treeScope
        await gateway.operate({location:location(side,parent),kind:'create',names:[],name,destination:'',permissions:'',confirmed:false})
        setAction(null)
        if(scopeNow.current!==scope){setMessage(tr("文件已创建；连接已变化，请刷新后查看。"));return}
        const memory=treeMemory(scope+':'+parent);memory.selected=new Set([created]);memory.anchor=created;memory.focused=created
        setQuery('');if(name.startsWith('.'))updateViewPrefs({showHidden:true});const refreshed=await load(parent);setMessage(refreshed?tr("空文件已创建。"):tr("空文件已创建，目录未刷新。"))
        if(openCreated&&scopeNow.current===scope)onEdit?.(created)
        return
      }
      if(action==='copy'){for(const group of groups){await gateway.transfer({source:location(side,group.path),destination:location(side,editDestination),names:group.names,policy:'ask'});completedGroups++};setTab('tasks')}
      else if(action==='mkdir'||action==='rename'||action==='move')await gateway.operate({location:location(side,actionBase.current),kind:action,names:action==='mkdir'?[]:nodes.map(n=>n.entry.name),name:editValue.trim(),destination:editDestination,permissions:'',confirmed:false})
      else for(const group of groups){await gateway.operate({location:location(side,group.path),kind:action,names:group.names,name:'',destination:'',permissions:editValue,confirmed:false});completedGroups++}
      setAction(null);setMessage(action==='copy'?tr("复制任务已加入队列。"):tr("文件操作已完成。"));if(action!=='copy')await load()
    }catch(cause){setError((completedGroups?tr("已有 {p0} 组操作完成，后续操作未完成。", completedGroups):'')+(cause instanceof Error?cause.message:tr("请刷新确认后重试。")))}
    finally{setSaving(false);busyAction.current=false}
  }
  async function deleteFiles(){
    if(busyAction.current)return;busyAction.current=true;setSaving(true);setError('')
    let deletedGroups=0
    try{for(const group of selectionGroups(operationItems.current)){await gateway.operate({location:location(side,group.path),kind:'delete',names:group.names,name:'',destination:'',permissions:'',confirmed:true});deletedGroups++};setDeleteOpen(false);setMessage(tr("所选文件已删除。"));await load()}
    catch(cause){setError((deletedGroups?tr("已有 {p0} 组删除完成，后续删除未完成。", deletedGroups):'')+(cause instanceof Error?cause.message:tr("请刷新确认后重试。")))}
    finally{setSaving(false);busyAction.current=false}
  }

  const rootSummary = useMemo(
    () => (loadedScope === treeScope ? summarizeRootDirectory(entries, showHidden) : { total: 0, visible: 0, hidden: 0 }),
    [loadedScope, treeScope, entries, showHidden],
  )
  const selectionSummary = useMemo(() => summarizeSelection(selectedItems), [selectedItems])
  const countText = !showHidden && rootSummary.hidden > 0
    ? tr("{p0} 项（已隐藏 {p1} 项）", rootSummary.visible, rootSummary.hidden)
    : tr("{p0} 项", rootSummary.visible)
  const selectionText = selectionSummary.count === 0
    ? ''
    : selectionSummary.fileCount > 0
      ? tr("已选 {p0} 项（{p1}）", selectionSummary.count, fileSize(selectionSummary.totalBytes))
      : tr("已选 {p0} 项", selectionSummary.count)
  const copyClipboard = useCallback(async (text: string, doneMessage: string) => {
    try {
      await writeClipboardText(text)
      if (mounted.current) setMessage(doneMessage)
    } catch {
      if (mounted.current) setError(tr("复制失败，请重试。"))
    }
  }, [])

  const activeTasks = tasks.filter((t) => !finished(t.state)).length
  const summary=dropHint?tr("释放以上传到 {p0}",dropHint)
    :tab==='tasks'?localizeMessage(message)||tr("{p0} 个任务",tasks.length)
    :!canBrowse?(side==='local'?tr("尚未选择本地目录"):'')
    :[countText,selectionText,localizeMessage(message)].filter(Boolean).join(' · ')
  useEffect(()=>{if(active)onStatus?.(side,summary,localizeMessage(error))},[active,onStatus,side,summary,error])
  const actionNames: Record<Action, string> = {
    create:tr("新建文件"),
    mkdir: tr("新建文件夹"),
    rename: tr("重命名"),
    copy: tr("复制文件"),
    move: tr("移动文件"),
    chmod: tr("修改权限"),
  }
  const fileActions = {
    open: {
      label: single?.entry.directory ? tr("在此目录浏览") : tr("打开文本"),
      isDisabled: !single || single.entry.link || (!single.entry.directory && (side !== 'remote' || !onEdit)),
      onClick: () => {
        if (!single) return
        if (single.entry.directory) void load(single.path)
        else onEdit?.(single.path)
      },
    },
    transfer: {
      label: side === 'remote' ? tr("下载所选") : tr("上传所选"),
      isDisabled: !connected || !selected.size,
      onClick: () => void enqueueGroups(groupsFor(selectedItems), side === 'remote' ? 'local' : 'remote'),
    },
    copyPath: {
      label: tr("复制路径"),
      isDisabled: !selectedItems.length && !(canBrowse && loadedScope === treeScope),
      onClick: () => void copyClipboard(selectedItems.length ? selectedItems.map(item => item.path).join('\n') : loadedRoot, tr("已复制路径")),
    },
    createFile: {
      label: tr("新建文件…"),
      isDisabled: !canBrowse || side !== 'remote' || busy || !loadedRoot || loadedScope !== treeScope,
      onClick: () => beginAction('create'),
    },
    createFolder: {label: tr("新建文件夹…"), isDisabled: !canBrowse, onClick: () => beginAction('mkdir')},
    rename: {label: tr("重命名…"), isDisabled: !single, onClick: () => beginAction('rename')},
    properties: {label: tr("属性"), isDisabled: !single, onClick: () => setProperties(single ?? null)},
    remove: {label: tr("删除所选…"), variant: 'destructive', isDisabled: !selected.size, onClick: beginDelete},
  } satisfies Record<string, DropdownMenuOption>
  const menuItems:DropdownMenuOption[]=[
    fileActions.open,
    fileActions.transfer,
    {label:side==='remote'?tr("使用 SCP 下载所选"):tr("使用 SCP 上传所选"),isDisabled:!connected||!selected.size,onClick:()=>void enqueueGroups(groupsFor(selectedItems),side==='remote'?'local':'remote',undefined,'scp')},
    ...(side==='remote'?[{label:tr("复制到另一台服务器…"),isDisabled:!connected||!selected.size,onClick:()=>{setRemoteCopySources(groupsFor(selectedItems));setRemoteCopyOpen(true)}}]:[]),
    ...(side==='remote'&&onEnterDirectory?[{label:tr("在终端进入此目录"),isDisabled:!connected||!!live?.directory?.integrated&&!live.directory.ready||selectedItems.length>1,onClick:()=>{const target=single?(single.entry.directory&&!single.entry.link?single.path:single.parent):path;void onEnterDirectory(target).catch(cause=>setError(cause instanceof Error?cause.message:tr("终端目录切换失败，请重试。")))}}]:[]),
    {type:'divider'},
    {label:tr("复制文件名"),isDisabled:!selectedItems.length,onClick:()=>void copyClipboard(selectedItems.map(item=>item.entry.name).join('\n'),tr("已复制文件名"))},
    fileActions.copyPath,
    {type:'divider'},
    fileActions.createFile,
    fileActions.createFolder,
    fileActions.rename,
    {label:tr("复制到…"),isDisabled:!selected.size,onClick:()=>beginAction('copy')},
    {label:tr("移动到…"),isDisabled:!single,onClick:()=>beginAction('move')},
    {label:tr("修改权限…"),isDisabled:!selected.size||side!=='remote',onClick:()=>beginAction('chmod')},
    fileActions.properties,
    {type:'divider'},
    fileActions.remove,
  ]
  const contextItems: DropdownMenuOption[] = selected.size ? [
    ...(!fileActions.open.isDisabled ? [fileActions.open] : []),
    fileActions.transfer,
    fileActions.copyPath,
    ...(single ? [fileActions.rename, fileActions.properties] : []),
    {type: 'divider'},
    fileActions.remove,
  ] : [
    ...(side === 'remote' ? [fileActions.createFile] : []),
    fileActions.createFolder,
    fileActions.copyPath,
  ]
  return (
    <>
      <Layout
        height="fill"
        onKeyDownCapture={event=>{if(!event.currentTarget.contains(event.target as Node))return;const action=findShortcut(event.nativeEvent,['file-back','file-forward']);if(action){event.preventDefault();event.stopPropagation();if(!event.repeat)browseHistory(action==='file-back'?-1:1)}}}
        header={<LayoutHeader padding={0}><Stack paddingInline={2} paddingBlock={1} gap={1}>
          <Stack direction="horizontal" align="center" gap={1}>
            <StackItem size="fill" style={{minWidth:0}}>{view?(view==='local'?<Button label={folder?tr("本地 · {p0}", folder.name):tr("选择本地目录")} variant="ghost" size="md" isDisabled={!native} onClick={()=>void chooseLocal()}/>:<Text type="label" maxLines={1}>{session ? tr("远程目录 · {p0}", sessionLabel?.(session.id) ?? session.host.name) : tr("远程服务器")}</Text>):<TabList role="tablist" aria-label={tr("文件位置与任务")} value={tab} onChange={changeTab} size="md">
              <Tab id={panelId+'-tab-remote'} value="remote" label={tr("远程")} panelId={panelId+'-browser'} onDragOver={e=>{if(e.dataTransfer.types.includes(fileDragType))e.preventDefault()}} onDrop={e=>drop(e,'remote')}/>
              <Tab id={panelId+'-tab-local'} value="local" label={import.meta.env.MODE==='browser'?tr('浏览器文件'):tr('本地')} panelId={panelId+'-browser'} onDragOver={e=>{if(e.dataTransfer.types.includes(fileDragType))e.preventDefault()}} onDrop={e=>drop(e,'local')}/>
              <Tab id={panelId+'-tab-tasks'} value="tasks" label={activeTasks?tr("任务 {p0}", activeTasks):tr("任务")} panelId={panelId+'-tasks'}/>
            </TabList>}</StackItem>
            <BrowserFileImport hidden={side!=='local'||tab==='tasks'} sessionId={session?.id??''} onFileGuard={onFileGuard} onProtection={onProtection} onError={setError} onImported={next=>{rememberLocalFolder(next);setFolder(next);setLocalPath('/');onLocalChanged?.();if(side==='local'&&tab!=='tasks')void loadRef.current('/',{root:next})}}/>
          {tab!=='tasks'&&<>
              <IconButton label={tr("显示隐藏文件")} tooltip={tr("显示隐藏文件")} aria-pressed={showHidden} size="md" variant={showHidden?'secondary':'ghost'} icon={<Icon icon={EyeIcon} size="sm"/>} onClick={()=>updateViewPrefs(previous=>({...previous,showHidden:!previous.showHidden}))}/>
              <IconButton label={tr("搜索文件")} tooltip={tr("筛选已加载的目录")} aria-expanded={searchOpen} size="md" variant={searchOpen?'secondary':'ghost'} icon={<Icon icon={MagnifyingGlassIcon} size="sm"/>} onClick={()=>{setSearchOpen(!searchOpen);if(searchOpen)setQuery('');else requestAnimationFrame(()=>document.getElementById(panelId+'-search')?.focus())}}/>
              <DropdownMenu presentation="popover" alignment="end" renderTrigger={props=><IconButton {...props} label={tr("文件与显示选项")} tooltip={tr("文件操作与显示选项")} size="md" variant="ghost" icon={<Icon icon={EllipsisHorizontalIcon} size="sm"/>}/> }>
                <DropdownMenuSubMenu label={tr("文件操作")}>
                  {menuItems.map((item,index)=>'type' in item?<DropdownMenuDivider key={index}/>:<DropdownMenuItem key={index} {...item}/>)}
                </DropdownMenuSubMenu>
                <DropdownMenuDivider/>
                {onFullWorkspace&&<DropdownMenuItem label={tr("打开双栏文件管理")} onClick={onFullWorkspace}/>}
                <DropdownMenuItem label={tr("在目录中搜索文件名…")} isDisabled={!canBrowse||loadedScope!==treeScope} onClick={()=>{setSearchContext({location:location(side,single?.entry.directory&&!single.entry.link?single.path:loadedRoot),scope:treeScope});setRecursiveSearchOpen(true)}}/>
                <DropdownMenuItem label={tr("比较本地与远程…")} isDisabled={!native||!connected||side!=='remote'||loadedScope!==treeScope} onClick={()=>{setCompareContext({remote:location('remote',single&&!single.entry.link?single.path:loadedRoot),scope:treeScope});setCompareOpen(true)}}/>
                <DropdownMenuItem label={syncRunning?tr("自动双向同步（运行中）…"):tr("自动双向同步…")} isDisabled={!native||!connected||side!=='remote'||loadedScope!==treeScope} onClick={()=>{if(!syncRunning){setSyncContext(location('remote',single?.entry.directory&&!single.entry.link?single.path:loadedRoot));setSyncGeneration(v=>v+1)};setSyncOpen(true)}}/>
                {side==='remote'&&onDirectorySync&&<DropdownMenuCheckboxItem label={tr("目录联动")} value={directorySync} onChange={onDirectorySync}/>}
                <DropdownMenuCheckboxItem label={tr("显示隐藏文件")} value={showHidden} onChange={value=>updateViewPrefs({showHidden:value})}/>
                <DropdownMenuCheckboxItem label={tr("文件名换行")} value={wrapNames} onChange={value=>updateViewPrefs({wrapNames:value})}/>
                {side==='remote'&&<DropdownMenuCheckboxItem label={tr("显示权限")} value={showPermissions} onChange={value=>updateViewPrefs({showPermissions:value})}/>}
                {side==='remote'&&<DropdownMenuCheckboxItem label={tr("显示所有者 UID")} value={showOwner} onChange={value=>updateViewPrefs({showOwner:value})}/>}
                <DropdownMenuItem label={tr("重置列宽")} isDisabled={!hasCustomWidths} onClick={()=>updateViewPrefs({columnWidths:{}})}/>
                <DropdownMenuSubMenu label={tr("排序")}>
                  <DropdownMenuRadioGroup label={tr("文件排序")} value={sort.key} onChange={key=>{
                    const nextKey = key as FileSortKey
                    updateViewPrefs({ sort: { key: nextKey, direction: nextKey === sort.key ? sort.direction : defaultDirectionForFileSort(nextKey) } })
                  }}>
                    <DropdownMenuRadioItem label={tr("按名称排序")} value="name"/>
                    <DropdownMenuRadioItem label={tr("按修改时间排序")} value="modified"/>
                    <DropdownMenuRadioItem label={tr("按大小排序")} value="size"/>
                    <DropdownMenuRadioItem label={tr("按类型排序")} value="type"/>
                  </DropdownMenuRadioGroup>
                  <DropdownMenuDivider/>
                  <DropdownMenuRadioGroup label={tr("排序")} value={sort.direction} onChange={direction=>{
                    updateViewPrefs({ sort: { ...sort, direction: direction === 'descending' ? 'descending' : 'ascending' } })
                  }}>
                    <DropdownMenuRadioItem label={tr("升序")} value="ascending"/>
                    <DropdownMenuRadioItem label={tr("降序")} value="descending"/>
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubMenu>
              </DropdownMenu></>}
          </Stack>
          {tab!=='tasks'&&<>
            {side==='local'&&!view&&<Button label={folder?tr("更换本地目录"):tr("选择本地目录")} size="md" isDisabled={!native} onClick={()=>void chooseLocal()}/>}
            <Stack direction="horizontal" align="center" gap={1}>
              <IconButton label={tr("后退目录")} tooltip={shortcutTooltip(tr('后退'),'file-back',shortcuts)} aria-keyshortcuts={shortcutAria('file-back',shortcuts)} size="md" variant="ghost" isDisabled={!canBrowse||busy||!directoryStep(currentHistory,-1)} icon={<Icon icon={ArrowLeftIcon} size="sm"/>} onClick={()=>browseHistory(-1)}/>
              <IconButton label={tr("前进目录")} tooltip={shortcutTooltip(tr('前进'),'file-forward',shortcuts)} aria-keyshortcuts={shortcutAria('file-forward',shortcuts)} size="md" variant="ghost" isDisabled={!canBrowse||busy||!directoryStep(currentHistory,1)} icon={<Icon icon={ArrowRightIcon} size="sm"/>} onClick={()=>browseHistory(1)}/>
              <IconButton label={tr("上级目录")} tooltip={tr("上级目录")} size="md" variant="ghost" isDisabled={!canBrowse||busy||currentPath==='/'} icon={<Icon icon={ArrowUpIcon} size="sm"/>} onClick={()=>void load(parentPath(currentPath))}/>
              <StackItem size="fill" style={{minWidth:0}}><TextInput id={panelId+'-path'} label={tr("当前目录")} isLabelHidden size="md" value={pathDraft} onChange={setPathDraft} autoComplete="off" isDisabled={!canBrowse} onKeyDown={e=>{if(e.key==='Enter'&&!e.nativeEvent.isComposing){e.preventDefault();void load(pathDraft)}else if(e.key==='Escape'&&!e.nativeEvent.isComposing){e.preventDefault();setPathDraft(loadedScope===treeScope?loadedRoot:currentPath);e.currentTarget.blur()}}}/></StackItem>
              {side==='remote'&&directorySync&&<Stack role="img" aria-label={tr("目录联动已开启")} align="center" justify="center" width="var(--spacing-5)"><Icon icon={LinkIcon} size="sm"/></Stack>}
              <IconButton label={tr("刷新文件")} tooltip={tr("刷新目录")} size="md" variant="ghost" isDisabled={!canBrowse||busy} isLoading={busy} icon={<Icon icon={ArrowPathIcon} size="sm"/>} onClick={()=>void load()}/>
              <IconButton id={panelId+'-wrap'} label={tr("文件名换行")} tooltip={tr("文件名换行")} aria-pressed={wrapNames} size="md" variant={wrapNames?'secondary':'ghost'} icon={<Icon icon={WrapTextIcon} size="sm"/>} onClick={()=>updateViewPrefs(previous=>({...previous,wrapNames:!previous.wrapNames}))}/>
            </Stack>
            <DirectoryBreadcrumbs path={loadedScope===treeScope?loadedRoot:currentPath} rootLabel={side==='local'?(folder?.name??tr("本地目录")):tr("根目录")} label={side==='local'?tr("本地目录层级"):tr("远程目录层级")} disabled={!canBrowse||busy} onNavigate={target=>{void load(target)}}/>
            {side==='remote'&&connected&&directorySync&&!live?.directory?.path&&<Stack direction="horizontal" wrap="wrap" gap={2} align="center">
              <Text type="supporting" color="secondary" role="status">{tr("等待终端报告目录；此终端可能尚未支持自动联动。")}</Text>
              {onShellIntegration&&<Button size="sm" variant="ghost" label={tr("Bash 终端集成…")} onClick={onShellIntegration}/>}
            </Stack>}
            {searchOpen&&<TextInput id={panelId+'-search'} label={tr("筛选已加载目录")} isLabelHidden size="md" type="search" placeholder={tr("筛选已加载目录…")} startIcon="search" value={query} onChange={setQuery} autoComplete="off" hasClear/>}
          </>}
        </Stack></LayoutHeader>}
        content={
          <LayoutContent padding={0} isScrollable={false} style={{ display: 'flex', minHeight: 0 }}>
            {tab === 'tasks' ? (
              <Stack
                id={panelId+'-tasks'}
                role={view?'region':'tabpanel'}
                aria-labelledby={panelId+'-tab-tasks'}
                height="100%"
                width="100%"
                padding={3}
                gap={2}
                style={{ overflow: 'auto' }}
              >
                <TransferTasks session={session} tasks={tasks} gateway={gateway} onError={setError} onChooseLocal={chooseLocal}/>
              </Stack>
            ) : (
              <Stack id={panelId+'-browser'} role={view?'region':'tabpanel'} aria-label={view?(view==='local'?tr("本地文件浏览"):tr("远程文件浏览")):undefined} aria-labelledby={view?undefined:panelId+'-tab-'+side} width="100%" height="100%" style={{minHeight:0,outline:dropHint?'var(--border-width) solid var(--color-border-emphasized)':undefined,outlineOffset:'calc(-1 * var(--border-width))'}}
                onDragOverCapture={e=>{if(active&&side==='remote'&&connected&&loadedScope===treeScope&&e.dataTransfer.types.includes('Files')){e.preventDefault();e.dataTransfer.dropEffect='copy';setDropHint((e.target as Element).closest('[data-native-upload-path]')?.getAttribute('data-native-upload-path')||loadedRoot)}}}
                onDragLeaveCapture={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node|null))setDropHint('')}}
                onDropCapture={e=>{setDropHint('');if(!e.dataTransfer.types.includes('Files'))return;nativeDestination.current=null;if(!active||side!=='remote'||!connected||loadedScope!==treeScope)return;e.preventDefault();const target=(e.target as Element).closest('[data-native-upload-path]')?.getAttribute('data-native-upload-path')||loadedRoot;nativeDestination.current={location:location('remote',target),x:e.clientX,y:e.clientY,at:Date.now()}}}
                onDragOver={e=>{if(e.dataTransfer.types.includes(fileDragType))e.preventDefault()}} onDrop={e=>drop(e,side)}>
                {!canBrowse ? side === 'local' ? <Stack padding={4}><Text type="supporting" color="secondary">{tr("选择本地目录后浏览文件。")}</Text></Stack>
                  : <Stack align="center" padding={2} style={{flex:1,minHeight:0,overflow:'auto'}}>
                    <EmptyState
                      title={remoteStatus}
                      description={native && remoteDetail !== remoteStatus ? remoteDetail : undefined}
                      icon={remotePrompt?.busy ? <Spinner size="lg"/> : <Icon icon={ServerStackIcon} size="lg"/>}
                      actions={session && remotePrompt && remotePrompt.actions.length > 0 &&
                        <ConnectionPromptActions actions={remotePrompt.actions} connectLabel={tr("立即连接")}/>}
                      style={{marginBlock:'auto',maxInlineSize:'100%',overflowWrap:'anywhere'}}/>
                  </Stack>
                  : loadedRoot&&loadedScope===treeScope?<SftpFileTree key={treeScope+loadedRoot} scope={treeScope} root={loadedRoot} entries={entries} hidden={showHidden} query={deferredQuery} sort={sort} onSort={next=>updateViewPrefs({sort:next})} wrapNames={wrapNames} showPermissions={side==='remote'&&showPermissions} showOwner={side==='remote'&&showOwner} columnWidths={columnWidths} onColumnWidthsChange={next=>updateViewPrefs({columnWidths:next})} onShowHidden={()=>updateViewPrefs({showHidden:true})} onClearQuery={()=>setQuery('')} readDirectory={readDirectory} onSelection={receiveSelection} onError={setError} onRename={()=>beginAction('rename')} onDelete={beginDelete} menu={contextItems} onOpen={node=>{if(node.entry.directory)void load(node.path);else if(side==='remote')onEdit?.(node.path)}} onDrag={dragStart} onDrop={(e,p)=>drop(e,side,p)}/>:<Stack padding={4}><Text type="supporting" color="secondary">{busy?tr("正在读取目录…"):tr("目录未能读取，请重试。")}</Text></Stack>}
              </Stack>
            )}
          </LayoutContent>
        }
        footer={!view?<LayoutFooter padding={2} hasDivider><Stack gap={1}>{error&&<Text type="supporting" role="alert">{localizeMessage(error)}</Text>}<Text type="supporting" color="secondary" role="status">{summary}</Text></Stack></LayoutFooter>:undefined}
      />
      {searchContext&&<Suspense fallback={null}><FileSearchDialog isOpen={recursiveSearchOpen} location={searchContext.location} hidden={showHidden} gateway={gateway} onClose={()=>setRecursiveSearchOpen(false)} onReveal={async hit=>{
        if(scopeNow.current!==searchContext.scope)throw Error(tr("连接或本地授权已变化，请重新开始搜索。"))
        const target=hit.directory&&!hit.link?hit.path:parentPath(hit.path)
        const memory=treeMemory(treeScope+':'+target);memory.selected=new Set([hit.path]);memory.focused=hit.path;memory.anchor=hit.path
        if(hit.name.startsWith('.'))updateViewPrefs({showHidden:true});setQuery('')
        if(!await load(target))throw Error(tr("结果所在目录未能打开，当前目录已保留。"))
      }}/></Suspense>}
      {compareContext&&<Suspense fallback={null}><FileCompareDialog isOpen={compareOpen} remote={compareContext.remote} local={folder} gateway={gateway} onClose={()=>setCompareOpen(false)} onLocal={next=>{rememberedLocalFolder=next;rememberedLocalPath='/';setFolder(next);setLocalPath('/');onLocalChanged?.()}} onQueued={()=>{setMessage(tr("已确认的复制已加入任务队列；逐项进度见任务。"));setTab('tasks')}} onText={(id,relative)=>{if(scopeNow.current!==compareContext.scope){setError(tr("连接已变化，请重新比较。"));return};onComparisonText?.(id,relative)}}/></Suspense>}
      {syncContext&&<Suspense fallback={null}><FileSyncDialog serverName={session?.host.name} key={syncGeneration} onFileGuard={onFileGuard} onProtection={onProtection} onReveal={()=>setSyncOpen(true)} isOpen={syncOpen} remote={syncContext} local={folder} gateway={gateway} onClose={()=>setSyncOpen(false)} onRunning={setSyncRunning} onLocal={next=>{rememberLocalFolder(next);setFolder(next);setLocalPath('/');onLocalChanged?.()}}/></Suspense>}
      {remoteCopySources.length>0&&<Suspense fallback={null}><RemoteCopyDialog isOpen={remoteCopyOpen} sources={remoteCopySources} gateway={gateway} sessionLabel={sessionLabel} onClose={()=>setRemoteCopyOpen(false)} onQueued={()=>{setMessage(tr("请在任务页查看远程复制进度。"));setTab('tasks')}}/></Suspense>}
      <EditorDialog
        key="file-action"
        isOpen={!!action}
        width="min(calc(var(--spacing-10) * 12), 92vw)"
        title={action ? actionNames[action] : ''}
        action={action === 'mkdir'||action==='create' ? 'add' : 'edit'}
        closeLabel={tr("关闭文件操作")}
        saveHint={action==='create'?tr('创建并编辑'):undefined}
        busy={saving}
        onClose={() => setAction(null)}
        formProps={{
          'aria-label': tr("文件操作"),
          onSubmit: (e) => {
            e.preventDefault()
            void saveAction(action==='create'&&!!onEdit)
          },
        }}
        actions={
          <>
          {action==='create'&&onEdit&&<Button label={tr("仅创建")} size="md" variant="secondary" isDisabled={saving} onClick={()=>{void saveAction(false)}}/>}
          <Button
            label={action==='create'?(onEdit?tr("创建并编辑"):tr("创建")):action === 'copy' ? tr("加入队列") : tr("确认")}
            type="submit"
            size="md"
            variant="primary"
            isDisabled={saving}
            isLoading={saving}
          />
          </>
        }
      >
        {action==='create'&&(
          <Stack
            padding={3}
            style={{
              borderRadius: 'var(--radius-element)',
              background: 'var(--color-background-muted)',
              border: 'var(--border-width) solid var(--color-border)',
            }}
          >
            <Text type="supporting" color="secondary" style={{overflowWrap:'anywhere'}}>
              {tr("创建位置：")}{actionBase.current}{tr("。同名文件不会覆盖；空文件创建后，正文需在编辑器中另行保存。")}
            </Text>
          </Stack>
        )}
        {(action === 'copy' || action === 'move') && (
          <TextInput
            label={tr("目标目录")}
            size="md"
            value={editDestination}
            onChange={setEditDestination}
            autoComplete="off"
          />
        )}
        {action !== 'copy' && (
          <TextInput
            label={action === 'chmod' ? tr("权限（例如 644 或 755）") : tr("名称")}
            size="md"
            value={editValue}
            onChange={setEditValue}
            autoComplete="off"
            hasAutoFocus
          />
        )}
        {error && (
          <Text type="supporting" role="alert">
            {localizeMessage(error)}
          </Text>
        )}
      </EditorDialog>
      <DialogPresence value={properties}>
        {(snapshot, isOpen, onAfterClose) => (
          <FilePropertiesDialog
            isOpen={isOpen}
            node={snapshot}
            onClose={() => setProperties(null)}
            onAfterClose={onAfterClose}
          />
        )}
      </DialogPresence>
      <ConfirmDelete
        isOpen={deleteOpen}
        title={tr("删除 {p0} 项文件？", operationItems.current.length)}
        description={tr("文件夹及其内容会一并删除，此操作无法撤销。")}
        busy={saving}
        error={localizeMessage(error)}
        onClose={() => setDeleteOpen(false)}
        onConfirm={() => void deleteFiles()}
      />
    </>
  )
})
