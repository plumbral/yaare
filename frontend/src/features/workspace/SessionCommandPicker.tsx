import { useEffect, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Icon } from '@astryxdesign/core/Icon'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Popover } from '@astryxdesign/core/Popover'
import { Stack, StackItem } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Cog6ToothIcon, PlusIcon } from '@heroicons/react/24/outline'
import { localizeMessage, tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import type { ServerSystem } from '../../shared/generated/mygo'
import OsIcon from '../../shared/icons/OsIcon'
import WindowedList from '../../shared/ui/WindowedList'
import { commandAvailable, commandIcon, type QuickCommand } from './shortcuts'

export type CommandTarget = {
  id: string
  attempt: number
  label: string
  system?: ServerSystem
}

export default function SessionCommandPicker({ commands, filterBySystem, target, resolveTarget, onSend, editable = true, onAdd, onManage }: {
  commands: readonly QuickCommand[]
  filterBySystem: boolean
  target?: CommandTarget
  resolveTarget: (id: string, attempt: number) => CommandTarget | undefined
  onSend: (command: QuickCommand, target: CommandTarget) => string | undefined
  editable?: boolean
  onAdd?: (system?: ServerSystem) => void
  onManage?: () => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [openingTarget, setOpeningTarget] = useState<CommandTarget>()
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string>()
  const current = openingTarget && resolveTarget(openingTarget.id, openingTarget.attempt)
  const needle = query.trim().toLowerCase()
  const canList = !!current && (!filterBySystem || !!current.system)
  const matches = open && canList ? commands.filter(command =>
    commandAvailable(command, filterBySystem, current.system) &&
    (!needle || command.name.toLowerCase().includes(needle) || command.command.toLowerCase().includes(needle))
  ) : []
  const label = tr("命令")
  const hasActions = Boolean(onAdd || onManage)

  const closePopover = () => {
    setOpen(false)
    setOpeningTarget(undefined)
    setQuery('')
    setError(undefined)
  }

  useEffect(() => {
    if (!open || !canList) return
    const frame = window.requestAnimationFrame(() => {
      document.getElementById('session-command-search')?.focus({ preventScroll: true })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [open, canList])

  return <Popover label={label} closeButtonLabel={tr("关闭")} width="min(calc(var(--spacing-10) * 9), 90vw)"
    isModal={false} hasAutoFocus={!canList} isOpen={open} onOpenChange={next => {
      if (next) {
        setOpen(true)
        setOpeningTarget(target)
        setQuery('')
        setError(undefined)
      } else {
        closePopover()
      }
    }}
    content={<Stack gap={2}>
      {(openingTarget || hasActions) && <Stack direction="horizontal" align="center" justify="between" gap={2} style={{ minWidth: 0 }}>
        <StackItem size="fill" style={{ minWidth: 0 }}>
          {openingTarget && <Text type="supporting" color="secondary" maxLines={1}>{openingTarget.label}</Text>}
        </StackItem>
        {hasActions && <Stack direction="horizontal" align="center" gap={1} style={{ flexShrink: 0 }}>
          {onAdd && <IconButton id="session-command-add" label={tr("新建命令")} tooltip={tr("新建命令")}
            variant="ghost" size="sm" isDisabled={!editable}
            icon={<Icon icon={PlusIcon} size="sm"/>}
            onClick={() => {
              const system = current?.system ?? openingTarget?.system
              closePopover()
              onAdd(system)
            }}/>}
          {onManage && <IconButton id="session-command-manage" label={tr("管理快捷命令")} tooltip={tr("管理快捷命令")}
            variant="ghost" size="sm"
            icon={<Icon icon={Cog6ToothIcon} size="sm"/>}
            onClick={() => {
              closePopover()
              onManage()
            }}/>}
        </Stack>}
      </Stack>}
      {canList && <TextInput id="session-command-search" label={tr("搜索名称或命令…")} isLabelHidden type="search" size="sm"
        value={query} onChange={setQuery} hasAutoFocus hasClear placeholder={tr("搜索名称或命令…")} autoComplete="off"/>}
      {matches.length > 0 && <Stack height={`min(48dvh, calc(var(--spacing-10) * ${Math.min(matches.length, 8) * 1.4 + 1}))`} style={{minHeight:0}}>
        <WindowedList items={matches} itemKey={command => command.id} estimateSize={() => 52} resetKey={query} label={label}
          row={command => ({
            label: command.name,
            startContent: <OsIcon name={commandIcon(command)}/>,
            description: <Text as="span" type="supporting" maxLines={1} style={{fontFamily:'var(--font-family-code)'}}>
              {command.command.trimStart().split('\n', 1)[0]}
            </Text>,
            onClick: () => {
              if (!current) return
              const failure = onSend(command, current)
              setError(failure)
              if (!failure) closePopover()
            },
          })}/>
      </Stack>}
      {(error || !matches.length) && <Text type="supporting" color="secondary" role={error ? 'alert' : 'status'}>
        {!current ? tr("当前终端不可用，请重新连接后选择。") : error ? localizeMessage(error)
          : !canList ? tr("当前会话的系统尚未识别。")
          : tr("没有匹配的快捷命令。")}
      </Text>}
    </Stack>}>
    <Button id="session-command-trigger" type="button" label={label} size="sm" variant="ghost" style={{flexShrink:0}}/>
  </Popover>
}

