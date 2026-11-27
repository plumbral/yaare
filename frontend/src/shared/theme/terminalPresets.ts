import presets from './terminal-presets.json'
import type { ColorMode } from './palettes'

export const terminalPaletteFamilies = [
  { id:'solarized-auto', name:'Solarized', light:'solarized-light', dark:'solarized-dark' },
  { id:'tokyo-auto', name:'Tokyo Night', light:'tokyo-day', dark:'tokyo-night' },
  { id:'catppuccin-auto', name:'Catppuccin', light:'catppuccin-latte', dark:'catppuccin-mocha' },
  { id:'gruvbox-auto', name:'Gruvbox', light:'gruvbox-light', dark:'gruvbox-dark' },
  { id:'ayu-auto', name:'Ayu', light:'ayu-light', dark:'ayu-dark' },
] as const
type TerminalPaletteFamily = typeof terminalPaletteFamilies[number]
export type FixedTerminalPalette = TerminalPaletteFamily[ColorMode]
export type ResolvedTerminalPalette = 'auto' | FixedTerminalPalette
export type TerminalPalette = ResolvedTerminalPalette | TerminalPaletteFamily['id']
export const terminalPaletteIds: readonly TerminalPalette[] = ['auto', ...terminalPaletteFamilies.flatMap(family=>[family.light,family.dark,family.id])]
export const normalizeTerminalPalette = (value: unknown): TerminalPalette => terminalPaletteIds.includes(value as TerminalPalette) ? value as TerminalPalette : 'auto'
export const terminalPresetSources = presets
export function terminalPaletteSelection(id: TerminalPalette): { family: TerminalPaletteFamily; mode: ColorMode | 'follow' | 'system' } {
  const family = terminalPaletteFamilies.find(item => item.id === id || item.light === id || item.dark === id) ?? terminalPaletteFamilies[0]
  const mode = id === 'auto' ? 'follow' : id === family.light ? 'light' : id === family.dark ? 'dark' : 'system'
  return { family, mode }
}
export function resolveTerminalPalette(id:TerminalPalette,systemMode:ColorMode):ResolvedTerminalPalette {
  const family=terminalPaletteFamilies.find(item=>item.id===id)
  if(family)return family[systemMode]
  return id as ResolvedTerminalPalette
}
export const terminalPaletteMode = (id: ResolvedTerminalPalette, mode: ColorMode):ColorMode =>
  id==='auto'?mode:terminalPaletteFamilies.some(family=>family.light===id)?'light':'dark'
export const ansiNames = ['black','red','green','yellow','blue','magenta','cyan','white','brightBlack','brightRed','brightGreen','brightYellow','brightBlue','brightMagenta','brightCyan','brightWhite'] as const
