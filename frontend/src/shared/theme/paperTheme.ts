import { defineTheme } from '@astryxdesign/core/theme'
import { workbenchTheme } from './workbenchTheme'
import { interfaceTokens } from './palettes'
export const paperTheme = defineTheme({
  name: 'yaare-paper', extends: workbenchTheme, tokens: interfaceTokens('paper'),
  components: { button: { 'variant:primary': {
    backgroundColor:'var(--color-text-primary)',color:'var(--color-background-body)',
  } } },
})
