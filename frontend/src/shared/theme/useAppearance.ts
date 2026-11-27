import { useLayoutEffect, useSyncExternalStore } from 'react'
import { resolveAppearance, type Preferences } from '../../features/workspace/preferences'

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)')
const subscribe = (notify: () => void) => {
  darkQuery.addEventListener('change', notify)
  return () => darkQuery.removeEventListener('change', notify)
}
export function useAppearance(mode: Preferences['mode']) {
  const systemDark = useSyncExternalStore(subscribe, () => darkQuery.matches)
  const resolved = resolveAppearance(mode, systemDark)
  useLayoutEffect(() => {
    document.documentElement.style.colorScheme = mode === 'system' ? 'light dark' : resolved
    document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', mode === 'system' ? 'light dark' : resolved)
  }, [mode, resolved])
  return { resolved, systemDark }
}
