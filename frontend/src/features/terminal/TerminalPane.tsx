import { tr, localizeMessage } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { lazy, Suspense, memo, useCallback,useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {createPortal} from 'react-dom'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { Heading } from '@astryxdesign/core/Heading'
import { Icon } from '@astryxdesign/core/Icon'
import { Spinner } from '@astryxdesign/core/Spinner'
import { Text } from '@astryxdesign/core/Text'
import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'
import {ContextMenu} from '@astryxdesign/core/ContextMenu'
import { ArrowPathIcon, ArrowRightOnRectangleIcon } from '@heroicons/react/24/outline'
import OsIcon from '../../shared/icons/OsIcon'
import type { SearchAddon } from '@xterm/addon-search'
import { readClipboardText, writeClipboardText } from '../../shared/bridge/application'
import type { Terminal } from '@xterm/xterm'
import { prepareTerminalFonts, prepareTerminalPrimary, terminalFontStack, type TerminalFont, type FallbackFont } from '../../shared/fonts/fonts'
import { Theme,useTheme } from '@astryxdesign/core/theme'
import { resolveTerminalPalette, terminalPaletteMode, type TerminalPalette } from '../../shared/theme/terminalPresets'
import { getWorkbenchThemeByName } from '../../shared/theme/currentTheme'
import { terminalTheme, terminalContrastRatio, terminalToken } from '../../shared/theme/terminalColors'
import type { HostProfile } from '../workspace/mockGateway'
import { connectionLabels, type ConnectionStatus } from '../workspace/sessionConnection'
import ReconnectActions from '../workspace/ReconnectActions'
import type { ReconnectWait } from '../connections/sshReconnect'
import { quickCommandInput } from '../workspace/shortcuts'
import { findShortcut, matchesShortcut, shortcutText, useShortcutOverrides } from '../workspace/shortcutReference'
import { writeRemotePacket, type TerminalTransport } from './remoteOutput'
import { createTerminalRenderer } from './terminalRenderer'
import { createTerminalAppearance, terminalFocusOptions } from './terminalAppearance'
import { focusTerminalInput } from './terminalFocus'
import {ZmodemEngine,type ZmodemView} from './zmodemEngine'
import type {RegisterFileGuard} from '../workspace/fileCloseGuard'
import '@xterm/xterm/css/xterm.css'
const ZmodemDialog=lazy(()=>import('./ZmodemDialog'))

export interface TerminalController {
  sendCommand: (command: string, expectedTransport: TerminalTransport) => string | undefined
  focus: () => boolean
  search: () => void
  copy: () => Promise<void>
  paste: () => Promise<void>
  clear: () => void
  zmodemUpload: () => void
}
export default memo(function TerminalPane(props: {
  host: HostProfile
  active: boolean
  focused?: boolean
  tabId?: string
  hideToolbar?: boolean
  mode: 'dark' | 'light'
  terminalPalette?: TerminalPalette
  fontSize: number
  fontFamily?: TerminalFont
  fallbackFont?: FallbackFont
  lineHeight?: number
  scrollback: number
  cursorBlink: boolean
  screenReaderMode?: boolean
  id: string
  transport?: TerminalTransport
  inputEnabled?: boolean
  connectionPrompt?: { status: Exclude<ConnectionStatus, 'connected'>; message?: string; onAction: () => void;
    waiting?: ReconnectWait; automatic?: boolean; onRetryNow?: () => void }
  attempt?:number
  onFileGuard?:RegisterFileGuard
  onProtection?:(key:string,value:boolean)=>void
  onReady: (id: string, controller: TerminalController | null) => void
  onFocusAvailable?: (id: string) => void
  onError: () => void
  onActionError?: (message: string) => void
}) {
  const language = useLocale()
  const shortcuts = useShortcutOverrides()
  const prompt = props.connectionPrompt
  const palette=resolveTerminalPalette(props.terminalPalette??'auto',props.mode)
  const idle = prompt?.status === 'idle'
  const connecting = prompt?.status === 'connecting' || prompt?.status === 'reconnecting'
  const [hasRemoteOutput, setHasRemoteOutput] = useState(false)
  const hideTerminal = !hasRemoteOutput && (idle || prompt?.status === 'connecting')

  const { tokens, name: themeName } = useTheme()
  const theme = useMemo(() => terminalTheme(tokens, palette), [tokens, palette])
  const desiredTheme = useRef(theme)
  const applyTheme = useRef<ReturnType<typeof createTerminalAppearance> | null>(null)
  const mount = useRef<HTMLElement>(null)
  const terminal = useRef<Terminal | null>(null)
  const renderer = useRef<ReturnType<typeof createTerminalRenderer> | null>(null)
  const output = useRef<{ attempt?: number; received?: boolean; bracketedAttempt?: number; pending?: Promise<void> }>({})
  const latest = useRef(props)
  const resize = useRef(() => {})
  const [ready, setReady] = useState(false)
  useEffect(() => {
    const instance = terminal.current
    if (!instance) return
    ;(instance.constructor as typeof Terminal).strings.tooMuchOutput = tr("输出过多，已暂停自动朗读。请滚动查看终端内容。")
    instance.textarea?.setAttribute('aria-label', tr("终端输入"))
    const back = shortcutText('session-focus', shortcuts)
    instance.textarea?.setAttribute('aria-description', back ? tr('SSH 终端。未连接时不接受输入。{p0} 返回会话标签。', back) : tr('SSH 终端。未连接时不接受输入。'))
  }, [language, ready, shortcuts])
  const search = useRef<SearchAddon | null>(null)
  const [searchOpen, setSearchOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [searchMessage, setSearchMessage] = useState('')
  const [hasSelection,setHasSelection]=useState(false)
  const zmodem=useRef<ZmodemEngine|null>(null)
  const [transferView,setTransferView]=useState<ZmodemView|null>(null)
  const transferring=!!transferView&&!['idle','completed','failed','cancelled'].includes(transferView.phase)
  useEffect(()=>{const key='zmodem:'+props.id;props.onFileGuard?.(props.id,key,{getCloseState:()=>({dirty:false,busy:zmodem.current?.active??false}),saveForClose:async()=>true,reveal:()=>{},cancelForClose:async()=>{await zmodem.current?.cancel()}});return()=>props.onFileGuard?.(props.id,key,null)},[props.id,props.onFileGuard])
  useEffect(()=>{const key='zmodem:'+props.id;props.onProtection?.(key,transferring);return()=>props.onProtection?.(key,false)},[props.id,props.onProtection,transferring])
  useEffect(()=>{if(!transferring)return;const protect=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue=''};window.addEventListener('beforeunload',protect);return()=>window.removeEventListener('beforeunload',protect)},[transferring])
  const fillContextTrigger=useCallback((node:HTMLElement|null)=>{if(node)Object.assign(node.style,{height:'100%',minHeight:'0'})},[])
  const find = (previous = false) => {
    if (query)
      setSearchMessage(
        (previous ? search.current?.findPrevious(query) : search.current?.findNext(query))
          ? tr("已选中匹配内容")
          : tr("没有匹配内容"),
      )
  }
  const copy = async () => {
    const selection = terminal.current?.getSelection()
    if (!selection) return
    try {
      await writeClipboardText(selection)
    } catch (cause) {
      latest.current.onActionError?.(cause instanceof Error ? cause.message : tr("复制失败，请重试。"))
    }
  }
  const paste = async () => {
    const target = latest.current.transport
    if (!latest.current.inputEnabled || !latest.current.active || latest.current.focused === false) return
    try {
      const text = await readClipboardText()
      if (latest.current.transport !== target || !latest.current.inputEnabled || !latest.current.active || !Boolean(latest.current.focused ?? true))
        return
      if (text.length > 262144) {
        latest.current.onActionError?.(tr("粘贴内容超过 256 KB，请分段粘贴。"))
        return
      }
      terminal.current?.paste(text)
      terminal.current?.focus()
    } catch (cause) {
      latest.current.onActionError?.(cause instanceof Error ? cause.message : tr("读取剪贴板失败，请重试。"))
    }
  }
  useLayoutEffect(() => {
    latest.current = props
    desiredTheme.current = theme
  })
  useEffect(() => {
    let disposed = false
    let instance: Terminal | undefined
    let observer: ResizeObserver | undefined
    let frame = 0
    async function initialize() {
      try {
        const [{ Terminal }, { FitAddon }, { SearchAddon }, { UnicodeGraphemesAddon }] = await Promise.all([
          import('@xterm/xterm'),
          import('@xterm/addon-fit'),
          import('@xterm/addon-search'),
          import('@xterm/addon-unicode-graphemes'),
        ])
        if (disposed || !mount.current) return
        const primary = await prepareTerminalPrimary(latest.current.fontFamily ?? 'Google Sans Code')
        if (disposed || !mount.current) return
        Terminal.strings.promptLabel = tr("终端输入")
        Terminal.strings.tooMuchOutput = tr("输出过多，已暂停自动朗读。请滚动查看终端内容。")
        const fit = new FitAddon()
        instance = new Terminal({
          allowProposedApi: true,
          fontFamily: terminalFontStack(primary, 'system'),
          fontSize: latest.current.fontSize,
          lineHeight: latest.current.lineHeight ?? 1.5,
          ...terminalFocusOptions(latest.current),
          scrollback: latest.current.scrollback,
          cursorStyle: 'bar',
          cursorInactiveStyle: 'underline',
          disableStdin: !latest.current.transport || latest.current.inputEnabled === false || latest.current.focused === false,
          minimumContrastRatio: terminalContrastRatio,
          drawBoldTextInBrightColors: false,
          theme: desiredTheme.current,
        })
        terminal.current = instance
        applyTheme.current = createTerminalAppearance(instance, desiredTheme.current)
        instance.loadAddon(fit)
        instance.open(mount.current)
        renderer.current = createTerminalRenderer(instance)
        instance.loadAddon(new UnicodeGraphemesAddon())
        instance.parser.registerCsiHandler({ prefix: '?', final: 'h' }, params => {
          if (params.includes(2004)) output.current.bracketedAttempt = output.current.attempt
          return false
        })
        search.current = new SearchAddon({ highlightLimit: 1000 })
        instance.loadAddon(search.current)
        instance.attachCustomKeyEventHandler((event) => {
          const action = findShortcut(event, ['terminal-search','terminal-copy','terminal-paste','session-focus'])
          if (!action) return true
          if (event.type === 'keydown') {
            event.preventDefault()
            if (event.repeat) return false
            if (action === 'terminal-search') {
              setSearchOpen(true)
              requestAnimationFrame(() => document.getElementById('terminal-search-' + latest.current.id)?.focus())
            } else if (action === 'session-focus') document.getElementById('tab-' + (latest.current.tabId ?? latest.current.id))?.focus()
            else void (action === 'terminal-copy' ? copy() : paste())
          }
          return false
        })
        instance.onData((data) => {
          if(latest.current.focused !== false && latest.current.inputEnabled !== false && latest.current.active)latest.current.transport?.send(data)
        })
        instance.onBinary((data) => {
          if(latest.current.focused !== false && latest.current.inputEnabled !== false && latest.current.active)latest.current.transport?.sendBinary?.(data)
        })
        instance.onResize(({ cols, rows }) => {
          latest.current.transport?.resize(cols, rows)
        })
        resize.current = () => {
          if (frame || !latest.current.active) return
          frame = requestAnimationFrame(() => {
            frame = 0
            if (disposed || !latest.current.active || !mount.current?.clientWidth || !mount.current?.clientHeight) return
            const dimensions = fit.proposeDimensions()
            if (dimensions && (instance?.cols !== dimensions.cols || instance.rows !== dimensions.rows))
              fit.fit()
          })
        }
        observer = new ResizeObserver(() => resize.current())
        observer.observe(mount.current)
        latest.current.onReady(latest.current.id, {
          focus: () => latest.current.active && latest.current.focused !== false && focusTerminalInput(instance?.textarea, () => instance?.focus()),
          search: () => {setSearchOpen(true);requestAnimationFrame(()=>document.getElementById(`terminal-search-${latest.current.id}`)?.focus())},
          copy, paste,
          zmodemUpload:()=>{if(latest.current.inputEnabled&&!zmodem.current?.active)void latest.current.transport?.startZmodemUpload?.().catch(cause=>latest.current.onActionError?.(cause instanceof Error?cause.message:tr("ZMODEM 上传尚未启动，请回到空命令行后重试。")))},
          clear: () => instance?.clear(),
          sendCommand: (command, expectedTransport) => {
            const { transport, inputEnabled, attempt } = latest.current
            const unavailable = tr("会话当前不可用，命令未发送，请重新选择或连接。")
            if (disposed || !instance || transport !== expectedTransport || inputEnabled === false) return unavailable
            const bracketed = attempt !== undefined && output.current.bracketedAttempt === attempt && instance.modes.bracketedPasteMode
            const prepared = quickCommandInput(command, bracketed)
            if ('error' in prepared) return prepared.error
            if (!transport.send(prepared.input)) return unavailable
          },
        })
        setReady(true)
        resize.current()
      } catch {
        if (!disposed) latest.current.onError()
      }
    }
    void initialize()
    return () => {
      disposed = true
      latest.current.onReady(latest.current.id, null)
      cancelAnimationFrame(frame)
      observer?.disconnect()
      renderer.current?.dispose()
      renderer.current = null
      instance?.dispose()
      search.current = null
      terminal.current = null
      applyTheme.current = null
      resize.current = () => {}
    }
  }, [])
  useLayoutEffect(() => {
    const current = renderer.current
    if (!ready || !props.active || hideTerminal || !current) return
    current.setVisible(true)
    return () => current.setVisible(false)
  }, [ready, props.active, hideTerminal])
  useEffect(() => {
    const transport = props.transport
    const instance = terminal.current
    if (!ready || !instance || !transport) return
    const controller = new AbortController()
    const sink = (bytes: Uint8Array) => {
      if (controller.signal.aborted) return Promise.resolve()
      if (bytes.length && !output.current.received) {
        output.current.received = true
        setHasRemoteOutput(true)
      }
      output.current.attempt = props.attempt
      return output.current.pending = writeRemotePacket(bytes, (chunk, done) => instance.write(chunk, done), () => controller.signal.aborted)
    }
    const engine=transport.beginTransfer&&transport.writeTransfer&&transport.endTransfer?new ZmodemEngine(transport as TerminalTransport&Required<Pick<TerminalTransport,'beginTransfer'|'writeTransfer'|'endTransfer'>>,props.id,props.attempt??0,sink,next=>{if(!controller.signal.aborted)setTransferView(next)}):null
    zmodem.current=engine
    setTransferView(null)
    void (async () => {
      try {
        await output.current.pending?.catch(() => {})
        if (controller.signal.aborted) return
        for await (const bytes of transport.read(controller.signal)) {
          if (controller.signal.aborted) break
          if(engine)await engine.consume(bytes);else await sink(bytes)
        }
      } catch {
        if (!controller.signal.aborted) latest.current.onError()
      } finally {
        if(engine?.active)await engine.cancel(tr("SSH 数据通道已结束；未完成的文件没有提交。"))
      }
    })()
    transport.resize(instance.cols, instance.rows)
    return () => {controller.abort();if(zmodem.current===engine)zmodem.current=null;void engine?.dispose()}
  }, [props.transport, ready,props.attempt])
  useEffect(() => {
    const instance = terminal.current
    const disableStdin = !props.transport || props.inputEnabled === false || props.focused === false || transferring
    if (instance && instance.options.disableStdin !== disableStdin)
      instance.options.disableStdin = disableStdin
    const tabIndex = props.focused === false ? -1 : 0
    if (instance?.textarea && instance.textarea.tabIndex !== tabIndex) instance.textarea.tabIndex = tabIndex
  }, [props.transport, props.inputEnabled, props.focused, ready, transferring])
  useEffect(() => {
    if (props.active) resize.current()
  }, [props.active])
  useLayoutEffect(() => {
    if (ready && props.active && props.focused !== false && !hideTerminal) props.onFocusAvailable?.(props.id)
  }, [ready, props.id, props.active, props.focused, hideTerminal, props.onFocusAvailable])
  useEffect(() => {
    const instance = terminal.current
    if (instance && instance.options.fontSize !== props.fontSize) {
      instance.options.fontSize = props.fontSize
      resize.current()
    }
  }, [props.fontSize])
  useEffect(() => {
    let cancelled = false
    void prepareTerminalFonts(props.fontFamily ?? 'Google Sans Code', props.fallbackFont ?? 'Maple Mono CN').then(family => {
      if (cancelled || !terminal.current || terminal.current.options.fontFamily === family) return
      terminal.current.options.fontFamily = family
      resize.current()
    })
    return () => { cancelled = true }
  }, [props.fontFamily, props.fallbackFont, ready])
  useEffect(() => {
    const instance = terminal.current
    const lineHeight = props.lineHeight ?? 1.5
    if (!instance || instance.options.lineHeight === lineHeight) return
    instance.options.lineHeight = lineHeight
    resize.current()
  }, [props.lineHeight, ready])
  useEffect(() => {
    const instance = terminal.current
    if (instance && instance.options.scrollback !== props.scrollback) instance.options.scrollback = props.scrollback
  }, [props.scrollback])
  useEffect(() => {
    const instance = terminal.current
    if (!instance) return
    const { cursorBlink, screenReaderMode } = terminalFocusOptions(props)
    if (instance.options.cursorBlink !== cursorBlink) instance.options.cursorBlink = cursorBlink
    if (instance.options.screenReaderMode !== screenReaderMode) instance.options.screenReaderMode = screenReaderMode
  }, [props.cursorBlink, props.screenReaderMode, props.active, props.focused])
  useLayoutEffect(() => {
    applyTheme.current?.(theme, props.active && !hideTerminal)
  }, [theme, props.active, hideTerminal])
  const connectionAction = prompt && (prompt.waiting || prompt.automatic ? <ReconnectActions waiting={prompt.waiting} onStop={prompt.onAction} onRetry={prompt.onRetryNow}/>
            : <Button label={connecting ? tr("取消连接") : idle ? tr("连接") : tr("重新连接")}
              aria-label={idle?tr("连接 {p0}",props.host.name||props.host.address):undefined}
              variant={connecting ? 'secondary' : 'primary'} size="md"
              icon={connecting ? undefined : <Icon icon={idle ? ArrowRightOnRectangleIcon : ArrowPathIcon} size="sm"/>}
              onClick={prompt.onAction}
              style={{paddingInline:'var(--spacing-4)'}}/>)

  return (<>
    <StackItem
      onKeyDown={(event) => {
        if (matchesShortcut(event.nativeEvent, 'terminal-search')) {
          event.preventDefault()
          if (event.repeat) return
          setSearchOpen(true)
          requestAnimationFrame(() => document.getElementById('terminal-search-' + props.id)?.focus())
        }
      }}
      size="fill"
      hidden={!props.active}
      role={props.hideToolbar ? 'region' : 'tabpanel'}
      id={`${props.hideToolbar?'terminal-panel':'panel'}-${props.id}`}
      aria-labelledby={`tab-${props.tabId ?? props.id}`}
      style={{
        display: props.active ? 'flex' : 'none',
        flexDirection: 'column',
        minHeight: 0,
        minWidth: 0,
        padding: props.hideToolbar ? 0 : 'var(--spacing-3)',
        overflow: 'hidden',
      }}
    >
      {(!props.hideToolbar||searchOpen)&&<Stack direction="horizontal" gap={2} align="center" justify="end" wrap="wrap" onKeyDown={event => {
        if (searchOpen && event.key === 'Escape' && !event.nativeEvent.isComposing) {
          event.preventDefault()
          event.stopPropagation()
          setSearchOpen(false)
          search.current?.clearDecorations()
          terminal.current?.focus()
        }
      }}>
        {searchOpen && (
          <>
            <TextInput
              id={`terminal-search-${props.id}`}
              label={tr("搜索终端历史")}
              isLabelHidden
              size="md"
              type="search"
              value={query}
              onChange={(value) => {
                setQuery(value)
                setSearchMessage('')
              }}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return
                if (event.key === 'Enter') {
                  event.preventDefault()
                  find(event.shiftKey)
                }
              }}
            />
            <Button label={tr("上一个")} size="md" variant="ghost" onClick={() => find(true)} />
            <Button label={tr("下一个")} size="md" variant="ghost" onClick={() => find()} />
          </>
        )}
        <Button
          label={searchOpen ? tr("关闭搜索") : tr("搜索")}
          size="md"
          variant="ghost"
          onClick={() => {
            setSearchOpen(!searchOpen)
            search.current?.clearDecorations()
            if (!searchOpen)
              requestAnimationFrame(() => document.getElementById(`terminal-search-${props.id}`)?.focus())
            else terminal.current?.focus()
          }}
        />
        {!props.hideToolbar&&<><Button label={tr("复制")} size="md" variant="ghost" onClick={() => void copy()} />
        <Button
          label={tr("粘贴")}
          size="md"
          variant="ghost"
          isDisabled={!props.inputEnabled}
          onClick={() => void paste()}
        /></>}
      </Stack>}
      <VisuallyHidden role="status">{searchOpen ? localizeMessage(searchMessage) : ''}</VisuallyHidden>
      {props.active && prompt && hasRemoteOutput && <Stack direction="horizontal" align="center" justify="between" wrap="wrap"
        paddingInline={2} paddingBlock={1} gap={2} style={{ flexShrink: 0 }}>
        <Text color="secondary" role="status">
          {[connectionLabels[prompt.status], prompt.message === connectionLabels[prompt.status] ? '' : prompt.message].filter(Boolean).join(' · ')}
        </Text>
        {connectionAction}
      </Stack>}
      <StackItem size="fill" style={{ minHeight: 0, minWidth: 0, position: 'relative' }}>
        <ContextMenu ref={fillContextTrigger} label={tr("终端操作")} presentation="adaptive" isDisabled={hideTerminal||!props.active||props.focused===false}
          onOpenChange={open=>{if(open)setHasSelection(!!terminal.current?.getSelection())}}
          onContextMenuCapture={event=>{if(terminal.current&&terminal.current.modes.mouseTrackingMode!=='none'&&!event.shiftKey){event.preventDefault();event.stopPropagation()}}}
          onContextMenu={event=>event.stopPropagation()}
          items={[{label:tr("复制所选内容"),isDisabled:!hasSelection,onClick:()=>void copy()},{label:tr("粘贴"),isDisabled:!props.inputEnabled,onClick:()=>void paste()},
            {label:tr("ZMODEM 上传…"),isDisabled:!props.inputEnabled||transferring||!props.transport?.startZmodemUpload,onClick:()=>{void props.transport?.startZmodemUpload?.().catch(cause=>latest.current.onActionError?.(cause instanceof Error?cause.message:tr("上传尚未启动，请回到空命令行后重试。")))}},
            {type:'divider'},{label:tr("搜索终端历史"),onClick:()=>{setSearchOpen(true);requestAnimationFrame(()=>document.getElementById('terminal-search-'+props.id)?.focus())}},
            {label:tr("清屏"),onClick:()=>terminal.current?.clear()}]}>
        <Stack
          height="100%"
          padding={2}
          inert={hideTerminal}
          data-terminal-font-wheel={props.active&&!hideTerminal?'':undefined}
          aria-label={tr("SSH 终端 {p0}", props.host.name)}
          style={{
            minHeight: 0,
            minWidth: 0,
            fontSizeAdjust: 'none',
            overflow: 'hidden',
            backgroundColor: `var(${terminalToken('background', palette)})`,
            color: `var(${terminalToken('foreground', palette)})`,
            colorScheme: terminalPaletteMode(palette, props.mode),
            visibility: hideTerminal ? 'hidden' : undefined,
          }}
        >
          <Stack ref={mount} height="100%" style={{minHeight:0,minWidth:0,overflow:'hidden'}}/>
        </Stack>
        </ContextMenu>
        {props.active && prompt && !hasRemoteOutput && <Stack align="center" padding={2}
          style={{position:'absolute',inset:0,pointerEvents:'none'}}>
          <Stack gap={3} style={{display:'grid',gridTemplateRows:'var(--spacing-6) minmax(0, 1fr) var(--size-element-md)',
            gridTemplateColumns:'minmax(0, 1fr)',justifyItems:'center',boxSizing:'border-box',marginBlock:'auto',
            inlineSize:'min(100%, calc(var(--spacing-10) * 11))',blockSize:'min(100%, calc(var(--spacing-10) * 5.5))',
            paddingInline:'var(--spacing-6)',paddingBlock:'min(var(--spacing-7), 3dvh)',textAlign:'center',
            pointerEvents:'auto',borderRadius:'var(--radius-container)',
            background:hideTerminal ? undefined : 'var(--color-background-popover)',boxShadow:hideTerminal ? undefined : 'var(--shadow-med)'}}>
            <Stack align="center" justify="center" aria-hidden="true">
              {connecting ? <Spinner size="lg"/> : <OsIcon name={props.host.icon} size="lg"/>}
            </Stack>
            <Stack width="100%" style={{minHeight:0,justifyContent:'safe center',overflow:'auto',overflowWrap:'anywhere',scrollbarGutter:'stable both-edges'}}>
              <Heading level={4} accessibilityLevel={3}>{props.host.name||props.host.address||connectionLabels[prompt.status]}</Heading>
              <Text color="secondary" role="status">
                {[idle&&props.host.address?`${props.host.username}@${props.host.address.includes(':')&&!props.host.address.startsWith('[')?'['+props.host.address+']':props.host.address}:${props.host.port}`:'',
                  connectionLabels[prompt.status],prompt.message===connectionLabels[prompt.status]?'':prompt.message].filter(Boolean).join(' · ')}
              </Text>
            </Stack>
            {connectionAction}
          </Stack>
        </Stack>}
      </StackItem>
    </StackItem>
    {transferView&&zmodem.current&&createPortal(<Theme theme={getWorkbenchThemeByName(themeName)} mode={props.mode}><Suspense fallback={null}><ZmodemDialog engine={zmodem.current} view={transferView} serverName={props.host.name}/></Suspense></Theme>,document.body)}
  </>)
})
