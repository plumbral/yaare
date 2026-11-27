import { automaticTerminalTokens } from './terminalColors'

export const interfacePaletteIds = ['neutral', 'paper', 'purple', 'blue', 'green'] as const
export type InterfacePalette = typeof interfacePaletteIds[number]
export type ColorMode = 'light' | 'dark'
type Pair = readonly [string, string]
interface Palette { label: string; body: Pair; surface: Pair; popover: Pair; text: Pair; secondary: Pair; accent: Pair; muted: Pair; border: Pair; controlBorder: Pair }
export const interfacePalettes = {
  neutral: { label: '中性', body: ['#eeefee', '#202122'], surface: ['#f6f7f6', '#28292a'], popover: ['#fafaf9', '#303132'], text: ['#282b2d', '#dddeda'], secondary: ['#575b5c', '#b1b4b5'], accent: ['#484d50', '#b9bdbf'], muted: ['#e0e2e1', '#36393a'], border: ['#d2d4d3', '#454748'], controlBorder: ['#7c8281', '#777d7d'] },
  paper: { label: '暖砂', body: ['#FAF9F5', '#141413'], surface: ['#F5F4ED', '#1A1918'], popover: ['#F5F4ED', '#262624'], text: ['#141413', '#FAF9F5'], secondary: ['#5E5D59', '#B0AEA5'], accent: ['#D97757', '#C46849'], muted: ['#F0EEE6', '#262624'], border: ['#B0AEA5', '#403E3A'], controlBorder: ['#87867F', '#87867F'] },
  purple: { label: '雾紫', body: ['#eeecef', '#242328'], surface: ['#f6f4f6', '#2b292f'], popover: ['#faf8fa', '#343139'], text: ['#302e33', '#dedbe2'], secondary: ['#5b5661', '#b5afbc'], accent: ['#695d74', '#b6a8c3'], muted: ['#e6e0eb', '#3c3544'], border: ['#d6d1d8', '#49444e'], controlBorder: ['#827887', '#8c8195'] },
  blue: { label: '静蓝', body: ['#ebeff1', '#22262a'], surface: ['#f4f6f7', '#292e33'], popover: ['#f8fafb', '#333940'], text: ['#293136', '#dce2e6'], secondary: ['#515c64', '#abb7c0'], accent: ['#4f6473', '#a6b9c8'], muted: ['#dee7ec', '#343f49'], border: ['#d0d8dd', '#424c54'], controlBorder: ['#75848e', '#81919e'] },
  green: { label: '苔绿', body: ['#eceee9', '#232722'], surface: ['#f5f6f2', '#2b302a'], popover: ['#f9faf6', '#343b33'], text: ['#2e342e', '#dee3db'], secondary: ['#535e52', '#afb9ab'], accent: ['#536651', '#acbea8'], muted: ['#e1e8dc', '#374235'], border: ['#d1d8ce', '#454f44'], controlBorder: ['#788575', '#83937f'] },
} as const satisfies Record<InterfacePalette, Palette>
export const normalizeInterfacePalette = (value: unknown): InterfacePalette => interfacePaletteIds.includes(value as InterfacePalette) ? value as InterfacePalette : 'neutral'
export const interfacePreviewTokens = Object.fromEntries(interfacePaletteIds.flatMap(id => ['body', 'surface', 'accent', 'text'].map(role => [`--color-preview-${id}-${role}`, [...interfacePalettes[id][role as 'body' | 'surface' | 'accent' | 'text']]]))) as Record<string, [string, string]>
export function interfaceTokens(id: InterfacePalette): Record<string, string | [string, string]> {
  const p = interfacePalettes[id]
  const pair = (value: Pair): [string, string] => [...value]
  return {
    ...automaticTerminalTokens(id),
    '--color-workspace-frame': id === 'paper' ? ['#F4F3EE', '#1A1918'] : pair(p.surface),
    '--color-background-body': pair(p.body), '--color-background-muted': pair(id==='paper'?p.muted:p.body),
    '--color-background-surface': pair(p.surface), '--color-background-card': pair(p.surface),
    '--color-background-popover': pair(p.popover), '--color-syntax-background': pair(p.surface),
    '--color-text-primary': pair(p.text), '--color-icon-primary': pair(p.text),
    '--color-text-secondary': pair(p.secondary), '--color-icon-secondary': pair(p.secondary),
    '--color-accent': pair(p.accent),
    '--color-text-accent': id==='paper'?['#9D472F','#E2987E']:pair(p.accent),
    '--color-icon-accent': id==='paper'?['#C6613F','#C46849']:pair(p.accent),
    '--focus-outline-color': id==='paper'?['#C6613F','#C46849']:pair(p.accent),
    '--color-accent-muted': pair(p.muted),
    '--color-neutral': id==='paper'?['#0000000f','#ffffff1a']:pair(p.muted),
    '--color-on-accent': id==='paper'?['#141413','#141413']:['#f8f7f3', '#242625'],
    '--color-border': pair(p.border), '--color-border-emphasized': pair(p.controlBorder), '--color-skeleton': pair(p.muted),
    '--color-text-info': ['#47617d', '#a4b7ce'], '--color-info': ['#47617d', '#a4b7ce'],
    '--color-text-success': ['#406447', '#a6bea6'], '--color-success': ['#406447', '#a6bea6'],
    '--color-text-warning': ['#735d27', '#cfb989'], '--color-warning': ['#735d27', '#cfb989'], '--color-on-warning': ['#f8f7f3', '#242625'],
    '--color-text-error': ['#9b4443', '#d6a39f'], '--color-error': ['#9b4443', '#d6a39f'],
  }
}
