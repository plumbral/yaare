import { afterEach, expect, spyOn, test } from 'bun:test'
import { plugin } from 'bun'
import { act } from 'react'
import { document, installWorkbenchDom, storage } from './fixtures/workbench-dom'

installWorkbenchDom()

const release = { tag: 'v1.2.3', body: 'Bundled release notes for this image.' }
plugin({
  name: 'embedded-release-note',
  setup(build) {
    build.onLoad({ filter: /[\\/](AboutSettingsPanel|ReleaseNotesDialog)\.tsx$/ }, async ({ path }) => ({
      contents: (await Bun.file(path).text()).replaceAll('import.meta.env.YAARE_RELEASE_NOTE', JSON.stringify(release)),
      loader: 'tsx',
    }))
  },
})

const previousMode = process.env.MODE
afterEach(() => {
  if (previousMode === undefined) delete process.env.MODE
  else process.env.MODE = previousMode
})

async function mountAbout(mode: string) {
  process.env.MODE = mode
  const { createRoot } = await import('react-dom/client')
  const { default: About } = await import('../frontend/src/features/workspace/AboutSettingsPanel')
  const { useApplicationUpdateRunner } = await import('../frontend/src/features/workspace/useApplicationUpdates')
  function Settings() {
    useApplicationUpdateRunner()
    return <About/>
  }
  const root = createRoot(document.querySelector('main')!)
  await act(async () => root.render(<Settings/>))
  return () => act(async () => root.unmount())
}

const info = { name: 'Yaare', version: '1.2.3', platform: 'linux', architecture: 'amd64' }
const status = { state: 'idle', currentVersion: info.version, latestVersion: '', notes: '', checkedAt: 0 }

test('desktop manual check works with automatic checks turned off', async () => {
  storage.set('yaare.application-updates.v1', JSON.stringify({ automatic: false }))
  const rpc = spyOn(globalThis.mygo!, 'callWith').mockImplementation(async (method, args) => {
    if (method === 'ApplicationService.GetInfo') return info
    if (method === 'ApplicationService.GetUpdateStatus') return status
    if (method === 'ApplicationService.CheckForUpdates' && args[0] === false) {
      return { ...status, state: 'available', latestVersion: '1.3.0', notes: 'New release' }
    }
    throw Error('Unexpected RPC: ' + method)
  })
  const close = await mountAbout('production')
  try {
    expect(rpc.mock.calls.some(([method]) => method === 'ApplicationService.CheckForUpdates')).toBe(false)
    await act(async () => document.querySelector<HTMLElement>('#about-check-updates')!.click())
    expect(document.querySelector('#about-update-status')!.textContent).toContain('1.3.0')
    expect(document.querySelector('#about-download')!.getAttribute('href')).toBe('https://github.com/plumbral/yaare/releases')
    expect(JSON.parse(storage.get('yaare.application-updates.v1')!).automatic).toBe(false)
  } finally { await close(); rpc.mockRestore() }
})

test('Docker shows the deployment guide and bundled notes without desktop update calls', async () => {
  const rpc = spyOn(globalThis.mygo!, 'callWith').mockResolvedValue(info)
  const close = await mountAbout('browser')
  try {
    expect(document.querySelector('#about-check-updates')).toBeNull()
    expect(document.querySelector('#about-automatic-updates')).toBeNull()
    expect(document.querySelector('#about-image-version')?.textContent).toContain(info.version)
    expect(document.querySelector('#about-docker-upgrade')?.getAttribute('href')).toContain('/deployment/docker/README.md#')
    expect(document.querySelector('#about-releases')?.getAttribute('href')).toBe('https://github.com/plumbral/yaare/releases')
    await act(async () => {
      document.querySelector<HTMLElement>('#about-release-notes')!.click()
      await import('../frontend/src/features/workspace/ReleaseNotesDialog')
    })
    const deadline = performance.now() + 2000
    while (!document.querySelector('dialog') && performance.now() < deadline) {
      await act(async () => { await Bun.sleep(20) })
    }
    expect(document.querySelector('dialog')?.textContent).toContain(release.body)
    expect(rpc.mock.calls.map(([method]) => method).filter(method => method !== 'ApplicationService.GetInfo')).toEqual([])
  } finally {
    await close()
    rpc.mockRestore()
  }
})
