import { useEffect, useRef, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Selector } from '@astryxdesign/core/Selector'
import { Text } from '@astryxdesign/core/Text'
import { WorkspaceService, type DeletedHost } from '../../shared/generated/mygo'
import { useLocale } from '../../shared/i18n/locale'
import { localizeMessage, tr } from '../../shared/i18n/messages'
import EditorDialog from '../../shared/ui/EditorDialog'

export default function DeletedHostRecoveryDialog({ isOpen, revision, onClose, onRestore }: {
  isOpen: boolean
  revision: number
  onClose: () => void
  onRestore: (backupId: string, hostId: string, revision: number) => Promise<void>
}) {
  const locale = useLocale()
  const [hosts, setHosts] = useState<DeletedHost[]>([])
  const [selected, setSelected] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')
  const [retry, setRetry] = useState(0)
  const restoring = useRef(false)
  useEffect(() => {
    if (!isOpen) return
    let cancelled = false
    setLoading(true)
    setHosts([])
    setSelected('')
    setProblem('')
    void WorkspaceService.deletedHosts().then(items => {
      if (!cancelled) setHosts(items ?? [])
    }, error => {
      if (!cancelled) setProblem(error instanceof Error ? error.message : tr('无法读取本地恢复记录。'))
    }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [isOpen, retry, revision])

  const candidate = hosts.find(entry => entry.host.id === selected)
  async function restore() {
    if (!isOpen || !candidate || loading || restoring.current) return
    restoring.current = true
    setBusy(true)
    setProblem('')
    try {
      await onRestore(candidate.backupId, candidate.host.id, revision)
      onClose()
    } catch (error) {
      setProblem(error instanceof Error ? error.message : tr('恢复未完成，请刷新后重试。'))
    } finally {
      restoring.current = false
      setBusy(false)
    }
  }

  const title = tr('恢复已删主机')
  return <EditorDialog isOpen={isOpen} title={title} action="add" busy={busy} onClose={onClose} closeLabel={tr('关闭')}
    subtitle={tr('只恢复所选主机，不覆盖现有配置，也不会自动连接。')} hint=""
    formProps={{ 'aria-label': title, onSubmit: event => { event.preventDefault(); void restore() } }}
    actions={<Button label={tr('恢复所选主机')} type="submit" variant="primary" isDisabled={!candidate || loading || busy} isLoading={busy} />}>
    {loading ? <Text role="status" type="supporting">{tr('正在读取本地恢复记录…')}</Text> : hosts.length > 0 ? <>
      <Selector id="deleted-host-selector" label={tr('主机')} value={selected} onChange={setSelected} isDisabled={busy}
        options={hosts.map(({ host }) => ({ value: host.id, label: host.name + ' · ' + host.address }))} />
      {candidate && <>
        <Text type="supporting" color="secondary" style={{ overflowWrap: 'anywhere' }}>
          {candidate.host.username}@{candidate.host.address}:{candidate.host.port}
        </Text>
        <Text type="supporting" color="secondary">
          {tr('备份时间：{p0}', new Date(candidate.created).toLocaleString(locale))}
        </Text>
      </>}
    </> : !problem && <Text type="supporting">{tr('没有可恢复的已删主机。')}</Text>}
    {problem && <>
      <Text role="alert" type="supporting">{localizeMessage(problem)}</Text>
      <Button label={tr('刷新')} type="button" variant="secondary" isDisabled={busy} onClick={() => setRetry(value => value + 1)} />
    </>}
  </EditorDialog>
}
