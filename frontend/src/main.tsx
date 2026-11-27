import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { isMyGo } from 'mygo-runtime'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import './shared/theme/focusPresentation.css'
import App from './app/App'
import { readPreferences } from './features/workspace/preferences'
import { desktopPreferences } from './features/workspace/desktopPreferences'
import LanguageProvider from './shared/i18n/LanguageProvider'
import { initializeLanguage, setLanguagePreference } from './shared/i18n/locale'
import { loadWorkbenchTheme } from './shared/theme/currentTheme'
window.addEventListener('contextmenu',event=>event.preventDefault())
async function start() {
  if (import.meta.env.MODE === 'browser') {
    await initializeLanguage()
    const { default: BrowserEntry } = await import('./app/BrowserEntry')
    createRoot(document.getElementById('app')!).render(<StrictMode><LanguageProvider><BrowserEntry/></LanguageProvider></StrictMode>)
    return
  }
  const native = isMyGo()
  const preferences = native
    ? await desktopPreferences.refresh()
    : readPreferences()
  await Promise.all([native ? setLanguagePreference(preferences.language) : initializeLanguage(), loadWorkbenchTheme(preferences.interfacePalette)])
  createRoot(document.getElementById('app')!).render(<StrictMode><LanguageProvider><App/></LanguageProvider></StrictMode>)
}
void start()
