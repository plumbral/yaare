import type { ITheme } from '@xterm/xterm'
import { readableColor } from './contrast'
import { ansiNames, resolveTerminalPalette, terminalPresetSources, type FixedTerminalPalette, type ResolvedTerminalPalette, type TerminalPalette } from './terminalPresets'
import type { ColorMode, InterfacePalette } from './palettes'

export const terminalColors = {
  background: ['#f9faf9', '#2d2e2f'],
  foreground: ['#262a30', '#dce0e6'],
  cursor: ['#262a30', '#dce0e6'],
  cursorAccent: ['#e9e9e7', '#202122'],
  selectionBackground: ['#cbd7e9', '#3c506b'],
  selectionForeground: ['#172b4d', '#f5f7fa'],
  selectionInactiveBackground: ['#dce2eb', '#354050'],
  black: ['#262626', '#20252c'],
  red: ['#b42332', '#e58b8b'],
  green: ['#25692c', '#a2c48a'],
  yellow: ['#795c00', '#d8bd81'],
  blue: ['#2455a6', '#8bacdf'],
  magenta: ['#8540a3', '#c0a0d8'],
  cyan: ['#006872', '#83c3c7'],
  white: ['#5b616b', '#c9ced6'],
  brightBlack: ['#62666d', '#959ba6'],
  brightRed: ['#a3152f', '#efa1a1'],
  brightGreen: ['#236328', '#b2d49a'],
  brightYellow: ['#785900', '#e6ce98'],
  brightBlue: ['#1f4fa0', '#a3bff0'],
  brightMagenta: ['#7e349f', '#d0b2e4'],
  brightCyan: ['#00636d', '#9bd2d5'],
  brightWhite: ['#4a4d54', '#f1f2f4'],
} as const
type ColorPair = readonly [light: string, dark: string]
type TerminalOverrides = Partial<Record<keyof typeof terminalColors, ColorPair>>
const automaticPalettes: Record<InterfacePalette, TerminalOverrides> = {
  neutral: {},
  paper: {
    background: ['#f8f7f2', '#201e1c'], foreground: ['#322a24', '#ece1d2'],
    cursor: ['#8a432c', '#eead91'],
    selectionBackground: ['#e4c9b7', '#614236'], selectionInactiveBackground: ['#e5d9cc', '#4c3b34'],
    red: ['#a62f36', '#ef9990'], green: ['#35612b', '#abc581'], yellow: ['#76540b', '#dfbd7a'],
    blue: ['#305789', '#92b3df'], magenta: ['#824774', '#cea4ca'], cyan: ['#23666a', '#91c1b6'],
    white: ['#655d52', '#d7cbbd'], brightBlack: ['#686158', '#a79a89'],
    brightRed: ['#92283a', '#f7aea6'], brightGreen: ['#2b5624', '#bed498'], brightYellow: ['#684900', '#eed095'],
    brightBlue: ['#26497e', '#abc7f1'], brightMagenta: ['#713361', '#deb8db'], brightCyan: ['#16595e', '#a4d2c7'],
    brightWhite: ['#4e453c', '#f7ede1'],
  },
  purple: {
    background: ['#faf8fb', '#302d35'], foreground: ['#342a41', '#e4daef'],
    cursor: ['#6d477f', '#cfb1e5'],
    selectionBackground: ['#d8c6e4', '#514064'], selectionInactiveBackground: ['#ded5e7', '#42354e'],
    red: ['#a43150', '#e99ab1'], green: ['#356140', '#a8c799'], yellow: ['#735718', '#dbc389'],
    blue: ['#3a5595', '#9aafe6'], magenta: ['#78468b', '#caa0e2'], cyan: ['#176575', '#90c1d4'],
    white: ['#60576e', '#cec3dc'], brightBlack: ['#62586d', '#a297b1'],
    brightRed: ['#93203f', '#f6aec2'], brightGreen: ['#2c5436', '#baddad'], brightYellow: ['#644908', '#ebd4a1'],
    brightBlue: ['#2b4689', '#b0c3f4'], brightMagenta: ['#703685', '#deb6f0'], brightCyan: ['#09576b', '#a4d1e3'],
    brightWhite: ['#4b415a', '#f4eaf9'],
  },
  blue: {
    background: ['#f8fafb', '#2e343a'], foreground: ['#253643', '#dce8f0'],
    cursor: ['#375c7c', '#a6c9e6'],
    selectionBackground: ['#c7d9e8', '#3b5570'], selectionInactiveBackground: ['#d1dfe9', '#34465a'],
    red: ['#a52d42', '#ef9aab'], green: ['#29634a', '#9ecaa6'], yellow: ['#765613', '#e0c184'],
    blue: ['#315a94', '#8bb2ed'], magenta: ['#794480', '#c8a8e1'], cyan: ['#116471', '#87c8d5'],
    white: ['#54606b', '#c5d5e0'], brightBlack: ['#586673', '#93a8bb'],
    brightRed: ['#94263a', '#f5b1bf'], brightGreen: ['#20583d', '#b2deb9'], brightYellow: ['#684a08', '#f0d49a'],
    brightBlue: ['#234a86', '#a6c7f8'], brightMagenta: ['#6a3477', '#dbbcef'], brightCyan: ['#065967', '#9bdce7'],
    brightWhite: ['#414f5b', '#f0f6fa'],
  },
  green: {
    background: ['#f9faf6', '#30362f'], foreground: ['#29372c', '#e1eadc'],
    cursor: ['#3d6448', '#b1d5a9'],
    selectionBackground: ['#c9dac4', '#405e48'], selectionInactiveBackground: ['#d6e1d0', '#374a3c'],
    red: ['#9e343c', '#ea9c91'], green: ['#34632c', '#a7cd8c'], yellow: ['#715713', '#ddc581'],
    blue: ['#33598d', '#96b7e2'], magenta: ['#7b447d', '#c9a7d1'], cyan: ['#216568', '#88c8bb'],
    white: ['#586357', '#cbd8c8'], brightBlack: ['#5e695e', '#9eae9b'],
    brightRed: ['#8e2833', '#f5b1a6'], brightGreen: ['#2a5724', '#badea0'], brightYellow: ['#644d09', '#ecd89b'],
    brightBlue: ['#274d83', '#adc9ef'], brightMagenta: ['#6e3671', '#dfbde7'], brightCyan: ['#12595e', '#9bdace'],
    brightWhite: ['#465548', '#f0f6eb'],
  },
}
export const terminalContrastRatio = 4.5
export const terminalToken = (name: string, preset: ResolvedTerminalPalette = 'auto') => '--color-terminal-' + (preset === 'auto' ? '' : preset + '-') + name.replace(/[A-Z]/g, c => '-' + c.toLowerCase())
type TerminalColors = Record<keyof typeof terminalColors, string>
function adapt(source: TerminalColors): TerminalColors {
  const result = { ...source }
  for (const name of ['foreground', ...ansiNames.filter(name => name !== 'black')] as const) result[name] = readableColor(result[name], result.background)
  result.cursor = readableColor(result.cursor, result.background)
  result.cursorAccent = result.background
  result.selectionForeground = readableColor(result.foreground, result.selectionBackground)
  return result
}
export const terminalPaletteColors = Object.fromEntries(terminalPresetSources.map(p => {
  const source = Object.fromEntries(ansiNames.map((name, index) => [name, p.ansi[index]]))
  return [p.id, adapt({ ...source, background: p.background, foreground: p.foreground, cursor: p.foreground, cursorAccent: p.background, selectionBackground: p.selection, selectionInactiveBackground: p.selection, selectionForeground: p.foreground } as TerminalColors)]
})) as Record<FixedTerminalPalette, TerminalColors>
function automaticColors(id: InterfacePalette, mode: ColorMode): TerminalColors {
  const index = mode === 'light' ? 0 : 1
  return adapt(Object.fromEntries(Object.entries({ ...terminalColors, ...automaticPalettes[id] })
    .map(([name, values]) => [name, values[index]])) as TerminalColors)
}
export function automaticTerminalTokens(id: InterfacePalette) {
  const light=automaticColors(id,'light'), dark=automaticColors(id,'dark')
  const pair=(a:string,b:string)=>`light-dark(${a}, ${b})`
  return {
    ...Object.fromEntries(Object.entries(light).map(([name,color])=>[terminalToken(name),pair(color,dark[name as keyof TerminalColors])])),
    [terminalToken('readableBlack')]:pair(readableColor(light.black,light.background),readableColor(dark.black,dark.background)),
  }
}
export function getTerminalColors(preset: TerminalPalette, mode: ColorMode, interfacePalette: InterfacePalette = 'neutral'): TerminalColors {
  const resolved=resolveTerminalPalette(preset,mode)
  return resolved === 'auto' ? automaticColors(interfacePalette, mode) : terminalPaletteColors[resolved]
}
export const terminalColorTokens = {
  ...Object.fromEntries(Object.entries(terminalPaletteColors).flatMap(([id, colors]) => Object.entries(colors).map(([name, color]) => [terminalToken(name, id as FixedTerminalPalette), color]))),
  ...Object.fromEntries(Object.entries(terminalPaletteColors).map(([id, colors]) => [terminalToken('readableBlack', id as FixedTerminalPalette), readableColor(colors.black, colors.background)])),
}
export function terminalTheme(tokens: Readonly<Record<string, unknown>>, preset: ResolvedTerminalPalette = 'auto'): ITheme {
  return Object.fromEntries(Object.keys(terminalColors).map(name => [name, tokens[terminalToken(name, preset)]]))
}
