import { localizeMessage } from '../shared/i18n/messages'
import { tr } from '../shared/i18n/messages'
import { useLocale, setLanguagePreference } from '../shared/i18n/locale'
import { useEffect, useRef, useState } from 'react'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { Text, Heading } from '@astryxdesign/core/Text'
import { TextInput } from '@astryxdesign/core/TextInput'
import { Button } from '@astryxdesign/core/Button'
import { useWorkbenchTheme } from '../shared/theme/currentTheme'
import { BrowserRuntime } from '../shared/bridge/browserRuntime'
import { desktopPreferences } from '../features/workspace/desktopPreferences'

import App from './App'

import AppContextMenu from '../shared/ui/AppContextMenu'
import { useAppearance } from '../shared/theme/useAppearance'
import { defaultPreferences } from '../features/workspace/preferences'
import { applyUIFont } from '../shared/fonts/fonts'

export default function BrowserEntry() {
  const language = useLocale()
  useEffect(() => {
    void applyUIFont(defaultPreferences.uiFont, language)
  }, [language])

  const { resolved } = useAppearance(defaultPreferences.mode)
  const theme = useWorkbenchTheme(defaultPreferences.interfacePalette)
  const [phase, setPhase] = useState<'loading' | 'login' | 'workspace'>(
      'loading',
    ),
    [user, setUser] = useState(''),
    [password, setPassword] = useState(''),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [retry, setRetry] = useState(0)
  const runtime = useRef<BrowserRuntime | null>(null),
    working = useRef(false)
  const generation = useRef(0)
  async function enter(session: { user: string; csrf: string }) {
    const turn = ++generation.current
    runtime.current?.dispose()
    const rt = new BrowserRuntime(session, () => {
      runtime.current = null
      setPhase('login')
      setError('')
    })
    runtime.current = rt
    try {
      await rt.connect()
      const prefs = await desktopPreferences.refresh()
      await setLanguagePreference(prefs.language)
      if (generation.current === turn) setPhase('workspace')
    } catch (e) {
      if (generation.current === turn) {
        setError(e instanceof Error ? e.message : tr('服务连接失败，请重试。'))
        setPhase('login')
      }
    }
  }
  useEffect(() => {
    let alive = true
    const abort = new AbortController()
    void fetch('/api/session', {
      cache: 'no-store',
      credentials: 'same-origin',
      signal: abort.signal,
    })
      .then(async (response) => {
        if (
          !response.headers.get('content-type')?.includes('application/json')
        ) {
          throw Error()
        }
        const session = await response.json()
        if (!alive) return
        if (session.authenticated) await enter(session)
        else setPhase('login')
      })
      .catch(() => {
        if (alive) {
          setError(tr('服务暂时无法连接，请检查网络后重试。'))
          setPhase('login')
        }
      })
    return () => {
      alive = false
      generation.current++
      abort.abort()
      runtime.current?.dispose()
      runtime.current = null
    }
  }, [retry])
  async function login() {
    if (working.current) return
    if (!user.trim() || !password) {
      setError(tr('请输入用户名和密码。'))
      return
    }
    working.current = true
    setBusy(true)
    setError('')
    const body = JSON.stringify({ user, password })
    setPassword('')
    try {
      const response = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body,
        credentials: 'same-origin',
        cache: 'no-store',
      })
      if (!response.ok) throw Error(await response.text())
      await enter(await response.json())
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('登录未完成，请重试。'))
    } finally {
      working.current = false
      setBusy(false)
    }
  }
  if (phase === 'workspace') return <App />
  return (
    <Theme theme={theme} mode={resolved}>
      <AppContextMenu>
        <Stack
          align="center"
          justify="center"
          padding={5}
          style={{
            minHeight: '100dvh',
            background: 'var(--color-background-body)',
          }}
        >
          <form
            aria-label={tr('登录 Yaare')}
            onSubmit={(e) => {
              e.preventDefault()
              void login()
            }}
            style={{ width: '100%', maxWidth: 'calc(var(--spacing-10) * 10)' }}
          >
            <Stack gap={4}>
              <Heading level={2}>Yaare</Heading>
              <Text color="secondary">{tr('登录你的浏览器工作区')}</Text>
              {phase === 'loading' ? (
                <Text role="status">{tr('正在连接服务…')}</Text>
              ) : (
                <>
                  <TextInput
                    label={tr('用户名')}
                    value={user}
                    onChange={setUser}
                    autoComplete="username"
                    hasAutoFocus
                    isDisabled={busy}
                  />
                  <TextInput
                    label={tr('密码')}
                    type="password"
                    value={password}
                    onChange={setPassword}
                    autoComplete="current-password"
                    isDisabled={busy}
                  />
                  {error && (
                    <Text role="alert" type="supporting">
                      {localizeMessage(error)}
                    </Text>
                  )}
                  <Button label={tr('登录')} type="submit" isLoading={busy} />
                  <Button
                    label={tr('重新连接服务')}
                    variant="ghost"
                    type="button"
                    isDisabled={busy}
                    onClick={() => {
                      setError('')
                      setPhase('loading')
                      setRetry((v) => v + 1)
                    }}
                  />
                </>
              )}
            </Stack>
          </form>
        </Stack>
      </AppContextMenu>
    </Theme>
  )
}
