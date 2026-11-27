import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { desktopAvailable } from './application'
import { KeyService, type KeyReference, type SaveKeyRequest } from '../generated/mygo'

export type { KeyReference, KeySelection, SaveKeyRequest } from '../generated/mygo'
export const listKeys = async (): Promise<KeyReference[]> => desktopAvailable() ? KeyService.list() : []
export async function chooseKey(): Promise<KeyReference | null> {
  if (!desktopAvailable()) throw new Error(tr("请在 Yaare 桌面程序中选择私钥。"))
  return await KeyService.choose() ?? null
}
export async function forgetKey(id: string): Promise<void> {
  if (desktopAvailable()) await KeyService.forget(id)
}
export const pickKey = async () => desktopAvailable() ? await KeyService.pick() ?? null : null
export const generateKey = async () => desktopAvailable() ? await KeyService.generate() ?? null : null
export const copyKeyPrivate = (id: string) => KeyService.copyPrivateKey(id)
export const readKeyPublic = (id: string) => KeyService.publicKey(id)
export const discardKeySelection = async (token: string) => { if (token && desktopAvailable()) await KeyService.discard(token) }
export async function saveKey(request: SaveKeyRequest): Promise<KeyReference> {
  if (!desktopAvailable()) throw new Error(tr("请在 Yaare 桌面程序中保存密钥。"))
  const key = await KeyService.save(request)
  if (!key) throw new Error(tr("密钥未保存，请重试。"))
  return key
}
