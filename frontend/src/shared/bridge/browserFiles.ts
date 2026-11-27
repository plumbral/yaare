import { tr } from '../i18n/messages'
import { call } from 'mygo-runtime'
import { encodeBase64 } from './base64'
import type { LocalFolder } from './files'
export function pickBrowserFiles(directory: boolean): Promise<File[] | null> {
  const input = document.createElement('input')
  input.type = 'file'
  input.multiple = true
  input.hidden = true
  if (directory) input.webkitdirectory = true
  document.body.append(input)
  return new Promise((resolve) => {
    const finish = (files: File[] | null) => {
      input.remove()
      resolve(files)
    }
    input.addEventListener(
      'change',
      () => finish(input.files?.length ? Array.from(input.files) : null),
      { once: true },
    )
    input.addEventListener('cancel', () => finish(null), { once: true })
    input.click()
  })
}
export async function readBrowserPrivateKey() {
  const files = await pickBrowserFiles(false)
  if (!files) return null
  if (files.length !== 1 || files[0]!.size > 1048576)
    throw Error(tr('请选择一个不超过 1 MiB 的私钥文件。'))
  return { name: files[0]!.name, content: await files[0]!.text() }
}
export async function uploadBrowserFiles(
  files: File[],
  signal: AbortSignal,
  progress: (done: number, total: number) => void,
  onFolder?: (folder: LocalFolder) => void,
): Promise<LocalFolder> {
  if (files.length > 10000)
    throw Error(tr('一次最多添加 10,000 个文件，请分批选择。'))
  const folder = await call<LocalFolder>('BrowserFiles.Begin')
  onFolder?.(folder)
  let done = 0,
    total = files.reduce((n, f) => n + f.size, 0)
  for (const file of files) {
    signal.throwIfAborted()
    const id = await call<string>('BrowserFiles.UploadBegin', {
      token: folder.token,
      path: file.webkitRelativePath || file.name,
      size: file.size,
    })
    let committed = false
    try {
      let offset = 0
      do {
        signal.throwIfAborted()
        const raw = new Uint8Array(
          await file.slice(offset, offset + 262144).arrayBuffer(),
        )
        signal.throwIfAborted()
        const data = encodeBase64(raw)
        offset += raw.length
        await call('BrowserFiles.UploadChunk', {
          id,
          data,
          offset: offset - raw.length,
          finish: offset === file.size,
        })
        done += raw.length
        progress(done, total)
      } while (offset < file.size)
      committed = true
    } finally {
      if (!committed)
        await call('BrowserFiles.UploadCancel', id).catch(() => {})
    }
  }
  return folder
}
export async function saveBrowserDownload(taskId: string) {
  const href = await call<string>('BrowserFiles.Download', taskId)
  saveBrowserURL(href)
}
export function saveBrowserURL(href: string) {
  const url = new URL(href, location.origin)
  if (
    url.origin !== location.origin ||
    !url.pathname.startsWith('/api/download/')
  )
    throw Error(tr('下载地址无效，请重新准备。'))
  const link = document.createElement('a')
  link.href = url.href
  link.download = ''
  link.hidden = true
  document.body.append(link)
  link.click()
  link.remove()
}
