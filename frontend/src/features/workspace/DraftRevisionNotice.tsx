import { Button } from '@astryxdesign/core/Button'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { tr } from '../../shared/i18n/messages'

export default function DraftRevisionNotice({ onReview, busy }: { onReview?: () => void; busy: boolean }) {
  return <Stack gap={2} align="start">
    <Text type="supporting" role="alert">{onReview
      ? tr('配置已更新。载入外部更改会替换双方都改过的字段。请核对后保存。')
      : tr('这条记录已删除。请关闭编辑器。')}</Text>
    {onReview && <Button type="button" label={tr('载入外部更改')} variant="secondary" size="sm" isDisabled={busy} onClick={onReview} />}
  </Stack>
}
