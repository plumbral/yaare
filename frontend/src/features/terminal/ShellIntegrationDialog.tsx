import { useRef, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Text } from '@astryxdesign/core/Text'
import { tr, localizeMessage } from '../../shared/i18n/messages'
import type { SSHTransport } from '../../shared/bridge/ssh'
import type { BashIntegrationPreview } from '../../shared/generated/mygo'
import EditorDialog from '../../shared/ui/EditorDialog'

export type ShellOperation = { kind: 'manage' | 'upload' } | { kind: 'directory'; path: string }

export default function ShellIntegrationDialog({ host, transport, integrated, operation, onContinue, onClose, onSaved }: {
  host: string
  transport: SSHTransport
  integrated: boolean
  operation: ShellOperation
  onContinue: () => Promise<void>
  onClose: () => void
  onSaved: () => void
}) {
  const [preview, setPreview] = useState<BashIntegrationPreview>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const pending = useRef(false)
  const run = async (task: () => Promise<void>) => {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    setError('')
    try { await task() }
    catch (cause) { setError(cause instanceof Error ? localizeMessage(cause.message) : tr('Bash 集成操作失败。')) }
    finally { pending.current = false; setBusy(false) }
  }
  const previewChanges = () => run(async () => setPreview(await transport.previewBashIntegration()))
  const save = () => run(async () => {
    if (preview) {
      await transport.applyBashIntegration(preview)
      onSaved()
    } else {
      if (!integrated) await transport.enableShellIntegration()
      await onContinue()
    }
  })
  const action = preview ? (preview.action === 'install' ? tr('安装') : tr('移除'))
    : operation.kind === 'directory' ? tr('启用并进入目录') : operation.kind === 'upload' ? tr('启用并启动上传')
    : integrated ? tr('完成') : tr('为本次会话启用')
  return <EditorDialog title={tr('Bash 终端集成')} subtitle={host} action="edit" busy={busy}
    hint={tr('Esc 关闭')} closeLabel={tr('关闭')} onClose={onClose}
    formProps={{ onSubmit: event => { event.preventDefault(); void save() } }}
    actions={<Button type="submit" label={action} isDisabled={busy} isLoading={busy}/>}>
    {preview ? <>
      <Text>{tr('以下改动作用于此远程账户，以后的交互式 SSH Bash 登录将使用更新后的配置。')}</Text>
      {preview.profileChange === 'append' && <Text>{tr('追加到 {p0}', preview.profilePath)}</Text>}
      {preview.profileChange === 'create' && <Text>{tr('创建启动文件 {p0}', preview.profilePath)}</Text>}
      {preview.profileChange === 'removeBlock' && <Text>{tr('从 {p0} 移除集成代码', preview.profilePath)}</Text>}
      {preview.profileChange === 'delete' && <Text>{tr('删除仅包含集成代码的启动文件 {p0}', preview.profilePath)}</Text>}
      {preview.keyChange === 'create' && <Text>{tr('创建仅供此账户使用的密钥文件 {p0}', preview.keyPath)}</Text>}
      {preview.keyChange === 'delete' && <Text>{tr('删除集成密钥文件 {p0}', preview.keyPath)}</Text>}
      {preview.block && <pre dir="ltr" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', userSelect: 'text' }}>{preview.block}</pre>}
      <Button label={tr('返回')} size="sm" variant="ghost" type="button" isDisabled={busy} onClick={() => { setPreview(undefined); setError('') }}/>
    </> : <>
      <Text>{integrated ? tr('本次会话已启用目录和提示符报告。')
        : tr('请确认终端停在空白的 Bash 命令提示符。启用后，Yaare 会在当前 shell 中加载目录和提示符报告。')}</Text>
      {operation.kind === 'directory' && <Text>{tr('目标目录：{p0}', operation.path)}</Text>}
      {operation.kind === 'upload' && <Text>{tr('集成就绪后，将在此终端启动 rz。')}</Text>}
      {operation.kind === 'manage' && <Button type="button" label={tr('预览此账户的自动加载设置…')} size="sm" variant="secondary" isDisabled={busy} onClick={() => void previewChanges()}/>}
    </>}
    {error && <Text role="alert">{error}</Text>}
  </EditorDialog>
}
