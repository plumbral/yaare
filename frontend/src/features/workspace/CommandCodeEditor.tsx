import { useEffect, useRef } from 'react'
import { StackItem } from '@astryxdesign/core/Stack'
import { EditorState, Compartment } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, drawSelection, highlightSpecialChars } from '@codemirror/view'
import { matchesShortcut } from './shortcutReference'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { searchKeymap } from '@codemirror/search'
import { StreamLanguage, syntaxHighlighting, bracketMatching, indentOnInput } from '@codemirror/language'
import { shell } from '@codemirror/legacy-modes/mode/shell'
import { colors, editorTheme } from '../../shared/ui/codeEditorTheme'
import {registerCodeEditorContextActions} from '../../shared/ui/codeEditorContextActions'


export default function CommandCodeEditor({ value, onChange, onSave, disabled = false, invalid = false,fieldId='command-code-input',labelledBy='command-code-label',describedBy='command-editor-help' }: {
  value: string; onChange: (value: string) => void; onSave: () => void; disabled?: boolean; invalid?: boolean
  fieldId?:string;labelledBy?:string;describedBy?:string
}) {
  const mount = useRef<HTMLElement>(null)
  const view = useRef<EditorView | null>(null)
  const unregisterContext=useRef<(()=>void)|null>(null)
  const latest = useRef({ value, onChange, onSave, disabled, invalid })
  latest.current = { value, onChange, onSave, disabled, invalid }
  const settings = useRef(new Compartment())
  useEffect(() => {
    const parent = mount.current
    if (!parent) return
    const extensions = () => [EditorState.readOnly.of(latest.current.disabled), EditorView.editable.of(!latest.current.disabled),
      EditorView.lineWrapping, EditorView.contentAttributes.of({ id:fieldId, 'aria-labelledby':labelledBy, 'aria-required': 'true',
        'aria-invalid': String(latest.current.invalid), 'aria-describedby':describedBy, spellcheck: 'false', autocorrect: 'off', autocapitalize: 'off', autocomplete: 'off' })]
    const create = () => {
      if (view.current) return
      view.current = new EditorView({ parent, state: EditorState.create({ doc: latest.current.value, extensions: [
        lineNumbers(), highlightSpecialChars(), history(), drawSelection(),
        StreamLanguage.define(shell), syntaxHighlighting(colors), bracketMatching(), indentOnInput(), editorTheme,
        EditorView.domEventHandlers({keydown(event, editor) { if(!matchesShortcut(event,'save'))return false; if(!event.repeat&&!editor.composing&&!latest.current.disabled)latest.current.onSave(); return true }}),
        keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap]),
        settings.current.of(extensions()), EditorView.updateListener.of(update => { if (update.docChanged) latest.current.onChange(update.state.doc.toString()) }),
      ] }) })
      unregisterContext.current=registerCodeEditorContextActions(view.current)
    }
    const destroy = () => { unregisterContext.current?.();unregisterContext.current=null;view.current?.destroy(); view.current = null }
    const dialog = parent.closest('dialog')
    const sync = () => { if (!dialog || dialog.open) create(); else destroy() }
    const observer = new MutationObserver(sync)
    if (dialog) observer.observe(dialog, { attributes: true, attributeFilter: ['open'] })
    sync()
    return () => { observer.disconnect(); destroy() }
  }, [])
  useEffect(() => {
    const instance = view.current
    if (instance && instance.state.doc.toString() !== value) instance.dispatch({ changes: { from: 0, to: instance.state.doc.length, insert: value } })
  }, [value])
  useEffect(() => {
    view.current?.dispatch({ effects: settings.current.reconfigure([EditorState.readOnly.of(disabled), EditorView.editable.of(!disabled), EditorView.lineWrapping,
      EditorView.contentAttributes.of({ id:fieldId, 'aria-labelledby':labelledBy, 'aria-required': 'true', 'aria-invalid': String(invalid), 'aria-describedby':describedBy, spellcheck: 'false', autocorrect: 'off', autocapitalize: 'off', autocomplete: 'off' })]) })
  }, [disabled, invalid])
  return <StackItem ref={mount} style={{ height: 'clamp(calc(var(--spacing-10) * 4), 28dvh, calc(var(--spacing-10) * 6))',
    minHeight: 'calc(var(--spacing-10) * 3)', maxHeight: '55dvh', minWidth: 0, flexShrink: 0, resize: 'vertical',
    border: `var(--border-width) solid var(${invalid ? '--color-error' : '--color-border-emphasized'})`,
    borderRadius: 'var(--radius-element)', overflow: 'hidden' }} />
}
