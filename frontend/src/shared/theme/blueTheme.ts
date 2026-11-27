import { defineTheme } from '@astryxdesign/core/theme'
import { workbenchTheme } from './workbenchTheme'
import { interfaceTokens } from './palettes'
export const blueTheme = defineTheme({ name: 'yaare-blue', extends: workbenchTheme, tokens: interfaceTokens('blue') })
