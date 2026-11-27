import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'
import manifest from '../package.json' with { type: 'json' }

const releaseTag = `v${process.env.YAARE_RELEASE_VERSION || manifest.version}`
const releaseNotes = new URL(`../docs/releases/${releaseTag}.md`, import.meta.url)

export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  base: './',
  define: {
    'import.meta.env.YAARE_RELEASE_NOTE': JSON.stringify({ tag: releaseTag, body: readFileSync(releaseNotes, 'utf8') }),
  },
  plugins: [react()],
  optimizeDeps: { exclude: ['@novnc/novnc'], include: ['@codemirror/state', '@codemirror/view', '@codemirror/commands', '@codemirror/language', '@codemirror/search', '@codemirror/legacy-modes/mode/shell', '@codemirror/legacy-modes/mode/javascript', '@codemirror/legacy-modes/mode/python', '@codemirror/legacy-modes/mode/yaml', '@codemirror/legacy-modes/mode/toml', '@codemirror/legacy-modes/mode/properties', '@codemirror/legacy-modes/mode/nginx', '@codemirror/legacy-modes/mode/dockerfile', '@codemirror/legacy-modes/mode/css', '@codemirror/legacy-modes/mode/xml', '@codemirror/legacy-modes/mode/go', '@codemirror/legacy-modes/mode/sql', '@codemirror/legacy-modes/mode/powershell', '@lezer/highlight'] },
  resolve: { alias: { '@yaare/novnc-logging': fileURLToPath(new URL('../node_modules/@novnc/novnc/core/util/logging.js', import.meta.url)), '@astryxdesign/theme-neutral/fonts.css': fileURLToPath(new URL('./src/shared/theme/fonts.css', import.meta.url)), '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { host: '127.0.0.1', port: 5173, strictPort: true },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        strictExecutionOrder: true,
        codeSplitting: {
          includeDependenciesRecursively: false,
          groups: [
            { name: 'react', tags: ['$initial'], test: /\/node_modules\/(?:react|react-dom|scheduler)\//, priority: 30 },
            { name: 'design', tags: ['$initial'], test: /\/node_modules\//, priority: 20 },
            { name: 'workbench', tags: ['$initial'], priority: 10 },
          ],
        },
      },
    },
  },
})

