import { defineConfig } from 'mygo-cli'
import manifest from './package.json' with { type: 'json' }

export default defineConfig({
  name: 'Yaare',
  slug: 'yaare',
  identifier: 'app.yaare',
  macos: { minimumSystemVersion: '13.3' },
  version: process.env.YAARE_RELEASE_VERSION || manifest.version,
  icon: 'resources/icon.png',
  main: './cmd/yaare',
  devUrl: 'http://127.0.0.1:5173',
  devCommand: 'bun run dev:web',
  buildCommand: 'bun run build:web',
  bindings: 'frontend/src/shared/generated/mygo.ts',
  out: 'build',
  resourcesExclude: ['README.md', '**/.gitkeep'],
  i18n: { dir: 'locales', source: 'en' },
})
