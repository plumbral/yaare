import { defineTheme } from '@astryxdesign/core/theme'
import { workbenchTheme } from './workbenchTheme'
import { interfaceTokens } from './palettes'
export const purpleTheme = defineTheme({ name: 'yaare-purple', extends: workbenchTheme, tokens: interfaceTokens('purple') })
