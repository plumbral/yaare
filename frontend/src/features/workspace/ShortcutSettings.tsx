import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { List, ListItem } from '@astryxdesign/core/List'
import { Section } from '@astryxdesign/core/Section'
import { Stack } from '@astryxdesign/core/Stack'
import { Heading, Text } from '@astryxdesign/core/Text'
import { ArrowUturnLeftIcon } from '@heroicons/react/24/outline'
import { tr } from '../../shared/i18n/messages'
import { formatShortcut, installShortcutRecorder, persistShortcut, shortcutFromEvent, shortcutLabel, shortcutReference, useShortcutOverrides, type ShortcutId } from './shortcutReference'

export default function ShortcutSettings({ showDesktopShortcuts }: { showDesktopShortcuts: boolean }) {
  const overrides = useShortcutOverrides()
  const [recording, setRecording] = useState<ShortcutId>()
  const [notice, setNotice] = useState<{ id: ShortcutId; text: string }>()

  const saving = useRef(false)
  const apply = useCallback(async (id: ShortcutId, keys: string | null | undefined) => {
    if (saving.current) return
    saving.current = true
    const result = await persistShortcut(id, keys).finally(() => { saving.current = false })
    if (result.ok) {
      setRecording(undefined)
      setNotice(undefined)
    } else {
      setNotice({ id, text: result.reason === 'conflict' ? tr('此快捷键已用于「{p0}」。', shortcutLabel(result.conflict))
        : result.reason === 'storage' ? tr('无法保存快捷键，原设置未更改。') : tr('不能使用这个组合。') })
    }
  }, [])

  useLayoutEffect(() => {
    if(!recording) return
    const cancel = () => { setRecording(undefined); setNotice(undefined) }
    const dismissOutside = (event: MouseEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest('#shortcut-' + recording)) cancel()
    }
    const dispose = installShortcutRecorder(event => {
      if (event.key === 'Tab') { cancel(); return true }
      if (event.isComposing || event.keyCode === 229 || event.getModifierState('AltGraph') || event.key === 'Dead') return true
      const clearOrCancel = !event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey
      const keys = shortcutFromEvent(event)
      event.preventDefault()
      event.stopImmediatePropagation()
      if(event.repeat) return true
      if(clearOrCancel && event.key === 'Escape') cancel()
      else if(clearOrCancel && event.key === 'Backspace') apply(recording, null)
      else if(keys) apply(recording, keys)
      else if(!['Control','Meta','Shift','Alt'].includes(event.key)) setNotice({ id: recording, text: tr('不能使用这个组合。') })
      return true
    })
    document.addEventListener('click', dismissOutside, true)
    return () => { dispose(); document.removeEventListener('click', dismissOutside, true) }
  }, [recording, apply])

  return <Section padding={0} variant="transparent">
    <Heading level={4} accessibilityLevel={3} weight="semibold" tabIndex={0} style={{width:'fit-content'}}>{tr('快捷键')}</Heading>
    <Stack gap={4} paddingBlockStart={3}>
      {shortcutReference(showDesktopShortcuts, overrides).map(group => <Stack key={group.id} gap={1}>
        <Heading id={'settings-shortcuts-' + group.id} level={5} accessibilityLevel={4} weight="semibold">{group.label}</Heading>
        <List aria-labelledby={'settings-shortcuts-' + group.id} density="compact" edge="flush">
          {group.items.map(({ id, label, keys, customized }) => {
            const active = recording === id
            const keyLabel = formatShortcut(keys) ?? tr('未设置')
            const description = notice?.id === id ? notice.text : active ? tr('Backspace 清除 · Esc 取消') : undefined
            const messageId = 'shortcut-message-' + id
            return <ListItem key={id} label={label} description={description && <Text id={messageId} type="supporting" role="status">{description}</Text>}
              endContent={<Stack direction="horizontal" gap={1} align="center">
                {customized && <IconButton label={tr('恢复「{p0}」的默认快捷键', label)} tooltip={tr('恢复默认')}
                  size="sm" variant="ghost" icon={<Icon icon={ArrowUturnLeftIcon} size="sm"/>} onClick={event=>{
                    event.currentTarget.ownerDocument.getElementById('shortcut-' + id)?.focus({preventScroll:true})
                    apply(id, undefined)
                  }}/>}
                <Button id={'shortcut-' + id} type="button" size="sm" variant={active ? 'secondary' : 'ghost'}
                  label={active ? tr('按下快捷键') : keyLabel} aria-label={tr('编辑「{p0}」的快捷键：{p1}', label, active ? tr('按下快捷键') : keyLabel)}
                  aria-pressed={active} aria-describedby={description ? messageId : undefined}
                  tooltip={active ? undefined : tr('点击修改快捷键')}
                  onClick={event=>{ event.currentTarget.focus({preventScroll:true}); setRecording(active ? undefined : id); setNotice(undefined) }}/>
              </Stack>}/>
          })}
        </List>
      </Stack>)}
    </Stack>
  </Section>
}
