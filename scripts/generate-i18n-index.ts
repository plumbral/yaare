import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
const source = JSON.parse(readFileSync('frontend/src/shared/i18n/sources.json', 'utf8'))
mkdirSync('locales/source', { recursive: true })
writeFileSync('locales/source/index.json', JSON.stringify(source, null, 2) + '\n')

const packed = JSON.stringify(JSON.stringify(source))
writeFileSync('frontend/src/shared/i18n/sources.generated.ts', [
  'let keys: Record<string, string> | undefined',
  'export function getSourceKeys(): Record<string, string> {',
  `  return keys ??= JSON.parse(${packed})`,
  '}',
  '',
].join('\n'))
