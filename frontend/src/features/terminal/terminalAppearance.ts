import type { ITheme, Terminal } from '@xterm/xterm'

type TerminalFocusOptions = { active: boolean; focused?: boolean; cursorBlink: boolean; screenReaderMode?: boolean }

export function terminalFocusOptions(state: TerminalFocusOptions) {
  const focused = state.active && state.focused !== false
  return { cursorBlink: focused && state.cursorBlink, screenReaderMode: focused && !!state.screenReaderMode }
}

export function createTerminalAppearance(terminal: Pick<Terminal, 'options' | 'refresh' | 'rows'>, initial: ITheme) {
  let applied = initial
  let needsRepaint = false

  return (theme: ITheme, visible: boolean) => {
    if (theme !== applied) {
      const colors = Object.keys(theme) as Array<keyof ITheme>
      if (colors.length !== Object.keys(applied).length || colors.some(color => theme[color] !== applied[color])) {
        terminal.options.theme = theme
        needsRepaint = true
      }
      applied = theme
    }
    if (visible && needsRepaint) {
      terminal.refresh(0, terminal.rows - 1)
      needsRepaint = false
    }
  }
}
