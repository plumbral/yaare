import { defineTheme } from '@astryxdesign/core/theme'
import { workbenchTheme } from './workbenchTheme'
import { interfaceTokens } from './palettes'
export const greenTheme = defineTheme({ name: 'yaare-green', extends: workbenchTheme, tokens: interfaceTokens('green') })
