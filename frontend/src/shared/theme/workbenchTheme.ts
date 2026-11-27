import { defineTheme } from '@astryxdesign/core/theme'
import { neutralTheme, neutralIconRegistry } from '@astryxdesign/theme-neutral'
import { terminalColorTokens } from './terminalColors'
import { interfaceTokens, interfacePreviewTokens } from './palettes'

const focusFill = {
  backgroundImage: 'linear-gradient(var(--color-overlay-hover), var(--color-overlay-hover))',
  outline: 'none',
  boxShadow: 'none',
}
const flatSurface = {
  boxShadow: 'none', outline: 'none',
  ':hover:where(:not([data-disabled], [aria-disabled="true"], :focus-within))': {
    borderColor: 'color-mix(in srgb, var(--color-border-emphasized) 85%, var(--color-text-primary))', boxShadow: 'none',
  },
  ':focus-within': focusFill,
  ':where(.astryx-input-group *)': { borderWidth: '0', boxShadow: 'none', outline: 'none', backgroundColor: 'transparent' },
  ':where(.astryx-input-group *):hover': { boxShadow: 'none', outline: 'none' },
  ':where(.astryx-input-group *):focus-within': { backgroundImage: 'none', boxShadow: 'none', outline: 'none' },
}
const flatStatus = (color: string) => ({
  borderColor: color, boxShadow: 'none',
  ':hover': { borderColor: color, boxShadow: 'none' },
  ':focus-within': { ...focusFill, borderColor: color },
  ':where(.astryx-input-group *):focus-within': { backgroundImage: 'none', boxShadow: 'none', outline: 'none' },
})
const flatField = {
  base: flatSurface,
  'status:error': flatStatus('var(--color-error)'),
  'status:warning': flatStatus('var(--color-warning)'),
  'status:success': flatStatus('var(--color-success)'),
}

export const workbenchTheme = defineTheme({
  name: 'yaare', extends: neutralTheme,
  icons: neutralIconRegistry,
  motion: { tempo: 0.8, character: 'crisp' },
  tokens: { ...terminalColorTokens, ...interfacePreviewTokens, ...interfaceTokens('neutral'), '--focus-outline-style': 'none', '--font-family-body': 'var(--yaare-ui-font, Inter, system-ui, sans-serif)', '--font-family-heading': 'var(--yaare-ui-font, Inter, system-ui, sans-serif)', '--duration-medium': 'var(--duration-fast)', '--duration-medium-min': 'var(--duration-fast-min)', '--duration-medium-max': 'var(--duration-fast-max)' },
  components: {
    toast: {
      base: { width: 'auto', paddingBlock: 'var(--spacing-2)', paddingInline: 'var(--spacing-3)',
        backgroundColor: 'var(--color-background-popover)', color: 'var(--color-text-primary)',
        border: 'var(--border-width) solid var(--color-border-emphasized)', borderRadius: 'var(--radius-element)', boxShadow: 'var(--shadow-med)' },
      'type:error': { borderColor: 'var(--color-error)' },
    },
    'text-input': flatField,
    'text-area': flatField,
    selector: flatField,
    'selector-search': { base: flatSurface },
    'field-status': {
      base: { backgroundColor: 'transparent', padding: '0', marginTop: 'var(--spacing-1)', borderRadius: '0', boxShadow: 'none' },
      'type:error': { color: 'var(--color-error)' },
      'type:warning': { color: 'var(--color-warning)' },
      'type:success': { color: 'var(--color-success)' },
    },
    'checkbox-indicator': {
      base: { width: 'var(--spacing-4)', height: 'var(--spacing-4)', borderRadius: 'var(--spacing-1)' },
    },
    'checkbox-indicator-check': {
      base: { width: 'var(--spacing-3)', height: 'var(--spacing-3)' },
    },
    'checkbox-indicator-dash': {
      base: { width: 'var(--spacing-2)', height: 'var(--border-width)' },
    },
    dialog: {
      base: { '::backdrop': { overflow: 'hidden', overscrollBehavior: 'contain' } },
    },
    'selector-option-row': {
      base: { borderRadius: 'var(--radius-inner)' },
    },
    'input-group': {
      ...flatField,
      base: {
        ...flatSurface,
        borderWidth: 'var(--border-width)', borderStyle: 'solid', borderColor: 'var(--color-border-emphasized)',
        borderRadius: 'var(--radius-element)', backgroundColor: 'var(--color-background-surface)',
      },
    },
  },
})
