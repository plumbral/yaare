import { useEffect, useState, useSyncExternalStore } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Popover } from '@astryxdesign/core/Popover'
import { ProgressBar } from '@astryxdesign/core/ProgressBar'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { ArchiveBoxIcon, CheckCircleIcon } from '@heroicons/react/24/outline'
import { cloudStatus } from '../../shared/bridge/cloud'
import { useLocale } from '../../shared/i18n/locale'
import { tr } from '../../shared/i18n/messages'

export default function SyncStatusPicker({ visible, onOpenChange, onHistory }: {
  visible: boolean; onOpenChange: (open: boolean) => void; onHistory: () => void
}) {
  const language = useLocale()
  const status = useSyncExternalStore(cloudStatus.subscribeTask, cloudStatus.readTask)
  const [open, setOpen] = useState(false)
  const shown = visible && !!status?.configured
  useEffect(() => {
    if (!shown && open) { setOpen(false); onOpenChange(false) }
  }, [shown, open, onOpenChange])
  if (!shown || !status) return null

  const changeOpen = (next: boolean) => { setOpen(next); onOpenChange(next) }
  const title = tr("同步与备份")
  const stage = status.stage
  const syncing = status.kind === 'sync'
  const runningLabel = syncing ? tr("正在同步") : tr("正在备份")
  const stages = [tr("准备配置"), syncing ? tr("同步变更") : tr("加密并上传"), syncing ? tr("确认同步") : tr("确认备份")]
  const index = stage === 'confirm' ? 2 : stage === 'upload' ? 1 : 0
  const finishedAt = Math.max(status.lastBackup, status.lastSync)
  const paused = status.paused || status.issue === 'conflict'
  const retrying = status.issue === 'retry' || status.issue === 'unavailable'
  const finished = finishedAt > 0 && !paused && !retrying
  const stateLabel = status.paused ? tr("后台任务已暂停") : status.issue === 'conflict' ? tr("有同步冲突")
    : retrying ? tr("等待重试") : finished ? (status.lastBackup >= status.lastSync ? tr("备份已完成") : tr("同步已完成")) : tr("暂无任务")
  return <Popover label={title} placement="above" alignment="end" isModal={false} padding={0}
    width="min(calc(var(--spacing-10) * 8), 92vw)" isOpen={open} onOpenChange={changeOpen} closeButtonLabel={tr("关闭")}
    content={<Stack>
      <Stack padding={3} style={{borderBlockEnd:'var(--border-width) solid var(--color-border)'}}>
        <Text type="label" weight="semibold">{title}</Text>
      </Stack>
      <Stack padding={3} gap={3} style={{minBlockSize:'calc(var(--spacing-10) * 3)'}} aria-live="polite">
        {stage ? <>
          <Text type="label" weight="semibold">{runningLabel}</Text>
          <ProgressBar label={runningLabel} isLabelHidden isIndeterminate value={0} variant="neutral" />
          <Stack direction="horizontal" gap={2} justify="between" wrap="wrap" as="ol" aria-label={runningLabel}
            style={{listStyle:'none',margin:0,padding:0}}>
            {stages.map((label, i) => <Stack as="li" key={label} direction="horizontal" align="center" gap={1}
              aria-current={i === index ? 'step' : undefined}>
              <Stack aria-hidden="true" width="var(--spacing-2)" height="var(--spacing-2)" style={{borderRadius:'var(--radius-full)',
                background:i <= index?'var(--color-text-primary)':undefined,border:'var(--border-width) solid var(--color-text-secondary)'}}/>
              <Text type="supporting" color={i === index ? 'primary' : 'secondary'}>{label}</Text>
            </Stack>)}
          </Stack>
        </> : <Stack direction="horizontal" align="start" gap={2}>
          <Icon icon={finished ? CheckCircleIcon : ArchiveBoxIcon} size="sm" />
          <Stack gap={1}>
            <Text type="label" weight="semibold">{stateLabel}</Text>
            {finished && <Text type="supporting" color="secondary">{new Date(finishedAt).toLocaleString(language)}</Text>}
            {!finished && !paused && !retrying && <Text type="supporting" color="secondary">{tr("有配置变化时自动处理")}</Text>}
          </Stack>
        </Stack>}
        {!stage && finished && <Button label={tr("查看历史")} variant="ghost" size="sm" style={{alignSelf:'start'}}
          onClick={() => { changeOpen(false); onHistory() }} />}
      </Stack>
    </Stack>}>
    <IconButton id="workspace-sync-status" label={title} tooltip={title} size="sm" variant={open ? 'secondary' : 'ghost'}
      icon={<Icon icon={ArchiveBoxIcon} size="sm" />} />
  </Popover>
}
