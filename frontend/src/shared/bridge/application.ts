import { isMyGo } from 'mygo-runtime'
import { ApplicationService } from '../generated/mygo'
import type { Info } from '../generated/mygo'
import type { Language } from '../i18n/locale'

export const desktopAvailable = isMyGo

export function supportsDesktopRecovery(hasBackend = desktopAvailable(), mode = import.meta.env?.MODE) {
  return hasBackend && mode !== 'browser'
}
export async function setInterfaceLanguage(language: Language): Promise<void> {
  if (desktopAvailable()) await ApplicationService.setLanguage(language)
}

export async function writeClipboardText(text:string):Promise<void>{
 if(desktopAvailable())await ApplicationService.writeClipboardText(text)
 else await navigator.clipboard.writeText(text)
}
export async function readClipboardText():Promise<string>{
 return desktopAvailable()?ApplicationService.readClipboardText():navigator.clipboard.readText()
}

let applicationInfo: Promise<Info> | undefined

export async function getApplicationInfo(): Promise<Info | null> {
  if (!desktopAvailable()) return null
  return applicationInfo ??= ApplicationService.getInfo().catch(error => {
    applicationInfo = undefined
    throw error
  })
}

export async function setWindowAppearance(mode: 'light' | 'dark'): Promise<void> {
  if (!desktopAvailable()) return
  await ApplicationService.setWindowAppearance(mode)
}

export async function setWindowFullscreen(enabled:boolean):Promise<boolean>{
 return ApplicationService.setWindowFullscreen(enabled)
}
