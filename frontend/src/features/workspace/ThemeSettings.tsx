import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { interfacePaletteIds, interfacePalettes, type InterfacePalette, type ColorMode } from '../../shared/theme/palettes'
import { ansiNames, resolveTerminalPalette, terminalPaletteMode } from '../../shared/theme/terminalPresets'
import { terminalToken } from '../../shared/theme/terminalColors'
import { terminalFontStack } from '../../shared/fonts/fonts'
import type { Preferences } from './preferences'

export function InterfacePalettePicker({ value, onChange }: { value: InterfacePalette; onChange: (id: InterfacePalette) => void }) {
  useLocale()
  return <Stack role="group" aria-label={tr('界面配色')} direction="horizontal" gap={2} wrap="wrap" paddingBlock={2}>
    {interfacePaletteIds.map(id => <Button key={id} label={tr(interfacePalettes[id].label)} aria-pressed={id === value} variant="secondary" size="md" onClick={() => onChange(id)}
      style={{ flex: '1 1 calc(var(--spacing-10) * 2)', height: 'auto', padding: 'var(--spacing-2)', border: 'var(--border-width) solid', borderColor: id === value ? 'var(--color-accent)' : 'transparent' }}>
      <Stack gap={2} width="100%" align="center">
        <Stack aria-hidden="true" width="calc(var(--spacing-10) * 1.8)" padding={2} gap={2} style={{ background: `var(--color-preview-${id}-body)`, color: `var(--color-preview-${id}-text)`, borderRadius: 'var(--radius-inner)' }}>
          <Stack direction="horizontal" gap={1} align="center">
            <StackItem style={{ background: `var(--color-preview-${id}-accent)`, width: 'var(--spacing-3)', height: 'var(--spacing-1)', borderRadius: 'var(--radius-inner)' }}/>
            <StackItem size="fill" style={{ background: `var(--color-preview-${id}-surface)`, height: 'var(--spacing-1)' }}/>
          </Stack>
          <Stack padding={1} style={{ background: `var(--color-preview-${id}-surface)`, borderRadius: 'var(--radius-inner)' }}><Text type="supporting" style={{ color: 'inherit' }}>Aa</Text></Stack>
        </Stack>
        <Text type="supporting" style={{ color: 'inherit', whiteSpace: 'normal', textAlign: 'center', textWrap: 'balance', lineBreak: 'strict' }}>{tr(interfacePalettes[id].label)}</Text>
      </Stack>
    </Button>)}
  </Stack>
}

export function TerminalPalettePreview({ preferences, mode, systemMode }: { preferences: Preferences; mode: ColorMode; systemMode: ColorMode }) {
  useLocale()
  const id = resolveTerminalPalette(preferences.terminalPalette,systemMode)
  const color = (name: string) => `var(${terminalToken(name, id)})`
  return <Stack aria-label={tr('终端配色预览')} gap={2} padding={3} style={{
    colorScheme: terminalPaletteMode(id, mode), backgroundColor: color('background'), color: color('foreground'),
    borderRadius: 'var(--radius-element)', border: 'var(--border-width) solid var(--color-border)',
    fontFamily: terminalFontStack(preferences.fontFamily, preferences.fallbackFont), fontSizeAdjust: 'none',
  }}>
    <Text type="supporting" style={{ color: 'inherit', fontFamily: 'inherit' }}>{'Aa 0123 · 中文 · 日本語'}</Text>
    <Stack direction="horizontal" wrap="wrap" gap={2}>
      {ansiNames.map((name, index) => <Text key={name} type="supporting" aria-label={tr('ANSI 色 {p0}', index)} style={{ fontFamily: 'inherit', color: color(name === 'black' ? 'readableBlack' : name) }}>{index.toString().padStart(2, '0') + ' Aa'}</Text>)}
    </Stack>
    <Stack direction="horizontal" gap={2} align="center">
      <Text type="supporting" style={{ fontFamily: 'inherit', backgroundColor: color('selectionBackground'), color: color('selectionForeground') }}>{tr('选中文字')}</Text>
      <Text aria-label={tr('光标')} style={{ fontFamily: 'inherit', color: color('cursor') }}>▎</Text>
    </Stack>
  </Stack>
}
