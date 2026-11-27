import { Stack } from '@astryxdesign/core/Stack'
import { Button } from '@astryxdesign/core/Button'
import { tr } from '../../shared/i18n/messages'
import HorizontalScrollArea from '../../shared/ui/HorizontalScrollArea'
import { settingsSections, type SettingsSection } from './settingsNavigation'

const labels: Record<SettingsSection, string> = {
  general: '通用', appearance: '外观', terminal: '终端', connection: '连接与传输',
  sync: '同步与备份', shortcuts: '快捷键', about: '关于',
}
export default function SettingsCategoryTabs({ value, compact, onChange }: {
  value: SettingsSection; compact: boolean; onChange: (section: SettingsSection) => void
}) {
  const tabs = <Stack role="tablist" aria-label={tr('设置分类')} aria-orientation={compact ? 'horizontal' : 'vertical'}
    direction={compact ? 'horizontal' : 'vertical'} gap={1} width={compact ? 'max-content' : '100%'}
    onKeyDown={event => {
      if (event.altKey || event.ctrlKey || event.metaKey) return
      const nextKey = compact ? 'ArrowRight' : 'ArrowDown', previousKey = compact ? 'ArrowLeft' : 'ArrowUp'
      const index = settingsSections.indexOf(value)
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? settingsSections.length - 1
        : event.key === nextKey ? (index + 1) % settingsSections.length
        : event.key === previousKey ? (index + settingsSections.length - 1) % settingsSections.length : -1
      if (next < 0) return
      event.preventDefault()
      const section = settingsSections[next]!
      onChange(section)
      event.currentTarget.querySelector<HTMLElement>(`#settings-tab-${section}`)?.focus()
    }}>
    {settingsSections.map(section => <Button key={section} id={`settings-tab-${section}`} role="tab"
      aria-selected={section === value} aria-controls={`settings-content-${section}`} tabIndex={section === value ? 0 : -1}
      label={tr(labels[section])} variant={section === value ? 'secondary' : 'ghost'} size="md"
      width={compact ? undefined : '100%'} onClick={() => onChange(section)}
      style={{ justifyContent: 'flex-start', flexShrink: 0,
        borderBlockEnd: `var(--spacing-0-5) solid ${compact && section === value ? 'var(--color-text-primary)' : 'transparent'}`,
      }}/>) }
  </Stack>
  return compact ? <HorizontalScrollArea id="settings-tabs" label={tr('设置分类')} value={value}>{tabs}</HorizontalScrollArea> : tabs
}
