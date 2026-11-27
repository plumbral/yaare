import { expect, test } from 'bun:test'
import {
  canAutoUpdateHostIcon,
  commandAvailable,
  commandIcon,
  commandInput,
  commandMatchesSystem,
  detectedSystemIcon,
  quickCommandExecutes,
  quickCommandInput,
  resolveDetectedHostIcon,
  sortCommandRows,
  validateQuickCommand,
  validCommand,
} from '../frontend/src/features/workspace/shortcuts'
import { previewChange, type SavedWorkspace } from '../frontend/src/features/workspace/savedWorkspace'

test('broadcast keeps explicit execution and preserves source formatting', () => {
  const source = "\n# 中文\n  printf 'ok'\n\tprintf 'done'\n"
  expect(validCommand(source)).toBe(true)
  expect(commandInput(source, false)).toBeNull()
  expect(commandInput(source, true)).toBe(source.replace(/\n/g, '\r') + '\r')
  expect(commandInput('pwd', false)).toBe('pwd')
  expect(commandInput('pwd\r\nls', true)).toBe('pwd\rls\r')
  expect(commandInput('\x1b[2J', true)).toBeNull()
  expect(validCommand('中'.repeat(21846))).toBe(false)
  expect(validateQuickCommand({ id: 'c', name: '检查', group: '运维', command: source })).toBeNull()
})

test('quick commands submit only with a final newline and preserve internal line breaks', () => {
  expect(quickCommandInput('pwd', false)).toEqual({ input: 'pwd' })
  expect(quickCommandInput('pwd\n', false)).toEqual({ input: 'pwd\r' })
  expect(quickCommandInput('pwd\r\n\r\n', true)).toEqual({ input: '\x1b[200~pwd\x1b[201~\r' })
  expect(quickCommandInput('pwd\n\n  ls', true)).toEqual({ input: '\x1b[200~pwd\r\r  ls\x1b[201~' })
  expect(quickCommandInput('pwd\n\n  ls\n', true)).toEqual({ input: '\x1b[200~pwd\r\r  ls\x1b[201~\r' })
  expect(quickCommandInput('pwd\nls\n', false)).toEqual({ input: 'pwd\rls\r' })
  expect(quickCommandExecutes('pwd\r\n')).toBe(true)
  expect(quickCommandExecutes('pwd\n ')).toBe(false)
})

test('quick commands cannot accidentally execute unbracketed multiline drafts', () => {
  expect(quickCommandInput('pwd\nls', false)).toHaveProperty('error')
  expect(quickCommandInput('echo\tword\n', false)).toHaveProperty('error')
  expect(quickCommandInput('echo\tword', true)).toEqual({ input: '\x1b[200~echo\tword\x1b[201~' })
  expect(quickCommandInput('ls\x1b[201~\n', true)).toHaveProperty('error')
  expect(quickCommandInput(' \n\n', true)).toHaveProperty('error')
})

test('command list sorts naturally and leaves ungrouped last in either direction', () => {
  const commands = [
    { id: 'missing', name: 'Check 1', command: 'pwd' },
    { id: 'b10', name: 'Check 10', group: 'Beta', command: 'pwd\n' },
    { id: 'alpha', name: 'Other', group: 'Alpha', command: 'pwd' },
    { id: 'b2', name: 'Check 2', group: 'Beta', command: 'pwd' },
    { id: 'ungrouped', name: 'Check 3', group: '未分组', command: 'pwd' },
  ]
  const collator = new Intl.Collator('en', { numeric: true })
  expect(sortCommandRows(commands, item => item, { key: 'name', direction: 'ascending' }, collator).map(item => item.id))
    .toEqual(['missing', 'b2', 'ungrouped', 'b10', 'alpha'])
  expect(sortCommandRows(commands, item => item, { key: 'group', direction: 'ascending' }, collator).map(item => item.id))
    .toEqual(['alpha', 'b2', 'b10', 'missing', 'ungrouped'])
  expect(sortCommandRows(commands, item => item, { key: 'group', direction: 'descending' }, collator).map(item => item.id))
    .toEqual(['b2', 'b10', 'alpha', 'missing', 'ungrouped'])
  expect(sortCommandRows(commands, item => item, null, collator)).toBe(commands)
  expect(commands[1]?.command).toBe('pwd\n')
})

test('command compatibility uses observed family and exact distro', () => {
  const generic = {id: 'pwd', name: 'pwd', command: 'pwd'}
  const debian = {family: 'linux', id: 'debian', label: 'Debian 12'}
  const ubuntu = {family: 'linux', id: 'ubuntu', label: 'Ubuntu 24.04'}
  const freebsd = {family: 'freebsd', id: 'freebsd', label: 'FreeBSD'}
  expect(commandIcon(generic)).toBe('linux')
  expect(commandMatchesSystem(generic, ubuntu)).toBe(true)
  expect(commandMatchesSystem(generic, freebsd)).toBe(false)
  expect(commandMatchesSystem(generic)).toBe(false)
  expect(commandMatchesSystem({...generic, icon: 'debian'}, debian)).toBe(true)
  expect(commandMatchesSystem({...generic, icon: 'debian'}, ubuntu)).toBe(false)
  expect(commandAvailable({...generic, icon: 'debian'}, false)).toBe(true)
  expect(commandAvailable({...generic, icon: 'debian'}, false, ubuntu)).toBe(true)
  expect(commandAvailable({...generic, icon: 'debian'}, true)).toBe(false)
  expect(commandAvailable({...generic, icon: 'debian'}, true, ubuntu)).toBe(false)
  expect(commandAvailable({...generic, icon: 'debian'}, true, debian)).toBe(true)
  expect(commandMatchesSystem({...generic, icon: 'alpinelinux'}, {family:'linux',id:'alpine',label:'Alpine'})).toBe(true)
  expect(commandMatchesSystem({...generic, icon: 'freebsd'}, freebsd)).toBe(true)
  expect(commandMatchesSystem({...generic, icon: 'cloud-aws'}, debian)).toBe(false)
  expect(validateQuickCommand({...generic, icon: 'cloud-aws'})).not.toBeNull()
})

test('command group changes preserve multiline code, host groups and older snapshots', () => {
  const current: SavedWorkspace = { revision: 0, hosts: [], groups: ['未分组', '主机分组'], commandGroups: ['未分组'], commands: [] }
  let next = previewChange(current, { kind: 'addCommandGroup', name: 'Docker' })
  const source = '\n  echo ok\n\tdate\n'
  next = previewChange(next, { kind: 'saveCommand', command: { id: 'multi', name: '脚本', group: 'Docker', command: source, icon: 'debian' } })
  next = previewChange(next, { kind: 'renameCommandGroup', name: 'Docker', newName: '容器' })
  next = previewChange(next, { kind: 'addCommandGroup', name: '空分组' })
  next = previewChange(next, { kind: 'reorderCommandGroups', commandGroups: ['未分组', '空分组', '容器'] })
  expect(next.commands[0]!.group).toBe('容器')
  next = previewChange(next, { kind: 'deleteCommandGroup', name: '容器' })
  expect(next.commands[0]).toEqual({ id: 'multi', name: '脚本', group: '未分组', command: source, icon: 'debian' })
  expect(next.groups).toEqual(current.groups)
  expect(current.commands).toEqual([])
  expect(current.commandGroups).toEqual(['未分组'])
  expect(() => previewChange(next, { kind: 'deleteCommandGroup', name: '未分组' })).toThrow()
  expect(() => previewChange(next, { kind: 'saveCommand', command: { id: 'bad', name: 'bad', group: 'missing', command: 'pwd' } })).toThrow()
})

test('detected server system maps to built-in OS icons and guards cloud/custom host icons', () => {
  expect(detectedSystemIcon(undefined)).toBeUndefined()
  expect(detectedSystemIcon({ family: 'linux', id: 'ubuntu', label: 'Ubuntu 24.04' })).toBe('ubuntu')
  expect(detectedSystemIcon({ family: 'linux', id: 'DEBIAN', label: 'Debian 12' })).toBe('debian')
  expect(detectedSystemIcon({ family: 'linux', id: 'alpine', label: 'Alpine' })).toBe('alpinelinux')
  expect(detectedSystemIcon({ family: 'linux', id: 'arch', label: 'Arch Linux' })).toBe('archlinux')
  expect(detectedSystemIcon({ family: 'linux', id: 'rhel', label: 'RHEL 9' })).toBe('redhat')
  expect(detectedSystemIcon({ family: 'linux', id: 'rocky', label: 'Rocky 9' })).toBe('rockylinux')
  expect(detectedSystemIcon({ family: 'linux', id: 'alma', label: 'AlmaLinux 9' })).toBe('almalinux')
  expect(detectedSystemIcon({ family: 'linux', id: 'centos-stream', label: 'CentOS Stream 9' })).toBe('centos')
  expect(detectedSystemIcon({ family: 'linux', id: 'opensuse-tumbleweed', label: 'openSUSE' })).toBe('opensuse')
  expect(detectedSystemIcon({ family: 'linux', id: 'opensuse-microos', label: 'openSUSE MicroOS' })).toBe('opensuse')
  expect(detectedSystemIcon({ family: 'linux', id: 'raspbian', label: 'Raspbian' })).toBe('raspberrypi')
  expect(detectedSystemIcon({ family: 'linux', id: 'kali', label: 'Kali' })).toBe('kalilinux')
  expect(detectedSystemIcon({ family: 'linux', id: 'mint', label: 'Linux Mint' })).toBe('linuxmint')
  expect(detectedSystemIcon({ family: 'linux', id: 'void', label: 'Void Linux' })).toBe('linux')
  expect(detectedSystemIcon({ family: 'freebsd', id: 'freebsd', label: 'FreeBSD 14' })).toBe('freebsd')
  expect(detectedSystemIcon({ family: 'openbsd', id: 'openbsd', label: 'OpenBSD' })).toBe('openbsd')
  expect(detectedSystemIcon({ family: 'darwin', id: 'macos', label: 'macOS' })).toBeUndefined()
  expect(detectedSystemIcon({ family: 'netbsd', id: 'netbsd', label: 'NetBSD' })).toBeUndefined()

  const cmd = { id: 'c', name: 'update', command: 'true' }
  expect(commandMatchesSystem({ ...cmd, icon: 'almalinux' }, { family: 'linux', id: 'alma', label: 'Alma' })).toBe(true)
  expect(commandMatchesSystem({ ...cmd, icon: 'kalilinux' }, { family: 'linux', id: 'KALI', label: 'Kali' })).toBe(true)
  expect(commandMatchesSystem({ ...cmd, icon: 'linuxmint' }, { family: 'linux', id: 'mint', label: 'Mint' })).toBe(true)
  expect(commandMatchesSystem({ ...cmd, icon: 'opensuse' }, { family: 'linux', id: 'opensuse-microos', label: 'MicroOS' })).toBe(true)

  const ubuntu = { family: 'linux', id: 'ubuntu', label: 'Ubuntu 24.04' }
  const unknownLinux = { family: 'linux', id: 'void', label: 'Void Linux' }
  const macos = { family: 'darwin', id: 'macos', label: 'macOS' }
  expect(canAutoUpdateHostIcon(undefined)).toBe(true)
  expect(canAutoUpdateHostIcon('')).toBe(true)
  expect(canAutoUpdateHostIcon('linux')).toBe(true)
  expect(canAutoUpdateHostIcon('debian')).toBe(true)
  expect(canAutoUpdateHostIcon('cloud-aws')).toBe(false)
  expect(canAutoUpdateHostIcon('custom-png:abc')).toBe(false)

  expect(resolveDetectedHostIcon('', ubuntu)).toBe('ubuntu')
  expect(resolveDetectedHostIcon('linux', ubuntu)).toBe('ubuntu')
  expect(resolveDetectedHostIcon('debian', ubuntu)).toBe('ubuntu')
  expect(resolveDetectedHostIcon('ubuntu', ubuntu)).toBeUndefined()
  expect(resolveDetectedHostIcon('', unknownLinux)).toBeUndefined()
  expect(resolveDetectedHostIcon('debian', unknownLinux)).toBe('linux')
  expect(resolveDetectedHostIcon('cloud-aws', ubuntu)).toBeUndefined()
  expect(resolveDetectedHostIcon('custom-png:abc', ubuntu)).toBeUndefined()
  expect(resolveDetectedHostIcon('linux', macos)).toBeUndefined()
})

test('previewChange setHostSystemIcon updates OS icons and protects cloud/custom icons', () => {
  const baseHost = {
    id: 'h1', name: 'Node 1', address: '10.0.0.1', port: 22, username: 'root',
    group: '未分组', system: 'Linux', icon: 'linux', authentication: 'none' as const,
  }
  const current: SavedWorkspace = {
    revision: 3,
    hosts: [
      baseHost,
      { ...baseHost, id: 'h2', name: 'AWS', icon: 'cloud-aws' },
      { ...baseHost, id: 'h3', name: 'Custom', icon: 'custom-png:data' },
      { ...baseHost, id: 'h4', name: 'Empty', icon: '' },
    ],
    groups: ['未分组'],
    commandGroups: ['未分组'],
    commands: [],
  }

  const updated = previewChange(current, { kind: 'setHostSystemIcon', id: 'h1', name: 'ubuntu' })
  expect(updated.revision).toBe(4)
  expect(updated.hosts[0]!.icon).toBe('ubuntu')
  expect(current.hosts[0]!.icon).toBe('linux')

  const switched = previewChange(updated, { kind: 'setHostSystemIcon', id: 'h1', name: 'debian' })
  expect(switched.hosts[0]!.icon).toBe('debian')

  expect(previewChange(switched, { kind: 'setHostSystemIcon', id: 'h1', name: 'debian' })).toBe(switched)
  expect(previewChange(current, { kind: 'setHostSystemIcon', id: 'h4', name: 'linux' })).toBe(current)

  expect(previewChange(current, { kind: 'setHostSystemIcon', id: 'h2', name: 'ubuntu' })).toBe(current)
  expect(previewChange(current, { kind: 'setHostSystemIcon', id: 'h3', name: 'ubuntu' })).toBe(current)

  expect(() => previewChange(current, { kind: 'setHostSystemIcon', id: 'h1', name: 'cloud-aws' })).toThrow()
  expect(() => previewChange(current, { kind: 'setHostSystemIcon', id: 'h1', name: '' })).toThrow()
  expect(() => previewChange(current, { kind: 'setHostSystemIcon', id: 'missing', name: 'ubuntu' })).toThrow()
})
