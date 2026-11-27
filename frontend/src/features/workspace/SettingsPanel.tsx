import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useMediaQuery } from '@astryxdesign/core/hooks'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Section } from '@astryxdesign/core/Section'
import { Text, Heading } from '@astryxdesign/core/Text'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { Selector } from '@astryxdesign/core/Selector'
import { Switch } from '@astryxdesign/core/Switch'
import { Tooltip } from '@astryxdesign/core/Tooltip'
import { Button } from '@astryxdesign/core/Button'
import BrowserStatus from '../../shared/ui/BrowserStatus'
import AboutSettingsPanel from './AboutSettingsPanel'
import SyncSettingsPanel from '../sync/SyncSettingsPanel'
import { maxTerminalFontSize, minTerminalFontSize, type Preferences } from './preferences'
import { restoreShortcuts } from './shortcutReference'
import ShortcutSettings from './ShortcutSettings'
import SettingsCategoryTabs from './SettingsCategoryTabs'
import { settingsSections, readSettingsNavigation, saveSettingsNavigation, canResetSettingsSection, resetSettingsSection, type SettingsSection } from './settingsNavigation'
import { languageOptions, normalizeLanguagePreference } from '../../shared/i18n/locale'
import FontSelector from '../../shared/fonts/FontSelector'
import {uiFonts,terminalFonts,fallbackFonts,terminalFontStack,normalizeUIFont,normalizeTerminalFont,normalizeFallbackFont} from '../../shared/fonts/fonts'
import { InterfacePalettePicker, TerminalPalettePreview } from './ThemeSettings'
import TerminalPalettePicker from './TerminalPalettePicker'
import type { RememberWorkspaceState } from './useRememberWorkspace'

const controlWidth = 'min(100%, calc(var(--spacing-10) * 6.5))'
const terminalFontSizeOptions = Array.from({ length: maxTerminalFontSize - minTerminalFontSize + 1 }, (_, index) => {
  const size = String(minTerminalFontSize + index)
  return { value: size, label: size }
})

function SettingRow({ label, controlId, description, children, divider = false }: {
  label: string; controlId?: string; description?: string
  children: ReactNode; divider?: boolean
}) {
  return <Stack direction="horizontal" align="center" justify="between" wrap="wrap" gap={3} paddingBlock={3}
    style={divider ? { borderBlockEnd: 'var(--border-width) solid var(--color-border)' } : undefined}>
    <StackItem size="fill" style={{ flexBasis: 'calc(var(--spacing-10) * 4)' }}>
      {controlId ? <label htmlFor={controlId}><Text type="label">{label}</Text></label> : <Text type="label">{label}</Text>}
      {description && <Text type="supporting" color="secondary" id={controlId + '-hint'}>{description}</Text>}
    </StackItem>
    <Stack width={controlWidth} align="end" style={{ flexShrink: 0 }}>{children}</Stack>
  </Stack>
}

function RememberWorkspaceStatus({ remember }: { remember: RememberWorkspaceState }) {
  useLocale()
  const busy = useSyncExternalStore(remember.subscribeBusy, remember.readBusy, remember.readBusy)
  if (!remember.error && !busy && remember.ready) return null
  return <Text type="supporting" role={remember.error ? 'alert' : 'status'} color="secondary">
    {remember.error || (busy ? tr("正在保存工作区…") : tr("正在读取工作区…"))}
  </Text>
}

export default function SettingsPanel({ tabsOnTop,onTabPosition,value, resolvedMode, systemMode, onChange, saved,remember,syncHistoryRequest=0,showManagementShortcuts=false }: {
  value: Preferences; onChange: (value: Preferences) => void; saved: boolean; resolvedMode: 'light' | 'dark'; systemMode: 'light' | 'dark'
  tabsOnTop:boolean;onTabPosition:(top:boolean)=>void
  syncHistoryRequest?: number
  showManagementShortcuts?: boolean
  remember?: RememberWorkspaceState
}) {
  useLocale()

  const compact = useMediaQuery('(max-width: 900px)')
  const [navigation] = useState(readSettingsNavigation)
  const [section, setSection] = useState(navigation.section)
  const canReset = section === 'shortcuts' || canResetSettingsSection(section)
  const [shortcutResetFailed, setShortcutResetFailed] = useState(false)
  const handledSyncHistory = useRef(0)
  const panels = useRef<Partial<Record<SettingsSection, HTMLElement | null>>>({})
  const saveTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const flush = () => {
    clearTimeout(saveTimer.current)
    saveSettingsNavigation(navigation)
  }
  useEffect(() => {
    window.addEventListener('pagehide', flush)
    return () => { window.removeEventListener('pagehide', flush); flush() }
  }, [navigation])
  useLayoutEffect(() => {
    const panel = panels.current[section]
    if (panel) panel.scrollTop = navigation.scroll[section] ?? 0
  }, [section, navigation])
  function select(next: SettingsSection) {
    if (next === section) return
    setShortcutResetFailed(false)
    navigation.section = next
    flush()
    setSection(next)
  }
  useEffect(() => {
    if (syncHistoryRequest) select('sync')
  }, [syncHistoryRequest])
  const content: Record<SettingsSection, ReactNode> = {
    general: <Stack gap={6}>
          <Section padding={0} variant="transparent">
            <Heading level={4} accessibilityLevel={3} weight="semibold">{tr("通用")}</Heading>
            <SettingRow label={tr("语言")}>
              <Selector id="settings-language" label={tr("语言")} isLabelHidden size="md" width="100%" value={value.language} options={languageOptions.map(option => ({ ...option, label: option.value === 'system' ? tr('跟随系统') : option.label }))}
                onChange={language => onChange({ ...value, language: normalizeLanguagePreference(language) })} />
            </SettingRow>
          </Section>
          {remember&&<Section padding={0} variant="transparent"><Heading level={4} accessibilityLevel={3} weight="semibold">{tr("工作区")}</Heading>
            <SettingRow label={tr('标签栏位置')}><SegmentedControl label={tr('标签栏位置')} value={tabsOnTop?'top':'left'}
              layout="hug" size="md" onChange={position=>onTabPosition(position==='top')}><SegmentedControlItem value="left" label={tr('左侧')}/><SegmentedControlItem value="top" label={tr('顶部')}/></SegmentedControl></SettingRow>
            <SettingRow label={tr("记住标签与分屏")} controlId="settings-remember-workspace">
              <Tooltip content={tr('关闭会清除布局恢复记录。')} hasHoverIndication={false} focusTrigger="always" isEnabled={remember.ready&&remember.enabled}>
                <Switch id="settings-remember-workspace" label={tr("记住标签与分屏")} isLabelHidden value={remember.enabled} onChange={remember.onChange} isDisabled={!remember.ready} size="sm"/>
              </Tooltip>
            </SettingRow>
            <RememberWorkspaceStatus remember={remember}/>
            {remember.error&&<Button label={tr("重试工作区保存")} size="md" variant="secondary" onClick={remember.retry}/>}
          </Section>}

      <Section padding={0} variant="transparent">
        <Heading level={4} accessibilityLevel={3} weight="semibold">{tr("快捷命令")}</Heading>
        <SettingRow label={tr("探测服务器系统")} controlId="settings-detect-server-system">
          <Switch id="settings-detect-server-system" label={tr("探测服务器系统")} isLabelHidden
            value={value.detectServerSystem} onChange={detectServerSystem=>onChange({...value,detectServerSystem})} size="sm"/>
        </SettingRow>
      </Section>
    </Stack>,
    appearance: <Section padding={0} variant="transparent">
            <Heading level={4} accessibilityLevel={3} weight="semibold">{tr("外观")}</Heading>
            <SettingRow label={tr('界面字体')} divider>
              <FontSelector id="settings-ui-font" label={tr('界面字体')} value={value.uiFont} options={uiFonts} defaultValue="Inter" onChange={uiFont=>onChange({...value,uiFont:normalizeUIFont(uiFont)})}/>
            </SettingRow>
            <SettingRow label={tr('外观模式')}>
              <SegmentedControl label={tr('外观模式')} value={value.mode}
                onChange={mode => onChange({ ...value, mode: mode === 'system' ? 'system' : mode === 'light' ? 'light' : 'dark' })}
                layout="hug" size="md">
                <SegmentedControlItem value="light" label={tr("亮色")} /><SegmentedControlItem value="dark" label={tr("暗色")} />
                <SegmentedControlItem value="system" label={tr("自动")} />
              </SegmentedControl>
            </SettingRow>
            <Stack gap={1} paddingBlock={2}>
              <Text type="label">{tr('界面配色')}</Text>
              <InterfacePalettePicker value={value.interfacePalette} onChange={interfacePalette => onChange({ ...value, interfacePalette })}/>
            </Stack>
            <SettingRow label={tr("内置图标")}>
              <SegmentedControl label={tr("内置图标")} value={value.iconStyle} layout="hug" size="md"
                onChange={iconStyle => onChange({ ...value, iconStyle: iconStyle === 'color' ? 'color' : 'monochrome' })}>
                <SegmentedControlItem value="monochrome" label={tr("单色")}/>
                <SegmentedControlItem value="color" label={tr("彩色")}/>
              </SegmentedControl>
            </SettingRow>
    </Section>,
    terminal: <Section padding={0} variant="transparent">
            <Heading level={4} accessibilityLevel={3} weight="semibold">{tr("终端")}</Heading>
            <Stack gap={1}>
              <Text type="label">{tr('终端配色')}</Text>
              <TerminalPalettePicker id="settings-terminal-palette" value={value.terminalPalette} systemMode={systemMode}
                onChange={terminalPalette=>onChange({...value,terminalPalette})}/>
            </Stack>
            <TerminalPalettePreview preferences={value} mode={resolvedMode} systemMode={systemMode}/>
            <SettingRow label={tr('终端主字体')} divider>
              <FontSelector id="settings-terminal-font" label={tr('终端主字体')} value={value.fontFamily} options={terminalFonts} defaultValue="Google Sans Code" monospaced onChange={fontFamily=>onChange({...value,fontFamily:normalizeTerminalFont(fontFamily)})}/>
            </SettingRow>
            <SettingRow label={tr('终端补充字体')} divider>
              <FontSelector id="settings-fallback-font" label={tr('终端补充字体')} value={value.fallbackFont} options={fallbackFonts} defaultValue="Maple Mono CN" systemLabel={tr('系统回退')} onChange={fallbackFont=>onChange({...value,fallbackFont:normalizeFallbackFont(fallbackFont)})}/>
            </SettingRow>
            <Stack gap={2} paddingBlock={3}>
              <Text type="label">{tr('字体预览')}</Text>
              <Text type="supporting" color="secondary">{tr('界面预览')}{' · 中文 · 繁體中文 · 日本語 · Aa 0123456789'}</Text>
              <Text style={{fontFamily:terminalFontStack(value.fontFamily,value.fallbackFont),fontSize:value.fontSize,lineHeight:value.lineHeight,fontSizeAdjust:'none',whiteSpace:'pre-wrap'}}>{'Aa 0O 1lI {} [] ->\n中文 / 繁體 / 日本語\n─ │ ┌ ┐ └ ┘ ░ ▒ ▓ █'}</Text>
            </Stack>
            <SettingRow label={tr("终端行高")} divider>
              <Selector label={tr("终端行高")} isLabelHidden size="md" width="100%" value={String(value.lineHeight)} options={[1,1.2,1.5,1.8,2].map(n=>({value:String(n),label:String(n)}))} onChange={n=>onChange({...value,lineHeight:Number(n)})}/>
            </SettingRow>
            <SettingRow label={tr("终端字号")} divider>
              <Selector id="settings-font-size" label={tr("终端字号")} isLabelHidden size="md" width="100%"
                value={String(value.fontSize)} options={terminalFontSizeOptions}
                onChange={size => onChange({ ...value, fontSize: Number(size) })} />
            </SettingRow>
            <SettingRow label={tr("回滚行数")} divider>
              <Tooltip content={tr('降低上限会裁掉较早的历史。')} hasHoverIndication={false} focusTrigger="always" isEnabled={value.scrollback>1000}>
                <Selector id="settings-scrollback" label={tr("回滚行数")} isLabelHidden size="md" width="100%"
                  value={String(value.scrollback)} options={[1000, 3000, 5000].map(lines => ({ value: String(lines), label: lines.toLocaleString(getLanguage()) + tr(' 行') }))}
                  onChange={lines => onChange({ ...value, scrollback: Number(lines) })} />
              </Tooltip>
            </SettingRow>
            <SettingRow label={tr("光标闪烁")} controlId="settings-cursor-blink" divider>
              <Switch id="settings-cursor-blink" label={tr("光标闪烁")} isLabelHidden value={value.cursorBlink}
                onChange={cursorBlink => onChange({ ...value, cursorBlink })} size="sm" />
            </SettingRow>
            <SettingRow label={tr('终端读屏模式')} controlId="settings-terminal-screen-reader" description={tr('让屏幕阅读器朗读当前窗格的终端输出。')}>
              <Switch id="settings-terminal-screen-reader" aria-describedby="settings-terminal-screen-reader-hint" label={tr('终端读屏模式')} isLabelHidden value={value.screenReaderMode}
                onChange={screenReaderMode => onChange({ ...value, screenReaderMode })} size="sm" />
            </SettingRow>
    </Section>,
    connection: <Stack gap={6}>
      <Section padding={0} variant="transparent"><Heading level={4} accessibilityLevel={3} weight="semibold">{tr("连接")}</Heading>
            <SettingRow label={tr("自动重连")} controlId="settings-reconnect">
              <Switch id="settings-reconnect" label={tr("自动重连")} isLabelHidden value={value.autoReconnect} onChange={autoReconnect=>onChange({...value,autoReconnect})} size="sm"/>
            </SettingRow>
          </Section>
          <Section padding={0} variant="transparent"><Heading level={4} accessibilityLevel={3} weight="semibold">{tr("文件与传输")}</Heading>
            <SettingRow label={tr("SFTP 跟随终端目录")} controlId="settings-directory-sync" divider>
              <Switch id="settings-directory-sync" label={tr("SFTP 跟随终端目录")} isLabelHidden value={value.directorySync} onChange={directorySync=>onChange({...value,directorySync})} size="sm"/>
            </SettingRow>
            <SettingRow label={tr("并发任务")}>
              <Selector label={tr("并发任务")} isLabelHidden size="md" width="100%" value={String(value.transferConcurrency)} options={Array.from({length:8},(_,i)=>({value:String(i+1),label:tr("{p0} 个任务", (i+1))}))} onChange={n=>onChange({...value,transferConcurrency:Number(n)})}/>
            </SettingRow>
          </Section>
    </Stack>,
    sync: <SyncSettingsPanel historyRequest={syncHistoryRequest} handledHistory={handledSyncHistory}/>,
    shortcuts: section === 'shortcuts' && <ShortcutSettings showDesktopShortcuts={showManagementShortcuts}/>,
    about: <><AboutSettingsPanel/>{import.meta.env.MODE==='browser'&&<BrowserStatus account/>}</>,
  }
  return <Stack id="settings-page" gap={5} padding={5} width="100%"
    style={{ height: '100%', minHeight: 0, maxWidth: 'calc(var(--spacing-10) * 29)', marginInline: 'auto', overflow: 'hidden' }}>
    <Stack id="settings-header" direction="horizontal" align="center" justify="between" gap={3} wrap="wrap" style={{ flexShrink: 0 }}>
      <Stack direction="horizontal" align="center" gap={3} wrap="wrap">
        <Heading level={4} accessibilityLevel={2} weight="semibold">{tr("设置")}</Heading>
        <Text id="settings-save-status" type="supporting" color="secondary" role="status">
          {shortcutResetFailed ? tr('无法保存快捷键，原设置未更改。') : saved ? tr("自动保存 · 立即生效") : tr("本地保存不可用，关闭后会重置")}
        </Text>
      </Stack>
      <Button id="settings-reset" label={tr('恢复本页默认')} variant="secondary" size="md"
        isDisabled={!canReset} aria-hidden={!canReset} style={{ visibility: canReset ? 'visible' : 'hidden' }}
        onClick={async () => section === 'shortcuts' ? setShortcutResetFailed(!await restoreShortcuts()) : onChange(resetSettingsSection(section, value))}/>
    </Stack>
    <Stack id="settings-columns" direction={compact ? 'vertical' : 'horizontal'} gap={compact ? 3 : 6}
      style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
      <Stack style={{ flexShrink: 0, width: compact ? '100%' : 'calc(var(--spacing-10) * 5.5)',
        minHeight: 0, overflowY: compact ? undefined : 'auto',
        paddingInlineEnd: compact ? undefined : 'var(--spacing-4)',
        borderInlineEnd: compact ? undefined : 'var(--border-width) solid var(--color-border)',
      }}>
        <SettingsCategoryTabs value={section} compact={compact} onChange={select}/>
      </Stack>
      {settingsSections.map(id => <Stack key={id} id={`settings-content-${id}`} role="tabpanel"
        aria-labelledby={`settings-tab-${id}`} tabIndex={-1} hidden={section !== id}
        ref={element => { panels.current[id] = element }}
        onFocus={event => {
          if (event.target === event.currentTarget) {
            event.currentTarget.ownerDocument.getElementById(`settings-tab-${id}`)?.focus({preventScroll:true})
          }
        }}
        onScroll={event => {
          if (id !== section || !event.currentTarget.clientHeight) return
          navigation.scroll[id] = event.currentTarget.scrollTop
          clearTimeout(saveTimer.current)
          saveTimer.current = setTimeout(flush, 150)
        }}
        style={{ display: section === id ? 'flex' : 'none', flex: 1, minHeight: 0, minWidth: 0,
          overflowY: 'auto', scrollbarGutter: 'stable', overflowX: 'hidden',
        }}>
        {section === id && <Stack gap={6} paddingInline={1} paddingBlockEnd={5} style={{ flexShrink: 0, minWidth: 0 }}>{content[id]}</Stack>}
      </Stack>)}
    </Stack>
  </Stack>
}
