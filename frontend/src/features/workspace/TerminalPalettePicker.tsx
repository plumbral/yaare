import { useEffect, useRef } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { Stack } from '@astryxdesign/core/Stack'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import type { ColorMode } from '../../shared/theme/palettes'
import { terminalToken } from '../../shared/theme/terminalColors'
import { terminalPaletteFamilies, terminalPaletteSelection, type ResolvedTerminalPalette, type TerminalPalette } from '../../shared/theme/terminalPresets'
import './terminalPalettePicker.css'

function SampleColors({ id, lightHalf = false }: { id: ResolvedTerminalPalette; lightHalf?: boolean }) {
  const color = (name: string) => `var(${terminalToken(name, id)})`
  return <Stack as="span" direction="horizontal" align="center" height="100%" paddingInline={2}
    className={lightHalf ? 'terminal-palette-light-half' : undefined}
    style={{ background: color('background'), color: color('foreground') }}>
    <code className="terminal-palette-command">
      <Stack as="span" style={{ display: 'inline', color: color('cyan') }}>$</Stack>{' '}
      <Stack as="span" style={{ display: 'inline', color: color('yellow') }}>echo</Stack>{' “Yaare”'}
    </code>
  </Stack>
}

function TerminalSample({ id, light }: { id: ResolvedTerminalPalette; light?: ResolvedTerminalPalette }) {
  return <Stack as="span" aria-hidden="true" width="calc(var(--spacing-10) * 3.55)" height="var(--spacing-7)"
    style={{ position: 'relative', border: 'var(--border-width) solid var(--color-border)',
      borderRadius: 'var(--radius-inner)', flexShrink: 0, overflow: 'hidden' }}>
    <SampleColors id={id}/>
    {light && <SampleColors id={light} lightHalf/>}
  </Stack>
}

export default function TerminalPalettePicker({ value, systemMode, onChange, id }: {
  value: TerminalPalette; systemMode: ColorMode; onChange: (id: TerminalPalette) => void; id?: string
}) {
  useLocale()
  const { family, mode } = terminalPaletteSelection(value)
  const lastFamily = useRef(family)
  useEffect(() => {
    if (mode !== 'follow') lastFamily.current = family
  }, [family, mode])

  const previewMode = mode === 'light' || mode === 'dark' ? mode : systemMode
  return <Stack id={id} gap={2} data-terminal-palette="">
    <SegmentedControl data-palette-mode-bar="" label={tr("终端配色")} value={mode} size="sm" layout="fill"
      onChange={next => {
        if (next === 'follow') onChange('auto')
        else if (next === 'system') onChange(lastFamily.current.id)
        else if (next === 'light' || next === 'dark') onChange(lastFamily.current[next])
      }}>
      <SegmentedControlItem value="follow" label={tr("跟随界面外观")}/>
      <SegmentedControlItem value="light" label={tr("亮色")}/>
      <SegmentedControlItem value="dark" label={tr("暗色")}/>
      <SegmentedControlItem value="system" label={tr("自动")}/>
    </SegmentedControl>
    <Stack role="group" aria-label={tr("终端配色")} style={{ borderBlock: 'var(--border-width) solid var(--color-border)' }}>
      {terminalPaletteFamilies.map((option, index) => {
        const selected = mode !== 'follow' && family.id === option.id
        return <Stack key={option.id} style={{
          borderBlockStart: index > 0 ? 'var(--border-width) solid var(--color-border)' : undefined,
        }}>
          <Button data-terminal-palette-row="" type="button" label={option.name} aria-pressed={selected} variant="ghost" size="sm"
            endContent={mode === 'system'
              ? <TerminalSample id={option.dark} light={option.light}/>
              : <TerminalSample id={option[previewMode]}/>}
            onClick={() => onChange(mode === 'light' || mode === 'dark' ? option[mode] : option.id)}
            style={{
              width: '100%', height: 'auto', flexDirection: 'row', alignItems: 'center',
              justifyContent: 'space-between', gap: 'var(--spacing-2)', textAlign: 'start',
              padding: 'var(--spacing-2) var(--spacing-2)', borderRadius: 'var(--radius-element)',
              backgroundColor: selected ? 'var(--color-neutral)' : undefined,
              boxShadow: selected ? 'inset 0 0 0 var(--border-width) var(--color-accent)' : undefined,
            }}/>
        </Stack>
      })}
    </Stack>
  </Stack>
}

