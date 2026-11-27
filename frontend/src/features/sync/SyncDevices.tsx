import { useEffect, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { tr } from '../../shared/i18n/messages'

async function request(method: string, id?: string) {
  const session = await fetch('/api/session', { cache: 'no-store' }).then((r) =>
    r.json(),
  )
  const response = await fetch('/api/devices', {
    method,
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'X-Yaare-CSRF': session.csrf,
      'Content-Type': 'application/json',
    },
    body: id ? JSON.stringify({ id }) : undefined,
  })
  if (!response.ok) throw Error(tr('设备令牌操作失败。'))
  return response
}

export default function SyncDevices() {
  const [devices, setDevices] = useState<string[]>([])
  const [token, setToken] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let active = true
    void request('GET')
      .then((r) => r.json())
      .then((v) => {
        if (active) setDevices(v)
      })
      .catch((e) => {
        if (active) setError(e.message)
      })
    return () => {
      active = false
    }
  }, [])
  async function change(id?: string) {
    setBusy(true)
    setError('')
    setToken('')
    try {
      const r = await request(id ? 'DELETE' : 'POST', id)
      if (id) {
        setDevices((previous) => previous.filter((item) => item !== id))
        setConfirm('')
      } else {
        const created = await r.json()
        setToken(created.token)
        setDevices((previous) => [...previous, created.id])
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('设备令牌操作失败。'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Stack gap={2}>
      <Text weight="semibold">{tr('桌面设备配对')}</Text>
      <Button
        label={tr('生成设备令牌')}
        variant="secondary"
        isDisabled={busy}
        onClick={() => void change()}
      />
      {token && (
        <Stack gap={1}>
          <Text>{tr('复制到桌面版的同步设置。关闭后无法再次查看。')}</Text>
          <Text style={{ userSelect: 'all', overflowWrap: 'anywhere' }}>
            {token}
          </Text>
          <Button
            label={tr('关闭')}
            variant="ghost"
            onClick={() => setToken('')}
          />
        </Stack>
      )}
      {devices.map((id) => (
        <Stack key={id} direction="horizontal" align="center" gap={2}>
          <Text>{id.slice(0, 12)}</Text>
          <Button
            variant="ghost"
            isDisabled={busy}
            label={confirm === id ? tr('确认撤销') : tr('撤销令牌')}
            onClick={() => (confirm === id ? void change(id) : setConfirm(id))}
          />
          {confirm === id && (
            <Button
              label={tr('取消')}
              variant="ghost"
              onClick={() => setConfirm('')}
            />
          )}
        </Stack>
      ))}
      {error && <Text role="alert">{error}</Text>}
    </Stack>
  )
}
