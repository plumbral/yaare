import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect, useRef, useState } from 'react'
import { chooseKey, forgetKey, listKeys, type KeyReference } from '../../shared/bridge/keys'

export function useKeyReferences() {
  const [keys, setKeys] = useState<KeyReference[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const choosing = useRef(false)
  async function reload() {
    try { setKeys(await listKeys()); setError('') }
    catch { setError(tr("无法读取密钥引用，请在配置恢复后重试。")) }
  }
  useEffect(() => {
    let disposed = false
    listKeys().then(result => { if (!disposed) setKeys(result) }).catch(() => { if (!disposed) setError(tr("无法读取密钥引用，请重启桌面程序后重试。")) })
    return () => { disposed = true }
  }, [])
  async function choose() {
    if (choosing.current) return null
    choosing.current = true; setBusy(true); setError('')
    try {
      const selected = await chooseKey()
      if (selected) setKeys(previous => previous.some(key => key.id === selected.id) ? previous : [...previous, selected])
      return selected
    } catch (cause) { setError(cause instanceof Error ? cause.message : tr("选择私钥失败，请重试。")); return null }
    finally { choosing.current = false; setBusy(false) }
  }
  async function forget(id: string) {
    setError('')
    try { await forgetKey(id); setKeys(previous => previous.filter(key => key.id !== id)); return true }
    catch { setError(tr("无法移除密钥引用，请重试。")); return false }
  }
  const added = (key: KeyReference) => { setKeys(previous=>previous.some(item=>item.id===key.id)?previous:[...previous,key]);setError('') }
  return { keys, busy, error, choose, forget, reload, added }
}
