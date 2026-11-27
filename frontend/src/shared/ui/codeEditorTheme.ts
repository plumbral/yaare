import { EditorView } from '@codemirror/view'
import { HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

export const colors = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--color-syntax-keyword)' },
  { tag: tags.string, color: 'var(--color-syntax-string)' },
  { tag: tags.comment, color: 'var(--color-syntax-comment)' },
  { tag: tags.number, color: 'var(--color-syntax-number)' },
  { tag: [tags.function(tags.variableName), tags.standard(tags.variableName)], color: 'var(--color-syntax-function)' },
  { tag: tags.variableName, color: 'var(--color-syntax-variable)' },
  { tag: tags.operator, color: 'var(--color-syntax-operator)' },
  { tag: tags.punctuation, color: 'var(--color-syntax-punctuation)' },
])
export const editorTheme = EditorView.theme({
  '&': { height: '100%', borderRadius: 'inherit', color: 'var(--color-text-primary)', backgroundColor: 'var(--color-background-surface)', fontSize: 'var(--text-supporting-size)' },
  '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-family-code)', lineHeight: '1.6' },
  '.cm-content': { paddingBlock: 'var(--spacing-2)', caretColor: 'var(--color-text-primary)' },
  '.cm-line': { paddingInline: 'var(--spacing-2)' },
  '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--color-text-secondary)', border: 'none' },
  '.cm-gutterElement': { paddingInline: 'var(--spacing-2)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-cursor': { borderInlineStartColor: 'var(--color-text-primary)' },
  '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'color-mix(in srgb, var(--color-text-primary) 16%, var(--color-background-surface))',
  },
  '&.cm-focused .cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--color-text-primary) 4%, transparent)' },
  '.cm-panels': { backgroundColor: 'var(--color-background-surface)', color: 'var(--color-text-primary)' },
  '.cm-panels-top': { borderBlockEnd: 'var(--border-width) solid var(--color-border-default)' },
})

