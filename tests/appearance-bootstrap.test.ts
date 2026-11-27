import { expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

const script = readFileSync(new URL('../frontend/public/appearance-bootstrap.js', import.meta.url), 'utf8')

function bootstrap(native: {mode: string; interfacePalette: string} | undefined, getItem: (key: string) => string | null, pageMode = 'development') {
  const meta = { content: 'light dark' }
  const root = { style: { colorScheme: '' }, dataset: { theme: '', astryxTheme: '' } }
  runInNewContext(script, {
    window: { __yaareStartupAppearance: native },
    localStorage: { getItem },
    document: { currentScript: { dataset: { mode: pageMode } }, querySelector: () => meta, documentElement: root },
  })
  return [meta.content, root.style.colorScheme, root.dataset.theme, root.dataset.astryxTheme]
}

test('desktop first paint uses SQLite appearance instead of stale browser storage', () => {
  expect(bootstrap({mode:'dark',interfacePalette:'paper'}, () => { throw Error('must not read localStorage') }))
    .toEqual(['dark','dark','dark','yaare-paper'])
  expect(bootstrap({mode:'system',interfacePalette:''}, () => '{"mode":"dark","interfacePalette":"green"}'))
    .toEqual(['light dark','light dark','system','yaare'])
})

test('only the standalone preview reads its local appearance, and a hosted login starts with system defaults', () => {
  expect(bootstrap(undefined, key => key === 'yaare.preview.preferences.v1' ? '{"mode":"light","interfacePalette":"blue"}' : null)).toEqual(['light','light','light','yaare-blue'])
  expect(bootstrap(undefined, () => '{"mode":"dark"}', 'browser')).toEqual(['light dark','light dark','system','yaare'])
  expect(bootstrap(undefined, () => { throw Error('storage unavailable') })).toEqual(['light dark','light dark','system','yaare'])
})
