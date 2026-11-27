import { useEffect, useState } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Button } from '@astryxdesign/core/Button'
import { Selector } from '@astryxdesign/core/Selector'
import { DialogHeader } from '@astryxdesign/core/Dialog'
import { Dialog } from '../../shared/ui/AppDialog'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'

const notices = import.meta.glob<string>('../../../../resources/licenses/*', { query: '?raw', import: 'default' })
const names = Object.keys(notices).filter(name => !name.endsWith('.gitkeep')).sort((a, b) => a.localeCompare(b, 'en'))
export default function LicenseDialog({ onClose }: { onClose: () => void }) {
  useLocale()
  const [selected, setSelected] = useState(names.find(name => name.endsWith('fonts-NOTICE.txt')) ?? names[0] ?? '')
  const [content, setContent] = useState(''), [error, setError] = useState(false)
  useEffect(() => {
    let disposed = false
    setContent(''); setError(false)
    void notices[selected]?.().then(text => { if (!disposed) setContent(text) }).catch(() => { if (!disposed) setError(true) })
    return () => { disposed = true }
  }, [selected])
  return <Dialog isOpen purpose="info" padding={4} width="calc(var(--spacing-10) * 18)" maxHeight="85dvh" onOpenChange={open => { if (!open) onClose() }}>
    <Stack gap={3} style={{ minHeight: 0 }}>
      <DialogHeader title={tr('第三方组件与字体许可')} hasDivider={false}/>
      <Text type="supporting" color="secondary">{tr('许可原文按各项目发布的内容展示。')}</Text>
      <Selector id="about-license-selector" label={tr('组件或字体')} size="md" width="100%" hasSearch value={selected} options={names.map(name => ({ value: name, label: name.slice(name.lastIndexOf('/') + 1) }))} onChange={setSelected}/>
      <Stack padding={3} style={{ overflow: 'auto', minHeight: 'calc(var(--spacing-10) * 3)', maxHeight: '50dvh', border: 'var(--border-width) solid var(--color-border)', borderRadius: 'var(--radius-md)', scrollbarGutter: 'stable' }}>
        <Text type="supporting" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }} role={error ? 'alert' : undefined}>{error ? tr('无法读取许可，请重新选择。') : content || tr('正在读取许可…')}</Text>
      </Stack>
      <Stack direction="horizontal" justify="end"><Button id="about-close-licenses" label={tr('关闭')} size="md" variant="secondary" onClick={onClose}/></Stack>
    </Stack>
  </Dialog>
}
