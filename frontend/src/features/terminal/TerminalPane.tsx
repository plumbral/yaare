import { tr, localizeMessage } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { lazy, Suspense, memo, useCallback,useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {createPortal} from 'react-dom'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { Tooltip } from '@astryxdesign/core/Tooltip'
import { VisuallyHidden } from '@astryxdesign/core/VisuallyHidden'
import {ContextMenu} from '@astryxdesign/core/ContextMenu'
import type { SearchAddon } from '@xterm/addon-search'
import { openWebLink, readClipboardText, writeClipboardText } from '../../shared/bridge/application'
import type { Terminal } from '@xterm/xterm'
import { prepareTerminalFonts, prepareTerminalPrimary, terminalFontStack, type TerminalFont, type FallbackFont } from '../../shared/fonts/fonts'
import { detectFontPlatform } from '../../shared/fonts/platformFonts'
import { Theme,useTheme } from '@astryxdesign/core/theme'
import { resolveTerminalPalette, terminalPaletteMode, type TerminalPalette } from '../../shared/theme/terminalPresets'
import { getWorkbenchThemeByName } from '../../shared/theme/currentTheme'
import { terminalTheme, terminalContrastRatio, terminalToken } from '../../shared/theme/terminalColors'
import type { HostProfile } from '../workspace/mockGateway'
import type { SessionConnectionPrompt } from '../workspace/sessionConnectionPrompt'
import TerminalConnectionNotice from './TerminalConnectionNotice'
import { quickCommandInput } from '../workspace/shortcuts'
import { findShortcut, matchesShortcut, shortcutText, useShortcutOverrides } from '../workspace/shortcutReference'
import { writeRemotePacket, type TerminalTransport } from './remoteOutput'
import { createTerminalRenderer, observeTerminalGeometry } from './terminalRenderer'
import { createTerminalAppearance, terminalFocusOptions } from './terminalAppearance'
import { focusTerminalInput } from './terminalFocus'
import { activateTerminalLink } from './terminalLinks'
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
  connectionPrompt?: SessionConnectionPrompt
  attempt?:number
  onFileGuard?:RegisterFileGuard
  onProtection?:(key:string,value:boolean)=>void
  onReady: (id: string, controller: TerminalController | null) => void
  onFocusAvailable?: (id: string) => void
  onZmodemUpload?: (id: string) => Promise<void>
  onError: () => void
  onActionError?: (message: string) => void
}) {
  const language = useLocale()
  const shortcuts = useShortcutOverrides()
  const prompt = props.connectionPrompt
  const palette=resolveTerminalPalette(props.terminalPalette??'auto',props.mode)
  const [hasRemoteOutput, setHasRemoteOutput] = useState(false)
  const noticePresentation = hasRemoteOutput ? 'banner' : 'card'
  const hideTerminal = !!prompt && noticePresentation === 'card'
  const connected = !!props.transport && props.inputEnabled !== false && !prompt
  const historyOnly = hasRemoteOutput && !connected

  const { tokens, name: themeName } = useTheme()
  const theme = useMemo(() => terminalTheme(tokens, palette), [tokens, palette])
  const desiredTheme = useRef(theme)
  const applyTheme = useRef<ReturnType<typeof createTerminalAppearance> | null>(null)
  const mount = useRef<HTMLElement>(null)
  const pane = useRef<HTMLElement>(null)
  const [linkHint, setLinkHint] = useState<{left: number; top: number} | null>(null)
  useEffect(() => {
    if (!props.active || hideTerminal) setLinkHint(null)
  }, [props.active, hideTerminal])
  const terminal = useRef<Terminal | null>(null)
  const renderer = useRef<ReturnType<typeof createTerminalRenderer> | null>(null)
  const output = useRef<{ attempt?: number; received?: boolean; bracketedAttempt?: number; pending?: Promise<void> }>({})
  const latest = useRef({ ...props, connected, hideTerminal })
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
  const transferring=!!transferView&&!['idle','completed','unconfirmed','failed','cancelled'].includes(transferView.phase)
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
  const acceptsInput = () => latest.current.connected && latest.current.active &&
    latest.current.focused !== false && terminal.current?.options.disableStdin === false
  const startUpload = () => {
    const current = latest.current
    if (!current.connected || zmodem.current?.active) return
    const request = current.onZmodemUpload ? current.onZmodemUpload(current.id) : current.transport?.startZmodemUpload?.()
    void request?.catch(cause => current.onActionError?.(cause instanceof Error ? cause.message : tr("上传尚未启动，请回到空命令行后重试。")))
  }
  const paste = async () => {
    const target = latest.current.transport
    if (!acceptsInput()) return
    try {
      const text = await readClipboardText()
      if (latest.current.transport !== target || !acceptsInput()) return
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
  const openSearch = () => {
    if (latest.current.hideTerminal) return
    setSearchOpen(true)
    requestAnimationFrame(() => document.getElementById('terminal-search-' + latest.current.id)?.focus())
  }
  const closeSearch = () => {
    setSearchOpen(false)
    search.current?.clearDecorations()
    if (historyOnly) mount.current?.focus()
    else if (connected) terminal.current?.focus()
  }
  useLayoutEffect(() => {
    latest.current = { ...props, connected, hideTerminal }
    desiredTheme.current = theme
  })
  useEffect(() => {
    let disposed = false
    let instance: Terminal | undefined
    let observer: ResizeObserver | undefined
    let frame = 0
    async function initialize() {
      try {
        const [{ Terminal }, { FitAddon }, { SearchAddon }, { UnicodeGraphemesAddon }, { WebLinksAddon }] = await Promise.all([
          import('@xterm/xterm'),
          import('@xterm/addon-fit'),
          import('@xterm/addon-search'),
          import('@xterm/addon-unicode-graphemes'),
          import('@xterm/addon-web-links'),
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
          ...terminalFocusOptions({ ...latest.current, inputEnabled: latest.current.connected }),
          scrollback: latest.current.scrollback,
          cursorStyle: 'bar',
          disableStdin: !latest.current.connected || !latest.current.active || latest.current.focused === false,
          minimumContrastRatio: terminalContrastRatio,
          drawBoldTextInBrightColors: false,
          theme: desiredTheme.current,
        })
        terminal.current = instance
        applyTheme.current = createTerminalAppearance(instance, desiredTheme.current)
        instance.loadAddon(fit)
        instance.loadAddon(new WebLinksAddon((event, uri) => {
          if (!instance?.hasSelection()) activateTerminalLink(event, uri, openWebLink)
        }, {
          hover: event => {
            const bounds = pane.current?.getBoundingClientRect()
            if (bounds && !event.buttons) setLinkHint({ left: event.clientX - bounds.left, top: event.clientY - bounds.top })
          },
          leave: () => setLinkHint(null),
        }))
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
            if (action === 'terminal-search') openSearch()
            else if (action === 'session-focus') document.getElementById('tab-' + (latest.current.tabId ?? latest.current.id))?.focus()
            else void (action === 'terminal-copy' ? copy() : paste())
          }
          return false
        })
        instance.onData((data) => {
          if (acceptsInput()) latest.current.transport?.send(data)
        })
        instance.onBinary((data) => {
          if (acceptsInput()) latest.current.transport?.sendBinary?.(data)
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
        observeTerminalGeometry(observer, mount.current)
        latest.current.onReady(latest.current.id, {
          focus: () => acceptsInput() && focusTerminalInput(instance?.textarea, () => instance?.focus()),
          search: openSearch,
          copy, paste,
          zmodemUpload:startUpload,
          clear: () => instance?.clear(),
          sendCommand: (command, expectedTransport) => {
            const { transport, connected, attempt } = latest.current
            const unavailable = tr("会话当前不可用，命令未发送，请重新选择或连接。")
            if (disposed || !instance || transport !== expectedTransport || !connected) return unavailable
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
    if (!historyOnly || !terminal.current) return
    const instance = terminal.current
    let cancelled = false
    void Promise.resolve(output.current.pending).then(() => {
      if (!cancelled && !latest.current.connected && instance.modes.mouseTrackingMode !== 'none')
        instance.write('\x18\x1b[?9;1000;1002;1003l')
    })
    return () => { cancelled = true }
  }, [historyOnly, ready])
  useLayoutEffect(() => {
    const instance = terminal.current
    if (!instance) return
    const disableStdin = !connected || !props.active || props.focused === false || transferring
    if (instance.options.disableStdin !== disableStdin)
      instance.options.disableStdin = disableStdin
    if (instance.textarea) {
      instance.textarea.disabled = !connected
      instance.textarea.tabIndex = disableStdin ? -1 : 0
    }
  }, [connected, props.active, props.focused, ready, transferring])
  useEffect(() => {
    if (props.active) resize.current()
  }, [props.active])
  useLayoutEffect(() => {
    if (ready && props.active && props.focused !== false && connected && !transferring) props.onFocusAvailable?.(props.id)
  }, [ready, props.id, props.active, props.focused, connected, transferring, props.onFocusAvailable])
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
  useLayoutEffect(() => {
    const instance = terminal.current
    if (!instance) return
    const { cursorBlink, cursorInactiveStyle, screenReaderMode } = terminalFocusOptions({ ...props, inputEnabled: connected })
    if (instance.options.cursorBlink !== cursorBlink) instance.options.cursorBlink = cursorBlink
    if (instance.options.cursorInactiveStyle !== cursorInactiveStyle) instance.options.cursorInactiveStyle = cursorInactiveStyle
    if (instance.options.screenReaderMode !== screenReaderMode) instance.options.screenReaderMode = screenReaderMode
  }, [props.cursorBlink, props.screenReaderMode, props.active, props.focused, connected, ready])
  useLayoutEffect(() => {
    applyTheme.current?.(theme, props.active && !hideTerminal)
  }, [theme, props.active, hideTerminal])
  return (<>
    <StackItem
      onKeyDown={(event) => {
        if (!hideTerminal && matchesShortcut(event.nativeEvent, 'terminal-search')) {
          event.preventDefault()
          if (event.repeat) return
          openSearch()
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
        position: 'relative',
        minHeight: 0,
        minWidth: 0,
        padding: props.hideToolbar ? 0 : 'var(--spacing-3)',
        overflow: 'hidden',
      }}
    >
      {!hideTerminal && (!props.hideToolbar||searchOpen)&&<Stack direction="horizontal" gap={2} align="center" justify="end" wrap="wrap" onKeyDown={event => {
        if (searchOpen && event.key === 'Escape' && !event.nativeEvent.isComposing) {
          event.preventDefault()
          event.stopPropagation()
          closeSearch()
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
          onClick={searchOpen ? closeSearch : openSearch}
        />
        {!props.hideToolbar&&<><Button label={tr("复制")} size="md" variant="ghost" onClick={() => void copy()} />
        <Button
          label={tr("粘贴")}
          size="md"
          variant="ghost"
          isDisabled={!connected}
          onClick={() => void paste()}
        /></>}
      </Stack>}
      <VisuallyHidden role="status">{searchOpen ? localizeMessage(searchMessage) : ''}</VisuallyHidden>
      {props.active && prompt && <TerminalConnectionNotice prompt={prompt} host={props.host} presentation={noticePresentation}/>}
      <StackItem ref={pane} size="fill" inert={hideTerminal} style={{
        minHeight: 0, minWidth: 0, position: hideTerminal ? 'absolute' : 'relative',
        inset: hideTerminal ? 0 : undefined, visibility: hideTerminal ? 'hidden' : undefined,
      }}
        onPointerDownCapture={() => setLinkHint(null)} onWheelCapture={() => setLinkHint(null)}>
        <ContextMenu ref={fillContextTrigger} label={tr("终端操作")} presentation="adaptive" isDisabled={hideTerminal||!props.active||props.focused===false}
          onOpenChange={open=>{if(open)setHasSelection(!!terminal.current?.getSelection())}}
          onContextMenuCapture={event=>{if(terminal.current&&terminal.current.modes.mouseTrackingMode!=='none'&&!event.shiftKey){event.preventDefault();event.stopPropagation()}}}
          onContextMenu={event=>event.stopPropagation()}
          items={[{label:tr("复制所选内容"),isDisabled:!hasSelection,onClick:()=>void copy()},{label:tr("粘贴"),isDisabled:!connected,onClick:()=>void paste()},
            {label:tr("ZMODEM 上传…"),isDisabled:!connected||transferring||!props.transport?.startZmodemUpload,onClick:startUpload},
            {type:'divider'},{label:tr("搜索终端历史"),onClick:openSearch},
            {label:tr("清屏"),onClick:()=>terminal.current?.clear()}]}>
        <Stack
          height="100%"
          padding={2}
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
          }}
        >
          <Stack ref={mount} height="100%" style={{minHeight:0,minWidth:0,overflow:'hidden'}}
            tabIndex={historyOnly ? (props.focused === false ? -1 : 0) : undefined}
            onPointerDownCapture={event => { if (historyOnly) event.currentTarget.focus({ preventScroll: true }) }}
            onKeyDown={event => {
              if (!historyOnly) return
              const action = findShortcut(event.nativeEvent, ['terminal-copy', 'session-focus'])
              if (!action) return
              event.preventDefault()
              if (action === 'terminal-copy') void copy()
              else document.getElementById('tab-' + (props.tabId ?? props.id))?.focus()
            }}/>
        </Stack>
        </ContextMenu>
        {linkHint && props.active && !hideTerminal && <div style={{ position: 'absolute', left: linkHint.left,
          top: linkHint.top, marginTop: '-0.75em', width: 1, height: '1.5em', pointerEvents: 'none' }}>
          <Tooltip isOpen placement="below" content={tr('{p0} + 单击', detectFontPlatform() === 'macos' ? '⌘' : 'Ctrl')}
            onOpenChange={open => { if (!open) setLinkHint(null) }} touchTrigger="none" focusTrigger="never">
            <span style={{ display: 'block', height: '100%' }}/>
          </Tooltip>
        </div>}
      </StackItem>
    </StackItem>
    {transferView&&zmodem.current&&createPortal(<Theme theme={getWorkbenchThemeByName(themeName)} mode={props.mode}><Suspense fallback={null}><ZmodemDialog engine={zmodem.current} view={transferView} serverName={props.host.name}/></Suspense></Theme>,document.body)}
  </>)
})
