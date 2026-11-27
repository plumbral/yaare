import {isMyGo} from 'mygo-runtime'
import {localFontId, localFontName} from './localFontId'

export interface LocalFontFamily {name: string; monospaced: boolean}

export function createLocalFontCatalog(read: () => Promise<LocalFontFamily[]>, available: () => boolean) {
  let pending: Promise<LocalFontFamily[]> | undefined
  return {
    available,
    list(): Promise<LocalFontFamily[]> {
      if (!available()) return Promise.resolve([])
      if (!pending) {
        pending = read().then(rows => {
          const seen = new Map<string, LocalFontFamily>()
          for (const row of rows) {
            if (!localFontName(localFontId(row.name)) || row.name.startsWith('@')) continue
            const key = row.name.toLowerCase()
            const old = seen.get(key)
            if (old) old.monospaced ||= row.monospaced
            else seen.set(key, {...row})
          }
          return [...seen.values()].sort((a,b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase(), 'en'))
        }).catch(cause => {pending = undefined; throw cause})
      }
      return pending
    },
  }
}

export const localFonts = createLocalFontCatalog(async () => {
  const {ApplicationService} = await import('../generated/mygo')
  return ApplicationService.listSystemFonts()
}, () => isMyGo() && import.meta.env.MODE !== 'browser' )
