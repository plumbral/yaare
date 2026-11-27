import { tr } from '../i18n/messages'

export type CustomHostIcon = `custom-png:${string}`
const prefix = 'custom-png:'
const maxPayload = 4 * Math.ceil(20 * 1024 / 3)

export function readCustomIcon(value?: string): { value: CustomHostIcon; label: string; src: string } | undefined {
  if (!value?.startsWith(prefix) || value.length > prefix.length + maxPayload + 600) return
  const separator = value.indexOf(':', prefix.length)
  if (separator < 0) return
  const payload = value.slice(separator + 1)
  if (payload.length > maxPayload || !/^[A-Za-z0-9+/]+={0,2}$/.test(payload)) return
  try {
    const label = decodeURIComponent(value.slice(prefix.length, separator))
    if (!label) return
    return { value: value as CustomHostIcon, label, src: 'data:image/png;base64,' + payload }
  } catch {
    return
  }
}

export async function importCustomIcon(file: File): Promise<CustomHostIcon> {
  const invalid = () => Error(tr("请选择有效的 PNG 图片，最大 4 MiB。"))
  if (file.size < 33 || file.size > 4 * 1024 * 1024) throw invalid()
  const header = new DataView(await file.slice(0, 24).arrayBuffer())
  if (header.getUint32(0) !== 0x89504e47 || header.getUint32(4) !== 0x0d0a1a0a ||
      header.getUint32(8) !== 13 || header.getUint32(12) !== 0x49484452) throw invalid()
  const width = header.getUint32(16)
  const height = header.getUint32(20)
  if (!width || !height || width > 4096 || height > 4096) throw invalid()

  const image = await createImageBitmap(file).catch(() => { throw invalid() })
  const canvas = document.createElement('canvas')
  canvas.width = canvas.height = 64
  try {
    const context = canvas.getContext('2d')
    if (!context) throw invalid()
    const scale = Math.min(64 / width, 64 / height)
    const w = Math.max(1, Math.round(width * scale))
    const h = Math.max(1, Math.round(height * scale))
    context.drawImage(image, Math.floor((64 - w) / 2), Math.floor((64 - h) / 2), w, h)
    const [, data] = canvas.toDataURL('image/png').split(',', 2)
    if (!data || data.length > maxPayload) throw invalid()
    const label = Array.from(file.name.replace(/\.png$/i, '').replace(/[\p{Cc}\u2028\u2029]/gu, '').trim())
      .slice(0, 40).join('').trim() || tr("自定义图标")
    return `custom-png:${encodeURIComponent(label)}:${data}`
  } finally {
    image.close()
  }
}
