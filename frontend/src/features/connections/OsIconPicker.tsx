import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { useEffect, useRef, useState } from 'react'
import { Stack } from '@astryxdesign/core/Stack'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { Text } from '@astryxdesign/core/Text'
import { Popover } from '@astryxdesign/core/Popover'
import { IconButton } from '@astryxdesign/core/IconButton'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import OsIcon, { osIconOptions, searchHostIcons, type HostIconCategory, type OsIconName } from '../../shared/icons/OsIcon'
import { importCustomIcon, readCustomIcon } from '../../shared/icons/customIcon'

export default function OsIconPicker({ value = 'linux', disabled, onChange, showLabel = false, customIcons = [], systemOnly = false }: {
  value?: OsIconName; disabled: boolean; onChange: (icon: OsIconName) => void; showLabel?: boolean; customIcons?: readonly (OsIconName | undefined)[]
  systemOnly?: boolean
}) {
  useLocale()

  const [open, setOpen] = useState(false)
  const [query,setQuery]=useState('')
  const [category, setCategory] = useState<HostIconCategory>('system')
  const [importing, setImporting] = useState(false)
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const importRevision = useRef(0)
  useEffect(() => () => { importRevision.current++ }, [])
  const options = open ? searchHostIcons(query, systemOnly ? 'system' : category, [...customIcons, value]) : []
  const title = systemOnly ? tr("选择系统图标") : tr("选择连接图标")
  const search = systemOnly ? tr("搜索系统图标") : tr("搜索连接图标")
  const changeOpen = (next: boolean) => {
    setOpen(next)
    if (!next) {
      importRevision.current++
      setImporting(false)
      setError('')
      setQuery('')
      setCategory('system')
    }
  }
  async function chooseFile(file: File) {
    const revision = ++importRevision.current
    setImporting(true)
    setError('')
    try {
      const icon = await importCustomIcon(file)
      if (revision === importRevision.current) {
        onChange(icon)
        changeOpen(false)
      }
    } catch (cause) {
      if (revision === importRevision.current) setError(cause instanceof Error ? cause.message : tr("自定义图标无效。"))
    } finally {
      if (revision === importRevision.current) setImporting(false)
    }
  }

  return <Popover label={title} closeButtonLabel={tr("关闭图标选择")} width="min(calc(var(--spacing-10) * 8), 90vw)" padding={2}
    isEnabled={!disabled} isOpen={!disabled && open} onOpenChange={changeOpen}
    content={<Stack gap={2} onKeyDown={event => {
      if (event.key === 'Enter' && event.target instanceof HTMLInputElement && event.target.type === 'search' && !event.nativeEvent.isComposing) event.preventDefault()
    }}>
      <TextInput label={search} isLabelHidden type="search" size="md" value={query} onChange={setQuery} hasClear placeholder={search} autoComplete="off"/>
      {!systemOnly && <SegmentedControl label={tr("图标分类")} value={category} size="sm" layout="fill"
        onChange={next => {
          importRevision.current++
          setImporting(false)
          setError('')
          setCategory(next === 'cloud' || next === 'custom' ? next : 'system')
        }}>
        <SegmentedControlItem value="system" label={tr("系统图标")} />
        <SegmentedControlItem value="cloud" label={tr("云厂商图标")} />
        <SegmentedControlItem value="custom" label={tr("自定义图标")} />
      </SegmentedControl>}
      {!systemOnly && category === 'custom' && <>
        <input ref={fileInput} type="file" accept=".png,image/png" hidden onChange={event => {
          const file = event.currentTarget.files?.[0]
          event.currentTarget.value = ''
          if (file) void chooseFile(file)
        }} />
        <Button type="button" label={tr("导入 PNG 图标…")} size="sm" variant="secondary" isLoading={importing} onClick={() => fileInput.current?.click()} />
        {error && <Text role="alert" type="supporting">{error}</Text>}
      </>}
      <Stack key={category} gap={1} style={{maxHeight:'min(calc(var(--spacing-10) * 9), 45dvh)',overflowY:'auto'}}>
      {options.map(option => <Button key={option.value} label={option.label} aria-pressed={value === option.value}
        type="button" variant={value === option.value ? 'secondary' : 'ghost'} size="md" width="100%" style={{justifyContent:'flex-start',textAlign:'start'}} icon={<OsIcon name={option.value} size="md" />}
        onClick={() => { onChange(option.value); changeOpen(false) }} />)}
      {!options.length && <Text type="supporting" color="secondary">{systemOnly ? tr("没有匹配的系统图标。") : tr("没有匹配的连接图标。")}</Text>}
      </Stack>
    </Stack>}>
    {showLabel
      ? <Button type="button" label={readCustomIcon(value)?.label ?? osIconOptions.find(option => option.value === value)?.label ?? title} icon={<OsIcon name={value} size="md" />} size="md" variant="secondary" isDisabled={disabled} />
      : <IconButton id={systemOnly ? 'command-icon-picker' : 'connection-icon-picker'} type="button" label={title} tooltip={title} icon={<OsIcon name={value} size="md" />} size="sm" variant="secondary" isDisabled={disabled} />}
  </Popover>
}
