import { previewPreferencesKey } from '../shared/previewPreferences'
import BrowserStatus from '../shared/ui/BrowserStatus'
import { indexWorkspaces } from '../features/workspace/workspaceNavigation'
import EditorDialog from '../shared/ui/EditorDialog'
import { TextInput } from '@astryxdesign/core/TextInput'
import './windowCaption.css'
import { SessionRail, SessionRailCaption, SessionRailHeader, SessionRailProvider, SessionRailReleaseNotes, TopSessionTabs, titlebarHeight } from './SessionRail'
import { localizeMessage } from '../shared/i18n/messages'
import SyncWorkspaceRunner from '../features/sync/SyncWorkspaceRunner'
import { tr } from '../shared/i18n/messages'
import { useLocale, getLanguage, setLanguagePreference } from '../shared/i18n/locale'
import { applyUIFont } from '../shared/fonts/fonts'
import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState, useSyncExternalStore, type CSSProperties, type SetStateAction } from 'react'
import { createPortal } from 'react-dom'
import { Layout, LayoutContent } from '@astryxdesign/core/Layout'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { useNotice } from '../shared/ui/useNotice'
import { Button } from '@astryxdesign/core/Button'
import { useMediaQuery } from '@astryxdesign/core/hooks'
import { Dialog, DialogPresence } from '../shared/ui/AppDialog'
import AppContextMenu from '../shared/ui/AppContextMenu'
import type { ContextMenuOption } from '@astryxdesign/core/ContextMenu'
import GroupPath from '../shared/ui/GroupPath'
import { Theme } from '@astryxdesign/core/theme'
import { BuiltInIconStyleContext } from '../shared/icons/OsIcon'
import SessionCommandPicker, { type CommandTarget } from '../features/workspace/SessionCommandPicker'
import { useWorkbenchTheme } from '../shared/theme/currentTheme'
import { resolveTerminalPalette } from '../shared/theme/terminalPresets'
import { useAppearance } from '../shared/theme/useAppearance'
import { useViewportWidth } from '../shared/ui/useViewportWidth'
import { usePanelReveal } from '../shared/ui/usePanelReveal'
import SessionToolbar from '../features/workspace/SessionToolbar'
import ToolSurface from '../shared/ui/ToolSurface'
import AuxiliaryPanel from '../shared/ui/AuxiliaryPanel'
import { connectionLabels, reconnectWaitMessage } from '../features/workspace/sessionConnection'
import { needsAuthenticationInput, savedConnectionCredentials, type ConnectionPasswordSave } from '../features/connections/authenticationDraft'
import { committedConnection, sameConnectionTarget, useHostSessions } from '../features/connections/useHostSessions'
import { type WorkspaceView } from '../features/workspace/SessionTabs'
import { desktopAvailable, getApplicationInfo, setWindowAppearance, setInterfaceLanguage } from '../shared/bridge/application'
import { WorkspaceService, type ServerSystem } from '../shared/generated/mygo'
import HostManager from '../features/connections/HostManager'
import { hostCopyActions, hostRowContextActions } from '../features/connections/hostContextMenu'
import SessionTools, { type SessionToolsHandle } from './SessionTools'
import { useIdentities } from '../features/connections/useIdentities'
import type { TerminalController } from '../features/terminal/TerminalPane'
import type { ShellOperation } from '../features/terminal/ShellIntegrationDialog'
import type { SSHTransport } from '../shared/bridge/ssh'
import TerminalSessionPane from '../features/workspace/TerminalSessionPane'
import { createTerminalFocus } from '../features/terminal/terminalFocus'
import type { TerminalTransport } from '../features/terminal/remoteOutput'
import ConnectionTabs, { type ConnectionTab } from '../features/workspace/ConnectionTabs'
import {forgetFileTrees} from '../features/workspace/fileTreeModel'
import { createSessionCloser, type CloseParticipant, type SessionCloseState } from '../features/workspace/sessionClose'
import type { FileCloseGuard, RegisterFileGuard } from '../features/workspace/fileCloseGuard'
import {fileGateway} from '../shared/bridge/files'
import type { RemoteTextController } from '../features/workspace/RemoteTextWorkspace'
import { useKeyReferences } from '../features/connections/useKeyReferences'
import { usePaneSSH, useSSHSessions, type Credentials } from '../features/connections/useSSHSessions'
import { defaultPreferences, readPreferences, savePreferences, normalizePreferences, stepTerminalFontSize, type Preferences } from '../features/workspace/preferences'
import { desktopPreferences } from '../features/workspace/desktopPreferences'
import { handleTerminalFontWheel } from '../features/terminal/terminalFontWheel'
import { canAutoUpdateHostIcon, commandAvailable, commandUngrouped, detectedSystemIcon, resolveDetectedHostIcon, type QuickCommand } from '../features/workspace/shortcuts'
import { findShortcut, managementPages, recordShortcutEvent, shortcutAria, shortcutTooltip, useShortcutOverrides, type ManagementPage } from '../features/workspace/shortcutReference'
import { type HostProfile } from '../features/workspace/mockGateway'
import { terminalWorkspaceReducer,groupFor,hidePaneView,setPaneView,leaves,minimumSize,canSplit,type SplitAxis } from '../features/workspace/terminalLayout'
import TerminalWorkspaceView,{type WorkspaceGeometry} from '../features/workspace/TerminalWorkspaceView'
import SplitLayoutDialog from '../features/workspace/SplitLayoutDialog'
import type {SplitPreset} from '../features/workspace/terminalLayout'
import CloseTerminalDialog from '../features/workspace/CloseTerminalDialog'
import { useSavedWorkspace } from '../features/workspace/useSavedWorkspace'
import {captureSessionLayout,restoreSessionLayout} from '../features/workspace/rememberWorkspace'
import {useRememberWorkspace} from '../features/workspace/useRememberWorkspace'
const ConfirmDelete = lazy(() => import('../shared/ui/ConfirmDelete'))
const ShellIntegrationDialog = lazy(() => import('../features/terminal/ShellIntegrationDialog'))
const RemoteTextWorkspace=lazy(()=>import('../features/workspace/RemoteTextWorkspace'))
const SftpWorkspace=lazy(()=>import('../features/workspace/SftpWorkspace'))
import { useApplicationUpdateRunner } from '../features/workspace/useApplicationUpdates'
const SSHConfigImportDialog=lazy(()=>import('../features/connections/SSHConfigImportDialog'))
const DeletedHostRecoveryDialog=lazy(()=>import('../features/connections/DeletedHostRecoveryDialog'))

const fill: CSSProperties = { height: '100%', minHeight: 0, minWidth: 0 }
const emptyPinOrder: readonly string[] = []
function useHostAction<Args extends unknown[], Result>(action: (...args: Args) => Result) {
  const latest = useRef(action)
  useLayoutEffect(() => { latest.current = action }, [action])
  return useCallback((...args: Args) => latest.current(...args), [])
}
type Tool = 'commands' | 'settings' | 'identities' | 'groups'
const loadIdentities = () => import('../features/connections/IdentityPanel')
const loadConnectionEditor = () => import('../features/connections/ConnectionEditor')
const loadSettings = () => import('../features/workspace/SettingsPanel')
const loadCommands = () => import('../features/workspace/QuickCommandsPanel')
const loadCommandEditor = () => import('../features/workspace/CommandEditorDialog')
const loadSessionPanel = () => import('../features/connections/SessionConnectionPanel')
const loadGroupManager = () => import('../features/workspace/GroupManagementPanel')
const workspaceFocusTarget=()=>[document.querySelector<HTMLElement>('#settings-page [role="tab"][aria-selected="true"]'),document.getElementById('new-connection'),...document.querySelectorAll<HTMLElement>('#session-tab-strip [aria-selected="true"]'),document.getElementById('tab-hosts')].find(node=>node&&node.getClientRects().length)

export default function Workbench() {
  const updateAvailable = useApplicationUpdateRunner()
  const language = useLocale()
  const setNotice = useNotice()

  const nativePreferences = desktopAvailable()
  const [browserPreferences, setBrowserPreferences] = useState(() => nativePreferences ? defaultPreferences : readPreferences())
  const storedPreferences = useSyncExternalStore(desktopPreferences.subscribe, desktopPreferences.read, desktopPreferences.read)
  const preferences = nativePreferences ? storedPreferences.preferences : browserPreferences
  const setPreferences = useCallback((next: SetStateAction<Preferences>) => {
    if (nativePreferences) desktopPreferences.update(next)
    else setBrowserPreferences(next)
  }, [nativePreferences])
  const interfaceTheme = useWorkbenchTheme(preferences.interfacePalette)
  useEffect(() => { let alive = true; void applyUIFont(preferences.uiFont, language).then(ok => { if (alive && !ok) setNotice(tr("无法加载 {p0}，界面暂时使用 Inter。", preferences.uiFont)) }); return () => { alive = false } }, [preferences.uiFont, language])
  const { mode: appearance, fontSize } = preferences
  const { resolved: mode, systemDark } = useAppearance(appearance)
  const systemMode = systemDark ? 'dark' : 'light'
  const terminalPalette = resolveTerminalPalette(preferences.terminalPalette, systemMode)
  const [browserPreferencesSaved, setBrowserPreferencesSaved] = useState(true)
  const preferencesSaved = nativePreferences ? storedPreferences.saved : browserPreferencesSaved
  const [view, setView] = useState<WorkspaceView>('hosts')
  const setShowHosts = useCallback((show:boolean) => {++toolRequest.current;setLoadingTool(null);setView(show?'hosts':'terminal')},[])
  const [desktopLoaded,setDesktopLoaded]=useState<Record<string,boolean>>({})
  const [desktopViews,setDesktopViews]=useState<Record<string,boolean>>({})
  const [authTarget,setAuthTarget] = useState('')
  const [integration, setIntegration] = useState<{ id: string; transport: SSHTransport; operation: ShellOperation } | null>(null)
  const sessionTools = useRef<SessionToolsHandle>(null)
  const promptedFailures = useRef(new WeakSet<object>())
  const [IdentityPanel,setIdentityPanel] = useState<typeof import('../features/connections/IdentityPanel').default|null>(null)
  const [SettingsPanel, setSettingsPanel] = useState<typeof import('../features/workspace/SettingsPanel').default | null>(null)
  const [QuickCommandsPanel, setQuickCommandsPanel] = useState<typeof import('../features/workspace/QuickCommandsPanel').default | null>(null)
  const [CommandEditorDialog, setCommandEditorDialog] = useState<typeof import('../features/workspace/CommandEditorDialog').default | null>(null)
  const [commandEditor, setCommandEditor] = useState<{ open: boolean; revision: number; command: QuickCommand | null }>({ open: false, revision: 0, command: null })
  const commandEditorRequest = useRef(0)
  const [ConnectionEditor, setConnectionEditor] = useState<typeof import('../features/connections/ConnectionEditor').default | null>(null)
  const [SessionConnectionPanel, setSessionConnectionPanel] = useState<typeof import('../features/connections/SessionConnectionPanel').default | null>(null)
  const [GroupManager, setGroupManager] = useState<typeof import('../features/workspace/GroupManagementPanel').default | null>(null)
  const [loadingTool, setLoadingTool] = useState<Tool | null>(null)
  const toolRequest = useRef(0)
  const savedWorkspace = useSavedWorkspace()
  const acknowledgePreferenceWrite = savedWorkspace.acknowledgePreferenceWrite
  useEffect(() => {
    if (!nativePreferences) return
    const stop = desktopPreferences.subscribePortableWrite(acknowledgePreferenceWrite)
    window.addEventListener('pagehide', desktopPreferences.flush)
    return () => {
      stop()
      window.removeEventListener('pagehide', desktopPreferences.flush)
      desktopPreferences.flush()
    }
  }, [nativePreferences, acknowledgePreferenceWrite])
  const identities = useIdentities(savedWorkspace.reload)
  const { commands, groups, commandGroups } = savedWorkspace.snapshot
  const editable = savedWorkspace.ready && !savedWorkspace.busy
  const keyReferences = useKeyReferences()
  const terminalControllers = useRef(new Map<string, TerminalController>())
  const [terminalFocus] = useState(() => createTerminalFocus(
    id => terminalControllers.current.get(id),
    () => document.activeElement === document.body ? null : document.activeElement,
    callback => window.requestAnimationFrame(callback),
    frame => window.cancelAnimationFrame(frame),
  ))
  const requestFocus = terminalFocus.request
  useLayoutEffect(() => terminalFocus.cancel, [terminalFocus])
  const remoteControllers=useRef(new Map<string,RemoteTextController>())
  const fileCloseGuards=useRef(new Map<string,{sessionId:string;guard:FileCloseGuard}>())
  const registerFileGuard=useCallback<RegisterFileGuard>((sessionId,key,guard)=>{if(guard)fileCloseGuards.current.set(key,{sessionId,guard});else fileCloseGuards.current.delete(key)},[])
  const [remoteRequests,setRemoteRequests]=useState<{id:string;sessionId:string;path:string}[]>([])
  const [dirtyDocuments,setDirtyDocuments]=useState<Record<string,boolean>>({})
  const markDocumentDirty=useCallback((id:string,dirty:boolean)=>setDirtyDocuments(previous=>{
    if(!!previous[id]===dirty)return previous
    const next={...previous};if(dirty)next[id]=true;else delete next[id];return next
  }),[])
  const [fileWorkspaceViews,setFileWorkspaceViews]=useState<Record<string,boolean>>({})
  const [remoteViews,setRemoteViews]=useState<Record<string,string|false>>({})
  const [logViews,setLogViews]=useState<Record<string,boolean>>({})
  const [diffRequests,setDiffRequests]=useState<Record<string,{id:string;relative:string}>>({}),[diffViews,setDiffViews]=useState<Record<string,boolean>>({})
  const hideWorkspaceTabs = useCallback((id: string) => {
    setFileWorkspaceViews(views => hidePaneView(views, id))
    setRemoteViews(views => hidePaneView(views, id))
  }, [])
  const showTerminal = useCallback((id: string, focus = true) => {
    hideWorkspaceTabs(id)
    setDesktopViews(views => hidePaneView(views, id))
    setLogViews(views => hidePaneView(views, id))
    setDiffViews(views => hidePaneView(views, id))
    if (focus) requestFocus(id)
  }, [hideWorkspaceTabs, requestFocus])
  const [remoteRefresh,setRemoteRefresh]=useState(0)
  const remoteProtection=useRef(new Map<string,boolean>()),protectionRevision=useRef(0)
  const protectRemote=useCallback((id:string,protectedValue:boolean)=>{if(protectedValue)remoteProtection.current.set(id,true);else remoteProtection.current.delete(id);if(fileGateway.available())void fileGateway.setTextProtection(++protectionRevision.current,remoteProtection.current.size>0).catch(()=>setNotice(tr("远程编辑关闭保护未启用，请保存文件后再退出程序。"), 'error'))},[])
  const registerRemote=useCallback((id:string,controller:RemoteTextController|null)=>{if(controller)remoteControllers.current.set(id,controller);else remoteControllers.current.delete(id)},[])
  const reportTerminalError = useCallback(() => setNotice(tr("终端数据暂不可用，请关闭此会话后重新打开。"), 'error'), [])
  const registerTerminal = useCallback((id: string, controller: TerminalController | null) => {
    if (controller) terminalControllers.current.set(id, controller)
    else terminalControllers.current.delete(id)
  }, [])
  const [runtime, dispatch] = useReducer(terminalWorkspaceReducer, {
    hosts: [],
    sessions: [], activeId: '', groups: [],
  })
  const state = { ...runtime, hosts: savedWorkspace.snapshot.hosts }
  const [deletion, setDeletion] = useState<{ kind: 'host'; host: HostProfile } | { kind: 'group'; name: string } | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [groupsOpen, setGroupsOpen] = useState(false)
  const [groupCreateRequest,setGroupCreateRequest]=useState<{version:number;parent?:string}>({version:0})
  const [importOpen,setImportOpen]=useState(false),[importLoaded,setImportLoaded]=useState(false)
  const [hostRecoveryOpen,setHostRecoveryOpen]=useState(false),[hostRecoveryLoaded,setHostRecoveryLoaded]=useState(false)
  const [detectedSystems, setDetectedSystems] = useState<Record<string, ServerSystem>>({})
  const systemsRequest = useRef(0)
  const refreshSystems = useCallback(() => {
    if (!desktopAvailable()) return
    const request = ++systemsRequest.current
    void WorkspaceService.serverSystems().then(result => {
      if (request === systemsRequest.current) setDetectedSystems(result)
    }).catch(() => {})
  }, [])
  useEffect(() => {
    if (savedWorkspace.ready) refreshSystems()
    return () => { systemsRequest.current++ }
  }, [savedWorkspace.ready, refreshSystems])
  const sessionById = useMemo(() => new Map(state.sessions.map(session => [session.id, session])), [state.sessions])
  const hostSessions = useHostSessions(state.hosts, runtime.groups, sessionById, state.activeId)
  const routeAvailable = (id: string, host: HostProfile) => {
    if (hostSessions.hopsMatch(id, host)) return true
    setNotice(tr('连接路径已变化，请从连接列表重新打开。'), 'error')
    return false
  }
  const ssh = useSSHSessions(useCallback((id, event) => dispatch({ type: 'connection', id, event }), []),preferences,refreshSystems,routeAvailable)
  const usedAttempts = useRef(new Map<string, number>())
  const submittedUsedAttempts = useRef(new Map<string, number>())
  useEffect(() => {
    if (savedWorkspace.busy || !savedWorkspace.ready) return
    for (const id of usedAttempts.current.keys()) {
      if (!state.sessions.some(session => session.id === id)) usedAttempts.current.delete(id)
    }
    for (const [id, entry] of Object.entries(ssh.entries)) {
      if (entry.phase !== 'connected' || usedAttempts.current.get(id) === entry.attempt ||
          submittedUsedAttempts.current.get(id) === entry.attempt) continue
      const session = state.sessions.find(session => session.id === id)
      if (!session || !state.hosts.some(host => host.id === session.host.id)) continue
      submittedUsedAttempts.current.set(id, entry.attempt)
      void savedWorkspace.apply({ kind: 'markHostUsed', id: session.host.id }).then(committed => {
        if (committed && submittedUsedAttempts.current.get(id) === entry.attempt) usedAttempts.current.set(id, entry.attempt)
      })
    }
  }, [ssh.entries, savedWorkspace.busy, savedWorkspace.ready, state.sessions, state.hosts])
  const [searchRequest,setSearchRequest] = useState(0)
  const [resetHostQuery, setResetHostQuery] = useState(0)
  const [hostGroup, setHostGroup] = useState('all')
  const activeSession = state.sessions.find(session => session.id === state.activeId)
  const activeGroup = groupFor(runtime,runtime.activeId)
  const spaces=useMemo(()=>indexWorkspaces(runtime.groups,sessionById),[runtime.groups,sessionById])
  const directHostIds = hostSessions.connected
  const [tabsOnTop,setTabsOnTop]=useState(()=>{try{return localStorage.getItem('yaare.tabs-position')==='top'}catch{return false}})
  const changeTabPosition=(top:boolean)=>{
    try{localStorage.setItem('yaare.tabs-position',top?'top':'left')}catch{}
    setTabsOnTop(top)
  }
  const [marked,setMarked]=useState<string[]>([])
  const [collapsed,setCollapsed]=useState<string[]>([])
  const [preview,setPreview]=useState<{groups:string[];panes:string[];preset:SplitPreset}|null>(null)
  const [renaming,setRenaming]=useState<{id:string;name:string;groups?:string[];pane?:boolean}|null>(null)
  const openPreview=(groups:string[],preset?:SplitPreset)=>{
    const panes=groups.flatMap(id=>{const g=runtime.groups.find(g=>g.id===id);return g?leaves(g.root):[]})
    if(panes.length)setPreview({groups,panes,preset:preset??(panes.length<=2?'side':panes.length===3?'three':'four')})
  }
  const layoutTitle=(group:NonNullable<typeof activeGroup>)=>{
    const siblings=spaces.get(group.workspaceId??group.id)!.groups
    return group.customName?group.name:tr("终端 {p0}",siblings.indexOf(group)+1)
  }
  const paneLabel=(id:string)=>{
    const pane=sessionById.get(id),group=groupFor(runtime,id)
    return pane&&group?tr("{p0} · 窗格 {p1}（标签 {p2}）",pane.host.name,leaves(group.root).indexOf(id)+1,spaces.get(group.workspaceId??group.id)!.groups.indexOf(group)+1):tr("终端")
  }
  const workspaceGeometry=useRef<WorkspaceGeometry>({width:0,height:0,focusOnly:false,spaceLimited:false,panes:new Map()})
  const activeSSH = ssh.entries[state.activeId]
  const hostsVisible = view === 'hosts'
  const terminalVisible = view === 'terminal' && !!activeSession
  const authOpen=terminalVisible&&authTarget===state.activeId
  const filesSelected=terminalVisible&&!!fileWorkspaceViews[state.activeId]
  const selectedDocument=terminalVisible?remoteViews[state.activeId]:false
  const sessionView = useRef<HTMLElement>(null)
  const visibleSessionView = selectedDocument || (filesSelected ? 'files-' + state.activeId
    : desktopViews[state.activeId] ? 'desktop-' + state.activeId
    : logViews[state.activeId] ? 'logs-' + state.activeId
    : diffViews[state.activeId] ? 'diff-' + state.activeId : 'terminal-' + activeGroup?.id)
  usePanelReveal(sessionView, terminalVisible, visibleSessionView)
  const visibleTerminalId = terminalVisible && !filesSelected && !selectedDocument && !desktopViews[state.activeId] && !logViews[state.activeId] && !diffViews[state.activeId] ? state.activeId : ''
  useLayoutEffect(() => { terminalFocus.setVisible(visibleTerminalId) }, [terminalFocus, visibleTerminalId])
  const [pickerOpen,setPickerOpen] = useState(false)
  const [selectedId, setSelectedId] = useState('')
  const [editor, setEditor] = useState<{ host?: HostProfile; open: boolean; revision: number; baseRevision?: number; temporary?:boolean }>({ open: false, revision: 0 })
  const editorRequest = useRef(0)
  const editorTrigger = useRef<HTMLElement | null>(null)
  const savedHostToReveal = useRef<string | undefined>(undefined)
  const [savedHostRow, setSavedHostRow] = useState<{key: string}>()
  const clearSavedHostRow = useCallback(() => setSavedHostRow(undefined), [])
  const editorWasOpen = useRef(false)
  const sessionIconAttempts = useRef(new Map<string, number>())
  const savedIconAttempts = useRef(new Map<string, number>())
  const submittedIconAttempts = useRef(new Map<string, number>())
  useEffect(() => {
    for (const id of sessionIconAttempts.current.keys()) {
      if (!state.sessions.some(s => s.id === id)) sessionIconAttempts.current.delete(id)
    }
    if (!preferences.detectServerSystem) return
    for (const [id, entry] of Object.entries(ssh.entries)) {
      if (entry.phase !== 'connected' || !entry.system || sessionIconAttempts.current.get(id) === entry.attempt) continue
      const session = state.sessions.find(s => s.id === id)
      if (!session || session.pending) continue
      const saved = state.hosts.find(h => h.id === session.host.id)
      sessionIconAttempts.current.set(id, entry.attempt)
      if (saved && !canAutoUpdateHostIcon(saved.icon)) continue
      const nextIcon = resolveDetectedHostIcon(session.host.icon, entry.system)
      if (!nextIcon) continue
      dispatch({ type: 'updateSessionHost', id, host: { ...session.host, icon: nextIcon } })
    }
  }, [preferences.detectServerSystem, ssh.entries, state.sessions, state.hosts])
  useEffect(() => {
    for (const id of savedIconAttempts.current.keys()) {
      if (!state.sessions.some(s => s.id === id)) savedIconAttempts.current.delete(id)
    }
    if (!preferences.detectServerSystem || savedWorkspace.busy || !savedWorkspace.ready || editor.open) return
    for (const [id, entry] of Object.entries(ssh.entries)) {
      if (entry.phase !== 'connected' || !entry.system || (savedIconAttempts.current.get(id) === entry.attempt || submittedIconAttempts.current.get(id) === entry.attempt)) continue
      const session = state.sessions.find(s => s.id === id)
      if (!session || session.pending) continue
      const saved = state.hosts.find(h => h.id === session.host.id)
      if (!saved) {
        savedIconAttempts.current.set(id, entry.attempt)
        continue
      }
      if (!sameConnectionTarget(saved, session.host) || !hostSessions.hopsMatch(id, saved)) {
        savedIconAttempts.current.set(id, entry.attempt)
        continue
      }
      const nextIcon = resolveDetectedHostIcon(saved.icon, entry.system)
      if (!nextIcon) {
        savedIconAttempts.current.set(id, entry.attempt)
        continue
      }
      submittedIconAttempts.current.set(id, entry.attempt)
      void savedWorkspace.apply({ kind: 'setHostSystemIcon', id: saved.id, name: nextIcon }).then(committed => {
        if (committed && submittedIconAttempts.current.get(id) === entry.attempt) savedIconAttempts.current.set(id, entry.attempt)
      })
      break
    }
  }, [preferences.detectServerSystem, ssh.entries, savedWorkspace.busy, savedWorkspace.ready, editor.open, state.sessions, state.hosts, hostSessions.hopsMatch])
  const [paths, setPaths] = useState<Record<string, string>>({})
  const [windowTools,setWindowTools] = useState<HTMLElement|null>(null)
  const [managementCaption,setManagementCaption]=useState<HTMLElement|null>(null)
  const viewportWidth = useViewportWidth()
  const [railPinned, setRailPinned] = useState(true)
  const [syncHistoryRequest,setSyncHistoryRequest]=useState(0)
  const [sizes, setSizes] = useState({ start: 216, end: 320 })
  const rememberedLayout = useMemo(() => captureSessionLayout(runtime,state.hosts,paths,fileWorkspaceViews,railPinned,sizes),
    [runtime,state.hosts,paths,fileWorkspaceViews,railPinned,sizes])
  const remember=useRememberWorkspace(rememberedLayout,savedWorkspace.ready,saved=>{
    if(runtime.sessions.length)return
    dispatch({type:'restore',workspace:restoreSessionLayout(saved,state.hosts)})
    setPaths(Object.fromEntries(saved.panes.map(p=>[p.id,p.path])))
    setFileWorkspaceViews(Object.fromEntries(saved.panes.filter(p=>p.filesOpen).map(p=>[p.id,true])))
    setRailPinned(saved.version===0||saved.startOpen)
    setSizes({start:saved.startWidth,end:saved.endWidth})
  })
  useEffect(()=>{if(remember.error)setNotice(remember.error, 'error')},[remember.error,setNotice])
  const railSurface = useRef<HTMLElement>(null)
  const commitRailWidth = useCallback((size: number) => {
    setSizes(previous => previous.start === size ? previous : { ...previous, start: size })
  }, [])
  const desiredMinimum=activeGroup&&!activeGroup.maximized?minimumSize(activeGroup.root).width:320
  useEffect(() => {
    const closed = editorWasOpen.current && !editor.open
    editorWasOpen.current = editor.open
    if (!closed) return
    let active = true
    const dialog = document.getElementById('connection-editor')?.closest('dialog')
    const restore = () => {
      const focused = document.activeElement
      const validFocus = focused instanceof HTMLElement && focused !== document.body && focused !== document.documentElement && focused.getClientRects().length > 0
      if (!active || document.querySelector('dialog[open]') || validFocus) return
      const trigger = editorTrigger.current
      const target = trigger?.isConnected && trigger.getClientRects().length ? trigger : workspaceFocusTarget()
      target?.focus({ preventScroll: true })
    }
    dialog?.addEventListener('close', restore)
    dialog?.addEventListener('transitionend', restore)
    dialog?.addEventListener('animationend', restore)
    queueMicrotask(restore)
    return () => {
      active = false
      dialog?.removeEventListener('close', restore)
      dialog?.removeEventListener('transitionend', restore)
      dialog?.removeEventListener('animationend', restore)
    }
  }, [editor.open])
  const path = paths[state.activeId] ?? '.'
  useEffect(()=>{
    if(!terminalVisible||!activeSSH)return
    if(activeSSH.phase==='hostkey'||activeSSH.phase==='authentication')setAuthTarget(state.activeId)
    else if(activeSSH.phase==='failed'&&needsAuthenticationInput(activeSSH.code)){
      if(!promptedFailures.current.has(activeSSH)){
        promptedFailures.current.add(activeSSH)
        setAuthTarget(state.activeId)
      }
    }else if(['connected','disconnected','failed'].includes(activeSSH.phase)){
      setAuthTarget(target=>target===state.activeId?'':target)
    }
  },[activeSSH,terminalVisible,state.activeId])
  useEffect(()=>{
    if(!authOpen||SessionConnectionPanel)return
    let current=true
    void loadSessionPanel().then(module=>{if(current)setSessionConnectionPanel(()=>module.default)}).catch(()=>{
      if(current){setAuthTarget('');setNotice(tr("认证面板加载失败，请点击连接重试。"), 'error')}
    })
    return ()=>{current=false}
  },[authOpen,SessionConnectionPanel])

  const openEditor = useCallback(async (host?: HostProfile, trigger?: HTMLElement, temporary=false) => {
    if (!editable) return
    const request = ++editorRequest.current
    if (editor.open) {
      if (editor.host?.id !== host?.id) setNotice(tr("请先保存或取消正在编辑的连接。"))
      return
    }
    editorTrigger.current = trigger ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null)
    try {
      const module = await loadConnectionEditor()
      if (request !== editorRequest.current) return
      const snapshot = savedWorkspace.getSnapshot()
      const current = host && snapshot.hosts.find(item => item.id === host.id)
      if (host && !current) { setNotice(tr('此主机配置已不存在。')); return }
      setConnectionEditor(() => module.default)
      setEditor(previous => ({ host: current, baseRevision: current ? snapshot.revision : undefined, open: true, revision: previous.revision + 1, temporary }))
    } catch {
      if (request === editorRequest.current) setNotice(tr("连接编辑器加载失败，请重试。"), 'error')
    }
  }, [editor.open, editor.host?.id, editable, savedWorkspace.getSnapshot, setNotice])
  const closeEditor = () => {
    ++editorRequest.current
    setEditor(previous => ({ ...previous, open: false }))
  }
  const openQuickCommandEditor = useCallback(async (system?: ServerSystem) => {
    if (!editable) return
    const request = ++commandEditorRequest.current
    try {
      let Editor = CommandEditorDialog
      if (!Editor) {
        const module = await loadCommandEditor()
        if (request !== commandEditorRequest.current) return
        Editor = module.default
        setCommandEditorDialog(() => Editor)
      }
      const icon = preferences.detectServerSystem && system ? detectedSystemIcon(system) : undefined
      setCommandEditor(previous => ({
        open: true,
        revision: previous.revision + 1,
        command: { id: crypto.randomUUID(), name: '', command: '', group: commandUngrouped, ...(icon ? { icon } : {}) },
      }))
    } catch {
      if (request === commandEditorRequest.current) setNotice(tr("工具加载失败，已保留当前内容，请重试。"), 'error')
    }
  }, [editable, CommandEditorDialog, preferences.detectServerSystem, setNotice])
  const focusSearch = () => {
    if (terminalVisible) { setPickerOpen(true); return }
    setShowHosts(true)
    setSearchRequest(value=>value+1)
  }
  const openTool = useCallback(async (next: Tool) => {
    const request = ++toolRequest.current
    setLoadingTool(next)
    try {
      if (next === 'identities' && !IdentityPanel) { const module = await loadIdentities(); if(request !== toolRequest.current)return; setIdentityPanel(()=>module.default) }
      if (next === 'settings' && !SettingsPanel) { const module = await loadSettings(); if (request !== toolRequest.current) return; setSettingsPanel(() => module.default) }
      if (next === 'commands' && !QuickCommandsPanel) { const module = await loadCommands(); if (request !== toolRequest.current) return; setQuickCommandsPanel(() => module.default) }
      if (next === 'groups' && !GroupManager) { const module = await loadGroupManager(); if (request !== toolRequest.current) return; setGroupManager(() => module.default) }
      if (request === toolRequest.current) {
        if(next==='groups')setGroupsOpen(true);else setView(next as WorkspaceView); setLoadingTool(null)
      }
    } catch { if (request === toolRequest.current) { setLoadingTool(null); setNotice(tr("工具加载失败，已保留当前内容，请重试。"), 'error') } }
  }, [SettingsPanel, QuickCommandsPanel, GroupManager, IdentityPanel])
  const updatePreferences = (next: Preferences) => setPreferences(normalizePreferences(next))
  const keyUsage = (id: string) => state.hosts.filter(host => host.keyId === id && host.authentication === 'key').length + state.sessions.filter(session => session.host.keyId === id && session.host.authentication === 'key').length + Object.values(ssh.entries).filter(entry => entry.keyId === id && (entry.phase === 'connecting' || entry.phase === 'hostkey' || entry.phase === 'connected')).length
  const removeKey = async (id: string) => {
    if (keyUsage(id)) { setNotice(tr("请先解除连接和会话中的密钥关联。")); return false }
    if (await keyReferences.forget(id)) { await savedWorkspace.reload(); setNotice(tr("密钥已移除，原文件未被修改。"), 'success'); return true }
    return false
  }
  const chooseKey = async () => { const key = await keyReferences.choose(); if (key) await savedWorkspace.reload(); return key }
  const closeGroups = () => { (document.getElementById('manage-groups')??document.getElementById('host-manager-more'))?.focus({preventScroll:true}); setGroupsOpen(false) }
  const createGroup = (parent?:string) => {if(!editable)return;setGroupCreateRequest(previous=>({version:previous.version+1,parent}));void openTool('groups')}
  const requestDelete = (value: NonNullable<typeof deletion>) => {
    const anchors=value.kind==='group'?[document.getElementById('new-host-group'),document.querySelector<HTMLElement>(`[data-group-filter-value="${CSS.escape('group:'+value.name)}"]`),document.getElementById('manage-groups')]:[document.getElementById('manager-host-search')]
    anchors.find(node=>node?.getClientRects().length)?.focus({preventScroll:true})
    setDeleteError('');setDeletion(value);setDeleteOpen(true)
  }
  const confirmDelete = async () => {
    if (!deleteOpen || !deletion || !editable) return
    const done = await savedWorkspace.apply(deletion.kind === 'host' ? { kind: 'deleteHost', id: deletion.host.id } : { kind: 'deleteGroup', name: deletion.name })
    if (done) { if (deletion.kind === 'host' && editor.host?.id === deletion.host.id && editor.open) closeEditor(); setDeleteOpen(false); setNotice(tr("配置已删除，已打开的会话保持原状。"), 'success'); requestAnimationFrame(() => workspaceFocusTarget()?.focus({preventScroll:true})) }
    else setDeleteError(tr("删除未完成，原有配置已保留，请查看配置提示后重试。"))
  }
  const sendCommand = (command: QuickCommand, targetId: string, expectedTransport?: TerminalTransport): string | undefined => {
    const target = state.sessions.find(session => session.id === targetId)
    const live = ssh.currentEntry(targetId)
    const controller = terminalControllers.current.get(targetId)
    if (!target || !controller || live?.phase !== 'connected' || (expectedTransport && expectedTransport !== live.transport)) {
      return tr("会话当前不可用，命令未发送，请重新选择或连接。")
    }
    const error = controller.sendCommand(command.command, live.transport)
    if (error) return error
    activateSession(target.id); showTerminal(target.id)
  }
  const resolveCommandTarget = (id: string, attempt: number): CommandTarget | undefined => {
    const live = ssh.entries[id]
    if (live?.phase !== 'connected' || live.attempt !== attempt || !state.sessions.some(session => session.id === id)) return undefined
    return {id, attempt, label:tr("发送到 {p0}",(sessionById.get(id)?.host.username??'')+'@'+(sessionById.get(id)?.host.name??'')), system:live.system}
  }
  const startSavedSession = useCallback((id:string, host:HostProfile) => {
    const credentials=savedConnectionCredentials(host)
    if(!credentials){setAuthTarget(id);return}
    setAuthTarget('')
    ssh.connect(id,host,credentials)
  },[ssh.connect])
  const openHost = useCallback((host: HostProfile, independent=false, parentGroupId?:string) => {
    const existing = independent ? undefined : hostSessions.preferred(host)
    const id = existing?.id ?? crypto.randomUUID()
    setSelectedId(host.id)
    setShowHosts(false)
    if(existing)dispatch({type:'activate',id});else dispatch(parentGroupId?{type:'workspaceTab',host,id,parentGroupId}:{type:'connect',host,id})
    const entry = ssh.currentEntry(id)
    if (entry?.retry) ssh.retryNow(id)
    else if (!entry || ['failed','disconnected'].includes(entry.phase)) {
      if (existing) dispatch({ type: 'updateSessionHost', id, host })
      startSavedSession(id, host)
    }
    showTerminal(id)
  }, [hostSessions, ssh.currentEntry, ssh.retryNow, startSavedSession, showTerminal])
  const activateSession = (id: string) => {
    const session = state.sessions.find(item => item.id === id)
    if (!session) return
    setSelectedId(session.host.id)
    setShowHosts(false)
    dispatch({ type: 'activate', id })
  }
  const activateWorkspacePane = (id: string, focus = true) => {
    hideWorkspaceTabs(id)
    activateSession(id)
    if (focus) requestFocus(id)
  }
  const connectSession = (id:string) => {
    const session=state.sessions.find(item=>item.id===id)
    if(!session||session.pending)return
    const entry=ssh.currentEntry(id)
    if(entry&&!['failed','disconnected'].includes(entry.phase))return
    if (!routeAvailable(id, session.host)) return
    activateSession(id)
    if (entry?.retry) ssh.retryNow(id)
    else startSavedSession(id,session.host)
    requestFocus(id)
  }
  const toggleSessionConnection = (id:string) => {
    const live = ssh.currentEntry(id)
    if (live && (live.retry || !['failed', 'disconnected'].includes(live.phase))) ssh.disconnect(id)
    else connectSession(id)
  }
  const closeSessionAuthentication = () => {
    if (ssh.currentEntry(state.activeId)?.phase === 'hostkey') ssh.disconnect(state.activeId)
    setAuthTarget('')
  }
  const openLogs = (id:string) => {
    activateSession(id)
    setDesktopViews(v=>hidePaneView(v,id));setFileWorkspaceViews(v=>hidePaneView(v,id))
    setDiffViews(v=>hidePaneView(v,id));setRemoteViews(v=>hidePaneView(v,id))
    setLogViews(v=>setPaneView(v,id,true))
  }
  const closeHostTab=(id:string)=>requestClose((spaces.get(id)?.panes??[]).map(s=>s.id),true)
  const locatePane=(id:string)=>{activateSession(id);showTerminal(id)}
  const openNewTerminal = (pane: typeof state.sessions[number], parentGroupId: string) => {
    if (!routeAvailable(pane.id, pane.host)) return
    const saved = savedWorkspace.getSnapshot().hosts.find(item => item.id === pane.host.id)
    openHost(saved && sameConnectionTarget(saved, pane.host) ? saved : pane.host, true, parentGroupId)
  }
  const moveToGroup=(paneId:string,workspaceId:string)=>{
    const group=groupFor(runtime,paneId),space=spaces.get(workspaceId)
    if(group&&space)dispatch({type:'regroup',groupIds:[group.id],workspaceId,name:space.name})
  }
  const orderTab=(paneId:string,targetId:string,after:boolean)=>{
    const from=groupFor(runtime,paneId),to=groupFor(runtime,targetId)
    if(!from||!to||from===to||!!(from.pinned||from.workspacePinned)!==!!(to.pinned||to.workspacePinned))return
    const target=spaces.get(to.workspaceId??to.id)!,source=spaces.get(from.workspaceId??from.id)!
    if(target!==source&&!from.pinned&&!from.workspacePinned){
      if(target.groups.length>1)moveToGroup(paneId,target.id)
      else if(source.groups.length>1)dispatch({type:'regroup',groupIds:[from.id],workspaceId:crypto.randomUUID(),name:from.name})
    }
    dispatch({type:'orderTab',id:from.id,target:to.id,after})
  }
  const terminalContextActions=(target:Element)=>{
    const id=target.closest<HTMLElement>('[data-terminal-context-pane]')?.dataset.terminalContextPane
    const session=id?sessionById.get(id):undefined
    if(!session)return undefined
    const layout=groupFor(runtime,session.id)
    if(!layout||!target.closest('[role="tablist"]')){
      if(session.pending)return undefined
      return [...hostCopyActions(session.host,setNotice),{label:tr("会话日志"),onClick:()=>openLogs(session.id)}]
    }
    const chosen=marked.includes('terminal-'+layout.id)?runtime.groups.filter(g=>marked.includes('terminal-'+g.id)).map(g=>g.id):[layout.id]
    const space=spaces.get(layout.workspaceId??layout.id)!
    const pinned=!!(layout.pinned||layout.workspacePinned)
    const peers=connectionTabs.filter(t=>t.terminalPaneId&&!!t.pinned===pinned).map(t=>t.terminalPaneId!)
    const index=peers.indexOf(layout.activeId)
    const previous=peers[index-1], next=index>=0?peers[index+1]:undefined
    const panes=leaves(layout.root)
    const otherGroups=[...spaces.values()].filter(w=>w!==space&&w.groups.length>1)
    const organize: ContextMenuOption[]=[]
    if(previous)organize.push({label:tr('向前移动标签'),onClick:()=>orderTab(layout.activeId,previous,false)})
    if(next)organize.push({label:tr('向后移动标签'),onClick:()=>orderTab(layout.activeId,next,true)})
    if(chosen.length>1)organize.push({label:tr('新建标签组'),onClick:()=>setRenaming({id:crypto.randomUUID(),name:space.name,groups:chosen})})
    if(otherGroups.length)organize.push({label:tr('移入标签组'),items:otherGroups.map(w=>({
      label:w.name,onClick:()=>dispatch({type:'regroup',groupIds:chosen,workspaceId:w.id,name:w.name}),
    }))})
    if(space.groups.length>1)organize.push(
      {label:tr('重命名标签组'),onClick:()=>setRenaming({id:space.id,name:space.name,groups:space.groups.map(g=>g.id)})},
      {label:tr('移出标签组'),onClick:()=>dispatch({type:'regroup',workspaceId:crypto.randomUUID(),groupIds:[layout.id],name:session.host.name})},
      {type:'divider'},
      {label:tr("关闭工作区 {p0}（{p1} 个终端）",space.name,space.panes.length),variant:'destructive',onClick:()=>closeHostTab(space.id)},
    )
    const actions: ContextMenuOption[]=[]
    if(!session.pending)actions.push(
      {label:tr("新建终端"),onClick:()=>openNewTerminal(session,layout.id)},
      {label:chosen.length>1?tr('组合为分屏'):tr('分屏'),onClick:()=>openPreview(chosen)},
    )
    if(panes.length>1)actions.push({label:tr('切换窗格'),items:panes.map(id=>({label:paneLabel(id),onClick:()=>locatePane(id)}))})
    actions.push(
      {label:tr('重命名…'),onClick:()=>setRenaming({id:layout.id,name:layoutTitle(layout)})},
      {label:pinned?tr('取消固定标签页'):tr('固定标签页'),onClick:()=>dispatch({type:'pinGroup',groupId:layout.id,pinned:!pinned})},
    )
    if(organize.length)actions.push({label:tr('整理标签'),items:organize})
    actions.push({type:'divider'},{label:tr('关闭标签页'),variant:'destructive',onClick:()=>requestClose(panes,true)})
    return actions
  }
  const runtimeLatest = useRef(runtime)
  runtimeLatest.current = runtime

  const releaseSessions = useHostAction((ids: string[]) => {
    const removed = new Set(ids)
    let remaining = runtime
    for (const id of removed) {
      if (!remaining.sessions.some(session => session.id === id)) continue
      forgetFileTrees(id)
      usedAttempts.current.delete(id)
      submittedUsedAttempts.current.delete(id)
      savedIconAttempts.current.delete(id)
      submittedIconAttempts.current.delete(id)
      ssh.remove(id)
      const action = { type: 'close' as const, id }
      remaining = terminalWorkspaceReducer(remaining, action)
      dispatch(action)
    }
    const withoutRemoved = <T,>(previous: Record<string, T>) => {
      const next = { ...previous }
      for (const id of removed) delete next[id]
      return next
    }
    setDesktopViews(withoutRemoved)
    setDesktopLoaded(withoutRemoved)
    setPaths(withoutRemoved)
    setFileWorkspaceViews(withoutRemoved)
    setRemoteViews(withoutRemoved)
    setLogViews(withoutRemoved)
    setDiffRequests(withoutRemoved)
    setDiffViews(withoutRemoved)
    for (const doc of remoteRequests) if (removed.has(doc.sessionId)) markDocumentDirty(doc.id, false)
    setRemoteRequests(previous => previous.filter(doc => !removed.has(doc.sessionId)))
    const next = remaining.sessions.find(session => session.id === remaining.activeId)
    setSelectedId(next?.host.id ?? '')
    setAuthTarget(target => !next || removed.has(target) ? '' : target)
    if (!next) setShowHosts(true)
    const focusId = hostsVisible || !next ? 'tab-hosts' : 'tab-terminal-' + (groupFor(remaining, next.id)?.id ?? next.id)
    requestAnimationFrame(() => document.getElementById(focusId)?.focus())
  })

  const closeParticipants = useHostAction((ids: string[]): CloseParticipant[] => {
    return ids.flatMap(id => {
      const session = sessionById.get(id)
      if (!session) return []
      const live = ssh.panes.read(id)
      const guards = new Map<string, FileCloseGuard>()
      for (const [key, item] of fileCloseGuards.current) {
        if (item.sessionId === id) guards.set(key, item.guard)
      }
      for (const doc of remoteRequests) {
        if (doc.sessionId !== id) continue
        const controller = remoteControllers.current.get(doc.id)
        if (controller) guards.set('document:' + doc.id, {
          getCloseState: controller.getCloseState,
          saveForClose: controller.saveForClose,
          reveal: () => revealDocument(doc),
        })
      }
      return [{ id, name: session.label || session.host.name, attempt: live?.attempt ?? 0, guards,
        forwards: Object.entries(live?.forwards ?? {}).filter(([, f]) => ['running','starting'].includes(f.state)).map(([key]) => key) }]
    })
  })
  const [{ review: closing, busy: closeBusy, error: closeError }, setCloseState] =
    useState<SessionCloseState>({ review: null, busy: false, error: '' })
  const [closer] = useState(() => createSessionCloser({
    files: fileGateway, participants: closeParticipants, release: releaseSessions,
    publish: setCloseState, report: message => setNotice(message, 'error'),
  }))
  useEffect(() => closer.reset, [closer])
  const requestClose = closer.request
  function closeSession(id: string) { void requestClose([id], false) }
  function detachPane(id:string){dispatch({type:'detach',id,groupId:crypto.randomUUID()});setView('terminal');requestFocus(id);closer.dismiss()}
  function splitPane(axis:SplitAxis,id=state.activeId,connectSame=false){
    const group=groupFor(runtime,id),source=state.sessions.find(s=>s.id===id)
    if(!source||source.pending||!group||leaves(group.root).length>=8)return
    if(workspaceGeometry.current.focusOnly||!canSplit(workspaceGeometry.current.panes.get(id),axis)){setNotice(tr("当前空间不足，请收起侧栏、调整排列或扩大窗口后分屏。"));return}
    if (connectSame && !routeAvailable(id, source.host)) return
    const newId=crypto.randomUUID()
    dispatch({type:'split',id,newId,nodeId:crypto.randomUUID(),axis});setView('terminal')
    if(connectSame){
      const snapshot={...source.host,forwards:source.host.forwards?.map(rule=>({...rule,enabled:false}))}
      dispatch({type:'attach',id:newId,host:snapshot})
      startSavedSession(newId,snapshot);requestFocus(newId)
    }
  }
  function attachPane(id:string,host:HostProfile,sameHost:boolean){
    const pending=state.sessions.find(s=>s.id===id);if(!pending?.pending)return
    if (sameHost && !routeAvailable(id, host)) return
    if (!sameHost) hostSessions.selectRoute(id, host)
    const snapshot={...host,forwards:sameHost?host.forwards?.map(rule=>({...rule,enabled:false})):host.forwards}
    dispatch({type:'attach',id,host:snapshot});dispatch({type:'activate',id});setView('terminal')
    startSavedSession(id,snapshot)
    requestFocus(id)
  }
  function openRemoteDesktop(){
    const id=state.activeId
    setDesktopLoaded(v=>setPaneView(v,id,true))
    setFileWorkspaceViews(v=>hidePaneView(v,id));setRemoteViews(v=>hidePaneView(v,id));setLogViews(v=>hidePaneView(v,id));setDiffViews(v=>hidePaneView(v,id));setDesktopViews(v=>setPaneView(v,id,true))
  }
  function openFileWorkspace(id:string){
    setDesktopViews(v=>hidePaneView(v,id))
    setRemoteViews(v=>hidePaneView(v,id));setLogViews(v=>hidePaneView(v,id));setDiffViews(v=>hidePaneView(v,id));setFileWorkspaceViews(v=>setPaneView(v,id,true))
  }
  function openComparison(sessionId:string,id:string,relative:string){setDesktopViews(v=>hidePaneView(v,sessionId));setFileWorkspaceViews(v=>hidePaneView(v,sessionId));setDiffRequests(v=>({...v,[sessionId]:{id,relative}}));setDiffViews(v=>setPaneView(v,sessionId,true));setRemoteViews(v=>hidePaneView(v,sessionId));setLogViews(v=>hidePaneView(v,sessionId));setView('terminal')}
  function revealDocument(doc:{id:string;sessionId:string}){
    const id=doc.sessionId
    activateSession(id)
    setDesktopViews(v=>hidePaneView(v,id));setFileWorkspaceViews(v=>hidePaneView(v,id))
    setDiffViews(v=>hidePaneView(v,id));setLogViews(v=>hidePaneView(v,id))
    setRemoteViews(v=>setPaneView(v,id,doc.id))
  }
  function openRemoteText(sessionId:string,path:string){
    if(ssh.entries[sessionId]?.phase!=='connected')return
    const host=sessionById.get(sessionId)?.host
    const existing=remoteRequests.find(doc=>{
      const owner=sessionById.get(doc.sessionId)?.host
      return groupFor(runtime,doc.sessionId)?.workspaceId===groupFor(runtime,sessionId)?.workspaceId&&doc.path===path&&(doc.sessionId===sessionId||!!host&&!!owner&&sameConnectionTarget(host,owner)&&hostSessions.hopsMatch(doc.sessionId,host)&&hostSessions.hopsMatch(sessionId,host))
    })
    if(existing){revealDocument(existing);return}
    if(remoteRequests.length>=8){setNotice(tr("最多同时编辑 8 个远程文件，请先关闭不再使用的文件。"));return}
    const doc={id:crypto.randomUUID(),sessionId,path}
    setRemoteRequests(previous=>[...previous,doc])
    revealDocument(doc)
  }
  function closeDocument(doc:(typeof remoteRequests)[number]){
    setRemoteRequests(previous=>previous.filter(item=>item.id!==doc.id))
    markDocumentDirty(doc.id,false)
    if(remoteViews[doc.sessionId]===doc.id){
      setRemoteViews(previous=>({...previous,[doc.sessionId]:false}))
      setFileWorkspaceViews(previous=>({...previous,[doc.sessionId]:true}))
      if(state.activeId===doc.sessionId)requestAnimationFrame(()=>document.getElementById('tab-files-'+doc.sessionId)?.focus({preventScroll:true}))
    }
  }
  async function performShellOperation(id: string, transport: SSHTransport, operation: ShellOperation) {
    const entry = ssh.currentEntry(id)
    if (entry?.phase !== 'connected' || entry.transport !== transport) throw Error(tr('SSH 连接已断开。'))
    if (operation.kind === 'directory') {
      await transport.changeDirectory(operation.path)
      if (runtimeLatest.current.activeId === id) showTerminal(id)
    } else if (operation.kind === 'upload') {
      await transport.startZmodemUpload?.()
    }
  }
  async function requestShellOperation(id: string, operation: ShellOperation) {
    const entry = ssh.currentEntry(id)
    if (entry?.phase !== 'connected') throw Error(tr('SSH 连接已断开。'))
    if (operation.kind === 'manage' || !entry.directory?.integrated) {
      activateSession(id)
      showTerminal(id, false)
      setIntegration({ id, transport: entry.transport, operation })
      return
    }
    await performShellOperation(id, entry.transport, operation)
  }
  const manageShellIntegration = (id: string) => {
    void requestShellOperation(id, {kind:'manage'}).catch(cause => setNotice(localizeMessage(cause.message), 'error'))
  }
  const integrationEntry = usePaneSSH(ssh.panes, integration?.id ?? '')
  const integrationOpen = !!integration && state.activeId === integration.id && integrationEntry?.phase === 'connected' &&
    integrationEntry.transport === integration.transport
  const closeIntegration = () => {
    setIntegration(current => current === integration ? null : current)
    if (integration && runtimeLatest.current.activeId === integration.id && ssh.currentEntry(integration.id)?.transport === integration.transport)
      requestFocus(integration.id)
  }
  useEffect(() => {
    if (integration && !integrationOpen) setIntegration(null)
  }, [integration, integrationOpen])
  const requestZmodemUpload = useHostAction((id: string) => requestShellOperation(id, {kind:'upload'}))
  async function saveHost(host: HostProfile, credentials?: Credentials, passwordSave?:ConnectionPasswordSave, expectedRevision?: number) {
    const receipt = await savedWorkspace.saveHost(host, expectedRevision, passwordSave)
    if (!receipt) return false
    if(passwordSave?.privateKey||passwordSave?.keyToken||passwordSave?.generatedKeyToken)await keyReferences.reload()
    setSelectedId(hostsVisible ? host.id : activeSession?.host.id ?? host.id)
    savedHostToReveal.current = hostsVisible && !credentials ? host.id : undefined
    if (!savedWorkspace.native || (!hostsVisible && !credentials)) {
      setNotice(tr("已保存 {p0}{p1}", host.name, savedWorkspace.native ? '' : tr(" · 浏览器演示，刷新后重置")), 'success')
    }
    closeEditor()
    if (credentials) {
      const committed = committedConnection(host.id, receipt.hosts, savedWorkspace.getSnapshot().hosts)
      if (!committed) {
        setNotice(tr('连接路径已变化，请从连接列表重新打开。'), 'error')
        return true
      }
      const id = crypto.randomUUID()
      setSelectedId(host.id); setShowHosts(false)
      dispatch({ type: 'connect', host:committed, id })
      setAuthTarget('')
      try { ssh.connect(id, committed, savedConnectionCredentials(committed)??credentials); requestFocus(id) }
      catch { setNotice(tr("配置已保存，但连接未启动，请在认证面板重试。"), 'error') }
    }
    return true
  }

  async function authenticateSession(sessionId:string, host:HostProfile, credentials:Credentials){
    const snapshot=savedWorkspace.getSnapshot()
    const saved=snapshot.hosts.find(item=>item.id===host.id)
    if (!hostSessions.hopsMatch(sessionId, host)) throw Error(tr('连接路径已变化，请从连接列表重新打开。'))
    if(!saved){ssh.connect(sessionId,host,credentials);requestFocus(sessionId);return true}
    if (!sameConnectionTarget(saved, host)) {
      throw Error(tr("此连接的已保存认证已变化，请重新打开连接。"))
    }
    const identity=identities.items.find(item=>item.id===credentials.identityId)
    const candidate={...saved,authentication:credentials.authentication,username:identity?.username??saved.username,identityId:credentials.identityId,
      keyId:credentials.keyId,useAgent:credentials.useAgent,timeoutSeconds:credentials.timeoutSeconds}
    const auth:ConnectionPasswordSave={rememberPassword:credentials.authentication==='password'&&!credentials.identityId,password:credentials.password,
      privateKey:credentials.privateKey,passphrase:credentials.passphrase,keyToken:credentials.temporaryKeyToken}
    const receipt = await savedWorkspace.saveHost(candidate, snapshot.revision, auth)
    if (!receipt) return false
    if(auth.privateKey||auth.keyToken)await keyReferences.reload()
    const committed = committedConnection(host.id, receipt.hosts, savedWorkspace.getSnapshot().hosts)
    if (!committed) throw Error(tr('连接路径已变化，请从连接列表重新打开。'))
    if (!runtimeLatest.current.sessions.some(item=>item.id===sessionId)) return false
    dispatch({type:'updateSessionHost',id:sessionId,host:committed})
    ssh.connect(sessionId,committed,savedConnectionCredentials(committed)??credentials)
    requestFocus(sessionId)
    return true
  }

  useEffect(() => { if (hostGroup !== 'all' && !groups.includes(hostGroup.slice(6))) setHostGroup('all') }, [groups, hostGroup])
  useEffect(() => { if (!hostsVisible) setGroupsOpen(false) }, [hostsVisible])

  useEffect(() => {
    let disposed = false
    getApplicationInfo().catch(() => { if (!disposed) setNotice(tr("桌面服务暂不可用"), 'error') })
    return () => { disposed = true }
  }, [setNotice])
  useEffect(() => {
    if (!nativePreferences) setBrowserPreferencesSaved(savePreferences(browserPreferences))
  }, [browserPreferences, nativePreferences])
  useEffect(() => {
    if (nativePreferences) void setLanguagePreference(preferences.language)
  }, [nativePreferences, preferences.language])
  useEffect(() => {
    const changeFontSize = (step: number) => setPreferences(previous => {
      const fontSize = stepTerminalFontSize(previous.fontSize, step)
      return fontSize === previous.fontSize ? previous : { ...previous, fontSize }
    })
    const onWheel = (event: WheelEvent) => {
      const overTerminal = event.ctrlKey && event.target instanceof Element &&
        event.target.closest('[data-terminal-font-wheel]') !== null
      handleTerminalFontWheel(event, overTerminal, changeFontSize)
    }
    window.addEventListener('wheel', onWheel, { capture: true, passive: false })
    return () => window.removeEventListener('wheel', onWheel, true)
  }, [])
  useEffect(() => { void setInterfaceLanguage(getLanguage()).catch(() => setNotice(tr('语言设置未能应用到原生对话框，请重新选择后重试。'), 'error')) }, [getLanguage()])
  useEffect(() => {if(fileGateway.available())void fileGateway.configureTransfers(preferences.transferConcurrency).catch(()=>setNotice(tr("传输并发设置未生效，请重新选择后重试。"), 'error'))},[preferences.transferConcurrency])
  useEffect(() => {
    let disposed = false
    setWindowAppearance(mode).catch(() => { if (!disposed) setNotice(tr("窗口按钮主题同步失败，请重新切换主题"), 'error') })
    return () => { disposed = true }
  }, [mode])
  useEffect(() => {
    if (nativePreferences) return
    const sync = (event: StorageEvent) => { if (event.key === previewPreferencesKey) setBrowserPreferences(readPreferences()) }
    window.addEventListener('storage', sync)
    return () => window.removeEventListener('storage', sync)
  }, [nativePreferences])
  const showManagementShortcuts=desktopAvailable()
  const shortcutOverrides = useShortcutOverrides()
  const openManagementPage=(next:ManagementPage)=>{if(next==='hosts')setShowHosts(true);else void openTool(next)}
  const handleGlobalKeydown = (event: KeyboardEvent) => {
    if (recordShortcutEvent(event) || event.defaultPrevented) return
    const target = event.target instanceof Element ? event.target : null
    const modalOpen = !!document.querySelector('dialog[open]')
    if(showManagementShortcuts){
      const blocked=authOpen||modalOpen||(terminalVisible&&!!desktopViews[state.activeId])||
        !!target?.closest('.xterm, input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="menu"], [role="listbox"]')
      const destination=!blocked && findShortcut(event,managementPages)
      if(destination){
        event.preventDefault();event.stopPropagation()
        if(!event.repeat){openManagementPage(destination);requestAnimationFrame(()=>document.getElementById('tab-'+destination)?.focus({preventScroll:true}))}
        return
      }
    }
    if(terminalVisible&&!modalOpen){
      const field=target?.closest('input,textarea,[contenteditable="true"]'),terminalField=target?.closest('.xterm')
      if(!field||terminalField){
        const paneAction=findShortcut(event,['next-pane','previous-pane','split-right','split-down','maximize-pane'])
        if(paneAction){
          event.preventDefault();event.stopPropagation()
          if(event.repeat)return
          if(paneAction==='next-pane'||paneAction==='previous-pane'){
            if(activeGroup){const ids=leaves(activeGroup.root),index=ids.indexOf(state.activeId),id=ids[(index+(paneAction==='previous-pane'?-1:1)+ids.length)%ids.length]!;activateWorkspacePane(id)}
          }else if(paneAction==='maximize-pane'){
            if(activeGroup&&(!workspaceGeometry.current.spaceLimited||activeGroup.maximized))dispatch({type:'maximize',groupId:activeGroup.id})
          }else splitPane(paneAction==='split-right'?'horizontal':'vertical')
          return
        }
      }
    }
    const shortcut=showManagementShortcuts && findShortcut(event,['new-tab','close-tab'])
    if (shortcut) {
      event.preventDefault();event.stopPropagation()
      if (modalOpen || authOpen || event.repeat) return
      if (shortcut==='new-tab') {
        if (terminalVisible) newTerminal?.()
        else void openEditor(undefined,document.getElementById('new-connection')??undefined)
      }
      else if (terminalVisible) connectionTabs.find(tab=>tab.id===selectedConnectionTab)?.onClose?.()
      return
    }
    const action = findShortcut(event, ['settings','host-search','new-connection'])
    if (!action) return
    event.preventDefault();event.stopPropagation()
    if (modalOpen || event.repeat) return
    if (action === 'settings') { openTool('settings'); requestAnimationFrame(() => document.getElementById('open-settings')?.focus()) }
    else if (action === 'host-search') focusSearch()
    else openEditor()
  }
  const globalKeydownRef = useRef(handleGlobalKeydown)
  globalKeydownRef.current = handleGlobalKeydown
  useEffect(() => {
    const listener = (event: KeyboardEvent) => globalKeydownRef.current(event)
    window.addEventListener('keydown', listener, true)
    return () => window.removeEventListener('keydown', listener, true)
  }, [])

  const newTerminal=activeSession&&activeGroup&&!activeSession.pending?()=>openNewTerminal(activeSession,activeGroup.id):undefined
  const hostDocuments=remoteRequests
  const documentName=(path:string)=>path.split('/').at(-1)??path
  const tabSpace = (paneId: string) => {
    const group = groupFor(runtime, paneId)
    const space = group && spaces.get(group.workspaceId ?? group.id)
    const pane = sessionById.get(paneId)
    return {
      groupId: space?.id,
      groupName: space?.name,
      onNewTerminal: group && pane && !pane.pending ? () => openNewTerminal(pane, group.id) : undefined,
    }
  }
  const connectionTabs:ConnectionTab[]=runtime.groups.map(group=>{
    const panes=leaves(group.root).map(id=>sessionById.get(id)!)
    const space=spaces.get(group.workspaceId??group.id)!
    const primary=sessionById.get(group.activeId)!
    const label=!group.customName&&space.name===group.name&&space.groups.length>1?layoutTitle(group):group.name
    return {id:'terminal-'+group.id,label,...tabSpace(primary.id),layout:group.root,terminalPaneId:group.activeId,icon:primary.host.icon,count:panes.length,
      pinned:group.pinned||!!group.workspacePinned,disconnected:panes.every(p=>p.connection?.status==='disconnected'),streaming:panes.some(p=>ssh.outputPanes.has(p.id)),
      detail:panes.map(p=>p.pending?tr('待连接'):p.host.username+'@'+p.host.address).join(' · '),
      onSelect:()=>locatePane(group.activeId),onClose:()=>requestClose(leaves(group.root),true)}
  })
  const fileSessions=state.sessions.filter(s=>!s.pending&&fileWorkspaceViews[s.id]!==undefined)
  for(const session of fileSessions){
    const id=session.id, siblings=fileSessions.filter(s=>s.host.id===session.host.id)
    const suffix=siblings.length>1?' · '+(siblings.indexOf(session)+1):''
    connectionTabs.push({...tabSpace(id),id:'files-'+id,label:tr("文件 · {p0}",session.host.name)+suffix,detail:paneLabel(id),
      onSelect:()=>{activateSession(id);openFileWorkspace(id)},onClose:()=>{
        const guard=[...fileCloseGuards.current.values()].find(item=>{
          const status = item.guard.getCloseState()
          return item.sessionId === id && (status.busy || status.dirty || status.syncing)
        })
        if(guard){activateSession(id);openFileWorkspace(id);guard.guard.reveal();return}
        if(filesSelected&&state.activeId===id)showTerminal(id)
        setFileWorkspaceViews(previous=>{const next={...previous};delete next[id];return next})
      }})
  }
  for(const doc of hostDocuments){
    const name=documentName(doc.path)
    const label=hostDocuments.some(other=>other!==doc&&documentName(other.path)===name)?(sessionById.get(doc.sessionId)?.host.name??'')+' · '+doc.path:name
    connectionTabs.push({...tabSpace(doc.sessionId),id:'document-'+doc.id,label,detail:doc.path,dirty:dirtyDocuments[doc.id],
      onSelect:()=>revealDocument(doc),onClose:()=>{
        const controller=remoteControllers.current.get(doc.id)
        if(controller)controller.guardClose(()=>closeDocument(doc));else closeDocument(doc)
      }})
  }
  const selectedConnectionTab=selectedDocument?'document-'+selectedDocument:filesSelected?'files-'+state.activeId:'terminal-'+activeGroup?.id
  const managementVisible=hostsVisible||view==='identities'||view==='commands'
  const toggleHostGroups=useHostAction(()=>{if(groupsOpen)closeGroups();else void openTool('groups')})
  const applyHostChange=useHostAction(savedWorkspace.apply)
  const openManagedHost=useHostAction(openHost)
  const editManagedHost=useHostAction(openEditor)
  const createManagedHost=useHostAction((trigger:HTMLElement)=>void openEditor(undefined,trigger))
  const importHosts=useCallback(()=>{setImportLoaded(true);setImportOpen(true)},[])
  const openHostRecovery=useCallback(()=>{setHostRecoveryLoaded(true);setHostRecoveryOpen(true)},[])
  const restoreDeletedHost=useHostAction(async(backupId:string,hostId:string,revision:number)=>{
    await savedWorkspace.restoreDeletedHost(backupId,hostId,revision)
    setSelectedId(hostId)
    setHostGroup('all')
    setResetHostQuery(value => value + 1)
    setNotice(tr('已恢复主机。'), 'success')
  })
  const quickConnectHost=useHostAction(()=>void openEditor(undefined,document.getElementById('new-connection')??undefined,true))
  const pinManagedHost=useHostAction((host:HostProfile)=>savedWorkspace.apply({kind:'pinHost',id:host.id,pinned:!host.pinned}))
  const copyManagedHost=useHostAction(async(host:HostProfile)=>{if(await savedWorkspace.apply({kind:'duplicateHost',id:host.id}))setNotice(tr('已创建「{p0}」的配置副本。',host.name), 'success')})
  const deleteManagedHost=useHostAction((host:HostProfile)=>requestDelete({kind:'host',host}))
  const deleteManagedGroup=useHostAction((name:string)=>requestDelete({kind:'group',name}))
  const closeManagedGroups=useHostAction(closeGroups)
  const createManagedGroup=useHostAction(createGroup)
  const openHostSettings=useHostAction(()=>void openTool('settings'))
  const globalContextActions = {
    newConnection: {label:tr('新建连接'),isDisabled:!editable,onClick:()=>void editManagedHost(undefined,document.getElementById('new-connection')??undefined)},
    settings: {label:tr('设置'),onClick:openHostSettings},
  }
  const chooseManagedPane = useHostAction(locatePane)
  const managedPaneLabel = useHostAction(paneLabel)
  const hostMenuItems = useCallback((host: HostProfile) => {
    const sessions = (hostSessions.byHost.get(host.id) ?? []).filter(pane => pane.connection?.status === 'connected')
    return [
      ...(sessions.length > 1 ? [{
        label: tr('选择会话'),
        items: sessions.map((pane, i) => ({
          id: pane.id,
          label: tr('会话 {p0} · {p1}', i + 1, pane.label || managedPaneLabel(pane.id)),
          onClick: () => chooseManagedPane(pane.id),
        })),
      }] : []),
      { label: tr('编辑连接'), isDisabled: !editable, onClick: () => editManagedHost(host, document.getElementById('manager-more-' + host.id) ?? undefined) },
      ...hostCopyActions(host, setNotice),
      { label: tr('创建配置副本'), isDisabled: !editable, onClick: () => copyManagedHost(host) },
      { type: 'divider' as const },
      { label: tr('删除连接配置'), variant: 'destructive' as const, isDisabled: !editable, onClick: () => deleteManagedHost(host) },
    ]
  }, [hostSessions.byHost, chooseManagedPane, managedPaneLabel, editable, editManagedHost, setNotice, copyManagedHost, deleteManagedHost, language])
  const groupPanelLabel=tr("主机分组管理")
  const hostGroupPanel=useMemo(()=><AuxiliaryPanel id="host-groups-panel" label={groupPanelLabel} open={groupsOpen}>
    {GroupManager&&<GroupManager kind="host" groups={groups} revision={savedWorkspace.snapshot.revision} open={groupsOpen} createRequest={groupCreateRequest.version?groupCreateRequest:undefined} busy={!editable} onApply={applyHostChange} onDelete={deleteManagedGroup} onClose={closeManagedGroups}/>}
  </AuxiliaryPanel>,[groupPanelLabel,groupsOpen,GroupManager,groups,savedWorkspace.snapshot.revision,groupCreateRequest,editable,applyHostChange,deleteManagedGroup,closeManagedGroups])
  const hostGroupActions=useCallback((name?:string)=>[
    {label:tr('新建分组'),isDisabled:!editable,onClick:()=>createManagedGroup()},
    ...(name&&groups.includes(name)?[
      {label:tr('创建子分组'),isDisabled:!editable||name.split('/').length>=8,onClick:()=>createManagedGroup(name)},
      {label:tr('删除分组'),variant:'destructive' as const,isDisabled:!editable,onClick:()=>deleteManagedGroup(name)},
    ]:[]),
    {type:'divider' as const},
    {label:tr('新建连接'),isDisabled:!editable,onClick:()=>void editManagedHost()},
  ],[editable,groups,createManagedGroup,deleteManagedGroup,editManagedHost])


  const showPaneTerminal = useHostAction(showTerminal)
  const connectPane = useHostAction(connectSession)
  const closePaneLogs = useCallback((id: string) => {
    setLogViews(previous => ({ ...previous, [id]: false }))
    requestFocus(id)
  }, [requestFocus])
  const closePaneDiff = useCallback((id: string) => {
    setDiffRequests(previous => { const next = { ...previous }; delete next[id]; return next })
    setDiffViews(previous => ({ ...previous, [id]: false }))
    requestFocus(id)
  }, [requestFocus])

  const toolbarSpace=managementVisible
    ? <Stack ref={setManagementCaption} style={{minWidth:0,flex:'1 1 0'}}/>
    : <StackItem id={tabsOnTop?undefined:'window-drag-region'} size="fill" crossAlignSelf="stretch" style={{minWidth:'var(--spacing-6)'}} aria-hidden="true" />
  const pageToolbar=(
    <Stack direction="horizontal" align="center" gap={2} paddingInline={tabsOnTop?2:0}
      style={{minWidth:0,flexShrink:0,minHeight:tabsOnTop
        ? managementVisible?'calc(var(--size-element-md) + 2 * var(--spacing-1-5))':'var(--spacing-10)'
        : titlebarHeight,background:'var(--color-workspace-frame)'}}>
      <SessionRailHeader/>
      {terminalVisible&&activeSession&&<Stack direction="horizontal" align="center" gap={1} style={{flexShrink:0}}>
        <Button id="tab-connection-files" type="button" label={tr("文件管理")} size="sm"
          variant="ghost" aria-controls={filesSelected?'panel-files-'+state.activeId:undefined}
          isDisabled={!!activeSession.pending} onClick={()=>openFileWorkspace(activeSession.id)}/>
        <SessionCommandPicker commands={commands} filterBySystem={preferences.detectServerSystem} target={activeSSH ? resolveCommandTarget(activeSession.id, activeSSH.attempt) : undefined}
          resolveTarget={resolveCommandTarget} editable={editable}
          onAdd={system => void openQuickCommandEditor(system)}
          onManage={() => {
            void openTool('commands').then(() => {
              requestAnimationFrame(() => {
                const target = [document.getElementById('command-search'), document.getElementById('tab-commands')]
                  .find(node => node && node.getClientRects().length)
                target?.focus({ preventScroll: true })
              })
            })
          }}
          onSend={(command, target) => {
            const live = ssh.currentEntry(target.id)
            if (live?.phase !== 'connected' || live.attempt !== target.attempt || !commandAvailable(command, preferences.detectServerSystem, live.system)) {
              return tr("当前终端不可用，请重新连接后选择。")
            }
            return sendCommand(command, target.id, live.transport)
          }}/>
      </Stack>}
      {toolbarSpace}
      {terminalVisible&&<Stack ref={setWindowTools} direction="horizontal" align="center" style={{flexShrink:0}}/>}
    </Stack>
  )
  return <BuiltInIconStyleContext value={preferences.iconStyle}><Theme theme={interfaceTheme} mode={appearance}>
    <AppContextMenu fill actionsForTarget={terminalContextActions} actions={target =>
      hostRowContextActions(target, state.hosts, globalContextActions, hostMenuItems)}>
    {import.meta.env.MODE==='browser'&&<BrowserStatus/>}
    <SessionRailProvider hasNewTerminal={!!newTerminal} top={tabsOnTop} onPosition={changeTabPosition} viewportWidth={viewportWidth} pinned={railPinned} startWidth={sizes.start} endWidth={sizes.end}
      desiredMinimum={desiredMinimum} surface={railSurface} onPinnedChange={setRailPinned} onCommitWidth={commitRailWidth}
      pickerOpen={pickerOpen} onPickerOpen={setPickerOpen} onConnect={host=>openHost(host,true)}
      onCreate={trigger=>void openEditor(undefined,trigger??undefined)}
      renderTabs={(top,compact,railWidth,onMenuOpenChange)=>top&&!connectionTabs.length?null:<AppContextMenu label={tr("会话更多操作")} actionsForTarget={terminalContextActions} onOpenChange={onMenuOpenChange}><ConnectionTabs tabs={connectionTabs} selected={terminalVisible?selectedConnectionTab:''} showShortcuts={showManagementShortcuts}
        onGroup={moveToGroup} onOrder={orderTab} onCombine={()=>openPreview(runtime.groups.filter(g=>marked.includes('terminal-'+g.id)).map(g=>g.id))}
        railWidth={railWidth} top={top} compact={compact} marked={marked} onMark={setMarked} collapsed={collapsed} onCollapse={id=>setCollapsed(old=>old.includes(id)?old.filter(k=>k!==id):[...old,id])} onNewTerminal={newTerminal}/></AppContextMenu>}
      hosts={state.hosts} pinOrder={savedWorkspace.snapshot.pinOrder??emptyPinOrder} editable={editable}
      view={view} showShortcuts={showManagementShortcuts} onView={openManagementPage}
      preferences={preferences} systemMode={systemMode} onAppearance={patch=>setPreferences(previous=>normalizePreferences({...previous,...patch}))}
      onHistory={()=>{setSyncHistoryRequest(request=>request+1);void openTool('settings')}}
      onSettings={()=>void openTool('settings')} updateAvailable={updateAvailable} settingsLoading={loadingTool==='settings'}
      shortcutOverrides={shortcutOverrides}>
    <Stack ref={railSurface} id="workbench-surface" height="100dvh" style={{ ...fill, background: 'var(--color-background-body)' }}>
      <Layout height="fill" header={<SessionRailCaption>
        {tabsOnTop?<TopSessionTabs/>:pageToolbar}
      </SessionRailCaption>} content={<LayoutContent padding={0} style={fill} isScrollable={false} role="main" aria-label={tr("Yaare 工作台")} aria-busy={loadingTool!==null}>
        <Stack style={fill}>
          {tabsOnTop&&pageToolbar}
          {(savedWorkspace.loading || savedWorkspace.error || savedWorkspace.warning) && <Stack padding={3} gap={2} style={{ flexShrink: 0 }}>
            <Text type="supporting" role={savedWorkspace.error ? 'alert' : 'status'}>{savedWorkspace.loading ? tr("正在读取本机配置…") : savedWorkspace.error || savedWorkspace.warning}</Text>
            {!savedWorkspace.ready && !savedWorkspace.loading && <Stack direction="horizontal" gap={2}>
              <Button label={tr("重新读取配置")} size="sm" variant="secondary" isDisabled={savedWorkspace.busy} onClick={async () => { if (await savedWorkspace.reload(true)) await keyReferences.reload() }} />
              {savedWorkspace.canRecover && <Button label={tr("从最近备份恢复")} size="sm" isDisabled={savedWorkspace.busy} isLoading={savedWorkspace.busy} onClick={async () => { await savedWorkspace.recover(); await keyReferences.reload(); if (nativePreferences) await desktopPreferences.refresh() }} />}
            </Stack>}
          </Stack>}
        <StackItem size="fill" style={fill}>
        <Layout height="fill" start={<>
          <SessionRail/>
        </>} content={<LayoutContent padding={0} isScrollable={false} style={fill}>
          <Layout height="fill" content={<LayoutContent padding={0} isScrollable={false} style={fill}>
            <Stack height="100%" style={fill}>
              <HostManager pinOrder={savedWorkspace.snapshot.pinOrder} active={hostsVisible} caption={hostsVisible?managementCaption:null} hosts={state.hosts} detectedSystems={detectedSystems} groups={groups} connectedHostIds={hostSessions.connectedAnywhere} directHostIds={directHostIds} selectedId={selectedId}
                editable={editable} storageReady={savedWorkspace.ready}
                onApply={applyHostChange} revision={savedWorkspace.snapshot.revision} identities={identities.items} storageError={localizeMessage(savedWorkspace.error)}
                onImport={importHosts}
                onRestoreDeleted={savedWorkspace.native?openHostRecovery:undefined}
                onQuickConnect={quickConnectHost}
                onManageGroups={toggleHostGroups} groupsOpen={groupsOpen}
                auxiliary={hostGroupPanel}
                onPin={pinManagedHost} menuItems={hostMenuItems} onSelectHost={setSelectedId}
                savedRow={savedHostRow} onSavedRowShown={clearSavedHostRow} searchRequest={searchRequest} resetQuery={resetHostQuery} group={hostGroup} onGroup={setHostGroup}
                onOpen={openManagedHost} onEdit={editManagedHost} onCreate={createManagedHost} groupContextActions={hostGroupActions}/>
              <ToolSurface id="identities" mainView active={view==='identities'}>{IdentityPanel&&<IdentityPanel store={identities} active={view==='identities'} caption={view==='identities'?managementCaption:null} hosts={state.hosts} keys={keyReferences.keys} keyBusy={keyReferences.busy} keyError={localizeMessage(keyReferences.error)} keyUsage={keyUsage} onChooseKey={chooseKey} onForgetKey={removeKey} onKeysChanged={keyReferences.reload} onKeyAdded={async key=>{keyReferences.added(key);await savedWorkspace.reload()}}/>}</ToolSurface>
              <ToolSurface id="commands" mainView active={view==='commands'}>{QuickCommandsPanel&&<QuickCommandsPanel revision={savedWorkspace.snapshot.revision} commands={commands} groups={commandGroups} onApply={savedWorkspace.apply} editable={editable} active={view==='commands'} caption={view==='commands'?managementCaption:null}
                targets={state.sessions.filter(session => ssh.entries[session.id]?.phase === 'connected').map(session => ({ id: session.id, name: paneLabel(session.id) }))} activeTarget={state.activeId} onSend={sendCommand}/>}</ToolSurface>
              <SyncWorkspaceRunner onReload={async()=>{if(nativePreferences)await desktopPreferences.refresh();await savedWorkspace.reload();await identities.reload();await keyReferences.reload()}}/>
      <ToolSurface id="settings" mainView triggerId="open-settings" active={view==='settings'}>{SettingsPanel&&<SettingsPanel tabsOnTop={tabsOnTop} onTabPosition={changeTabPosition} value={preferences} resolvedMode={mode} systemMode={systemMode} onChange={updatePreferences} saved={preferencesSaved} remember={remember} syncHistoryRequest={syncHistoryRequest} showManagementShortcuts={showManagementShortcuts}/>}</ToolSurface>
              <Stack ref={sessionView} hidden={!terminalVisible} inert={!terminalVisible}
                style={{...fill,display:terminalVisible?'flex':'none'}}>
              <TerminalWorkspaceView state={state} hosts={state.hosts} visible={terminalVisible&&!filesSelected&&!selectedDocument}
                roundLeadingCorners={!desktopViews[state.activeId]&&!logViews[state.activeId]&&!diffViews[state.activeId]}
                onActivate={activateWorkspacePane} onSplit={splitPane} onQuickSplit={()=>splitPane('horizontal',state.activeId,true)} onAttach={attachPane}
                onRename={id=>{const p=sessionById.get(id);if(p)setRenaming({id,name:p.label??p.host.name,pane:true})}}
                onPreset={preset=>{if(activeGroup)openPreview([activeGroup.id],preset)}}
                onDock={(source,target,side,wholeTab)=>{
                  if(source===target)return
                  const action={type:'dock' as const,source,target,side,wholeTab,nodeId:crypto.randomUUID()}
                  if(wholeTab&&groupFor(runtime,source)===groupFor(runtime,target))return
                  if(terminalWorkspaceReducer(runtime,action)===runtime){setNotice(tr('每个标签最多 8 个窗格'));return}
                  dispatch(action);locatePane(source)
                }}
                renderToolbar={controls=>terminalVisible&&windowTools&&createPortal(<SessionToolbar controls={controls} pending={!!activeSession?.pending}
                  connected={activeSSH?.phase==='connected'} busy={activeSSH?.phase==='connecting'||activeSSH?.phase==='hostkey'||activeSSH?.phase==='authentication'}
                  reconnecting={!!activeSSH?.retry || !!activeSSH?.automatic && ['connecting','hostkey','authentication'].includes(activeSSH.phase)}
                  onConnection={()=>{if(activeSession)toggleSessionConnection(activeSession.id)}}
                  onForwards={()=>sessionTools.current?.openForwards()} forwards={Object.values(activeSSH?.forwards??{}).filter(rule=>rule.state==='running').length}
                  onShellIntegration={()=>manageShellIntegration(state.activeId)}
                  zmodemAvailable={!!activeSSH?.transport.startZmodemUpload}
                  broadcastAvailable={!!activeGroup&&leaves(activeGroup.root).some(id=>ssh.entries[id]?.phase==='connected')} onBroadcast={()=>{if(activeGroup)sessionTools.current?.openBroadcast(activeGroup.id)}}
                  fontSize={fontSize} onFontSize={fontSize=>updatePreferences({...preferences,fontSize})}
                  remoteView={!!desktopViews[state.activeId]||filesSelected||!!selectedDocument||!!logViews[state.activeId]||!!diffViews[state.activeId]}
                  onDesktop={()=>openRemoteDesktop()}
                  onLogs={()=>openLogs(state.activeId)} recording={activeSSH?.log?.state==='recording'&&activeSSH.log.attempt===activeSSH.attempt}
                  onTerminal={action=>void terminalControllers.current.get(state.activeId)?.[action]()}/>,windowTools)}
                onMove={(id,target)=>{dispatch({type:'move',id,target});requestFocus(id)}} onSwap={(id,target)=>{dispatch({type:'swap',id,target});locatePane(id)}} onDetach={detachPane} onClose={closeSession}
                onRatio={(nodeId,ratio)=>{if(activeGroup)dispatch({type:'ratio',groupId:activeGroup.id,nodeId,ratio})}}
                onArrange={columns=>{if(activeGroup)dispatch({type:'arrange',groupId:activeGroup.id,columns})}} onMaximize={()=>{if(activeGroup)dispatch({type:'maximize',groupId:activeGroup.id})}}
                onGeometry={geometry=>{workspaceGeometry.current=geometry}}
                renderPane={(id,shown,focused,tabId)=>
                  <TerminalSessionPane session={sessionById.get(id)!} source={ssh.panes} shown={shown} focused={focused} tabId={tabId}
                    desktopLoaded={!!desktopLoaded[id]} desktopOpen={!!desktopViews[id]} logsOpen={!!logViews[id]}
                    diffOpen={!!diffViews[id]} diffRequest={diffRequests[id]}
                    showTerminal={showPaneTerminal} closeLogs={closePaneLogs} closeDiff={closePaneDiff}
                    connect={connectPane} disconnect={ssh.disconnect}
                    mode={mode} terminalPalette={terminalPalette} fontSize={fontSize} fontFamily={preferences.fontFamily}
                    fallbackFont={preferences.fallbackFont} lineHeight={preferences.lineHeight} scrollback={preferences.scrollback} cursorBlink={preferences.cursorBlink} screenReaderMode={preferences.screenReaderMode}
                    onFileGuard={registerFileGuard} onProtection={protectRemote} onReady={registerTerminal}
                    onFocusAvailable={terminalFocus.ready} onZmodemUpload={requestZmodemUpload} onError={reportTerminalError} onActionError={message=>setNotice(message, 'error')}/>}/>
              <Stack id={filesSelected?'panel-files-'+state.activeId:undefined} role="tabpanel" aria-labelledby={filesSelected?'tab-files-'+state.activeId:undefined}
                hidden={!filesSelected} inert={!filesSelected} style={{...fill,display:filesSelected?'flex':'none'}}>
                {state.sessions.filter(session=>!session.pending&&fileWorkspaceViews[session.id]!==undefined).map(session=>{
                  const id=session.id,active=filesSelected&&id===state.activeId
                  return <Suspense key={id} fallback={active?<Text type="supporting">{tr("正在打开文件工作区…")}</Text>:null}>
                    <SftpWorkspace sessionLabel={paneLabel} onFileGuard={registerFileGuard} onProtection={protectRemote}
                      session={session} source={ssh.panes} active={active} path={paths[id]??'.'} onPath={next=>setPaths(v=>({...v,[id]:next}))}
                      onConnect={() => connectSession(id)} onCancelConnection={() => ssh.disconnect(id)}
                      onEdit={path=>openRemoteText(id,path)} reloadVersion={remoteRefresh} directorySync={preferences.directorySync}
                      onDirectorySync={directorySync=>updatePreferences({...preferences,directorySync})} onEnterDirectory={path=>requestShellOperation(id,{kind:'directory',path})}
                      onShellIntegration={()=>manageShellIntegration(id)}
                      onComparisonText={(comparison,relative)=>openComparison(id,comparison,relative)}/>
                  </Suspense>
                })}
              </Stack>
              {remoteRequests.map(doc=>{
                const id=doc.sessionId,live=ssh.entries[id],active=selectedDocument===doc.id
                return <Stack key={doc.id} id={'panel-document-'+doc.id} role="tabpanel" aria-labelledby={'tab-document-'+doc.id}
                  hidden={!active} inert={!active} style={{...fill,display:active?'flex':'none'}}>
                  <Suspense fallback={active?<Text type="supporting">{tr("正在打开远程编辑器…")}</Text>:null}>
                    <RemoteTextWorkspace documentId={doc.id} sessionId={id} attempt={live?.attempt??0}
                      connected={live?.phase==='connected'} active={active} path={doc.path}
                      onController={registerRemote} onProtection={protectRemote} onReveal={()=>revealDocument(doc)} onDirtyChange={markDocumentDirty}
                      onClose={()=>closeDocument(doc)} onSaved={()=>setRemoteRefresh(previous=>previous+1)}/>
                  </Suspense>
                </Stack>
              })}
              </Stack>
            </Stack>
          </LayoutContent>} />
        </LayoutContent>} />
        </StackItem>
        </Stack>
      </LayoutContent>} />
      <SessionTools ref={sessionTools} visible={terminalVisible} session={activeSession} live={activeSSH} runtime={runtime} entries={ssh.entries}/>
      {importLoaded&&<Suspense fallback={null}><SSHConfigImportDialog isOpen={importOpen} onClose={()=>setImportOpen(false)} onImported={async()=>{await savedWorkspace.reload();await keyReferences.reload();setNotice(tr("SSH Config 导入完成。"), 'success')}}/></Suspense>}
      {hostRecoveryLoaded&&<Suspense fallback={null}><DeletedHostRecoveryDialog isOpen={hostRecoveryOpen} revision={savedWorkspace.snapshot.revision} onClose={()=>setHostRecoveryOpen(false)} onRestore={restoreDeletedHost}/></Suspense>}
      {preview&&<SplitLayoutDialog ids={preview.panes} initial={preview.preset} label={id=>{
        const pane=sessionById.get(id)
        if(!pane)return tr('待连接')
        if(pane.label)return pane.label+' · '+pane.host.name
        const same=preview.panes.filter(key=>sessionById.get(key)?.host.name===pane.host.name)
        return same.length>1?pane.host.name+' · '+(same.indexOf(id)+1):pane.host.name
      }} onClose={()=>setPreview(null)} onApply={(preset,paneOrder)=>{
        const action={type:'preset' as const,groupIds:preview.groups,paneOrder,preset,token:crypto.randomUUID()}
        const next=terminalWorkspaceReducer(runtime,action)
        if(next!==runtime){dispatch(action);locatePane(next.activeId);setMarked([])}else setNotice(tr('布局已变化，请重新选择标签。'))
        setPreview(null)
      }}/>}
      {renaming&&<EditorDialog title={renaming.groups?tr('标签组名称'):tr("重命名")} action="edit" onClose={()=>setRenaming(null)} formProps={{onSubmit:e=>{e.preventDefault();if(renaming.name.trim()){dispatch(renaming.groups?{type:'regroup',groupIds:renaming.groups,workspaceId:renaming.id,name:renaming.name}:renaming.pane?{type:'renamePane',id:renaming.id,name:renaming.name}:{type:'renameGroup',groupId:renaming.id,name:renaming.name});setMarked([]);setRenaming(null)}}}} actions={<Button type="submit" label={tr("保存")} isDisabled={!renaming.name.trim()}/>}>
        <TextInput label={tr("名称")} value={renaming.name} hasAutoFocus onChange={name=>setRenaming({...renaming,name:name.slice(0,64)})}/>
      </EditorDialog>}
      <SessionRailReleaseNotes/>
      {ConnectionEditor && <ConnectionEditor key={editor.revision} initialRevision={editor.baseRevision} revision={savedWorkspace.snapshot.revision} host={editor.host} isOpen={editor.open} temporary={editor.temporary}
        groups={groups} hosts={savedWorkspace.snapshot.hosts} identities={identities.items}
        defaultGroup={hostsVisible && hostGroup.startsWith('group:') ? hostGroup.slice(6) : '未分组'} editable={editable} storageError={localizeMessage(savedWorkspace.error)}
        onEditorClosed={() => {
          const key = savedHostToReveal.current
          savedHostToReveal.current = undefined
          if (key && hostsVisible) setSavedHostRow({ key })
        }}
        onConnectClosed={()=>{if(terminalVisible&&terminalFocus.wasRequested(state.activeId)&&!document.querySelector('dialog[open]'))requestFocus(state.activeId)}}
        onClose={closeEditor} onSave={saveHost} onQuickConnect={async(host,credentials)=>{const id=crypto.randomUUID();setNotice(tr("临时连接 · 此连接未保存。"));closeEditor();setSelectedId(host.id);setShowHosts(false);dispatch({type:'connect',host,id});setAuthTarget('');ssh.connect(id,host,credentials);requestFocus(id);return true}}
        keys={keyReferences.keys} choosingKey={keyReferences.busy} keyError={localizeMessage(keyReferences.error)} onChooseKey={chooseKey} />}
      {CommandEditorDialog && <CommandEditorDialog key={commandEditor.revision} isOpen={commandEditor.open}
        command={commandEditor.command} existing={false} revision={savedWorkspace.snapshot.revision}
        latest={commandEditor.open ? commands.find(item => item.id === commandEditor.command?.id) : undefined} groups={commandGroups} editable={editable}
        onApply={savedWorkspace.apply}
        onClose={() => { ++commandEditorRequest.current; setCommandEditor(previous => ({ ...previous, open: false })) }}
        onAfterClose={() => {
          if (document.querySelector('dialog[open]')) return
          const focused = document.activeElement
          const validFocus = focused instanceof HTMLElement && focused !== document.body && focused !== document.documentElement && focused.getClientRects().length > 0
          if (validFocus) return
          if (visibleTerminalId) requestFocus(visibleTerminalId)
          else (document.getElementById('session-command-trigger') ?? workspaceFocusTarget())?.focus({ preventScroll: true })
        }}
        onSaved={saved => setNotice(tr("已保存「{p0}」", saved.name), 'success')} />}
      {integrationOpen && integration && activeSession && <Suspense fallback={null}>
        <ShellIntegrationDialog key={integration.id + ':' + integrationEntry.attempt}
          host={activeSession.host.name + ' · ' + activeSession.host.username + '@' + activeSession.host.address}
          transport={integration.transport} integrated={!!integrationEntry.directory?.integrated} operation={integration.operation}
          onClose={closeIntegration}
          onSaved={() => { closeIntegration(); setNotice(tr('已更新远程 Bash 登录配置。'), 'success') }}
          onContinue={async () => {
            if (runtimeLatest.current.activeId !== integration.id) return
            await performShellOperation(integration.id, integration.transport, integration.operation)
            closeIntegration()
          }}/>
      </Suspense>}
      <DialogPresence value={authOpen&&activeSession&&!activeSession.pending&&SessionConnectionPanel ? {
        session:activeSession,live:activeSSH,Panel:SessionConnectionPanel,keys:keyReferences.keys,
        identities:identities.items,keyError:localizeMessage(keyReferences.error),
      } : null}>
        {({session,live,Panel,keys,identities,keyError},isOpen,onAfterClose)=><Dialog isOpen={isOpen} purpose="form" padding={0}
          width="min(calc(var(--spacing-10) * 11), 92vw)" maxHeight="90dvh" onOpenChange={open=>{if(!open)closeSessionAuthentication()}}
          onAfterClose={()=>{
            onAfterClose()
            if(terminalVisible&&state.activeId===session.id&&activeSSH?.phase==='connected'&&!document.querySelector('dialog[open]'))requestFocus(session.id)
          }}>
          <Panel key={session.id} host={session.host} live={live} keys={keys} identities={identities} keyError={keyError} onChooseKey={chooseKey} active={isOpen}
            allowFileImport={false} onConnect={credentials=>authenticateSession(session.id,session.host,credentials)}
            onDisconnect={()=>ssh.disconnect(session.id)} onClose={closeSessionAuthentication}/>
        </Dialog>}
      </DialogPresence>
      <DialogPresence value={closing ? (()=>{
        const singlePane=closing.ids.length===1?closing.ids[0]:undefined
        const splitGroup=singlePane?groupFor(runtime,singlePane):undefined
        return {review:closing,group:closing.group,busy:closeBusy,error:localizeMessage(closeError),
          detachable:splitGroup&&leaves(splitGroup.root).length>1?singlePane:undefined,
          panes:closing.panes,
        }
      })() : null}>
        {({review,group,busy,error,detachable,panes},isOpen,onAfterClose)=><CloseTerminalDialog isOpen={isOpen} onAfterClose={onAfterClose}
          group={group} busy={busy} error={error} panes={panes} onCancel={closer.dismiss} onClose={save=>void closer.confirm(review,save)}
          onDetach={detachable?()=>detachPane(detachable):undefined}
          onDetachFocus={detachable?()=>{if(terminalVisible&&state.activeId===detachable&&!document.querySelector('dialog[open]'))requestFocus(detachable)}:undefined}/>}
      </DialogPresence>
      {deletion && <Suspense fallback={null}><ConfirmDelete title={deletion.kind === 'host' ? tr("删除「{p0}」？", deletion.host.name) : tr('删除分组')}
        subject={deletion.kind==='group'?<GroupPath name={deletion.name}/>:undefined}
        isOpen={deleteOpen}
        description={deletion.kind === 'host' ? tr("只删除已保存的连接配置，已打开的会话保持原状。") : tr("删除该分组及其子分组，其中的主机保留并取消分组归属；已打开的会话保持原状。")}
        busy={savedWorkspace.busy} error={localizeMessage(deleteError)} onClose={() => setDeleteOpen(false)} onConfirm={() => { void confirmDelete() }} /></Suspense>}
    </Stack>
    </SessionRailProvider>
    </AppContextMenu>
  </Theme></BuiltInIconStyleContext>
}
