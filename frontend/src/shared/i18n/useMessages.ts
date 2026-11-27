import { useMemo } from 'react'
import { messagesIn } from './messages'
import { useLocale } from './locale'
export function useMessages() {
  const language = useLocale()
  return useMemo(() => ({ language, t: messagesIn(language) }), [language])
}
