import type { ReactNode } from 'react'
import { InternationalizationProvider } from '@astryxdesign/core/i18n'
import { getAstryxLocale, getAstryxMessages } from './catalogs'
import { useLocale } from './locale'
export default function LanguageProvider({ children }: { children: ReactNode }) {
  const language = useLocale()
  return <InternationalizationProvider locale={getAstryxLocale(language)} messages={getAstryxMessages(language)}>{children}</InternationalizationProvider>
}
