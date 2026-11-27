import { tr } from '../i18n/messages'
import { useLocale } from '../i18n/locale'
import { useEffect, useId, useRef, useState } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { DropdownMenu } from '@astryxdesign/core/DropdownMenu'
import { browserMode } from '../bridge/browserRuntime'
import { pickBrowserFiles, uploadBrowserFiles } from '../bridge/browserFiles'
import type { LocalFolder } from '../bridge/files'
import type { RegisterFileGuard } from '../../features/workspace/fileCloseGuard'
export default function BrowserFileImport({
  sessionId,
  onImported,
  onError,
  onProtection,
  onFileGuard,
  hidden = false,
}: {
  sessionId: string
  onImported: (folder: LocalFolder) => void
  onError: (text: string) => void
  onProtection?: (key: string, value: boolean) => void
  onFileGuard?: RegisterFileGuard
  hidden?: boolean
}) {
  useLocale()

  const id = useId(),
    controller = useRef<AbortController | null>(null),
    [busy, setBusy] = useState(false),
    [progress, setProgress] = useState(''),
    alive = useRef(true),
    operation = useRef<Promise<LocalFolder> | null>(null),
    latest = useRef({ onImported, onError })
  latest.current = { onImported, onError }
  useEffect(() => {
    alive.current = true
    const key = 'browser-import-' + id
    onFileGuard?.(sessionId, key, {
      getCloseState: () => ({ dirty: false, busy: !!controller.current }),
      saveForClose: async () => true,
      reveal: () => {},
      cancelForClose: async () => {
        controller.current?.abort()
        await operation.current?.catch(() => {})
      },
    })
    return () => {
      alive.current = false
      controller.current?.abort()
      onFileGuard?.(sessionId, key, null)
      onProtection?.(key, false)
    }
  }, [id, sessionId, onFileGuard, onProtection])
  async function choose(directory: boolean) {
    if (controller.current) return
    const selected = pickBrowserFiles(directory)
    let root: LocalFolder | null = null
    try {
      const files = await selected
      if (!files) return
      const abort = new AbortController()
      controller.current = abort
      setBusy(true)
      setProgress(tr('正在添加文件…'))
      onProtection?.('browser-import-' + id, true)
      const upload = uploadBrowserFiles(
        files,
        abort.signal,
        (done, total) => {
          if (alive.current)
            setProgress(
              tr(
                '正在添加 {p0}%',
                total ? Math.round((done / total) * 100) : 100,
              ),
            )
        },
        (folder) => {
          root = folder
          if (alive.current) latest.current.onImported(folder)
        },
      )
      operation.current = upload
      const folder = await upload
      if (alive.current) {
        latest.current.onImported(folder)
        setProgress(tr('文件已添加，选择后可上传到服务器。'))
      }
    } catch (e) {
      if (alive.current)
        latest.current.onError(
          e instanceof Error && e.name !== 'AbortError'
            ? e.message
            : tr('已停止添加，已提交的文件保留。'),
        )
    } finally {
      operation.current = null
      if (root && alive.current) latest.current.onImported(root)
      controller.current = null
      if (alive.current) setBusy(false)
      onProtection?.('browser-import-' + id, false)
    }
  }
  if (!browserMode()) return null
  return (
    <Stack hidden={hidden} gap={1}>
      <Stack direction="horizontal" align="center" gap={2}>
        <DropdownMenu
          button={{
            label: tr('添加本地文件…'),
            variant: 'secondary',
            isDisabled: busy,
          }}
          items={[
            { label: tr('选择文件…'), onClick: () => void choose(false) },
            { label: tr('选择文件夹…'), onClick: () => void choose(true) },
          ]}
        />
        {busy && (
          <Button
            label={tr('停止添加')}
            variant="ghost"
            onClick={() => controller.current?.abort()}
          />
        )}
      </Stack>
      <Text type="supporting" color="secondary" role="status">
        {progress || tr('仅添加你选择的文件；文件区随此页面关闭而清空。')}
      </Text>
    </Stack>
  )
}
