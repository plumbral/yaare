const sources = ['workbench', 'paper', 'purple', 'blue', 'green'].map(name => `frontend/src/shared/theme/${name}Theme.ts`)
const result = Bun.spawnSync([process.execPath, 'run', 'astryx', 'theme', 'build', ...sources, '--icons-specifier', './neutralBase.js', ...process.argv.slice(2)], { stdout: 'inherit', stderr: 'inherit' })
process.exit(result.exitCode)
