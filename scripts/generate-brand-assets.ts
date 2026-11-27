import { fileURLToPath } from 'node:url'

const result = await Bun.spawn(['go', 'run', './tools/brandgen'], {
  cwd: fileURLToPath(new URL('..', import.meta.url)),
  stdin: 'inherit', stdout: 'inherit', stderr: 'inherit',
}).exited
process.exit(result)
