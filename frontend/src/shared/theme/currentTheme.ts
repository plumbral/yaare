import { useEffect, useState } from 'react'
import type { DefinedTheme } from '@astryxdesign/core/theme'
import '../../../../astryx-themes'
import './yaare.css'
import { yaareTheme } from './yaare.js'
import type { InterfacePalette } from './palettes'

const loaders = {
  paper: () => Promise.all([import('./yaare-paper.js'), import('./yaare-paper.css')]).then(([module]) => module.yaarePaperTheme),
  purple: () => Promise.all([import('./yaare-purple.js'), import('./yaare-purple.css')]).then(([module]) => module.yaarePurpleTheme),
  blue: () => Promise.all([import('./yaare-blue.js'), import('./yaare-blue.css')]).then(([module]) => module.yaareBlueTheme),
  green: () => Promise.all([import('./yaare-green.js'), import('./yaare-green.css')]).then(([module]) => module.yaareGreenTheme),
}
const workbenchThemes: Partial<Record<InterfacePalette, DefinedTheme>> = { neutral: yaareTheme }
const loading = new Map<InterfacePalette, Promise<DefinedTheme | undefined>>()
export const workbenchTheme = yaareTheme
export const getWorkbenchThemeByName = (name: string) => Object.values(workbenchThemes).find(theme => theme?.name === name) ?? yaareTheme

export function loadWorkbenchTheme(id: InterfacePalette): Promise<DefinedTheme | undefined> {
  if (id === 'neutral') return Promise.resolve(yaareTheme)
  const theme = workbenchThemes[id]
  if (theme) return Promise.resolve(theme)
  const pending = loading.get(id)
  if (pending) return pending
  const request = loaders[id]().then(
    theme => workbenchThemes[id] = theme,
    () => undefined,
  ).finally(() => { loading.delete(id) })
  loading.set(id, request)
  return request
}

export function useWorkbenchTheme(id: InterfacePalette): DefinedTheme {
  const [previous, setPrevious] = useState(() => workbenchThemes[id] ?? workbenchTheme)
  useEffect(() => {
    let cancelled = false
    const cached = workbenchThemes[id]
    if (cached) setPrevious(cached)
    else void loadWorkbenchTheme(id).then(theme => { if (!cancelled && theme) setPrevious(theme) })
    return () => { cancelled = true }
  }, [id])
  return workbenchThemes[id] ?? previous
}
